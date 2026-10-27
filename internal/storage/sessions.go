package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

type sessionTransaction struct {
	tx    *sql.Tx
	store *Store
}

func (s *Store) WithSessionTransaction(ctx context.Context, fn func(service.SessionTransaction) error) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return fn(&sessionTransaction{tx: tx, store: s}) })
}

func (t *sessionTransaction) Exam(ctx context.Context, id string) (domain.Exam, error) {
	return scanExam(t.tx.QueryRowContext(ctx, `SELECT `+examColumns+` FROM exams WHERE exam_id = ?`, id))
}

func (t *sessionTransaction) SessionByID(ctx context.Context, id string) (domain.Session, error) {
	return scanSession(t.tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE session_id = ?`, id))
}

func (t *sessionTransaction) AppendVersion(ctx context.Context, id string, answers domain.Answers, now time.Time) (domain.Version, error) {
	return t.store.appendVersion(ctx, t.tx, id, answers, now)
}

func (t *sessionTransaction) LatestVersion(ctx context.Context, id string) (domain.Version, error) {
	return scanVersion(t.tx.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM submission_versions WHERE session_id = ? ORDER BY version_seq DESC LIMIT 1`, id))
}

func (t *sessionTransaction) Version(ctx context.Context, sessionID string, id int64) (domain.Version, error) {
	return scanVersion(t.tx.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM submission_versions WHERE session_id = ? AND id = ?`, sessionID, id))
}

func (t *sessionTransaction) Finalize(ctx context.Context, id string, versionID int64, status domain.SessionStatus, now time.Time) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE sessions SET status = ?, submitted_at = ?, final_version_id = ?, locked_at = NULL, lock_reason = NULL
		WHERE session_id = ? AND status IN ('active', 'grace')`, status, timestamp(now), versionID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("finalization did not change exactly one session")
	}
	return nil
}

const sessionColumns = `session_id, account_number, exam_id, token, started_at, deadline_at, grace_until, last_seen, submitted_at, status, final_version_id, hostname, remote_addr, network_gap_since, volume_percent, volume_muted, current_exercise, policy_json, locked_at, lock_reason, unlocked_at`

type scanner interface{ Scan(...any) error }

func scanSession(row scanner) (domain.Session, error) {
	var session domain.Session
	var started, deadline, grace string
	var lastSeen, submitted, networkGap sql.NullString
	var final, volume, muted sql.NullInt64
	var policy, lockedAt, lockReason, unlockedAt sql.NullString
	err := row.Scan(&session.ID, &session.AccountNumber, &session.ExamID, &session.Token, &started, &deadline, &grace, &lastSeen, &submitted, &session.Status, &final, &session.Hostname, &session.RemoteAddr, &networkGap, &volume, &muted, &session.CurrentExercise, &policy, &lockedAt, &lockReason, &unlockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return session, domain.ErrNotFound
	}
	if err != nil {
		return session, err
	}
	for _, field := range []struct {
		src string
		dst *time.Time
	}{{started, &session.StartedAt}, {deadline, &session.DeadlineAt}, {grace, &session.GraceUntil}} {
		if *field.dst, err = time.Parse(time.RFC3339Nano, field.src); err != nil {
			return session, err
		}
	}
	if session.LastSeen, err = nullableTime(lastSeen); err != nil {
		return session, err
	}
	if session.SubmittedAt, err = nullableTime(submitted); err != nil {
		return session, err
	}
	if session.NetworkGapSince, err = nullableTime(networkGap); err != nil {
		return session, err
	}
	if final.Valid {
		session.FinalVersionID = &final.Int64
	}
	if policy.Valid && policy.String != "" {
		if err := json.Unmarshal([]byte(policy.String), &session.Policy); err != nil {
			return session, err
		}
	}
	if session.LockedAt, err = nullableTime(lockedAt); err != nil {
		return session, err
	}
	session.LockReason = lockReason.String
	if session.UnlockedAt, err = nullableTime(unlockedAt); err != nil {
		return session, err
	}
	if volume.Valid {
		percent := int(volume.Int64)
		session.Sound.VolumePercent = &percent
	}
	if muted.Valid {
		silenced := muted.Int64 != 0
		session.Sound.Muted = &silenced
	}
	return session, nil
}

func nullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value.String)
	return &t, err
}

func (t *sessionTransaction) SessionByAccount(ctx context.Context, account, examID string) (domain.Session, error) {
	return scanSession(t.tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE account_number = ? AND exam_id = ?`, account, examID))
}

func (t *sessionTransaction) SessionByToken(ctx context.Context, token string) (domain.Session, error) {
	return scanSession(t.tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE token = ?`, token))
}

func (t *sessionTransaction) InsertSession(ctx context.Context, session domain.Session) error {
	policy, err := json.Marshal(session.Policy)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO sessions
		(session_id, account_number, exam_id, token, started_at, deadline_at, grace_until, status, policy_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.AccountNumber, session.ExamID, session.Token, timestamp(session.StartedAt), timestamp(session.DeadlineAt), timestamp(session.GraceUntil), session.Status, string(policy))
	return err
}

// AdvanceExercise moves an unfinished session from exercise from to the next one; false means it was not on from.
func (t *sessionTransaction) AdvanceExercise(ctx context.Context, id string, from int) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `UPDATE sessions SET current_exercise = current_exercise + 1 WHERE session_id = ? AND current_exercise = ? AND status IN ('active', 'grace')`, id, from)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (t *sessionTransaction) EnterGrace(ctx context.Context, id string) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `UPDATE sessions SET status = 'grace' WHERE session_id = ? AND status = 'active'`, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count != 0, err
}

func (t *sessionTransaction) AddEvent(ctx context.Context, event domain.Event) error {
	_, err := t.RecordEvent(ctx, event)
	return err
}

func (t *sessionTransaction) UnfinishedSessions(ctx context.Context) ([]domain.Session, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE status IN ('active', 'grace') ORDER BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []domain.Session{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}
