package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/shared/calc"
)

type SaveResult struct {
	Sequence int64
	SavedAt  time.Time
}
type ExamView struct {
	ID         string
	Title      string
	Navigation domain.Navigation
	Content    domain.ExamContent
	// CurrentExercise is the exercise a forward-only student is on; it is 0 under free navigation.
	CurrentExercise int
	Answers         domain.Answers
	Latest          *SaveResult
	// Tools is the exam's tools configuration with every default applied (calc.Effective); nil when the exam has
	// no calculator. This is the only config the student application ever sees.
	Tools *calc.Tools
}

// AdvanceResult is where a forward-only student is after leaving an exercise, and the version that kept their answers.
type AdvanceResult struct {
	CurrentExercise int
	Sequence        int64
	SavedAt         time.Time
}

// ErrExerciseChanged means the student is not on the exercise they asked to leave, as when another window moved on.
var ErrExerciseChanged = errors.New("the student is no longer on that exercise")

type SubmitResult struct {
	Status         domain.SessionStatus
	SubmittedAt    time.Time
	FinalVersionID int64
	Sequence       int64
}

func terminal(session domain.Session) bool {
	return session.Status == domain.Submitted || session.Status == domain.ForceSubmitted
}

// withStudent runs fn for the session the token names. permitFinal allows the operation on an already submitted session
// and permitLocked allows it while the session is locked; both are refused with their own error otherwise.
func (s *Sessions) withStudent(ctx context.Context, token string, permitFinal, permitLocked bool, fn func(SessionTransaction, Principal) error) error {
	if token == "" {
		return ErrInvalidToken
	}
	var decisionErr error
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		session, err := tx.SessionByToken(ctx, token)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		principal, _, err := s.reconcileSession(ctx, tx, session, now)
		if err != nil {
			return err
		}
		if terminal(principal.Session) && !permitFinal {
			// Reject the operation without rolling back an overdue finalization.
			decisionErr = ErrSessionLocked
			return nil
		}
		if principal.Session.LockedAt != nil && !permitLocked {
			// The exam is locked: the student may not see or answer it until a proctor unlocks it.
			decisionErr = ErrSessionOnHold
			return nil
		}
		return fn(tx, principal)
	})
	if err != nil {
		return err
	}
	return decisionErr
}

func (s *Sessions) GetExam(ctx context.Context, token string) (ExamView, error) {
	var result ExamView
	err := s.withStudent(ctx, token, false, false, func(tx SessionTransaction, principal Principal) error {
		exam, err := tx.Exam(ctx, principal.Session.ExamID)
		if err != nil {
			return err
		}
		result = ExamView{ID: exam.ID, Title: exam.Title, Navigation: exam.Navigation, Content: exam.Content, CurrentExercise: principal.Session.CurrentExercise}
		if exam.Tools != nil && exam.Tools.On() {
			effective := exam.Tools.Effective()
			result.Tools = &effective
		}
		latest, err := latestVersion(ctx, tx, principal.Session.ID)
		if err != nil {
			return err
		}
		result.Answers = startingAnswers(exam.Content)
		if latest != nil {
			result.Latest = &SaveResult{Sequence: latest.Sequence, SavedAt: latest.SavedAt}
			for id := range result.Answers {
				result.Answers[id] = latest.Answers[id]
			}
		}
		return nil
	})
	if err != nil {
		return ExamView{}, err
	}
	return result, nil
}

func (s *Sessions) Autosave(ctx context.Context, token string, answers domain.Answers) (SaveResult, error) {
	var result SaveResult
	err := s.withStudent(ctx, token, false, false, func(tx SessionTransaction, principal Principal) error {
		exam, err := tx.Exam(ctx, principal.Session.ExamID)
		if err != nil {
			return err
		}
		answers, err := s.acceptAnswers(ctx, tx, exam, principal.Session, answers)
		if err != nil {
			return err
		}
		version, err := tx.AppendVersion(ctx, principal.Session.ID, answers, principal.ServerTime)
		if err != nil {
			return err
		}
		result = SaveResult{Sequence: version.Sequence, SavedAt: version.SavedAt}
		return nil
	})
	if err != nil {
		return SaveResult{}, err
	}
	return result, nil
}

func (s *Sessions) Submit(ctx context.Context, token string, answers domain.Answers) (SubmitResult, error) {
	var result SubmitResult
	err := s.withStudent(ctx, token, true, false, func(tx SessionTransaction, principal Principal) error {
		session := principal.Session
		if !terminal(session) {
			exam, err := tx.Exam(ctx, session.ExamID)
			if err != nil {
				return err
			}
			answers, err := s.acceptAnswers(ctx, tx, exam, session, answers)
			if err != nil {
				return err
			}
			version, err := tx.AppendVersion(ctx, session.ID, answers, principal.ServerTime)
			if err != nil {
				return err
			}
			session, err = s.finalize(ctx, tx, session, principal.ServerTime, &version, finalization{Status: domain.Submitted, Reason: "explicit_submit"})
			if err != nil {
				return err
			}
		}
		var err error
		result, err = submissionResult(ctx, tx, session)
		return err
	})
	if err != nil {
		return SubmitResult{}, err
	}
	return result, nil
}

