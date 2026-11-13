package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"exam-platform/internal/runner"
)

// RunnerAPI is what the exam server asks of cbe-runner; runner.Client is it.
type RunnerAPI interface {
	Create(ctx context.Context, req runner.CreateRequest) (runner.Status, error)
	Seed(ctx context.Context, session string, bundle io.Reader) (runner.Status, error)
	Start(ctx context.Context, session string) (runner.Status, error)
	Pause(ctx context.Context, session string) (runner.Status, error)
	Unpause(ctx context.Context, session string) (runner.Status, error)
	Stop(ctx context.Context, session string) (runner.Status, error)
	Remove(ctx context.Context, session string) error
	Status(ctx context.Context, session string) (runner.Status, error)
	Stats(ctx context.Context, session string) (runner.Stats, error)
	ExamStats(ctx context.Context, exam string) ([]runner.Stats, error)
	Archive(ctx context.Context, session, path string) (io.ReadCloser, error)
	ConnectAgent(ctx context.Context, session string) (net.Conn, error)
	ImageInspect(ctx context.Context, name string) (runner.ImageStatus, error)
}

var _ RunnerAPI = (*runner.Client)(nil)

// Options are what a Manager is made of.
type Options struct {
	Repo   Repository
	Runner RunnerAPI
	Blobs  Blobs
	Logger *slog.Logger
	Now    func() time.Time
	// SnapshotEvery is the interval of periodic snapshots when the exam does not set one (decision D8: 60 seconds). Tick is how often
	// the supervisor looks at every session. HeartbeatTimeout is how long an agent may be silent before it is taken for lost.
	SnapshotEvery    time.Duration
	Tick             time.Duration
	HeartbeatTimeout time.Duration
	// Retention is how long a frozen container is kept after the exam ended before it is removed (its snapshots stay).
	Retention time.Duration
	Limits    SnapshotLimits
	// ReadyTimeout is how long provisioning waits for the agent to come up.
	ReadyTimeout time.Duration
}

func (o *Options) fill() {
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.SnapshotEvery <= 0 {
		o.SnapshotEvery = 60 * time.Second
	}
	if o.Tick <= 0 {
		o.Tick = 2 * time.Second
	}
	if o.HeartbeatTimeout <= 0 {
		o.HeartbeatTimeout = 20 * time.Second
	}
	if o.Retention <= 0 {
		o.Retention = 48 * time.Hour
	}
	if o.Limits.MaxFiles == 0 {
		o.Limits = DefaultSnapshotLimits()
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = 45 * time.Second
	}
}

// Manager keeps every workspace session's container in step with the session: it makes it when the student logs in, watches it while
// the exam lasts, takes the snapshots, and freezes it when the time is up.
type Manager struct {
	opts      Options
	Snapshots *Snapshots

	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	relays map[string]*relay
	silent map[string]int
}

// NewManager makes the manager of the exam server's workspace sessions.
func NewManager(opts Options) *Manager {
	opts.fill()
	return &Manager{opts: opts, Snapshots: NewSnapshots(opts.Repo, opts.Blobs, opts.Runner, opts.Limits, opts.Now), locks: map[string]*sync.Mutex{}, relays: map[string]*relay{}, silent: map[string]int{}}
}

