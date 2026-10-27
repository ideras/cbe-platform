package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"exam-platform/internal/clock"
	"exam-platform/internal/domain"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid session token")
	ErrAdmissionClosed    = errors.New("exam admission is closed")
	ErrAlreadySubmitted   = errors.New("session already submitted")
	ErrNoCurrentExam      = errors.New("no current exam is available")
)

type SessionTransaction interface {
	Exam(context.Context, string) (domain.Exam, error)
	// CurrentExam is the exam the proctor published and the class admitted to it, or "" for both when admission is closed.
	CurrentExam(context.Context) (string, string, error)
	// StudentInClass says whether the account belongs to the class.
	StudentInClass(ctx context.Context, classID, account string) (bool, error)
	SessionByAccount(context.Context, string, string) (domain.Session, error)
	SessionByToken(context.Context, string) (domain.Session, error)
	InsertSession(context.Context, domain.Session) error
	EnterGrace(context.Context, string) (bool, error)
	// AdvanceExercise moves an unfinished session from the given exercise to the next; false means it was elsewhere.
	AdvanceExercise(context.Context, string, int) (bool, error)
	AddEvent(context.Context, domain.Event) error
	UnfinishedSessions(context.Context) ([]domain.Session, error)
	SessionByID(context.Context, string) (domain.Session, error)
	AppendVersion(context.Context, string, domain.Answers, time.Time) (domain.Version, error)
	LatestVersion(context.Context, string) (domain.Version, error)
	Version(context.Context, string, int64) (domain.Version, error)
	VersionMetadata(context.Context, string, int64) (domain.Version, error)
	Finalize(context.Context, string, int64, domain.SessionStatus, time.Time) error
	RecordEvent(context.Context, domain.Event) (int64, error)
	LatestFocusEvent(context.Context, string) (domain.Event, error)
	EverOverdue(context.Context, string) (bool, error)
	SetNetworkGap(context.Context, string, time.Time) error
	TouchHeartbeat(context.Context, string, string, string, domain.Sound, time.Time) error
	StudentsWithoutSession(context.Context, string) ([]domain.Student, error)
	ProctorSessions(context.Context, string) ([]domain.ProctorRecord, error)
	ProctorSession(context.Context, string) (domain.ProctorRecord, error)
	LatestReportedFocus(context.Context, string) (domain.Event, error)
	FocusInterrupted(context.Context, string, int64) (bool, error)
	FinalizationEvent(context.Context, string) (domain.Event, error)
	HistoryWatermark(context.Context, string) (domain.TimelineWatermark, error)
	// HistoryPage returns bounded candidates; version answers are hydrated on demand.
	HistoryPage(context.Context, string, domain.TimelinePosition, domain.TimelineWatermark, int) ([]domain.HistoryRecord, error)
	// Settings reads the monitoring settings of the server; the zero policy means none are stored.
	Settings(context.Context) (domain.Policy, time.Time, error)
	// SaveSettings stores the monitoring settings of the server.
	SaveSettings(context.Context, domain.Policy, time.Time) error
	// AverageScreenshotBytes is the mean size of the stored screenshots; nil when there are none.
	AverageScreenshotBytes(context.Context) (*int64, error)
	// LockSession locks a running session; false means it was locked already or is over.
	LockSession(context.Context, string, time.Time, string) (bool, error)
	// UnlockSession lifts the lock and moves the deadline and grace; false means it was not locked or is over.
	UnlockSession(context.Context, string, time.Time, time.Time, time.Time) (bool, error)
	// LatestLockEvent is the record of the session's latest lock.
	LatestLockEvent(context.Context, string) (domain.Event, error)
	// FocusEventAtLock is the focus state the session was in when it was last locked.
	FocusEventAtLock(context.Context, string) (domain.Event, error)
	// LockHistories reads the locks and unlocks of every session of an exam, keyed by session.
	LockHistories(context.Context, string) (map[string][]domain.LockRecord, error)
}

type SessionRepository interface {
	WithSessionTransaction(context.Context, func(SessionTransaction) error) error
}

// Sessions runs the students' sessions. The monitoring settings are read from the store: a session follows the copy it took
// at login, and the proctor's Settings apply to the exams published afterwards.
type Sessions struct {
	repository SessionRepository
	clock      clock.Clock
	limits     AnswerLimits
}

func NewSessions(repository SessionRepository, clock clock.Clock) *Sessions {
	return &Sessions{repository: repository, clock: clock, limits: DefaultAnswerLimits()}
}

func NewSessionsWithLimits(repository SessionRepository, clock clock.Clock, limits AnswerLimits) (*Sessions, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	s := NewSessions(repository, clock)
	s.limits = limits
	return s, nil
}

type LoginResult struct {
	Policy     ClientPolicy
	Token      string
	SessionID  string
	DeadlineAt time.Time
	ServerTime time.Time
	// MinVolumePercent is the exam's required minimum system volume (0 = leave the student's volume alone).
	MinVolumePercent int
	// Locked says that the session is locked, since when and why; nil while it is not.
	Locked *LockInfo
}

// LockInfo says that a session is locked, since when and why.
type LockInfo struct {
	Since  time.Time `json:"since"`
	Reason string    `json:"reason"`
}

