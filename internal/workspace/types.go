// Package workspace is the exam server's side of workspace exams (workspace plan, section 4): importing a package, creating a
// student's container when they log in, relaying their editor and terminals to the agent in it, taking snapshots of their files,
// and keeping watch over the container until the exam ends. It never holds the container runtime's socket: everything it asks of
// a container it asks of cbe-runner (internal/runner), which is the only process that does.
package workspace

import (
	"context"
	"errors"
	"time"

	"exam-platform/shared/calc"
)

// ErrNotFound is returned for an exam or a session that is not a workspace one.
var ErrNotFound = errors.New("workspace: not found")

// Session states, in the order of a session's life.
const (
	StateNew          = "new"
	StateProvisioning = "provisioning"
	StateReady        = "ready"
	StateFrozen       = "frozen"
	StateRemoved      = "removed"
	StateFailed       = "failed"
)

// Snapshot reasons: the moments of decision D8.
const (
	ReasonPeriodic  = "periodic"
	ReasonSubmit    = "submit"
	ReasonAgentLoss = "agent_loss"
	ReasonDeadline  = "deadline"
)

// Resources are a container's limits, as the exam document states them.
type Resources struct {
	CPU      float64 `json:"cpu"`
	MemoryMB int     `json:"memory_mb"`
	DiskMB   int     `json:"disk_mb"`
}

// Settings are the `workspace` part of the compiled document (plan, section 5.4).
type Settings struct {
	Image           string    `json:"image"`
	Terminal        bool      `json:"terminal"`
	Run             string    `json:"run,omitempty"`
	Check           string    `json:"check,omitempty"`
	Submit          []string  `json:"submit,omitempty"`
	Ignore          []string  `json:"ignore"`
	Resources       Resources `json:"resources"`
	SnapshotSeconds *int64    `json:"snapshot_seconds,omitempty"`
}

// Todo is one item of the TODO list.
type Todo struct {
	ID       string   `json:"id"`
	TextHTML string   `json:"text_html"`
	Points   *float64 `json:"points,omitempty"`
}

// Content is what the student reads.
type Content struct {
	Instructions string `json:"instructions,omitempty"`
	Todos        []Todo `json:"todos"`
}

// Document is the compiled workspace exam as the server takes it (the document of shared/examdoc, which the server does not import).
type Document struct {
	Kind             string      `json:"kind"`
	Title            string      `json:"title"`
	PIN              string      `json:"pin,omitempty"`
	DurationSeconds  int64       `json:"duration_seconds"`
	GraceSeconds     *int64      `json:"grace_seconds,omitempty"`
	MinVolumePercent *int        `json:"min_volume_percent,omitempty"`
	OpensAt          *time.Time  `json:"opens_at,omitempty"`
	ClosesAt         *time.Time  `json:"closes_at,omitempty"`
	Tools            *calc.Tools `json:"tools,omitempty"`
	Workspace        Settings    `json:"workspace"`
	Content          Content     `json:"content"`
	StarterBundle    string      `json:"starter_bundle"`
	GradingBundle    string      `json:"grading_bundle,omitempty"`
}

// ExamRecord is an imported workspace exam, as it is stored.
type ExamRecord struct {
	ExamID           string
	ClassID          string
	Title            string
	PIN              string
	DurationSeconds  int64
	GraceSeconds     int64
	MinVolumePercent int
	OpensAt          *time.Time
	ClosesAt         *time.Time
	Tools            *calc.Tools
	// Document is the compiled document without its PIN. StarterHash and GradingHash name the bundle files.
	Document    Document
	StarterHash string
	GradingHash string
	CreatedAt   time.Time
}

// SessionInfo is a student's session of a workspace exam, with where its container is in its life.
type SessionInfo struct {
	SessionID string
	ExamID    string
	Account   string
	// Status is the session's own status (active, grace, submitted, force_submitted).
	Status     string
	DeadlineAt time.Time
	GraceUntil time.Time
	// EndsAt is the proctor's cap on the exam's end, when there is one.
	EndsAt         *time.Time
	State          string
	AgentRestarts  int
	ReadyAt        *time.Time
	FrozenAt       *time.Time
	LastSnapshotAt *time.Time
	Failure        string
}

// Deadline is when the student's time is up: the end of the exam's length, or the proctor's cap when that comes first.
func (s SessionInfo) Deadline() time.Time {
	if s.EndsAt != nil && s.EndsAt.Before(s.DeadlineAt) {
		return *s.EndsAt
	}
	return s.DeadlineAt
}

// FreezeAt is when the container is frozen: the deadline and the grace period after it, which a proctor's cap on the end time moves
// earlier by as much as it moved the deadline.
func (s SessionInfo) FreezeAt() time.Time {
	grace := s.GraceUntil.Sub(s.DeadlineAt)
	if grace < 0 {
		grace = 0
	}
	return s.Deadline().Add(grace)
}

// Over says whether the session no longer takes work: its time and its grace are up, or it was submitted for good.
func (s SessionInfo) Over(now time.Time) bool {
	return s.Status == "submitted" || s.Status == "force_submitted" || !now.Before(s.FreezeAt())
}

// SnapshotRecord is a row of the snapshot list.
type SnapshotRecord struct {
	ID           int64
	SessionID    string
	Reason       string
	TakenAt      time.Time
	ManifestHash string
	Files        int
	Bytes        int64
	Skipped      int
}

// Repository is what the exam server's database does for workspace exams; internal/storage implements it.
type Repository interface {
	// CreateExam stores a workspace exam in its class: the exams row (kind workspace, no exercises) and the workspace row.
	CreateExam(ctx context.Context, exam ExamRecord) error
	// Exam reads an imported exam; ErrNotFound when the exam is not a workspace exam.
	Exam(ctx context.Context, examID string) (ExamRecord, error)
	// Session reads a student's session of a workspace exam (ErrNotFound when it is not one), with its workspace state.
	Session(ctx context.Context, sessionID string) (SessionInfo, error)
	// EnsureSession makes the workspace row of a session if it has none, and returns the session.
	EnsureSession(ctx context.Context, sessionID string, now time.Time) (SessionInfo, error)
	// SetState moves a session along its life; failure is kept when the state is failed.
	SetState(ctx context.Context, sessionID, state, failure string, now time.Time) error
	// Sessions lists the workspace sessions that are not removed or failed, for the supervisor.
	Sessions(ctx context.Context) ([]SessionInfo, error)
	AddAgentRestart(ctx context.Context, sessionID string) error
	RecordSnapshot(ctx context.Context, snapshot SnapshotRecord) (int64, error)
	Snapshot(ctx context.Context, id int64) (SnapshotRecord, error)
	LatestSnapshot(ctx context.Context, sessionID string) (SnapshotRecord, error)
	Snapshots(ctx context.Context, sessionID string) ([]SnapshotRecord, error)
	// TouchSnapshot records that a snapshot was taken now, though nothing had changed since the last one.
	TouchSnapshot(ctx context.Context, sessionID string, now time.Time) error
	// Todo reads and writes the TODO state a student ticked: a JSON object of TODO ID to done.
	Todo(ctx context.Context, sessionID string) (map[string]bool, error)
	SetTodo(ctx context.Context, sessionID string, todo map[string]bool) error
}
