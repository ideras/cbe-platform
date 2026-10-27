package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"exam-platform/internal/clock"
	"exam-platform/internal/domain"
	"exam-platform/shared/calc"
)

var (
	// ErrSelectionChanged means the published exam is no longer the one the caller based its decision on.
	ErrSelectionChanged = errors.New("current exam changed; refresh and retry")
)

// ActiveSessionsError refuses to replace or close an exam while students can still be working in it.
type ActiveSessionsError struct{ Count int }

func (e *ActiveSessionsError) Error() string {
	return fmt.Sprintf("%d students are still working in the current exam", e.Count)
}

// MaxListedExams bounds the exam catalog; the newest exams come first.
const MaxListedExams = 500

type PublishRepository interface {
	WithPublishTransaction(context.Context, func(PublishTransaction) error) error
}

type PublishTransaction interface {
	Exam(context.Context, string) (domain.Exam, error)
	Class(context.Context, string) (domain.Class, error)
	CurrentExam(context.Context) (examID, classID string, err error)
	SetCurrentExam(ctx context.Context, examID string, now time.Time) error
	ClearCurrentExam(context.Context) error
	ListExams(context.Context, time.Time, int) ([]domain.ExamSummary, error)
	ActiveSessions(context.Context, string, time.Time) (int, error)
	// Settings reads the monitoring settings of the server; the zero policy means none are stored.
	Settings(context.Context) (domain.Policy, time.Time, error)
	// SetExamPolicy freezes the monitoring settings in an exam, at its publish.
	SetExamPolicy(ctx context.Context, examID string, policy domain.Policy) error
}

// Publisher chooses which stored exam new students are admitted to. Only the students of the exam's class can log in.
type Publisher struct {
	repository PublishRepository
	clock      clock.Clock
}

func NewPublisher(repository PublishRepository, clock clock.Clock) *Publisher {
	return &Publisher{repository: repository, clock: clock}
}

type ExamEntry struct {
	ID               string            `json:"exam_id"`
	ClassID          string            `json:"class_id"`
	ClassName        string            `json:"class_name"`
	Title            string            `json:"title"`
	ExerciseCount    int               `json:"exercise_count"`
	Navigation       domain.Navigation `json:"navigation"`
	DurationSeconds  int64             `json:"duration_seconds"`
	GraceSeconds     int64             `json:"grace_seconds"`
	MinVolumePercent int               `json:"min_volume_percent"`
	CreatedAt        time.Time         `json:"created_at"`
	OpensAt          *time.Time        `json:"opens_at"`
	ClosesAt         *time.Time        `json:"closes_at"`
	Published        bool              `json:"published"`
	ActiveSessions   int               `json:"active_sessions"`
	TotalSessions    int               `json:"total_sessions"`
	// LockedSessions are the running sessions a proctor has to unlock.
	LockedSessions int `json:"locked_sessions"`
	// Tools is the exam's effective tools configuration; omitted when the exam has no calculator.
	Tools *calc.Tools `json:"tools,omitempty"`
}

type ExamCatalog struct {
	ServerTime       time.Time   `json:"server_time"`
	CurrentExamID    string      `json:"current_exam_id"`
	CurrentClassID   string      `json:"current_class_id"`
	CurrentClassName string      `json:"current_class_name"`
	Exams            []ExamEntry `json:"exams"`
}

