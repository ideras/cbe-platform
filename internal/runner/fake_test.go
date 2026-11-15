package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// run is what a grading container does between its start and its end; the default exits 0 with nothing printed. active counts the
	// grading containers that have started and not ended, and peak the most there were at once.
	run          func(ctx context.Context, c *fakeContainer) (exit int, out Output)
	active, peak int
	// infos is what the runtime says of an image it has (an image with no entry here is a plain one); build is what an image build does, in
	// place of the default, which adds one layer to the base and tags it; agentSums is the SHA-256 the checking container prints for the
	// agent of an image, by reference (the base's label when there is none).
	infos         map[string]ImageInfo
	build         func(ctx context.Context, spec ImageBuildSpec, buildContext []byte, log io.Writer) error
	agentSums     map[string]string
	builds        int
	buildSpecs    []ImageBuildSpec
	buildContexts [][]byte
	buildActive   int
	buildPeak     int
}

// theAgent is the hash the base image's label gives its agent; baseImage is the base's description: the course image of the plan.
var theAgent = strings.Repeat("a", 64)

func baseImage() ImageInfo {
	return ImageInfo{ID: "sha256:image", RepoDigests: []string{"localhost/cbe/cpp20@sha256:" + strings.Repeat("b", 64)}, Labels: map[string]string{LabelAgent: theAgent, "io.cbe.course": "cpp20"},
		User: "1000:1000", Entrypoint: []string{AgentPath}, Cmd: []string{}, WorkDir: "/work", Layers: []string{"sha256:layer1", "sha256:layer2"}}
}

type fakeContainer struct {
	spec   ContainerSpec
	state  string
	copied []byte
	files  map[string]string // what CopyTo extracted: path -> content
	exit   int
	oom    bool
	out    Output
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{containers: map[string]*fakeContainer{}, images: map[string]bool{"localhost/cbe/cpp20:latest": true},
		infos: map[string]ImageInfo{"localhost/cbe/cpp20:latest": baseImage()}, agentSums: map[string]string{}}
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
	if err := f.transition(name, "start", "", StateRunning); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.containers[name]; c != nil && c.spec.Labels[LabelRole] == RoleGrading {
		if f.active++; f.active > f.peak {
			f.peak = f.active
		}
	}
	return nil
}

