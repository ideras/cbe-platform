//go:build linux

// Package workspacetest has a fake of cbe-runner for the tests of the exam server's side of workspace exams: containers that are real
// agents (internal/agent) in this process, each on a folder of its own, so the relay, the snapshots and the supervisor are tested with
// the real agent protocol and the real file operations, without a container runtime.
package workspacetest

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"exam-platform/internal/agent"
	"exam-platform/internal/runner"
	"exam-platform/shared/containerfile"
)

// GradeCall is one grading run as the fake was asked for it: the request, the files of the submission and the files of the bundle.
type GradeCall struct {
	Request runner.GradeRequest
	Files   map[string]string
	Bundle  map[string]string
}

// BuildCall is one image build as the fake was asked for it: the request and the files of the build context.
type BuildCall struct {
	Request runner.BuildRequest
	Files   map[string]string
	// Log sends text to whoever is watching the build, as the runner's stream does while a build goes; a hook calls it to print.
	Log func(text string)
}

// BaseDigest is the digest the fake says its course image has.
const BaseDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

// Runner is the fake. Its methods are those of workspace.RunnerAPI.
type Runner struct {
	dir string

	mu       sync.Mutex
	sessions map[string]*session
	// Images are the names this runner approves and has.
	Images map[string]bool
	// Calls lists the operations asked of it, as "operation session".
	Calls []string
	// ArchiveHook, when set, replaces what Archive returns (to test what a hostile /work looks like).
	ArchiveHook func(session string) io.ReadCloser
	connectHook func(ctx context.Context, session string) (net.Conn, error)
	// SilentAgents makes the agents of these sessions look as if their heartbeat had stopped.
	SilentAgents map[string]bool
	// FailArchive makes Archive fail.
	FailArchive error
	// GradeHook, when set, decides what a grading run returns; it is given the request and the files of the submission and of the bundle.
	// Without it a run ends with exit code 0 and "{}" on standard output.
	GradeHook func(ctx context.Context, call GradeCall) (runner.GradeResult, error)
	// Gradings lists the runs that were asked of it, and GradePeak is the most that were going at once.
	Gradings  []GradeCall
	GradePeak int
	gradeNow  int
	// BuildHook, when set, decides what an image build returns; it is given the request and the files of the context. Without it a build
	// applies the real checker to the Containerfile and, when it passes, makes the image: named by the exam and a hash of the context, with a
	// cached answer for files that were built already (unless the request is a rebuild).
	BuildHook func(ctx context.Context, call BuildCall) (runner.BuildResult, error)
	// Builds lists the builds that were asked of it, and BuildPeak is the most that were going at once.
	Builds    []BuildCall
	BuildPeak int
	buildNow  int
	// Built are the images the runner has made, by name (cbe-exam/<exam>:<hash>), with the exam each was built for; Forget removes one.
	Built map[string]string
	// CreatedBuilds is the build each container was made from, by session ("" for the course image).
	CreatedBuilds map[string]string
	// Now is the clock the agents' heartbeats are stamped with; tests that move the exam server's clock move it too.
	Now func() time.Time
}

type session struct {
	exam   string
	image  string
	state  string
	seeded bool
	work   string
	run    string
	agent  *agent.Agent
	cancel context.CancelFunc
	done   chan struct{}
	res    runner.Resources
	starts int
}

// New makes a fake whose containers live under a temporary folder.
func New(tmp string) *Runner {
	dir, err := os.MkdirTemp("", "wsr")
	if err != nil {
		panic(err)
	}
	return &Runner{dir: dir, sessions: map[string]*session{}, Images: map[string]bool{"cbe/cpp20": true}, SilentAgents: map[string]bool{}, Now: time.Now,
		Built: map[string]string{}, CreatedBuilds: map[string]string{}}
}

// Close stops every agent and removes the folders.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		r.stopAgent(s)
	}
	os.RemoveAll(r.dir)
}

func (r *Runner) note(op, session string) { r.Calls = append(r.Calls, op+" "+session) }

func (r *Runner) get(id string) (*session, error) {
	s := r.sessions[id]
	if s == nil {
		return nil, runner.ErrNotFound
	}
	return s, nil
}

// Work is the folder of a session's /work.
func (r *Runner) Work(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil {
		return s.work
	}
	return ""
}

// Starts is how many times the container of a session was started.
func (r *Runner) Starts(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil {
		return s.starts
	}
	return 0
}

// State is the container's state: created, running, paused, exited, or missing.
func (r *Runner) State(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil {
		return s.state
	}
	return runner.StateMissing
}

// Resources are the limits the container was made with.
func (r *Runner) Resources(id string) runner.Resources {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil {
		return s.res
	}
	return runner.Resources{}
}

