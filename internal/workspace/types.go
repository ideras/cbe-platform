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

// ErrBuildBusy is returned when an exam's image is asked to be built and a build of it is already waiting or going.
var ErrBuildBusy = errors.New("workspace: the exam's image is being built already")

// ErrNoImage is returned when an exam is asked for an image it does not have: it has no image folder.
var ErrNoImage = errors.New("workspace: the exam has no image folder")

// ErrNotGraded is returned when a grading is asked to be confirmed and it is not done (it is waiting, running, or failed).
var ErrNotGraded = errors.New("workspace: the grading is not done")

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
	// ReasonFinal is the snapshot taken when the exam ended before its time: the student finished it, or a proctor did.
	ReasonFinal = "final"
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
	// GradeTimeoutSeconds is how long a grading run of the exam may take; nil is the server's default (5 minutes).
	GradeTimeoutSeconds *int64 `json:"grade_timeout_seconds,omitempty"`
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
	ImageBundle      string      `json:"image_bundle,omitempty"`
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
	// Document is the compiled document without its PIN. StarterHash, GradingHash and ImageHash name the bundle files; the last two are empty
	// when the exam has no grading folder or no image folder.
	Document    Document
	StarterHash string
	GradingHash string
	ImageHash   string
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
	// LockedAt is when a proctor's lock or the monitoring's lock was put on the session; nil while it is not locked. A locked session
	// reaches nothing of its workspace, as it is sent no questions (the container goes on running; nobody can use it).
	LockedAt *time.Time
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

// The kinds of submission: the student pressing Save (called Submit until 2026-11-14), and the one made when the exam ends.
const (
	SubmissionSubmit = "submit"
	SubmissionFinal  = "final"
)

// SubmissionRecord is a row of the submission list. The submission's files are the manifest named by ManifestHash.
type SubmissionRecord struct {
	ID           int64
	SessionID    string
	SnapshotID   int64
	Kind         string
	SubmittedAt  time.Time
	ManifestHash string
	Files        int
	Bytes        int64
}

// The kinds of terminal a transcript is of.
const (
	TerminalShell   = "shell"
	TerminalRun     = "run"
	TerminalCheck   = "check"
	TerminalProgram = "program"
)

// TerminalRecord is a row of the transcript list; CastFile is the transcript's name under the workspace folder's transcripts.
type TerminalRecord struct {
	ID        int64
	SessionID string
	Term      uint32
	Kind      string
	StartedAt time.Time
	EndedAt   *time.Time
	ExitCode  *int
	Bytes     int64
	Truncated bool
	CastFile  string
}

// RosterRow is what the proctor's roster knows of a student's workspace.
type RosterRow struct {
	SessionID      string
	Account        string
	State          string
	AgentRestarts  int
	LastSnapshotAt *time.Time
	Todo           map[string]bool
	// Submissions counts the submissions made; LastSubmission is the latest one's time and Final says it was made at the end.
	Submissions    int
	LastSubmission *time.Time
	Final          bool
	// LastEvent is when the latest workspace event of the session was recorded.
	LastEvent *time.Time
	// Grade is the student's grade as it stands: the confirmed grading, or the latest; nil before any grading.
	Grade *GradeSummary
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
	// RecordEvent writes a record in the session's history, the same one the monitoring writes to (event_log).
	RecordEvent(ctx context.Context, sessionID, eventType string, at time.Time, detail any) error
	RecordSubmission(ctx context.Context, submission SubmissionRecord) (int64, error)
	Submission(ctx context.Context, id int64) (SubmissionRecord, error)
	// Submissions lists a session's submissions, oldest first.
	Submissions(ctx context.Context, sessionID string) ([]SubmissionRecord, error)
	RecordTerminal(ctx context.Context, terminal TerminalRecord) (int64, error)
	// EndTerminal records how a terminal ended and how much of it was written.
	EndTerminal(ctx context.Context, id int64, endedAt time.Time, exitCode *int, bytes int64, truncated bool) error
	Terminal(ctx context.Context, id int64) (TerminalRecord, error)
	Terminals(ctx context.Context, sessionID string) ([]TerminalRecord, error)
	// Roster lists the students of a workspace exam, the ones who have logged in.
	Roster(ctx context.Context, examID string) ([]RosterRow, error)

	// Image builds (Phase 7). CreateImageBuild queues a build of the exam's image bundle (ErrBuildBusy when one is queued or running for the
	// exam). StartImageBuild marks it running; FinishImageBuild records how it ended (the record's state, failure, base, warnings and log).
	// InterruptImageBuilds marks every build that is queued or running as failed (the server that was running it stopped). ImageBuild reads
	// a build with its log; ImageBuilds lists an exam's builds, newest first, without their logs; LatestImageBuild reads the newest build
	// of an exam's image bundle with the state ok (ErrNotFound when there is none).
	CreateImageBuild(ctx context.Context, examID, bundleHash string, rebuild bool, now time.Time) (int64, error)
	StartImageBuild(ctx context.Context, id int64, now time.Time) error
	FinishImageBuild(ctx context.Context, record ImageBuildRecord) error
	InterruptImageBuilds(ctx context.Context, now time.Time) (int, error)
	ImageBuild(ctx context.Context, id int64) (ImageBuildRecord, error)
	ImageBuilds(ctx context.Context, examID string, limit int) ([]ImageBuildRecord, error)
	LatestImageBuild(ctx context.Context, examID, bundleHash string) (ImageBuildRecord, error)
	// UpdateImageBuildLog writes the log of a build that is running, so far (nothing happens to a build that is not running).
	UpdateImageBuildLog(ctx context.Context, id int64, log string) error
	// ImageSummaries lists every workspace exam with its TODO count, its image bundle and its latest image build (without its log), for the exam list.
	ImageSummaries(ctx context.Context) ([]ImageSummary, error)

	// Gradings (Phase 6). CreateGrading queues a run of a submission. StartGrading marks it running. FinishGrading records how it ended
	// (the record's state, failure, output, points and results). ConfirmGrading makes it the session's confirmed grade, and takes the
	// confirmation from any other run of the session; UnconfirmGrading takes it back. InterruptGradings marks every run that is queued or
	// running as failed (the server that was running it stopped) and says how many there were.
	CreateGrading(ctx context.Context, session string, submission int64, now time.Time) (int64, error)
	StartGrading(ctx context.Context, id int64, now time.Time) error
	FinishGrading(ctx context.Context, record GradingRecord) error
	ConfirmGrading(ctx context.Context, id int64, now time.Time) error
	UnconfirmGrading(ctx context.Context, id int64) error
	InterruptGradings(ctx context.Context, now time.Time) (int, error)
	// Grading reads a run with its results and what it printed; Gradings lists a session's runs, newest first, with their results and
	// without what they printed.
	Grading(ctx context.Context, id int64) (GradingRecord, error)
	Gradings(ctx context.Context, session string) ([]GradingRecord, error)
}