// Wait runs the grading container's program (f.run) and ends the container; a container that is not a grading one ends at once.
func (f *fakeRuntime) Wait(ctx context.Context, name string) (int, error) {
	f.mu.Lock()
	c, err := f.get(name)
	run := f.run
	f.mu.Unlock()
	if err != nil {
		return 0, err
	}
	exit, out := 0, Output{}
	switch {
	case len(c.spec.Entrypoint) > 0 && c.spec.Entrypoint[0] == "/usr/bin/sha256sum":
		// The checking container of an image build: it prints the hash of the agent in the image it was made from.
		f.mu.Lock()
		sum, found := f.agentSums[c.spec.Image]
		f.mu.Unlock()
		if !found {
			sum = theAgent
		}
		out = Output{Stdout: []byte(sum + "  " + strings.Join(c.spec.Cmd, " ") + "\n")}
	case run != nil:
		exit, out = run(ctx, c)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.exit, c.out, c.state = exit, out, StateExited
	if c.spec.Labels[LabelRole] == RoleGrading {
		f.active--
	}
	return exit, nil
}

// fakeCollector hands back what the container printed, cut at the limit, once the container has ended.
type fakeCollector struct {
	f     *fakeRuntime
	name  string
	limit int64
}

func (c *fakeCollector) Collect(context.Context) (Output, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.f.note("collect " + c.name)
	container, err := c.f.get(c.name)
	if err != nil {
		return Output{}, err
	}
	out := container.out
	if int64(len(out.Stdout)) > c.limit {
		out.Stdout, out.StdoutTruncated = out.Stdout[:c.limit], true
	}
	if int64(len(out.Stderr)) > c.limit {
		out.Stderr, out.StderrTruncated = out.Stderr[:c.limit], true
	}
	return out, nil
}
func (c *fakeCollector) Close() error { return nil }

func (f *fakeRuntime) Attach(_ context.Context, name string, limit int64) (Collector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("attach " + name)
	if _, err := f.get(name); err != nil {
		return nil, err
	}
	return &fakeCollector{f: f, name: name, limit: limit}, nil
}
func (f *fakeRuntime) Pause(_ context.Context, name string) error {
	return f.transition(name, "pause", StateRunning, StatePaused)
}
func (f *fakeRuntime) Unpause(_ context.Context, name string) error {
	return f.transition(name, "unpause", StatePaused, StateRunning)
}
func (f *fakeRuntime) Stop(_ context.Context, name string, _ time.Duration) error {
	f.mu.Lock()
	if c := f.containers[name]; c != nil && c.spec.Labels[LabelRole] == RoleGrading && c.state == StateRunning {
		f.active--
		c.exit = 137
	}
	f.mu.Unlock()
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
	return Inspected{ID: "id-" + name, Name: name, Labels: c.spec.Labels, State: c.state, ExitCode: c.exit, OOMKilled: c.oom}, nil
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.images[ref] {
		return ImageInfo{}, ErrNotFound
	}
	if info, found := f.infos[ref]; found {
		return info, nil
	}
	return ImageInfo{ID: "sha256:image"}, nil
}

// addImage makes the runtime have an image under a name.
func (f *fakeRuntime) addImage(ref string, info ImageInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.images[ref] = true
	f.infos[ref] = info
}

// ImageBuild is f.build, or by default a build that prints two steps and adds a layer to the base.
func (f *fakeRuntime) ImageBuild(ctx context.Context, spec ImageBuildSpec, buildContext io.Reader, log io.Writer) error {
	data, _ := io.ReadAll(buildContext)
	f.mu.Lock()
	f.builds++
	f.buildSpecs = append(f.buildSpecs, spec)
	f.buildContexts = append(f.buildContexts, data)
	if f.buildActive++; f.buildActive > f.buildPeak {
		f.buildPeak = f.buildActive
	}
	hook := f.build
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.buildActive--
		f.mu.Unlock()
	}()
	if hook != nil {
		return hook(ctx, spec, data, log)
	}
	fmt.Fprintf(log, "STEP 1/2: FROM localhost/cbe/cpp20\nSTEP 2/2: RUN true\nSuccessfully tagged %s\n", spec.Tag)
	f.addImage(spec.Tag, f.builtOn(baseImage(), spec))
	return nil
}

// builtOn is the image a build makes on a base: one more layer, and the labels it was asked for.
func (f *fakeRuntime) builtOn(base ImageInfo, spec ImageBuildSpec) ImageInfo {
	built := base
	built.ID = fmt.Sprintf("sha256:built%d", f.builds)
	built.RepoDigests = nil
	built.Layers = append(append([]string{}, base.Layers...), fmt.Sprintf("sha256:added%d", f.builds))
	built.Labels = map[string]string{}
	for k, v := range base.Labels {
		built.Labels[k] = v
	}
	for k, v := range spec.Labels {
		built.Labels[k] = v
	}
	return built
}

func (f *fakeRuntime) ImageTag(_ context.Context, ref, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("tag " + ref + " " + tag)
	if !f.images[ref] {
		return ErrNotFound
	}
	f.images[tag], f.infos[tag] = true, f.infos[ref]
	return nil
}

func (f *fakeRuntime) ImageRemove(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("rmi " + ref)
	if !f.images[ref] {
		return ErrNotFound
	}
	delete(f.images, ref)
	delete(f.infos, ref)
	return nil
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
	cfg.WorkRoot, cfg.RunRoot, cfg.StateRoot, cfg.GradeRoot = filepath.Join(base, "work"), filepath.Join(base, "run"), filepath.Join(base, "state"), filepath.Join(base, "grade")
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