// Kill ends the agent, and with it the container, as a student ending PID 1 would.
func (r *Runner) Kill(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil && s.state == runner.StateRunning {
		r.stopAgent(s)
		s.state = runner.StateExited
	}
}

func (r *Runner) stopAgent(s *session) {
	if s.agent != nil {
		s.agent.Shutdown()
		s.cancel()
		<-s.done
		s.agent = nil
	}
}

func (r *Runner) Create(_ context.Context, req runner.CreateRequest) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("create", req.Session)
	if !r.Images[req.Image] {
		return runner.Status{}, fmt.Errorf("%w: %q", runner.ErrNotApproved, req.Image)
	}
	if req.Build != "" && r.Built[runner.BuiltName(req.Exam, req.Build)] != req.Exam {
		return runner.Status{}, fmt.Errorf("%w: the runtime does not have the image built for exam %s", runner.ErrNotFound, req.Exam)
	}
	if s := r.sessions[req.Session]; s != nil {
		return r.status(req.Session, s), nil
	}
	r.CreatedBuilds[req.Session] = req.Build
	base := filepath.Join(r.dir, req.Session)
	s := &session{exam: req.Exam, image: req.Image, state: runner.StateCreated, work: filepath.Join(base, "work"), run: filepath.Join(base, "run"), res: req.Resources}
	for _, dir := range []string{s.work, s.run} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return runner.Status{}, err
		}
	}
	r.sessions[req.Session] = s
	return r.status(req.Session, s), nil
}

func (r *Runner) Seed(_ context.Context, id string, bundle io.Reader) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("seed", id)
	s, err := r.get(id)
	if err != nil {
		return runner.Status{}, err
	}
	if s.seeded || s.state != runner.StateCreated {
		return runner.Status{}, fmt.Errorf("%w: seeded already", runner.ErrConflict)
	}
	var clean bytes.Buffer
	if _, err := runner.SanitizeSeed(bundle, &clean); err != nil {
		return runner.Status{}, err
	}
	reader := tar.NewReader(&clean)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return runner.Status{}, err
		}
		path := filepath.Join(s.work, filepath.FromSlash(strings.TrimSuffix(header.Name, "/")))
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(path, 0o755)
		case tar.TypeSymlink:
			err = os.Symlink(header.Linkname, path)
		default:
			if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				var data []byte
				if data, err = io.ReadAll(reader); err == nil {
					err = os.WriteFile(path, data, fs.FileMode(header.Mode&0o777))
				}
			}
		}
		if err != nil {
			return runner.Status{}, err
		}
	}
	s.seeded = true
	return r.status(id, s), nil
}

func (r *Runner) Start(_ context.Context, id string) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("start", id)
	s, err := r.get(id)
	if err != nil {
		return runner.Status{}, err
	}
	if !s.seeded {
		return runner.Status{}, fmt.Errorf("%w: not seeded", runner.ErrConflict)
	}
	switch s.state {
	case runner.StateRunning:
	case runner.StatePaused:
		s.state = runner.StateRunning
	default:
		a, err := agent.New(agent.Config{Socket: filepath.Join(s.run, "agent.sock"), Work: s.work, Home: filepath.Join(s.run, "home"), Shell: "/bin/sh",
			Heartbeat: 200 * time.Millisecond, KillGrace: 300 * time.Millisecond, MemoryEvents: filepath.Join(s.run, "none")})
		if err != nil {
			return runner.Status{}, err
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.agent, s.cancel, s.done = a, cancel, make(chan struct{})
		go func() { _ = a.Serve(ctx); close(s.done) }()
		for range 500 {
			if a.Ready() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		s.state = runner.StateRunning
		s.starts++
	}
	return r.status(id, s), nil
}

func (r *Runner) Pause(_ context.Context, id string) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("pause", id)
	s, err := r.get(id)
	if err != nil {
		return runner.Status{}, err
	}
	if s.state == runner.StateRunning {
		s.state = runner.StatePaused
	}
	return r.status(id, s), nil
}

func (r *Runner) Unpause(_ context.Context, id string) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("unpause", id)
	s, err := r.get(id)
	if err != nil {
		return runner.Status{}, err
	}
	if s.state == runner.StatePaused {
		s.state = runner.StateRunning
	}
	return r.status(id, s), nil
}

func (r *Runner) Stop(_ context.Context, id string) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("stop", id)
	s, err := r.get(id)
	if err != nil {
		return runner.Status{}, err
	}
	r.stopAgent(s)
	if s.state != runner.StateCreated {
		s.state = runner.StateExited
	}
	return r.status(id, s), nil
}

