package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"exam-platform/internal/agentproto"
	"exam-platform/internal/quota"
)

// Quota is the quota helper's side the runner uses: the folder of /work, with its hard limits.
type Quota interface {
	Create(session string, sizeMB, inodes int) (quota.Folder, error)
	Remove(session string) error
}

// meta is what the runner keeps about a container between its own restarts. It lives in the state folder, which only the runner can
// write, never in the folder the student can reach.
type meta struct {
	Session   string    `json:"session"`
	Exam      string    `json:"exam"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Resources Resources `json:"resources"`
	Seeded    bool      `json:"seeded"`
	CreatedAt time.Time `json:"created_at"`
}

// Service is the runner: the operations of Appendix E over a Runtime, the quota helper and the agents.
type Service struct {
	cfg    Config
	rt     Runtime
	quota  Quota
	logger *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex
	links map[string]*agentLink
}

// NewService makes the runner's service. It makes the run and state folders, and adopts the containers that are already there
// (a restarted runner must find them again).
func NewService(cfg Config, rt Runtime, q Quota, logger *slog.Logger) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	for _, dir := range []string{cfg.RunRoot, cfg.StateRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	s := &Service{cfg: cfg, rt: rt, quota: q, logger: logger, locks: map[string]*sync.Mutex{}, links: map[string]*agentLink{}}
	return s, nil
}

// lock serializes the operations on one session.
func (s *Service) lock(session string) func() {
	s.mu.Lock()
	l := s.locks[session]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[session] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (s *Service) metaPath(session string) string {
	return filepath.Join(s.cfg.StateRoot, session+".json")
}

func (s *Service) readMeta(session string) (meta, error) {
	var m meta
	data, err := os.ReadFile(s.metaPath(session))
	if err != nil {
		return m, ErrNotFound
	}
	return m, json.Unmarshal(data, &m)
}

func (s *Service) writeMeta(m meta) error {
	data, _ := json.Marshal(m)
	tmp := s.metaPath(m.Session) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath(m.Session))
}

func (s *Service) runDir(session string) string { return filepath.Join(s.cfg.RunRoot, session) }

// Reconcile finds the containers the runner made, writes down what it did not know, and connects to the agents of the running ones.
// A runner that was restarted calls it once.
func (s *Service) Reconcile(ctx context.Context) (int, error) {
	list, err := s.rt.List(ctx, LabelManaged, "1")
	if err != nil {
		return 0, err
	}
	for _, c := range list {
		session := c.Labels[LabelSession]
		if !ValidID(session) {
			continue
		}
		if _, err := s.readMeta(session); err != nil {
			_ = s.writeMeta(meta{Session: session, Exam: c.Labels[LabelExam], Name: c.Name, Image: c.Labels[LabelImage], Seeded: c.State != StateCreated, CreatedAt: time.Now().UTC()})
		}
		if c.State == StateRunning || c.State == StatePaused {
			s.ensureLink(session)
		}
	}
	return len(list), nil
}

func (s *Service) ensureLink(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.links[session] == nil {
		s.links[session] = startAgentLink(filepath.Join(s.runDir(session), "agent.sock"))
	}
}

func (s *Service) dropLink(session string) {
	s.mu.Lock()
	l := s.links[session]
	delete(s.links, session)
	s.mu.Unlock()
	if l != nil {
		l.stop()
	}
}

// Close ends the connections to the agents; the containers keep running.
func (s *Service) Close() {
	s.mu.Lock()
	links := s.links
	s.links = map[string]*agentLink{}
	s.mu.Unlock()
	for _, l := range links {
		l.stop()
	}
}

// Create is container.create: the folder of /work with its quota, the folder of the agent's socket, and the container, not started.
// Asking again for a session that has its container returns it, so an exam server that does not know whether its last call arrived
// can ask again.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Status, error) {
	resources, reference, err := s.cfg.Check(req)
	if err != nil {
		return Status{}, err
	}
	defer s.lock(req.Session)()
	if existing, err := s.readMeta(req.Session); err == nil {
		if existing.Exam != req.Exam || existing.Image != req.Image {
			return Status{}, fmt.Errorf("%w: session %s already has a container for another exam or image", ErrConflict, req.Session)
		}
		return s.statusLocked(ctx, existing)
	}
	// A container of this name without a note of ours (the runner stopped between the two) is taken over.
	name := ContainerName(req.Exam, req.Session)
	if found, err := s.rt.Inspect(ctx, name); err == nil && found.Labels[LabelManaged] == "1" && found.Labels[LabelSession] == req.Session {
		m := meta{Session: req.Session, Exam: req.Exam, Name: name, Image: req.Image, Resources: resources, Seeded: found.State != StateCreated, CreatedAt: time.Now().UTC()}
		if err := s.writeMeta(m); err != nil {
			return Status{}, err
		}
		return s.statusLocked(ctx, m)
	}
	if _, err := s.rt.ImageInspect(ctx, reference); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Status{}, fmt.Errorf("%w: the runtime does not have image %s", ErrNotFound, reference)
		}
		return Status{}, err
	}
	if _, err := s.quota.Create(req.Session, resources.DiskMB, s.cfg.Inodes); err != nil {
		return Status{}, fmt.Errorf("the /work folder: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(s.runDir(req.Session))
		_ = s.quota.Remove(req.Session)
	}
	run := s.runDir(req.Session)
	if err := os.MkdirAll(run, 0o777); err != nil {
		cleanup()
		return Status{}, err
	}
	// The student's agent creates its socket here, so the folder must be open to uid 1000 (mapped); its parent, RunRoot, is private.
	if err := os.Chmod(run, 0o777); err != nil {
		cleanup()
		return Status{}, err
	}
	if _, err := s.rt.Create(ctx, BuildSpec(s.cfg, req, resources, reference)); err != nil {
		cleanup()
		return Status{}, fmt.Errorf("the container: %w", err)
	}
	m := meta{Session: req.Session, Exam: req.Exam, Name: name, Image: req.Image, Resources: resources, CreatedAt: time.Now().UTC()}
	if err := s.writeMeta(m); err != nil {
		_ = s.rt.Remove(ctx, name)
		cleanup()
		return Status{}, err
	}
	s.logger.Info("container created", "session", req.Session, "exam", req.Exam, "image", req.Image)
	return s.statusLocked(ctx, m)
}

// Seed is container.seed: it puts the exam's starter bundle into /work, once, before the container first starts. It is not in the
// plan's Appendix E: without it the starter cannot reach the container (decision P3-4).
func (s *Service) Seed(ctx context.Context, session string, bundle io.Reader) (Status, error) {
	if !ValidID(session) {
		return Status{}, badRequest("session must be an ID")
	}
	defer s.lock(session)()
	m, err := s.readMeta(session)
	if err != nil {
		return Status{}, err
	}
	if m.Seeded {
		return Status{}, fmt.Errorf("%w: the starter was already put in", ErrConflict)
	}
	found, err := s.rt.Inspect(ctx, m.Name)
	if err != nil {
		return Status{}, err
	}
	if found.State != StateCreated {
		return Status{}, fmt.Errorf("%w: the container has been started; the starter goes in before that", ErrConflict)
	}
	// Sanitize into memory first (at most 20 MiB), so a bundle that is refused leaves /work untouched.
	var clean bytes.Buffer
	if _, err := SanitizeSeed(io.LimitReader(bundle, MaxSeedBytes+MaxSeedFiles*1024+1<<20), &clean); err != nil {
		return Status{}, err
	}
	if err := s.rt.CopyTo(ctx, m.Name, WorkPath, &clean); err != nil {
		return Status{}, fmt.Errorf("putting the starter in /work: %w", err)
	}
	m.Seeded = true
	if err := s.writeMeta(m); err != nil {
		return Status{}, err
	}
	return s.statusLocked(ctx, m)
}

// Start is container.start; a container that is running is left as it is.
func (s *Service) Start(ctx context.Context, session string) (Status, error) {
	return s.change(ctx, session, func(m meta, c Inspected) error {
		if !m.Seeded {
			return fmt.Errorf("%w: the starter has not been put in yet", ErrConflict)
		}
		switch c.State {
		case StateRunning:
			return nil
		case StatePaused:
			return s.rt.Unpause(ctx, m.Name)
		}
		return s.rt.Start(ctx, m.Name)
	}, true)
}

// Pause is container.pause: it freezes every process, the agent included. A container that is not running is left as it is.
func (s *Service) Pause(ctx context.Context, session string) (Status, error) {
	return s.change(ctx, session, func(m meta, c Inspected) error {
		if c.State != StateRunning {
			return nil
		}
		return s.rt.Pause(ctx, m.Name)
	}, false)
}

// Unpause is container.unpause.
func (s *Service) Unpause(ctx context.Context, session string) (Status, error) {
	return s.change(ctx, session, func(m meta, c Inspected) error {
		if c.State != StatePaused {
			return nil
		}
		return s.rt.Unpause(ctx, m.Name)
	}, true)
}

// Stop is container.stop: the agent is asked to end, and killed if it does not within the stop time.
func (s *Service) Stop(ctx context.Context, session string) (Status, error) {
	status, err := s.change(ctx, session, func(m meta, c Inspected) error {
		if c.State != StateRunning && c.State != StatePaused {
			return nil
		}
		if c.State == StatePaused {
			if err := s.rt.Unpause(ctx, m.Name); err != nil {
				return err
			}
		}
		return s.rt.Stop(ctx, m.Name, s.cfg.StopTimeout())
	}, false)
	if err == nil {
		s.dropLink(session)
	}
	return status, err
}

func (s *Service) change(ctx context.Context, session string, act func(meta, Inspected) error, link bool) (Status, error) {
	if !ValidID(session) {
		return Status{}, badRequest("session must be an ID")
	}
	defer s.lock(session)()
	m, err := s.readMeta(session)
	if err != nil {
		return Status{}, err
	}
	c, err := s.rt.Inspect(ctx, m.Name)
	if err != nil {
		return Status{}, err
	}
	if err := act(m, c); err != nil {
		return Status{}, err
	}
	if link {
		s.ensureLink(session)
	}
	return s.statusLocked(ctx, m)
}

// Remove is container.remove: the container, then the /work folder with its quota, then the agent's socket folder, then the runner's
// note. It is done in that order because nothing may be left running in a folder that is being deleted. A session that is already
// gone is not an error.
func (s *Service) Remove(ctx context.Context, session string) error {
	if !ValidID(session) {
		return badRequest("session must be an ID")
	}
	defer s.lock(session)()
	s.dropLink(session)
	m, err := s.readMeta(session)
	if err == nil {
		if err := s.rt.Remove(ctx, m.Name); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	if err := s.quota.Remove(session); err != nil {
		return fmt.Errorf("the /work folder: %w", err)
	}
	if err := os.RemoveAll(s.runDir(session)); err != nil {
		s.logger.Warn("the agent's socket folder could not be removed", "session", session, "error", err)
	}
	if err := os.Remove(s.metaPath(session)); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.logger.Info("container removed", "session", session)
	return nil
}

// Status is container.status.
func (s *Service) Status(ctx context.Context, session string) (Status, error) {
	if !ValidID(session) {
		return Status{}, badRequest("session must be an ID")
	}
	m, err := s.readMeta(session)
	if err != nil {
		return Status{Session: session, State: StateMissing}, nil
	}
	return s.statusLocked(ctx, m)
}

func (s *Service) statusLocked(ctx context.Context, m meta) (Status, error) {
	status := Status{Session: m.Session, Exam: m.Exam, Seeded: m.Seeded, Resources: m.Resources, State: StateMissing}
	c, err := s.rt.Inspect(ctx, m.Name)
	switch {
	case errors.Is(err, ErrNotFound):
		return status, nil
	case err != nil:
		return Status{}, err
	}
	status.State, status.StartedAt = c.State, c.StartedAt
	if c.State == StateExited {
		code := c.ExitCode
		status.ExitCode = &code
	}
	s.mu.Lock()
	link := s.links[m.Session]
	s.mu.Unlock()
	if link != nil {
		status.Agent = link.current()
	}
	return status, nil
}

// Stats is container.stats for one session.
func (s *Service) Stats(ctx context.Context, session string) (Stats, error) {
	if !ValidID(session) {
		return Stats{}, badRequest("session must be an ID")
	}
	m, err := s.readMeta(session)
	if err != nil {
		return Stats{}, err
	}
	return s.statsOf(ctx, m)
}

func (s *Service) statsOf(ctx context.Context, m meta) (Stats, error) {
	raw, err := s.rt.Stats(ctx, m.Name)
	if err != nil {
		return Stats{}, err
	}
	return Stats{Session: m.Session, CPUPercent: raw.CPUPercent, MemoryBytes: raw.MemoryBytes, MemoryLimit: raw.MemoryLimit, Pids: raw.Pids}, nil
}

// ExamStats is container.stats for every running container of an exam.
func (s *Service) ExamStats(ctx context.Context, exam string) ([]Stats, error) {
	if !ValidID(exam) {
		return nil, badRequest("exam must be an ID")
	}
	list, err := s.rt.List(ctx, LabelExam, exam)
	if err != nil {
		return nil, err
	}
	var out []Stats
	for _, c := range list {
		if c.State != StateRunning {
			continue
		}
		m, err := s.readMeta(c.Labels[LabelSession])
		if err != nil {
			continue
		}
		if one, err := s.statsOf(ctx, m); err == nil {
			out = append(out, one)
		}
	}
	return out, nil
}

// ArchivePath checks a path the exam server asks to read and returns it as the runtime takes it. Only /work and what is under it:
// "/work/." (the contents, which names the entries the same way in a running and in a paused container, P0-6) or "/work/<name>…".
func ArchivePath(p string) (string, error) {
	if p == WorkPath || p == WorkPath+"/." {
		return WorkPath + "/.", nil
	}
	rel, found := strings.CutPrefix(p, WorkPath+"/")
	if !found || rel == "" {
		return "", badRequest("the archive path must be /work/. or a path under /work")
	}
	if err := agentproto.CheckPath(rel); err != nil || rel == "" || path.Clean(rel) != rel {
		return "", badRequest("the archive path is not a clean path under /work")
	}
	return WorkPath + "/" + rel, nil
}

// Archive is container.archive: a tar stream of a path under /work, read through the runtime (D14), from a running or a paused
// container. The caller closes it.
func (s *Service) Archive(ctx context.Context, session, p string) (io.ReadCloser, error) {
	if !ValidID(session) {
		return nil, badRequest("session must be an ID")
	}
	clean, err := ArchivePath(p)
	if err != nil {
		return nil, err
	}
	m, err := s.readMeta(session)
	if err != nil {
		return nil, err
	}
	return s.rt.CopyFrom(ctx, m.Name, clean)
}

// ErrFrozen means the container is paused: its agent cannot answer.
var ErrFrozen = errors.New("the container is paused")

// ConnectAgent is agent.connect: a connection to the agent's socket in a running container. The caller relays bytes both ways.
func (s *Service) ConnectAgent(ctx context.Context, session string) (net.Conn, error) {
	if !ValidID(session) {
		return nil, badRequest("session must be an ID")
	}
	m, err := s.readMeta(session)
	if err != nil {
		return nil, err
	}
	c, err := s.rt.Inspect(ctx, m.Name)
	if err != nil {
		return nil, err
	}
	switch c.State {
	case StateRunning:
	case StatePaused:
		return nil, ErrFrozen
	default:
		return nil, fmt.Errorf("%w: the container is %s", ErrConflict, c.State)
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	return dialer.DialContext(ctx, "unix", filepath.Join(s.runDir(session), "agent.sock"))
}

// ImageInspect is image.inspect: whether an image is approved, and whether the runtime has it.
func (s *Service) ImageInspect(ctx context.Context, name string) (ImageStatus, error) {
	status := ImageStatus{Name: name}
	reference, approved := s.cfg.Images[name]
	if !approved {
		return status, nil
	}
	status.Approved = true
	info, err := s.rt.ImageInspect(ctx, reference)
	if errors.Is(err, ErrNotFound) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Present, status.ID = true, info.ID
	return status, nil
}