func (p *Publisher) Catalog(ctx context.Context) (ExamCatalog, error) {
	var catalog ExamCatalog
	err := p.repository.WithPublishTransaction(ctx, func(tx PublishTransaction) error {
		now := p.clock.Now().UTC()
		current, currentClass, err := tx.CurrentExam(ctx)
		if err != nil {
			return err
		}
		var className string
		if currentClass != "" {
			class, err := tx.Class(ctx, currentClass)
			if err != nil {
				return err
			}
			className = class.Name
		}
		summaries, err := tx.ListExams(ctx, now, MaxListedExams)
		if err != nil {
			return err
		}
		catalog = ExamCatalog{ServerTime: now, CurrentExamID: current, CurrentClassID: currentClass, CurrentClassName: className, Exams: make([]ExamEntry, 0, len(summaries))}
		for _, e := range summaries {
			entry := ExamEntry{ID: e.ID, ClassID: e.ClassID, ClassName: e.ClassName, Title: e.Title, ExerciseCount: e.ExerciseCount, Navigation: e.Navigation, DurationSeconds: e.DurationSeconds,
				GraceSeconds: e.GraceSeconds, MinVolumePercent: e.MinVolumePercent, CreatedAt: e.CreatedAt, OpensAt: e.OpensAt, ClosesAt: e.ClosesAt,
				Published: e.ID == current, ActiveSessions: e.ActiveSessions, TotalSessions: e.TotalSessions, LockedSessions: e.LockedSessions}
			if e.Tools != nil && e.Tools.On() {
				effective := e.Tools.Effective()
				entry.Tools = &effective
			}
			catalog.Exams = append(catalog.Exams, entry)
		}
		return nil
	})
	return catalog, err
}

type PublishResult struct {
	CurrentExamID   string `json:"current_exam_id"`
	CurrentClassID  string `json:"current_class_id"`
	ReplacedExamID  string `json:"replaced_exam_id"`
	ReplacedClassID string `json:"replaced_class_id"`
	// ActiveSessions were still working in the replaced exam; non-zero only when the caller allowed it.
	ActiveSessions int `json:"active_sessions"`
}

func validExpected(id string) bool { return id == "" || validProctorID(id) }

// Publish admits the students of the exam's class to examID. expected is the exam the caller saw published, or "" for none:
// if it has changed since, nothing happens. Replacing an exam whose students are still working needs allowActive.
func (p *Publisher) Publish(ctx context.Context, examID, expected string, allowActive bool) (PublishResult, error) {
	if !validProctorID(examID) || !validExpected(expected) {
		return PublishResult{}, ErrInvalidInput
	}
	return p.change(ctx, examID, expected, allowActive)
}

// Unpublish closes admission. Students already in the exam keep their sessions.
func (p *Publisher) Unpublish(ctx context.Context, expected string, allowActive bool) (PublishResult, error) {
	if !validExpected(expected) {
		return PublishResult{}, ErrInvalidInput
	}
	return p.change(ctx, "", expected, allowActive)
}

func (p *Publisher) change(ctx context.Context, target, expected string, allowActive bool) (PublishResult, error) {
	var result PublishResult
	err := p.repository.WithPublishTransaction(ctx, func(tx PublishTransaction) error {
		now := p.clock.Now().UTC()
		var targetClass string
		if target != "" {
			exam, err := tx.Exam(ctx, target)
			if err != nil {
				return err
			}
			targetClass = exam.ClassID
		}
		stored, storedClass, err := tx.CurrentExam(ctx)
		if err != nil {
			return err
		}
		if stored != expected {
			return ErrSelectionChanged
		}
		result = PublishResult{CurrentExamID: target, CurrentClassID: targetClass}
		if stored == target {
			if target != "" {
				// Publishing an exam again freezes the current settings in it again: students who log in afterwards follow
				// them, and the students already in it keep their own copy.
				return p.freeze(ctx, tx, target)
			}
			return nil
		}
		if stored != "" {
			active, err := tx.ActiveSessions(ctx, stored, now)
			if err != nil {
				return err
			}
			if active > 0 && !allowActive {
				return &ActiveSessionsError{Count: active}
			}
			result.ReplacedExamID, result.ReplacedClassID, result.ActiveSessions = stored, storedClass, active
		}
		if target == "" {
			return tx.ClearCurrentExam(ctx)
		}
		if err := p.freeze(ctx, tx, target); err != nil {
			return err
		}
		return tx.SetCurrentExam(ctx, target, now)
	})
	if err != nil {
		return PublishResult{}, err
	}
	return result, nil
}

// freeze stores the monitoring settings of the server in the exam, so every student of it follows the same rules.
func (p *Publisher) freeze(ctx context.Context, tx PublishTransaction, examID string) error {
	policy, _, err := currentSettings(ctx, tx)
	if err != nil {
		return err
	}
	return tx.SetExamPolicy(ctx, examID, policy)
}
