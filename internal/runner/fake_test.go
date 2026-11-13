package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/quota"
)

// fakeRuntime is a container runtime in memory that remembers what it was told.
type fakeRuntime struct {
	mu         sync.Mutex
	containers map[string]*fakeContainer
	images     map[string]bool
	specs      []ContainerSpec
	calls      []string
	failCreate error
}

type fakeContainer struct {
	spec   ContainerSpec
	state  string
	copied []byte
	files  map[string]string // what CopyTo extracted: path -> content
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{containers: map[string]*fakeContainer{}, images: map[string]bool{"localhost/cbe/cpp20:latest": true}}
}

func (f *fakeRuntime) note(call string) { f.calls = append(f.calls, call) }

func (f *fakeRuntime) Ping(context.Context) error { return nil }

func (f *fakeRuntime) Create(_ context.Context, spec ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("create " + spec.Name)
	if f.failCreate != nil {
		return "", f.failCreate
	}
	if _, found := f.containers[spec.Name]; found {
		return "", errors.New("name in use")
	}
	f.containers[spec.Name] = &fakeContainer{spec: spec, state: StateCreated}
	f.specs = append(f.specs, spec)
	return "id-" + spec.Name, nil
}

func (f *fakeRuntime) get(name string) (*fakeContainer, error) {
	c := f.containers[name]
	if c == nil {
		return nil, ErrNotFound
	}
	return c, nil
}

func (f *fakeRuntime) transition(name, call, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note(call + " " + name)
	c, err := f.get(name)
	if err != nil {
		return err
	}
	if from != "" && c.state != from {
		return errors.New("wrong state " + c.state)
	}
	c.state = to
	return nil
}

func (f *fakeRuntime) Start(_ context.Context, name string) error {
	return f.transition(name, "start", "", StateRunning)
}
func (f *fakeRuntime) Pause(_ context.Context, name string) error {
	return f.transition(name, "pause", StateRunning, StatePaused)
}
func (f *fakeRuntime) Unpause(_ context.Context, name string) error {
	return f.transition(name, "unpause", StatePaused, StateRunning)
}
func (f *fakeRuntime) Stop(_ context.Context, name string, _ time.Duration) error {
	return f.transition(name, "stop", "", StateExited)
}
func (f *fakeRuntime) Remove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("remove " + name)
	if _, err := f.get(name); err != nil {
		return err
	}
	delete(f.containers, name)
	return nil
}

func (f *fakeRuntime) Inspect(_ context.Context, name string) (Inspected, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(name)
	if err != nil {
		return Inspected{}, err
	}
	return Inspected{ID: "id-" + name, Name: name, Labels: c.spec.Labels, State: c.state}, nil
}

func (f *fakeRuntime) List(_ context.Context, key, value string) ([]Inspected, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Inspected
	for name, c := range f.containers {
		if c.spec.Labels[key] == value {
			out = append(out, Inspected{ID: "id-" + name, Name: name, Labels: c.spec.Labels, State: c.state})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeRuntime) Stats(_ context.Context, name string) (RawStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.get(name); err != nil {
		return RawStats{}, err
	}
	return RawStats{CPUPercent: 12.5, MemoryBytes: 1 << 20, MemoryLimit: 64 << 20, Pids: 3}, nil
}

func (f *fakeRuntime) CopyTo(_ context.Context, name, folder string, archive io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(name)
	if err != nil {
		return err
	}
	data, _ := io.ReadAll(archive)
	c.copied = data
	c.files = map[string]string{}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err != nil {
			return nil
		}
		content, _ := io.ReadAll(reader)
		c.files[header.Name] = string(content)
	}
}

func (f *fakeRuntime) CopyFrom(_ context.Context, name, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("copyfrom " + name + " " + path)
	if _, err := f.get(name); err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("TAR:" + path)), nil
}

func (f *fakeRuntime) ImageInspect(_ context.Context, ref string) (ImageInfo, error) {
	if !f.images[ref] {
		return ImageInfo{}, ErrNotFound
	}
	return ImageInfo{ID: "sha256:image"}, nil
}

// fakeQuota remembers the folders it was asked for.
type fakeQuota struct {
	mu      sync.Mutex
	folders map[string]int
	fail    error
}

func (q *fakeQuota) Create(session string, sizeMB, inodes int) (quota.Folder, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.fail != nil {
		return quota.Folder{}, q.fail
	}
	if q.folders == nil {
		q.folders = map[string]int{}
	}
	q.folders[session] = sizeMB
	return quota.Folder{Session: session, SizeMB: sizeMB, Inodes: inodes}, nil
}

func (q *fakeQuota) Remove(session string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.folders, session)
	return nil
}

type rig struct {
	svc    *Service
	rt     *fakeRuntime
	quota  *fakeQuota
	cfg    Config
	secret string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	base, err := os.MkdirTemp("", "runner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	cfg := DefaultConfig()
	cfg.WorkRoot, cfg.RunRoot, cfg.StateRoot = filepath.Join(base, "work"), filepath.Join(base, "run"), filepath.Join(base, "state")
	rt, q := newFakeRuntime(), &fakeQuota{}
	svc, err := NewService(cfg, rt, q, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Close)
	return &rig{svc: svc, rt: rt, quota: q, cfg: cfg, secret: "0123456789abcdef0123456789abcdef0123456789abcdef"}
}

func (r *rig) request(session string) CreateRequest {
	return CreateRequest{Session: session, Exam: "exam1", Image: "cbe/cpp20", Resources: Resources{CPU: 1, MemoryMB: 512, DiskMB: 50}}
}

// api serves the handler on a real Unix socket and returns a client of it.
func (r *rig) api(t *testing.T) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "api")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "r.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = Serve(ctx, listener, Handler(r.svc, r.secret, slog.New(slog.NewTextHandler(io.Discard, nil))))
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return NewClient(socket, r.secret)
}
