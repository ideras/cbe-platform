package workspace

import (
	"context"
	"errors"
	"strings"
	"time"
)

// What a proctor reads about gradings, and what a proctor does with them: start them for one student or the class, read the results of
// each TODO with the logs, and confirm a grade.

// AdminGradingTodo is a TODO of the exam, as the grading table shows it.
type AdminGradingTodo struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`
	Points *float64 `json:"points,omitempty"`
}

// AdminGradingResult is what a run said of one TODO.
type AdminGradingResult struct {
	TodoID string  `json:"todo_id"`
	Known  bool    `json:"known"`
	Passed bool    `json:"passed"`
	Points float64 `json:"points"`
	Log    string  `json:"log"`
}

// AdminGrading is one run. Stdout and Stderr are only there when the run is read by itself.
type AdminGrading struct {
	ID              int64                `json:"id"`
	SubmissionID    int64                `json:"submission"`
	State           string               `json:"state"`
	Failure         string               `json:"failure,omitempty"`
	FailureText     string               `json:"failure_text,omitempty"`
	QueuedAt        time.Time            `json:"queued_at"`
	StartedAt       *time.Time           `json:"started_at,omitempty"`
	FinishedAt      *time.Time           `json:"finished_at,omitempty"`
	ExitCode        *int                 `json:"exit_code,omitempty"`
	TimedOut        bool                 `json:"timed_out,omitempty"`
	DurationMS      int64                `json:"duration_ms,omitempty"`
	Skipped         []string             `json:"skipped,omitempty"`
	Points          *float64             `json:"points,omitempty"`
	Max             *float64             `json:"max_points,omitempty"`
	Confirmed       bool                 `json:"confirmed"`
	ConfirmedAt     *time.Time           `json:"confirmed_at,omitempty"`
	Results         []AdminGradingResult `json:"results"`
	Stdout          string               `json:"stdout,omitempty"`
	Stderr          string               `json:"stderr,omitempty"`
	StdoutTruncated bool                 `json:"stdout_truncated,omitempty"`
	StderrTruncated bool                 `json:"stderr_truncated,omitempty"`
}

// AdminGradings is a student's runs, newest first, with the exam's TODOs to read them against.
type AdminGradings struct {
	SessionID string             `json:"session_id"`
	Todos     []AdminGradingTodo `json:"todos"`
	Gradings  []AdminGrading     `json:"gradings"`
}

func adminGrading(record GradingRecord, withOutput bool) AdminGrading {
	out := AdminGrading{ID: record.ID, SubmissionID: record.SubmissionID, State: record.State, Failure: record.Failure, FailureText: record.FailureText, QueuedAt: record.QueuedAt,
		StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, ExitCode: record.ExitCode, TimedOut: record.TimedOut, DurationMS: record.DurationMS, Skipped: record.Skipped,
		Points: record.Points, Max: record.Max, Confirmed: record.ConfirmedAt != nil, ConfirmedAt: record.ConfirmedAt, Results: []AdminGradingResult{}}
	for _, item := range record.Results {
		out.Results = append(out.Results, AdminGradingResult{TodoID: item.TodoID, Known: item.Known, Passed: item.Passed, Points: item.Points, Log: item.Log})
	}
	if withOutput {
		out.Stdout, out.Stderr, out.StdoutTruncated, out.StderrTruncated = record.Stdout, record.Stderr, record.StdoutTruncated, record.StderrTruncated
	}
	return out
}

func (s *Service) todosOf(ctx context.Context, examID string) ([]AdminGradingTodo, error) {
	exam, err := s.Repo.Exam(ctx, examID)
	if err != nil {
		return nil, err
	}
	out := make([]AdminGradingTodo, 0, len(exam.Document.Content.Todos))
	for _, todo := range exam.Document.Content.Todos {
		out = append(out, AdminGradingTodo{ID: todo.ID, Text: strings.TrimSpace(markdownOf(todo.TextHTML)), Points: todo.Points})
	}
	return out, nil
}

// Gradings reads a student's runs, without what they printed.
func (s *Service) Gradings(ctx context.Context, session string) (AdminGradings, error) {
	info, err := s.Repo.Session(ctx, session)
	if err != nil {
		return AdminGradings{}, err
	}
	todos, err := s.todosOf(ctx, info.ExamID)
	if err != nil {
		return AdminGradings{}, err
	}
	records, err := s.Repo.Gradings(ctx, session)
	if err != nil {
		return AdminGradings{}, err
	}
	out := AdminGradings{SessionID: session, Todos: todos, Gradings: []AdminGrading{}}
	for _, record := range records {
		out.Gradings = append(out.Gradings, adminGrading(record, false))
	}
	return out, nil
}

// Grading reads one run with what it printed.
func (s *Service) Grading(ctx context.Context, id int64) (AdminGrading, error) {
	record, err := s.Repo.Grading(ctx, id)
	if err != nil {
		return AdminGrading{}, err
	}
	return adminGrading(record, true), nil
}

func (s *Service) grader() (*Grader, error) {
	if s.Grader == nil {
		return nil, errors.New("workspace: grading is not set up on this server")
	}
	return s.Grader, nil
}

// GradeExam queues the latest submission of the exam's students, or of those named.
func (s *Service) GradeExam(ctx context.Context, examID string, sessions []string) (Started, error) {
	g, err := s.grader()
	if err != nil {
		return Started{}, err
	}
	return g.StartExam(ctx, examID, sessions)
}

// GradeSession queues one student's submission (the latest when submission is 0) and returns the run's number.
func (s *Service) GradeSession(ctx context.Context, session string, submission int64) (int64, error) {
	g, err := s.grader()
	if err != nil {
		return 0, err
	}
	return g.StartSession(ctx, session, submission)
}

// ConfirmGrading makes a finished grading the student's grade; UnconfirmGrading takes it back.
func (s *Service) ConfirmGrading(ctx context.Context, id int64) error {
	g, err := s.grader()
	if err != nil {
		return err
	}
	return g.Confirm(ctx, id)
}

func (s *Service) UnconfirmGrading(ctx context.Context, id int64) error {
	g, err := s.grader()
	if err != nil {
		return err
	}
	return g.Unconfirm(ctx, id)
}