// Advance saves a forward-only student's answers and moves them from exercise from to the next one, which closes from to
// them for good. Repeating a request that already moved them on changes nothing, so an unclear failure can be retried.
func (s *Sessions) Advance(ctx context.Context, token string, from int, answers domain.Answers) (AdvanceResult, error) {
	var result AdvanceResult
	err := s.withStudent(ctx, token, false, false, func(tx SessionTransaction, principal Principal) error {
		session := principal.Session
		exam, err := tx.Exam(ctx, session.ExamID)
		if err != nil {
			return err
		}
		if exam.Navigation != domain.ForwardOnly || from < 0 || from >= len(exam.Content.Exercises)-1 {
			return ErrInvalidInput
		}
		if session.CurrentExercise == from+1 {
			latest, err := latestVersion(ctx, tx, session.ID)
			if err != nil {
				return err
			}
			if latest != nil {
				result = AdvanceResult{CurrentExercise: from + 1, Sequence: latest.Sequence, SavedAt: latest.SavedAt}
				return nil
			}
		}
		if session.CurrentExercise != from {
			return ErrExerciseChanged
		}
		answers, err := s.acceptAnswers(ctx, tx, exam, session, answers)
		if err != nil {
			return err
		}
		version, err := tx.AppendVersion(ctx, session.ID, answers, principal.ServerTime)
		if err != nil {
			return err
		}
		moved, err := tx.AdvanceExercise(ctx, session.ID, from)
		if err != nil {
			return err
		}
		if !moved {
			return fmt.Errorf("advancing did not change exactly one session")
		}
		result = AdvanceResult{CurrentExercise: from + 1, Sequence: version.Sequence, SavedAt: version.SavedAt}
		return nil
	})
	if err != nil {
		return AdvanceResult{}, err
	}
	return result, nil
}

// acceptAnswers validates a student's answers and, under forward-only navigation, keeps every exercise but the current one
// as last saved.
func (s *Sessions) acceptAnswers(ctx context.Context, tx SessionTransaction, exam domain.Exam, session domain.Session, answers domain.Answers) (domain.Answers, error) {
	if err := s.validateAnswers(exam, answers); err != nil {
		return nil, err
	}
	if exam.Navigation != domain.ForwardOnly {
		return answers, nil
	}
	latest, err := latestVersion(ctx, tx, session.ID)
	if err != nil {
		return nil, err
	}
	return lockAnswers(exam, session.CurrentExercise, answers, latest), nil
}

// latestVersion is the session's newest saved version, or nil before the first save.
func latestVersion(ctx context.Context, tx SessionTransaction, sessionID string) (*domain.Version, error) {
	latest, err := tx.LatestVersion(ctx, sessionID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &latest, nil
}

// ForceSubmit is a proctor operation; terminal results are immutable and idempotent.
func (s *Sessions) ForceSubmit(ctx context.Context, sessionID, operatorReason string) (SubmitResult, error) {
	operatorReason = strings.TrimSpace(operatorReason)
	if !validProctorID(sessionID) || !validText(operatorReason, 1024) {
		return SubmitResult{}, ErrInvalidInput
	}
	var result SubmitResult
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		session, err := tx.SessionByID(ctx, sessionID)
		if err != nil {
			return err
		}
		principal, _, err := s.reconcileSession(ctx, tx, session, now)
		if err != nil {
			return err
		}
		session, err = s.finalize(ctx, tx, principal.Session, now, nil, finalization{Status: domain.ForceSubmitted, Reason: "force_submit", OperatorReason: operatorReason})
		if err != nil {
			return err
		}
		result, err = submissionResult(ctx, tx, session)
		return err
	})
	if err != nil {
		return SubmitResult{}, err
	}
	return result, nil
}

func submissionResult(ctx context.Context, tx SessionTransaction, session domain.Session) (SubmitResult, error) {
	if !terminal(session) || session.FinalVersionID == nil || session.SubmittedAt == nil {
		return SubmitResult{}, fmt.Errorf("invalid terminal session")
	}
	version, err := tx.Version(ctx, session.ID, *session.FinalVersionID)
	if err != nil {
		return SubmitResult{}, err
	}
	if err := ValidateFinalVersion(session.ID, version); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{Status: session.Status, SubmittedAt: *session.SubmittedAt, FinalVersionID: version.ID, Sequence: version.Sequence}, nil
}