func (m *Manager) lock(session string) func() {
	m.mu.Lock()
	l := m.locks[session]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[session] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// ErrOver means the session's time is up: no container is made for it, and none is opened to it.
var ErrOver = errors.New("workspace: the exam is over for this session")

// IsWorkspaceSession says whether a session belongs to a workspace exam.
func (m *Manager) IsWorkspaceSession(ctx context.Context, session string) (bool, error) {
	if _, err := m.opts.Repo.Session(ctx, session); err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// OnLogin is called when a student has logged in. For a workspace exam it starts making their container, without making the login
// wait for it; the connection to the workspace waits for it instead.
func (m *Manager) OnLogin(session string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if ok, err := m.IsWorkspaceSession(ctx, session); err != nil || !ok {
			if err != nil {
				m.opts.Logger.Error("workspace: login", "session", session, "error", err)
			}
			return
		}
		if _, err := m.Ensure(ctx, session); err != nil && !errors.Is(err, ErrOver) {
			m.opts.Logger.Error("workspace: the container could not be made", "session", session, "error", err)
		}
	}()
}

// Ensure makes sure a workspace session has a container that is running with its agent up, making it (with the starter in /work) the
// first time. It is safe to ask again at any moment, from any number of places: a container that is there is left as it is.
func (m *Manager) Ensure(ctx context.Context, session string) (SessionInfo, error) {
	defer m.lock(session)()
	info, err := m.opts.Repo.EnsureSession(ctx, session, m.opts.Now())
	if err != nil {
		return info, err
	}
	now := m.opts.Now()
	if info.Over(now) || info.State == StateFrozen || info.State == StateRemoved {
		return info, ErrOver
	}
	if info.State == StateReady {
		status, err := m.opts.Runner.Status(ctx, session)
		if err == nil {
			switch status.State {
			case runner.StateRunning:
				return info, nil
			case runner.StatePaused:
				if _, err := m.opts.Runner.Unpause(ctx, session); err != nil {
					return info, err
				}
				return info, nil
			case runner.StateExited, runner.StateCreated:
				// The agent was lost (the student ended it, or it failed): the same path as the supervisor's, so that wherever it is noticed
				// first the files are snapshotted, the container is started again on the same volume, and it is counted and told.
				if err := m.restart(ctx, info, "the container ended", false); err != nil {
					return info, err
				}
				return m.opts.Repo.Session(ctx, session)
			}
		}
	}
	return m.provision(ctx, info)
}

func (m *Manager) provision(ctx context.Context, info SessionInfo) (SessionInfo, error) {
	fail := func(err error) (SessionInfo, error) {
		_ = m.opts.Repo.SetState(ctx, info.SessionID, StateFailed, err.Error(), m.opts.Now())
		info.State, info.Failure = StateFailed, err.Error()
		return info, err
	}
	if err := m.opts.Repo.SetState(ctx, info.SessionID, StateProvisioning, "", m.opts.Now()); err != nil {
		return info, err
	}
	exam, err := m.opts.Repo.Exam(ctx, info.ExamID)
	if err != nil {
		return fail(err)
	}
	settings := exam.Document.Workspace
	status, err := m.opts.Runner.Create(ctx, runner.CreateRequest{Session: info.SessionID, Exam: info.ExamID, Image: settings.Image,
		Resources: runner.Resources{CPU: settings.Resources.CPU, MemoryMB: settings.Resources.MemoryMB, DiskMB: settings.Resources.DiskMB}})
	if err != nil {
		return fail(fmt.Errorf("creating the container: %w", err))
	}
	if !status.Seeded {
		starter, err := m.opts.Blobs.Open(exam.StarterHash)
		if err != nil {
			return fail(fmt.Errorf("the starter bundle: %w", err))
		}
		_, err = m.opts.Runner.Seed(ctx, info.SessionID, starter)
		starter.Close()
		if err != nil {
			return fail(fmt.Errorf("putting the starter in /work: %w", err))
		}
	}
	if _, err := m.opts.Runner.Start(ctx, info.SessionID); err != nil {
		return fail(fmt.Errorf("starting the container: %w", err))
	}
	if err := m.waitAgent(ctx, info.SessionID); err != nil {
		return fail(err)
	}
	if err := m.opts.Repo.SetState(ctx, info.SessionID, StateReady, "", m.opts.Now()); err != nil {
		return info, err
	}
	m.opts.Logger.Info("workspace ready", "session", info.SessionID, "exam", info.ExamID)
	return m.opts.Repo.Session(ctx, info.SessionID)
}

// waitAgent waits until the runner has heard the agent.
func (m *Manager) waitAgent(ctx context.Context, session string) error {
	deadline := time.Now().Add(m.opts.ReadyTimeout)
	for {
		status, err := m.opts.Runner.Status(ctx, session)
		if err == nil && status.State == runner.StateRunning && status.Agent.Connected && !status.Agent.LastSeen.IsZero() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the agent did not come up within %s (container %s)", m.opts.ReadyTimeout, status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// ClientStatus is what a student's application is told about its workspace (the ws.status message of section 6).
type ClientStatus struct {
	// State is preparing, running, restarting, frozen, ended or failed.
	State           string     `json:"state"`
	LastSnapshotAt  *time.Time `json:"last_snapshot_at,omitempty"`
	DeadlineAt      time.Time  `json:"deadline_at"`
	SecondsLeft     int64      `json:"seconds_left"`
	AgentRestarts   int        `json:"agent_restarts"`
	TerminalAllowed bool       `json:"terminal"`
	Failure         string     `json:"failure,omitempty"`
}

// Status describes a session's workspace to its student.
func (m *Manager) Status(ctx context.Context, session string) (ClientStatus, error) {
	info, err := m.opts.Repo.Session(ctx, session)
	if err != nil {
		return ClientStatus{}, err
	}
	now := m.opts.Now()
	status := ClientStatus{LastSnapshotAt: info.LastSnapshotAt, DeadlineAt: info.Deadline(), AgentRestarts: info.AgentRestarts, Failure: info.Failure}
	if left := info.Deadline().Sub(now); left > 0 {
		status.SecondsLeft = int64(left / time.Second)
	}
	switch {
	case info.State == StateFailed:
		status.State = "failed"
	case info.State == StateFrozen || info.State == StateRemoved:
		status.State = "frozen"
	case info.Over(now):
		status.State = "ended"
	case info.State == StateReady:
		status.State = "running"
		if running, err := m.opts.Runner.Status(ctx, session); err != nil || running.State != runner.StateRunning || !running.Agent.Connected {
			status.State = "restarting"
		}
	default:
		status.State = "preparing"
	}
	if exam, err := m.opts.Repo.Exam(ctx, info.ExamID); err == nil {
		status.TerminalAllowed = exam.Document.Workspace.Terminal
	}
	return status, nil
}

// Supervise watches every workspace session until ctx ends: it takes the periodic snapshots, notices a lost agent and restarts the
// container, freezes a session whose time is up, and removes what has been frozen for the retention period.
func (m *Manager) Supervise(ctx context.Context) {
	ticker := time.NewTicker(m.opts.Tick)
	defer ticker.Stop()
	for {
		m.SuperviseOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SuperviseOnce makes one round.
func (m *Manager) SuperviseOnce(ctx context.Context) {
	sessions, err := m.opts.Repo.Sessions(ctx)
	if err != nil {
		m.opts.Logger.Error("workspace: listing sessions", "error", err)
		return
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, info := range sessions {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			m.superviseOne(ctx, info.SessionID)
		}()
	}
	wg.Wait()
}

func (m *Manager) superviseOne(ctx context.Context, session string) {
	defer m.lock(session)()
	// Read again under the lock: another goroutine may have changed it since the list was made.
	info, err := m.opts.Repo.Session(ctx, session)
	if err != nil {
		return
	}
	now := m.opts.Now()
	log := m.opts.Logger.With("session", session)
	switch info.State {
	case StateProvisioning:
		// A provisioning that nobody is finishing (the server restarted in the middle of it) is taken up again.
		if now.Sub(m.provisioningSince(info)) > m.opts.ReadyTimeout+10*time.Second && !info.Over(now) {
			if _, err := m.provision(ctx, info); err != nil {
				log.Error("workspace: provisioning again failed", "error", err)
			}
		}
	case StateReady:
		if info.Over(now) {
			m.freeze(ctx, info)
			return
		}
		m.watchReady(ctx, info)
	case StateFrozen:
		if info.FrozenAt != nil && now.Sub(*info.FrozenAt) >= m.opts.Retention {
			if err := m.opts.Runner.Remove(ctx, session); err != nil {
				log.Error("workspace: removing the container after the retention period failed", "error", err)
				return
			}
			_ = m.opts.Repo.SetState(ctx, session, StateRemoved, "", now)
			log.Info("workspace removed after the retention period")
		}
	}
}

func (m *Manager) provisioningSince(info SessionInfo) time.Time {
	if info.ReadyAt != nil {
		return *info.ReadyAt
	}
	return time.Time{}
}

// freeze takes the last snapshot, at the deadline, and pauses the container: its processes stop, its files stay.
func (m *Manager) freeze(ctx context.Context, info SessionInfo) {
	log := m.opts.Logger.With("session", info.SessionID)
	if record, _, err := m.Snapshots.Take(ctx, info.SessionID, ReasonDeadline); err != nil {
		log.Error("workspace: the final snapshot failed; the container stays as it is and the next round tries again", "error", err)
		return
	} else {
		log.Info("workspace frozen", "snapshot", record.ID, "files", record.Files)
	}
	if _, err := m.opts.Runner.Pause(ctx, info.SessionID); err != nil && !errors.Is(err, runner.ErrNotFound) {
		log.Error("workspace: pausing the container failed", "error", err)
		return
	}
	_ = m.opts.Repo.SetState(ctx, info.SessionID, StateFrozen, "", m.opts.Now())
	m.closeRelay(info.SessionID, "the exam is over")
}

// watchReady looks at a running session: a container that is gone or stopped, or an agent that has gone quiet, is a lost agent; a
// healthy one gets its periodic snapshot.
func (m *Manager) watchReady(ctx context.Context, info SessionInfo) {
	log := m.opts.Logger.With("session", info.SessionID)
	status, err := m.opts.Runner.Status(ctx, info.SessionID)
	if err != nil {
		log.Warn("workspace: the runner did not answer", "error", err)
		return
	}
	switch status.State {
	case runner.StateMissing:
		_ = m.opts.Repo.SetState(ctx, info.SessionID, StateFailed, "the container is gone", m.opts.Now())
		log.Error("workspace: the container is gone")
		return
	case runner.StateExited, runner.StateCreated:
		if err := m.restart(ctx, info, "the container ended", false); err != nil {
			log.Error("workspace: the container could not be restarted", "error", err)
		}
		return
	case runner.StatePaused:
		// A container is paused only at the deadline; one that is paused before it is let go again.
		if _, err := m.opts.Runner.Unpause(ctx, info.SessionID); err != nil {
			log.Error("workspace: unpausing failed", "error", err)
		}
		return
	}
	silent := !status.Agent.Connected || status.Agent.LastSeen.IsZero() || m.opts.Now().Sub(status.Agent.LastSeen) > m.opts.HeartbeatTimeout
	m.mu.Lock()
	if silent {
		m.silent[info.SessionID]++
	} else {
		delete(m.silent, info.SessionID)
	}
	rounds := m.silent[info.SessionID]
	m.mu.Unlock()
	if silent && rounds >= 3 {
		if err := m.restart(ctx, info, "the agent went quiet", true); err != nil {
			log.Error("workspace: the container could not be restarted", "error", err)
		}
		return
	}
	interval := m.opts.SnapshotEvery
	if exam, err := m.opts.Repo.Exam(ctx, info.ExamID); err == nil && exam.Document.Workspace.SnapshotSeconds != nil {
		interval = time.Duration(*exam.Document.Workspace.SnapshotSeconds) * time.Second
	}
	if info.LastSnapshotAt == nil || m.opts.Now().Sub(*info.LastSnapshotAt) >= interval {
		if _, _, err := m.Snapshots.Take(ctx, info.SessionID, ReasonPeriodic); err != nil {
			log.Error("workspace: the periodic snapshot failed", "error", err)
		}
	}
}

// restart is the answer to a lost agent (the caller holds the session's lock): a snapshot first, so that whatever happens next the student's files are kept, then the
// container is started again on the same volume, and the student's workspace is told.
func (m *Manager) restart(ctx context.Context, info SessionInfo, why string, stop bool) error {
	log := m.opts.Logger.With("session", info.SessionID)
	log.Warn("workspace: agent lost", "why", why)
	if _, _, err := m.Snapshots.Take(ctx, info.SessionID, ReasonAgentLoss); err != nil {
		log.Error("workspace: the snapshot at the loss of the agent failed", "error", err)
	}
	if stop {
		if _, err := m.opts.Runner.Stop(ctx, info.SessionID); err != nil {
			log.Error("workspace: stopping the container failed", "error", err)
		}
	}
	if _, err := m.opts.Runner.Start(ctx, info.SessionID); err != nil {
		return fmt.Errorf("restarting the container: %w", err)
	}
	if err := m.waitAgent(ctx, info.SessionID); err != nil {
		return fmt.Errorf("the agent did not come back: %w", err)
	}
	_ = m.opts.Repo.AddAgentRestart(ctx, info.SessionID)
	m.mu.Lock()
	delete(m.silent, info.SessionID)
	m.mu.Unlock()
	// workspace.agent_restarted is logged here and, in Phase 5, goes into the event log.
	log.Info("workspace.agent_restarted")
	m.notifyRestart(info.SessionID)
	return nil
}
