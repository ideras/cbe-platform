package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"exam-platform/internal/domain"
)

const DefaultTimelineLimit = 100
const MaxTimelineLimit = 200
const TimelinePageBytes = 2 << 20

// proctorPolicy reports the signals of the exam the proctor watches, with the rules it was published with.
func proctorPolicy(policy domain.Policy) ProctorPolicy {
	return ProctorPolicy{policy.HeartbeatInterval.Milliseconds(), policy.OverdueThreshold.Milliseconds(), policy.FocusThreshold.Milliseconds(),
		policy.AlertInterval.Milliseconds(), policy.CaptureInterval.Milliseconds(), policy.LockAwayAfter.Milliseconds(), policy.LockNoContactAfter.Milliseconds()}
}

// examPolicy is the rules an exam follows: the ones frozen at its latest publish, or, for an exam never published, the
// ones it would get if it were published now.
func examPolicy(ctx context.Context, tx SessionTransaction, exam domain.Exam) (domain.Policy, error) {
	if !exam.Policy.IsZero() {
		return exam.Policy, nil
	}
	policy, _, err := currentSettings(ctx, tx)
	return policy, err
}

type ProctorStudent struct {
	AccountNumber string `json:"account_number"`
	Name          string `json:"student_name"`
}
type FocusReport struct {
	ID         int64     `json:"event_log_id"`
	Type       string    `json:"event_type"`
	OccurredAt time.Time `json:"occurred_at"`
}
type FinalSummary struct {
	VersionID      int64     `json:"final_version_id"`
	Sequence       int64     `json:"version_seq"`
	SavedAt        time.Time `json:"saved_at"`
	SubmittedAt    time.Time `json:"submitted_at"`
	Reason         string    `json:"reason"`
	OperatorReason string    `json:"operator_reason,omitempty"`
}
type ProctorSession struct {
	ID     string `json:"session_id"`
	ExamID string `json:"exam_id"`
	ProctorStudent
	RosterActive             bool                 `json:"roster_active"`
	Status                   domain.SessionStatus `json:"status"`
	PersistedStatus          domain.SessionStatus `json:"persisted_status"`
	FinalizationPending      bool                 `json:"finalization_pending"`
	StartedAt                time.Time            `json:"started_at"`
	DeadlineAt               time.Time            `json:"deadline_at"`
	GraceUntil               time.Time            `json:"grace_until"`
	SecondsRemaining         int64                `json:"seconds_remaining"`
	Presence                 domain.Presence      `json:"presence"`
	LastFocusEvent           *FocusReport         `json:"last_focus_event"`
	LastKnownUnfocusedMillis *int64               `json:"last_known_unfocused_duration_ms"`
	NetworkWarning           bool                 `json:"network_warning"`
	FocusWarning             bool                 `json:"focus_warning"`
	// Locked says that the exam is locked, since when and why; nil while it is not.
	Locked *LockInfo `json:"locked"`
	// Locks are the locks and unlocks of the session, in the order they happened.
	Locks []domain.LockRecord `json:"locks"`
	Final *FinalSummary       `json:"final_version"`
}
type ProctorPolicy struct {
	HeartbeatIntervalMillis  int64 `json:"heartbeat_interval_ms"`
	OverdueThresholdMillis   int64 `json:"overdue_threshold_ms"`
	FocusThresholdMillis     int64 `json:"focus_threshold_ms"`
	AlertIntervalMillis      int64 `json:"alert_interval_ms"`
	CaptureIntervalMillis    int64 `json:"capture_interval_ms"`
	LockAwayAfterMillis      int64 `json:"lock_away_after_ms"`
	LockNoContactAfterMillis int64 `json:"lock_no_contact_after_ms"`
}
type SessionList struct {
	ExamID                 string           `json:"exam_id"`
	ServerTime             time.Time        `json:"server_time"`
	Policy                 ProctorPolicy    `json:"policy"`
	Sessions               []ProctorSession `json:"sessions"`
	StudentsWithoutSession []ProctorStudent `json:"students_without_session"`
}

func validProctorID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *Sessions) ProctorSessions(ctx context.Context, examID string) (SessionList, error) {
	if !validProctorID(examID) {
		return SessionList{}, ErrInvalidInput
	}
	var result SessionList
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		exam, err := tx.Exam(ctx, examID)
		if err != nil {
			return err
		}
		records, err := tx.ProctorSessions(ctx, examID)
		if err != nil {
			return err
		}
		policy, err := examPolicy(ctx, tx, exam)
		if err != nil {
			return err
		}
		locks, err := tx.LockHistories(ctx, examID)
		if err != nil {
			return err
		}
		result = SessionList{ExamID: examID, ServerTime: now, Policy: proctorPolicy(policy), Sessions: []ProctorSession{}, StudentsWithoutSession: []ProctorStudent{}}
		for _, record := range records {
			view, err := s.proctorSession(ctx, tx, record, locks[record.Session.ID], now)
			if err != nil {
				return err
			}
			result.Sessions = append(result.Sessions, view)
		}
		students, err := tx.StudentsWithoutSession(ctx, examID)
		if err != nil {
			return err
		}
		for _, student := range students {
			result.StudentsWithoutSession = append(result.StudentsWithoutSession, ProctorStudent{student.AccountNumber, student.Name})
		}
		return nil
	})
	if err != nil {
		return SessionList{}, err
	}
	return result, nil
}