func (r *Runner) Remove(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("remove", id)
	if s := r.sessions[id]; s != nil {
		r.stopAgent(s)
		os.RemoveAll(filepath.Dir(s.work))
		delete(r.sessions, id)
	}
	return nil
}

func (r *Runner) status(id string, s *session) runner.Status {
	st := runner.Status{Session: id, Exam: s.exam, State: s.state, Seeded: s.seeded, Resources: s.res}
	if s.state == runner.StateRunning && s.agent != nil {
		st.Agent = runner.AgentStatus{Connected: true, LastSeen: r.Now()}
		if r.SilentAgents[id] {
			st.Agent = runner.AgentStatus{Connected: true, LastSeen: r.Now().Add(-time.Hour)}
		}
	}
	if s.state == runner.StateExited {
		code := 0
		st.ExitCode = &code
	}
	return st
}

func (r *Runner) Status(_ context.Context, id string) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sessions[id]
	if s == nil {
		return runner.Status{Session: id, State: runner.StateMissing}, nil
	}
	return r.status(id, s), nil
}

func (r *Runner) Stats(_ context.Context, id string) (runner.Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.get(id); err != nil {
		return runner.Stats{}, err
	}
	return runner.Stats{Session: id, CPUPercent: 1, CPUKnown: true, MemoryBytes: 1 << 20, MemoryLimit: 64 << 20, Pids: 2}, nil
}

func (r *Runner) ExamStats(_ context.Context, exam string) ([]runner.Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []runner.Stats
	for id, s := range r.sessions {
		if s.exam == exam && s.state == runner.StateRunning {
			out = append(out, runner.Stats{Session: id, CPUPercent: 1, CPUKnown: true, MemoryBytes: 1 << 20, MemoryLimit: 64 << 20, Pids: 2})
		}
	}
	return out, nil
}

// Archive tars the session's /work as the runtime's archive endpoint does: names relative to /work, links as links.
func (r *Runner) Archive(_ context.Context, id, path string) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note("archive", id)
	if r.FailArchive != nil {
		return nil, r.FailArchive
	}
	s, err := r.get(id)
	if err != nil {
		return nil, err
	}
	if r.ArchiveHook != nil {
		return r.ArchiveHook(id), nil
	}
	if path != "/work/." {
		return nil, runner.ErrNotFound
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	err = filepath.WalkDir(s.work, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == s.work {
			return err
		}
		name, _ := filepath.Rel(s.work, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		header := &tar.Header{Name: name, Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()}
		switch {
		case d.IsDir():
			header.Typeflag, header.Name = tar.TypeDir, name+"/"
		case info.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			header.Typeflag, header.Linkname = tar.TypeSymlink, target
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			header.Typeflag, header.Size = tar.TypeReg, int64(len(data))
			if err := writer.WriteHeader(header); err != nil {
				return err
			}
			_, err = writer.Write(data)
			return err
		default:
			return nil
		}
		return writer.WriteHeader(header)
	})
	if err != nil {
		return nil, err
	}
	_ = writer.Close()
	return io.NopCloser(&buffer), nil
}