// lockInfo is the lock of a session, or nil when it is not locked.
func lockInfo(session domain.Session) *LockInfo {
	if session.LockedAt == nil {
		return nil
	}
	return &LockInfo{Since: *session.LockedAt, Reason: session.LockReason}
}

type Principal struct {
	Policy     ClientPolicy
	Session    domain.Session
	Lifecycle  domain.Lifecycle
	ServerTime time.Time
	// Locked says that the exam is locked, since when and why; nil while it is not.
	Locked *LockInfo
}

func (s *Sessions) Login(ctx context.Context, accountNumber, pin string) (LoginResult, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	var result LoginResult
	var decisionErr error
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		examID, classID, err := tx.CurrentExam(ctx)
		if err != nil {
			return err
		}
		if examID == "" {
			return ErrNoCurrentExam
		}
		exam, err := tx.Exam(ctx, examID)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrNoCurrentExam
		}
		if err != nil {
			return err
		}
		active, err := tx.StudentInClass(ctx, classID, accountNumber)
		if err != nil {
			return err
		}
		expected, provided := sha256.Sum256([]byte(exam.PIN)), sha256.Sum256([]byte(pin))
		if subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 || !active {
			return ErrInvalidCredentials
		}
		session, err := tx.SessionByAccount(ctx, accountNumber, exam.ID)
		resumed := err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if !resumed {
			if (exam.OpensAt != nil && now.Before(*exam.OpensAt)) || (exam.ClosesAt != nil && !now.Before(*exam.ClosesAt)) {
				return ErrAdmissionClosed
			}
			duration, grace, err := examTiming(exam)
			if err != nil {
				return err
			}
			id, token, err := sessionCredentials()
			if err != nil {
				return err
			}
			// The session copies the settings the exam was published with: everything about it follows that copy.
			session = domain.Session{ID: id, AccountNumber: accountNumber, ExamID: exam.ID, Token: token,
				StartedAt: now, DeadlineAt: now.Add(duration), GraceUntil: now.Add(duration).Add(grace), Status: domain.Active, Policy: exam.Policy}
			if err := tx.InsertSession(ctx, session); err != nil {
				return err
			}
		}
		principal, _, err := s.reconcileSession(ctx, tx, session, now)
		if err != nil {
			return err
		}
		if principal.Lifecycle.Status == domain.Submitted || principal.Lifecycle.Status == domain.ForceSubmitted {
			// Commit any overdue grace transition even though this login is rejected.
			decisionErr = ErrAlreadySubmitted
			return nil
		}
		detail, err := json.Marshal(struct {
			Resumed bool `json:"resumed"`
		}{Resumed: resumed})
		if err != nil {
			return err
		}
		if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "login", OccurredAt: now, Detail: detail}); err != nil {
			return err
		}
		result = LoginResult{Policy: principal.Policy, Token: session.Token, SessionID: session.ID, DeadlineAt: session.DeadlineAt, ServerTime: now, MinVolumePercent: exam.MinVolumePercent, Locked: lockInfo(principal.Session)}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	if decisionErr != nil {
		return LoginResult{}, decisionErr
	}
	return result, nil
}

func (s *Sessions) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrInvalidToken
	}
	var principal Principal
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		session, err := tx.SessionByToken(ctx, token)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		principal, _, err = s.reconcileSession(ctx, tx, session, now)
		return err
	})
	if err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func (s *Sessions) reconcileSession(ctx context.Context, tx SessionTransaction, session domain.Session, now time.Time) (Principal, bool, error) {
	lifecycle := domain.EvaluateSession(session, now)
	changed := false
	if session.Status == domain.Active && lifecycle.Status != domain.Active {
		var err error
		changed, err = tx.EnterGrace(ctx, session.ID)
		if err != nil {
			return Principal{}, false, err
		}
		if changed {
			detail, err := json.Marshal(struct {
				DeadlineAt time.Time `json:"deadline_at"`
			}{session.DeadlineAt})
			if err != nil {
				return Principal{}, false, err
			}
			if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "grace_started", OccurredAt: now, Detail: detail}); err != nil {
				return Principal{}, false, err
			}
			session.Status = domain.Grace
		}
	}
	if lifecycle.FinalizationPending {
		var err error
		session, err = s.finalize(ctx, tx, session, now, nil, finalization{Status: domain.Submitted, Reason: "grace_expired"})
		if err != nil {
			return Principal{}, false, err
		}
		lifecycle = domain.EvaluateSession(session, now)
	}
	return Principal{Policy: clientPolicy(session.Policy), Session: session, Lifecycle: lifecycle, ServerTime: now, Locked: lockInfo(session)}, changed, nil
}

func examTiming(exam domain.Exam) (time.Duration, time.Duration, error) {
	const maxSeconds = math.MaxInt64 / int64(time.Second)
	if exam.DurationSeconds <= 0 || exam.GraceSeconds <= 0 || exam.DurationSeconds > maxSeconds || exam.GraceSeconds > maxSeconds-exam.DurationSeconds {
		return 0, 0, fmt.Errorf("invalid exam timing")
	}
	return time.Duration(exam.DurationSeconds) * time.Second, time.Duration(exam.GraceSeconds) * time.Second, nil
}

func randomUUID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func sessionCredentials() (string, string, error) {
	id, err := randomUUID()
	if err != nil {
		return "", "", err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	return id, base64.RawURLEncoding.EncodeToString(token[:]), nil
}