// proctorSession is what the proctor sees of one session; locks are its locks and unlocks, in the order they happened.
func (s *Sessions) proctorSession(ctx context.Context, tx SessionTransaction, record domain.ProctorRecord, locks []domain.LockRecord, now time.Time) (ProctorSession, error) {
	session := record.Session
	policy := session.Policy
	lifecycle := domain.EvaluateSession(session, now)
	presence, err := s.presence(ctx, tx, session, now)
	if err != nil {
		return ProctorSession{}, err
	}
	view := ProctorSession{ID: session.ID, ExamID: session.ExamID, ProctorStudent: ProctorStudent{session.AccountNumber, record.StudentName}, RosterActive: record.RosterActive, Status: lifecycle.Status, PersistedStatus: session.Status, FinalizationPending: lifecycle.FinalizationPending, StartedAt: session.StartedAt, DeadlineAt: session.DeadlineAt, GraceUntil: session.GraceUntil, SecondsRemaining: lifecycle.SecondsRemaining, Presence: presence, NetworkWarning: presence.Overdue, Locked: lockInfo(session), Locks: locks}
	if view.Locks == nil {
		view.Locks = []domain.LockRecord{}
	}
	last, err := tx.LatestReportedFocus(ctx, session.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return ProctorSession{}, err
	}
	if err == nil {
		view.LastFocusEvent = &FocusReport{last.ID, last.Type, last.OccurredAt}
		if last.Type == "focus_lost" && session.LastSeen != nil {
			var detail struct {
				Unconfirmed bool `json:"connection_unconfirmed"`
			}
			if err := json.Unmarshal(last.Detail, &detail); err != nil {
				return ProctorSession{}, err
			}
			latest, err := tx.LatestFocusEvent(ctx, session.ID)
			if err != nil {
				return ProctorSession{}, err
			}
			interrupted, err := tx.FocusInterrupted(ctx, session.ID, last.ID)
			if err != nil {
				return ProctorSession{}, err
			}
			if !detail.Unconfirmed && !interrupted && (latest.ID == last.ID || latest.Type == "network_gap") {
				until := now
				if until.After(session.GraceUntil) {
					until = session.GraceUntil
				}
				if session.SubmittedAt != nil && until.After(*session.SubmittedAt) {
					until = *session.SubmittedAt
				}
				gapAt := session.LastSeen.Add(policy.OverdueThreshold)
				if session.NetworkGapSince != nil {
					gapAt = *session.NetworkGapSince
				}
				if until.After(gapAt) {
					until = gapAt
				}
				duration := domain.NonnegativeElapsed(until, last.OccurredAt)
				millis := duration.Milliseconds()
				view.LastKnownUnfocusedMillis = &millis
				view.FocusWarning = presence.Monitoring && duration > policy.FocusThreshold
			}
		}
	}
	if terminal(session) {
		if session.FinalVersionID == nil || session.SubmittedAt == nil {
			return ProctorSession{}, fmt.Errorf("invalid terminal session")
		}
		version, err := tx.VersionMetadata(ctx, session.ID, *session.FinalVersionID)
		if err != nil {
			return ProctorSession{}, err
		}
		if err := ValidateFinalVersion(session.ID, version); err != nil {
			return ProctorSession{}, err
		}
		if version.ID != *session.FinalVersionID {
			return ProctorSession{}, fmt.Errorf("inconsistent final version")
		}
		ending, err := finalizationEnding(ctx, tx, session)
		if err != nil {
			return ProctorSession{}, err
		}
		view.Final = &FinalSummary{version.ID, version.Sequence, version.SavedAt, *session.SubmittedAt, ending.Reason, ending.OperatorReason}
	}
	return view, nil
}

// finalizationInfo is how a finalized session ended, as its finalization event records it.
type finalizationInfo struct {
	Reason, OperatorReason string
}

