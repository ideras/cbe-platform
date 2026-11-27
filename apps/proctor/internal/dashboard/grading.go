package dashboard

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"
)

// Grading (workspace plan, Phase 6): the proctor starts the hidden tests of a workspace exam for one student or the class, reads what each
// TODO scored and what the run printed, and confirms a grade. A grade is advisory until it is confirmed; the server says which.

// WorkspaceGrade is a student's grade in the roster: the confirmed run, or the latest.
type WorkspaceGrade struct {
	GradingID int64     `json:"grading"`
	State     string    `json:"state"`
	Failure   string    `json:"failure"`
	Points    *float64  `json:"points"`
	Max       *float64  `json:"max_points"`
	Confirmed bool      `json:"confirmed"`
	At        time.Time `json:"at"`
}

func finite(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0)
}

func validState(state string) bool {
	return state == "queued" || state == "running" || state == "done" || state == "failed"
}

func validGrade(grade *WorkspaceGrade) bool {
	return grade == nil || (grade.GradingID > 0 && validState(grade.State) && finite(grade.Points) && finite(grade.Max) && (grade.State == "done" || !grade.Confirmed) && len(grade.Failure) <= 32)
}

// GradingTodo is a TODO of the exam, to read a run's results against. Text is the TODO as the exam wrote it, in Markdown, and is shown as text.
type GradingTodo struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`
	Points *float64 `json:"points"`
}

// GradingResult is what a run said of one TODO. Known is false for a key of the grade program's output that is not a TODO of the exam.
type GradingResult struct {
	TodoID string  `json:"todo_id"`
	Known  bool    `json:"known"`
	Passed bool    `json:"passed"`
	Points float64 `json:"points"`
	Log    string  `json:"log"`
}

// Grading is one run. Stdout and Stderr are only there when the run is read by itself (WorkspaceGrading).
type Grading struct {
	ID              int64           `json:"id"`
	SubmissionID    int64           `json:"submission"`
	State           string          `json:"state"`
	Failure         string          `json:"failure"`
	FailureText     string          `json:"failure_text"`
	QueuedAt        time.Time       `json:"queued_at"`
	StartedAt       *time.Time      `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
	ExitCode        *int            `json:"exit_code"`
	TimedOut        bool            `json:"timed_out"`
	DurationMS      int64           `json:"duration_ms"`
	Skipped         []string        `json:"skipped"`
	Points          *float64        `json:"points"`
	Max             *float64        `json:"max_points"`
	Confirmed       bool            `json:"confirmed"`
	ConfirmedAt     *time.Time      `json:"confirmed_at"`
	Results         []GradingResult `json:"results"`
	Stdout          string          `json:"stdout"`
	Stderr          string          `json:"stderr"`
	StdoutTruncated bool            `json:"stdout_truncated"`
	StderrTruncated bool            `json:"stderr_truncated"`
}

// WorkspaceGradings is a student's runs, newest first, with the exam's TODOs.
type WorkspaceGradings struct {
	SessionID string        `json:"session_id"`
	CanvasID  string        `json:"canvas_id,omitempty"`
	Todos     []GradingTodo `json:"todos"`
	Gradings  []Grading     `json:"gradings"`
}

func validGrading(g Grading) bool {
	if g.ID <= 0 || g.SubmissionID <= 0 || !validState(g.State) || !finite(g.Points) || !finite(g.Max) || g.DurationMS < 0 || len(g.Failure) > 32 || len(g.FailureText) > 4096 ||
		len(g.Stdout) > 2<<20 || len(g.Stderr) > 2<<20 || !utf8.ValidString(g.FailureText) || !utf8.ValidString(g.Stdout) || !utf8.ValidString(g.Stderr) || len(g.Skipped) > 100 || len(g.Results) > 1000 {
		return false
	}
	for _, r := range g.Results {
		if r.TodoID == "" || len(r.TodoID) > 256 || !utf8.ValidString(r.TodoID) || r.Points < 0 || math.IsNaN(r.Points) || math.IsInf(r.Points, 0) || len(r.Log) > 64<<10 || !utf8.ValidString(r.Log) {
			return false
		}
	}
	return !g.Confirmed || g.State == "done"
}

// WorkspaceGradings reads a student's runs without what they printed.
func (s *Service) WorkspaceGradings(sessionID string) (WorkspaceGradings, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return WorkspaceGradings{}, err
	}
	if !validID(sessionID) {
		return WorkspaceGradings{}, errors.New("Invalid session selection.")
	}
	var out WorkspaceGradings
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/session/"+sessionID+"/gradings", nil, &out); err != nil {
		return WorkspaceGradings{}, err
	}
	if err := s.stillLinked(link); err != nil {
		return WorkspaceGradings{}, err
	}
	if out.SessionID != sessionID || out.Todos == nil || out.Gradings == nil {
		return WorkspaceGradings{}, errors.New("Invalid grading response.")
	}
	for _, todo := range out.Todos {
		if todo.ID == "" || len(todo.Text) > 64<<10 || !utf8.ValidString(todo.Text) || !finite(todo.Points) {
			return WorkspaceGradings{}, errors.New("Invalid grading response.")
		}
	}
	for _, g := range out.Gradings {
		if !validGrading(g) {
			return WorkspaceGradings{}, errors.New("Invalid grading response.")
		}
	}
	return out, nil
}