// Grading states and the codes of why a grading failed (Phase 6).
const (
	GradingQueued  = "queued"
	GradingRunning = "running"
	GradingDone    = "done"
	GradingFailed  = "failed"

	FailMalformed   = "malformed"
	FailTimeout     = "timeout"
	FailCrash       = "crash"
	FailOOM         = "oom"
	FailRunner      = "runner"
	FailInterrupted = "interrupted"
)

// GradingResult is what a done run said of one TODO. Known is false for a key of the output that is not a TODO of the exam.
type GradingResult struct {
	TodoID string
	Known  bool
	Passed bool
	Points float64
	Log    string
}

// GradingRecord is a row of the grading list. Points and Max are set when the run is done: the points of the TODOs it graded, and the
// points the TODOs are worth together (nil when they have none). Results are only set when the record is read with them.
type GradingRecord struct {
	ID              int64
	SessionID       string
	SubmissionID    int64
	State           string
	Failure         string
	FailureText     string
	QueuedAt        time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	ExitCode        *int
	TimedOut        bool
	DurationMS      int64
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	Skipped         []string
	Points          *float64
	Max             *float64
	ConfirmedAt     *time.Time
	Results         []GradingResult
}

// GradeSummary is what the roster says of a student's grade: the confirmed grading if there is one, else the latest.
type GradeSummary struct {
	GradingID int64
	State     string
	Failure   string
	Points    *float64
	Max       *float64
	Confirmed bool
	At        time.Time
}

// Image build states, and the codes of why a build failed (Phase 7): the runner's (containerfile, base, build, timeout, verify), and the
// server's own (runner: it did not answer or refused; interrupted: the exam server stopped).
const (
	BuildQueued  = "queued"
	BuildRunning = "running"
	BuildOK      = "ok"
	BuildFailed  = "failed"

	BuildFailContainerfile = "containerfile"
	BuildFailBase          = "base"
	BuildFailBuild         = "build"
	BuildFailTimeout       = "timeout"
	BuildFailVerify        = "verify"
	BuildFailRunner        = "runner"
	BuildFailInterrupted   = "interrupted"
)

// ImageBuildRecord is a row of an exam's image builds. Hash, Tag-less: the runner names the image by its Hash (16 hexadecimal digits), which
// is what container.create and grading.run are given.
type ImageBuildRecord struct {
	ID         int64
	ExamID     string
	BundleHash string
	State      string
	Rebuild    bool
	Failure    string
	Message    string
	Cached     bool
	Hash       string
	ImageID    string
	BaseName   string
	BaseDigest string
	Warnings   []string
	// Log is what the build printed; it is only set when the record is read on its own.
	Log        string
	DurationMS int64
	QueuedAt   time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// ImageSummary is a workspace exam as the exam list sees it: how many TODO items it has, the image it starts from, its image bundle ("" when
// it has none), its latest build (nil when none) and whether a good build of that bundle exists.
type ImageSummary struct {
	ExamID    string
	Image     string
	ImageHash string
	Todos     int
	Latest    *ImageBuildRecord
	HasGood   bool
}