// finalizationEnding reads the finalization event of a terminal session (one with a final version) and checks that it agrees
// with the session: the same final version, status and reason.
func finalizationEnding(ctx context.Context, tx SessionTransaction, session domain.Session) (finalizationInfo, error) {
	event, err := tx.FinalizationEvent(ctx, session.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return finalizationInfo{}, fmt.Errorf("missing finalization event")
	}
	if err != nil {
		return finalizationInfo{}, err
	}
	var detail struct {
		Reason         string `json:"reason"`
		OperatorReason string `json:"operator_reason"`
		VersionID      int64  `json:"final_version_id"`
	}
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		return finalizationInfo{}, err
	}
	if detail.VersionID != *session.FinalVersionID || event.Type != string(session.Status) || (session.Status == domain.ForceSubmitted && detail.Reason != "force_submit") || (session.Status == domain.Submitted && detail.Reason != "explicit_submit" && detail.Reason != "grace_expired") {
		return finalizationInfo{}, fmt.Errorf("inconsistent finalization event")
	}
	return finalizationInfo{detail.Reason, detail.OperatorReason}, nil
}

type TimelineEvent struct {
	ID          int64                       `json:"event_log_id"`
	Type        string                      `json:"event_type"`
	Detail      json.RawMessage             `json:"detail"`
	Screenshots []domain.ScreenshotMetadata `json:"screenshots"`
}
type TimelineVersion struct {
	ID       int64          `json:"version_id"`
	Sequence int64          `json:"version_seq"`
	Answers  domain.Answers `json:"answers"`
	IsFinal  bool           `json:"is_final"`
}
type TimelineItem struct {
	ID         string           `json:"id"`
	Kind       string           `json:"kind"`
	OccurredAt time.Time        `json:"occurred_at"`
	Event      *TimelineEvent   `json:"event,omitempty"`
	Version    *TimelineVersion `json:"answer_version,omitempty"`
}
type TimelinePage struct {
	ServerTime       time.Time                `json:"server_time"`
	Session          ProctorSession           `json:"session"`
	Policy           ProctorPolicy            `json:"policy"`
	HistoryWatermark domain.TimelineWatermark `json:"history_watermark"`
	Items            []TimelineItem           `json:"items"`
	NextCursor       *string                  `json:"next_cursor"`
}
type timelineCursor struct {
	Version   int                      `json:"v"`
	SessionID string                   `json:"session_id"`
	After     domain.TimelinePosition  `json:"after"`
	Watermark domain.TimelineWatermark `json:"watermark"`
}

func decodeTimelineCursor(encoded, id string) (timelineCursor, error) {
	var cursor timelineCursor
	if len(encoded) > 1024 {
		return cursor, ErrInvalidInput
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return cursor, ErrInvalidInput
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return cursor, ErrInvalidInput
	}
	at, err := time.Parse(domain.TimelineTimeLayout, cursor.After.Time)
	if err != nil || at.UTC().Format(domain.TimelineTimeLayout) != cursor.After.Time || cursor.Version != 1 || cursor.SessionID != id || cursor.After.ID <= 0 || cursor.Watermark.EventID < 0 || cursor.Watermark.VersionID < 0 {
		return cursor, ErrInvalidInput
	}
	switch cursor.After.Kind {
	case "event":
		if cursor.After.ID > cursor.Watermark.EventID {
			return cursor, ErrInvalidInput
		}
	case "answer_version":
		if cursor.After.ID > cursor.Watermark.VersionID {
			return cursor, ErrInvalidInput
		}
	default:
		return cursor, ErrInvalidInput
	}
	return cursor, nil
}
func (s *Sessions) ProctorTimeline(ctx context.Context, id string, limit int, encoded string) (TimelinePage, error) {
	if !validProctorID(id) || limit < 1 || limit > MaxTimelineLimit {
		return TimelinePage{}, ErrInvalidInput
	}
	cursor := timelineCursor{Version: 1, SessionID: id}
	if encoded != "" {
		var err error
		cursor, err = decodeTimelineCursor(encoded, id)
		if err != nil {
			return TimelinePage{}, err
		}
	}
	var result TimelinePage
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		record, err := tx.ProctorSession(ctx, id)
		if err != nil {
			return err
		}
		locks, err := tx.LockHistories(ctx, record.Session.ExamID)
		if err != nil {
			return err
		}
		view, err := s.proctorSession(ctx, tx, record, locks[record.Session.ID], now)
		if err != nil {
			return err
		}
		mark, err := tx.HistoryWatermark(ctx, id)
		if err != nil {
			return err
		}
		if encoded == "" {
			cursor.Watermark = mark
		} else if cursor.Watermark.EventID > mark.EventID || cursor.Watermark.VersionID > mark.VersionID {
			return ErrInvalidInput
		}
		records, err := tx.HistoryPage(ctx, id, cursor.After, cursor.Watermark, limit+1)
		if err != nil {
			return err
		}
		result = TimelinePage{ServerTime: now, Session: view, Policy: proctorPolicy(record.Session.Policy), HistoryWatermark: cursor.Watermark, Items: []TimelineItem{}}
		bytesUsed := 0
		for _, entry := range records {
			if len(result.Items) == limit {
				break
			}
			pos := entry.Position()
			item := TimelineItem{ID: fmt.Sprintf("%s:%d", pos.Kind, pos.ID), Kind: pos.Kind}
			if entry.Version != nil {
				v, err := tx.Version(ctx, id, entry.Version.ID)
				if err != nil {
					return err
				}
				if v.SessionID != id || v.ID != entry.Version.ID {
					return fmt.Errorf("inconsistent timeline version")
				}
				item.OccurredAt = v.SavedAt
				item.Version = &TimelineVersion{v.ID, v.Sequence, v.Answers, record.Session.FinalVersionID != nil && *record.Session.FinalVersionID == v.ID}
			} else {
				e := entry.Event
				item.OccurredAt = e.OccurredAt
				item.Event = &TimelineEvent{e.ID, e.Type, e.Detail, entry.Screenshots}
			}
			data, err := json.Marshal(item)
			if err != nil {
				return err
			}
			// Include an oversized first record so pagination always makes progress.
			if len(result.Items) > 0 && bytesUsed+len(data) > TimelinePageBytes {
				break
			}
			bytesUsed += len(data)
			result.Items = append(result.Items, item)
		}
		if len(result.Items) < len(records) {
			cursor.After = records[len(result.Items)-1].Position()
			data, err := json.Marshal(cursor)
			if err != nil {
				return err
			}
			next := base64.RawURLEncoding.EncodeToString(data)
			result.NextCursor = &next
		}
		return nil
	})
	if err != nil {
		return TimelinePage{}, err
	}
	return result, nil
}