// WorkspaceGrading reads one run with what it printed (standard output and standard error), which is the student's program's and the
// teacher's grade program's text: it is shown as text, never as markup.
func (s *Service) WorkspaceGrading(id int64) (Grading, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return Grading{}, err
	}
	if id <= 0 {
		return Grading{}, errors.New("Invalid grading selection.")
	}
	var out Grading
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/grading/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		return Grading{}, err
	}
	if err := s.stillLinked(link); err != nil {
		return Grading{}, err
	}
	if out.ID != id || !validGrading(out) {
		return Grading{}, errors.New("Invalid grading response.")
	}
	return out, nil
}

// GradeSkip says why a student was left out of a request to grade the class.
type GradeSkip struct {
	SessionID string `json:"session_id"`
	Account   string `json:"account_number"`
	Reason    string `json:"reason"`
}

// GradeStarted is what asking for gradings did: the runs queued, and who was left out and why.
type GradeStarted struct {
	Queued  []int64     `json:"queued"`
	Skipped []GradeSkip `json:"skipped"`
}

// GradeExam queues the latest submission of every student of the exam being monitored, or of those named. The runs go on in the
// background; the roster shows each student's grade as it comes.
func (s *Service) GradeExam(sessionIDs []string) (GradeStarted, error) {
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return GradeStarted{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	examID, workspace := s.view.ExamID, s.view.Workspace != nil
	s.mu.Unlock()
	if examID == "" || !workspace {
		return GradeStarted{}, errors.New("Select a workspace exam first.")
	}
	if len(sessionIDs) > 5000 {
		return GradeStarted{}, errors.New("Too many students selected.")
	}
	for _, id := range sessionIDs {
		if !validID(id) {
			return GradeStarted{}, errors.New("Invalid session selection.")
		}
	}
	body, err := json.Marshal(map[string][]string{"sessions": sessionIDs})
	if err != nil {
		return GradeStarted{}, errors.New("Cannot encode request.")
	}
	var out GradeStarted
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, "/admin/workspace/exam/"+url.PathEscape(examID)+"/grade", "application/json", body, &out); err != nil {
		return GradeStarted{}, err
	}
	if out.Queued == nil || out.Skipped == nil {
		return GradeStarted{}, errors.New("The server's answer was invalid. Look at the roster to see which students are being graded.")
	}
	for _, skip := range out.Skipped {
		if len(skip.Reason) > 256 || !utf8.ValidString(skip.Reason) || len(skip.Account) > 64 {
			return GradeStarted{}, errors.New("The server's answer was invalid. Look at the roster to see which students are being graded.")
		}
	}
	return out, nil
}

// GradeStudent queues one student's submission, the one named (submissionID) or the latest (0), and returns the run's number.
func (s *Service) GradeStudent(sessionID string, submissionID int64) (int64, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return 0, err
	}
	if !validID(sessionID) || submissionID < 0 {
		return 0, errors.New("Invalid session selection.")
	}
	body, err := json.Marshal(map[string]int64{"submission": submissionID})
	if err != nil {
		return 0, errors.New("Cannot encode request.")
	}
	var out struct {
		Grading int64 `json:"grading"`
	}
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, "/admin/workspace/session/"+sessionID+"/grade", "application/json", body, &out); err != nil {
		return 0, err
	}
	if err := s.stillLinked(link); err != nil {
		return 0, err
	}
	if out.Grading <= 0 {
		return 0, errors.New("The server's answer was invalid. Look at the student's gradings.")
	}
	return out.Grading, nil
}

// ConfirmGrading makes a finished run the student's grade (confirm true), or takes the confirmation back. Until confirmed, a grade is
// advisory; confirming one run of a student takes the confirmation from another run of theirs.
func (s *Service) ConfirmGrading(id int64, confirm bool) error {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return err
	}
	if id <= 0 {
		return errors.New("Invalid grading selection.")
	}
	path := "/admin/workspace/grading/" + strconv.FormatInt(id, 10) + "/confirm"
	if !confirm {
		path = "/admin/workspace/grading/" + strconv.FormatInt(id, 10) + "/unconfirm"
	}
	var out struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, path, "application/json", nil, &out); err != nil {
		return err
	}
	if out.Confirmed != confirm {
		return errors.New("The server's answer was not what was asked. Look at the student's gradings.")
	}
	return s.stillLinked(link)
}
