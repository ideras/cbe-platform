package workspace

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Service is what the HTTP layer uses: the importer and the manager together.
type Service struct {
	Manager  *Manager
	Importer *Importer
	Repo     Repository
}

// OnLogin is called when a student has logged in.
func (s *Service) OnLogin(session string) { s.Manager.OnLogin(session) }

// Serve is the student's WebSocket.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, session string) {
	s.Manager.Serve(w, r, session)
}

// Import stores a package as a new exam of the class.
func (s *Service) Import(ctx context.Context, classID string, document, starter, grading []byte) (string, error) {
	return s.Importer.Import(ctx, classID, document, starter, grading)
}

// StudentView is what a student's application reads before it opens the workspace: the instructions, the TODO list with what is
// ticked, and what the workspace allows. It holds nothing the student must not have (no image, no limits, no commands, no grading).
type StudentView struct {
	Kind      string          `json:"kind"`
	ExamID    string          `json:"exam_id"`
	Title     string          `json:"title"`
	Terminal  bool            `json:"terminal"`
	CanRun    bool            `json:"can_run"`
	CanCheck  bool            `json:"can_check"`
	Content   Content         `json:"content"`
	Done      map[string]bool `json:"done"`
	Status    ClientStatus    `json:"status"`
	SessionID string          `json:"session_id"`
}

// Student describes a session's workspace to its student.
func (s *Service) Student(ctx context.Context, session string) (StudentView, error) {
	info, err := s.Repo.Session(ctx, session)
	if err != nil {
		return StudentView{}, err
	}
	if info.LockedAt != nil {
		return StudentView{}, ErrLocked
	}
	exam, err := s.Repo.Exam(ctx, info.ExamID)
	if err != nil {
		return StudentView{}, err
	}
	done := map[string]bool{}
	if info.State != StateNew {
		if done, err = s.Repo.Todo(ctx, session); err != nil {
			return StudentView{}, err
		}
	}
	status, err := s.Manager.Status(ctx, session)
	if err != nil {
		return StudentView{}, err
	}
	w := exam.Document.Workspace
	return StudentView{Kind: "workspace", ExamID: exam.ExamID, Title: exam.Title, Terminal: w.Terminal, CanRun: w.Run != "", CanCheck: w.Check != "",
		Content: exam.Document.Content, Done: done, Status: status, SessionID: session}, nil
}

// AdminSession is what a proctor reads about one student's workspace.
type AdminSession struct {
	SessionID      string          `json:"session_id"`
	ExamID         string          `json:"exam_id"`
	State          string          `json:"state"`
	AgentRestarts  int             `json:"agent_restarts"`
	LastSnapshotAt *time.Time      `json:"last_snapshot_at,omitempty"`
	Failure        string          `json:"failure,omitempty"`
	Snapshots      []AdminSnap     `json:"snapshots"`
	Done           map[string]bool `json:"done"`
}

// AdminSnap is a row of a student's snapshot list.
type AdminSnap struct {
	ID     int64     `json:"id"`
	Reason string    `json:"reason"`
	At     time.Time `json:"taken_at"`
	Files  int       `json:"files"`
	Bytes  int64     `json:"bytes"`
	Hash   string    `json:"manifest"`
}

// Admin describes one session's workspace to a proctor.
func (s *Service) Admin(ctx context.Context, session string) (AdminSession, error) {
	info, err := s.Repo.Session(ctx, session)
	if err != nil {
		return AdminSession{}, err
	}
	snapshots, err := s.Repo.Snapshots(ctx, session)
	if err != nil {
		return AdminSession{}, err
	}
	out := AdminSession{SessionID: session, ExamID: info.ExamID, State: info.State, AgentRestarts: info.AgentRestarts, LastSnapshotAt: info.LastSnapshotAt, Failure: info.Failure, Snapshots: []AdminSnap{}, Done: map[string]bool{}}
	for _, snap := range snapshots {
		out.Snapshots = append(out.Snapshots, AdminSnap{ID: snap.ID, Reason: snap.Reason, At: snap.TakenAt, Files: snap.Files, Bytes: snap.Bytes, Hash: snap.ManifestHash})
	}
	if info.State != StateNew {
		if out.Done, err = s.Repo.Todo(ctx, session); err != nil {
			return AdminSession{}, err
		}
	}
	return out, nil
}

// SnapshotTar writes a snapshot as a tar archive.
func (s *Service) SnapshotTar(ctx context.Context, id int64, w io.Writer) error {
	return s.Manager.Snapshots.WriteTar(ctx, id, w)
}