// SetConnectHook makes ConnectAgent return what the hook returns instead of a connection to the session's agent, until it is set to nil: the
// way a test plays an agent that is hostile (the agent runs as the student, so whatever it says is hostile input to the server). The hook is
// asked by everything that connects to an agent (the relay, the recorder, the wait for the agent to come up), so a test sets it once the
// container is up.
func (r *Runner) SetConnectHook(hook func(ctx context.Context, session string) (net.Conn, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectHook = hook
}

func (r *Runner) ConnectAgent(ctx context.Context, id string) (net.Conn, error) {
	r.mu.Lock()
	s, err := r.get(id)
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	state, socket, hook := s.state, filepath.Join(s.run, "agent.sock"), r.connectHook
	r.mu.Unlock()
	switch state {
	case runner.StateRunning:
	case runner.StatePaused:
		return nil, runner.ErrFrozen
	default:
		return nil, fmt.Errorf("%w: the container is %s", runner.ErrConflict, state)
	}
	if hook != nil {
		return hook(ctx, id)
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
}

func (r *Runner) ImageInspect(_ context.Context, name string) (runner.ImageStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rest, built := strings.CutPrefix(name, "cbe-exam/"); built {
		exam, _, _ := strings.Cut(rest, ":")
		present := r.Built[name] != ""
		return runner.ImageStatus{Name: name, Approved: present && r.Built[name] == exam, Present: present, ID: "sha256:fakebuilt"}, nil
	}
	return runner.ImageStatus{Name: name, Approved: r.Images[name], Present: r.Images[name], ID: "sha256:fake"}, nil
}

// Have makes the runner have the image of a build, for a BuildHook that makes an image of its own.
func (r *Runner) Have(exam, hash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Built[runner.BuiltName(exam, hash)] = exam
}

// Forget makes the runner lose the image of a build, as a person removing it from the runtime would.
func (r *Runner) Forget(exam, hash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Built, runner.BuiltName(exam, hash))
}

// BuildImage is image.build: it reads the build context (so that whoever writes it to a pipe is let go), notes the call, and returns what
// BuildHook says, or what the default build makes of it.
func (r *Runner) BuildImage(ctx context.Context, req runner.BuildRequest, buildContext io.Reader, onLog func(text string)) (runner.BuildResult, error) {
	if onLog == nil {
		onLog = func(string) {}
	}
	raw, err := io.ReadAll(buildContext)
	if err != nil {
		return runner.BuildResult{}, err
	}
	files, err := readFiles(bytes.NewReader(raw))
	if err != nil {
		return runner.BuildResult{}, fmt.Errorf("%w: the build context is not a tar archive: %v", runner.ErrBadRequest, err)
	}
	call := BuildCall{Request: req, Files: files, Log: onLog}
	r.mu.Lock()
	r.Builds = append(r.Builds, call)
	hook := r.BuildHook
	if r.buildNow++; r.buildNow > r.BuildPeak {
		r.BuildPeak = r.buildNow
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.buildNow--
		r.mu.Unlock()
	}()
	if hook != nil {
		result, err := hook(ctx, call)
		result.Exam = req.Exam
		return result, err
	}
	if !r.Images[req.Image] {
		return runner.BuildResult{}, fmt.Errorf("%w: %q", runner.ErrNotApproved, req.Image)
	}
	text, found := files["Containerfile"]
	if !found {
		return runner.BuildResult{}, fmt.Errorf("%w: the build context has no file called Containerfile", runner.ErrBadRequest)
	}
	result := runner.BuildResult{Exam: req.Exam, BaseName: req.Image, BaseDigest: BaseDigest}
	// say adds to the log and sends it on, as the runner does.
	say := func(text string) {
		result.Log += text
		onLog(text)
	}
	say("Base image: " + req.Image + " digest " + BaseDigest + "\n")
	check := containerfile.Check(text, req.Image)
	result.Warnings = check.Warnings
	if len(check.Problems) > 0 {
		result.Failure, result.Message = runner.FailContainerfile, "the Containerfile is not accepted: "+strings.Join(check.Problems, "; ")
		say(result.Message + "\n")
		return result, nil
	}
	sum := sha256.Sum256(append([]byte(BaseDigest), raw...))
	result.Hash = hex.EncodeToString(sum[:8])
	result.Tag = runner.BuiltReference(req.Exam, result.Hash)
	result.ImageID = "sha256:fake" + result.Hash
	name := runner.BuiltName(req.Exam, result.Hash)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Built[name] != "" && !req.Rebuild {
		result.Cached = true
		say("The image " + result.Tag + " is built already: nothing was built.\n")
	} else {
		say("STEP 1/1: FROM " + req.Image + "\n")
		say("Verified. Tagged " + result.Tag + "\n")
	}
	r.Built[name] = req.Exam
	result.OK = true
	return result, nil
}

// readFiles reads a tar archive into name -> content.
func readFiles(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(reader)
			if err != nil {
				return out, err
			}
			out[header.Name] = string(data)
		}
	}
}

// Grade is grading.run: it reads the two archives it is given (so that whoever writes them to a pipe is let go), notes the call, and returns
// what GradeHook says.
func (r *Runner) Grade(ctx context.Context, req runner.GradeRequest, submission, bundle io.Reader) (runner.GradeResult, error) {
	files, err := readFiles(submission)
	if err != nil {
		return runner.GradeResult{}, fmt.Errorf("%w: the submission is not a tar archive: %v", runner.ErrBadRequest, err)
	}
	bundled, err := readFiles(bundle)
	if err != nil {
		return runner.GradeResult{}, fmt.Errorf("%w: the bundle is not a tar archive: %v", runner.ErrBadRequest, err)
	}
	call := GradeCall{Request: req, Files: files, Bundle: bundled}
	r.mu.Lock()
	r.Gradings = append(r.Gradings, call)
	hook := r.GradeHook
	if r.gradeNow++; r.gradeNow > r.GradePeak {
		r.GradePeak = r.gradeNow
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.gradeNow--
		r.mu.Unlock()
	}()
	if hook != nil {
		result, err := hook(ctx, call)
		result.Grading = req.Grading
		return result, err
	}
	return runner.GradeResult{Grading: req.Grading, Stdout: "{}"}, nil
}