// SubmissionVersion is one saved set of answers in a student's submission.
type SubmissionVersion struct {
	VersionID int64          `json:"version_id"`
	Sequence  int64          `json:"version_seq"`
	SavedAt   time.Time      `json:"saved_at"`
	Answers   domain.Answers `json:"answers"`
}

// Submission is one session's answers, as the proctor downloads them. Final is true once the session is finalized (submitted
// by the student, when time ran out, or by the proctor); Version is then the final version. Otherwise Version is the latest
// save, or null before the first one, and the dashboard skips the session. Status is the persisted session status, so a session
// waiting for the background worker to finalize it after its grace period comes back as "grace" with Final false.
type Submission struct {
	SessionID      string               `json:"session_id"`
	ExamID         string               `json:"exam_id"`
	AccountNumber  string               `json:"account_number"`
	StudentName    string               `json:"student_name"`
	Status         domain.SessionStatus `json:"status"`
	Final          bool                 `json:"final"`
	Reason         string               `json:"reason,omitempty"`
	OperatorReason string               `json:"operator_reason,omitempty"`
	SubmittedAt    *time.Time           `json:"submitted_at"`
	Version        *SubmissionVersion   `json:"version"`
}

// Submission reads one session's final (or latest) answers. One session per request keeps the response under the answers'
// limit whatever the class size.
func (s *Sessions) Submission(ctx context.Context, id string) (Submission, error) {
	if !validProctorID(id) {
		return Submission{}, ErrInvalidInput
	}
	var result Submission
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		record, err := tx.ProctorSession(ctx, id)
		if err != nil {
			return err
		}
		session := record.Session
		lifecycle := domain.EvaluateSession(session, s.clock.Now().UTC())
		status := lifecycle.Status
		if lifecycle.FinalizationPending {
			// The session's time is over but the worker has not finalized it yet; report it as still finishing, not as submitted.
			status = domain.Grace
		}
		result = Submission{SessionID: session.ID, ExamID: session.ExamID, AccountNumber: session.AccountNumber, StudentName: record.StudentName, Status: status, Final: terminal(session), SubmittedAt: session.SubmittedAt}
		var version domain.Version
		if terminal(session) {
			if session.FinalVersionID == nil || session.SubmittedAt == nil {
				return fmt.Errorf("invalid terminal session")
			}
			version, err = tx.Version(ctx, id, *session.FinalVersionID)
			if err != nil {
				return err
			}
			if version.SessionID != id || version.ID != *session.FinalVersionID {
				return fmt.Errorf("inconsistent final version")
			}
			ending, err := finalizationEnding(ctx, tx, session)
			if err != nil {
				return err
			}
			result.Reason, result.OperatorReason = ending.Reason, ending.OperatorReason
		} else {
			version, err = tx.LatestVersion(ctx, id)
			if errors.Is(err, domain.ErrNotFound) {
				return nil // the student never saved; version stays null
			}
			if err != nil {
				return err
			}
			if version.SessionID != id {
				return fmt.Errorf("inconsistent latest version")
			}
		}
		result.Version = &SubmissionVersion{VersionID: version.ID, Sequence: version.Sequence, SavedAt: version.SavedAt, Answers: version.Answers}
		return nil
	})
	if err != nil {
		return Submission{}, err
	}
	return result, nil
}
