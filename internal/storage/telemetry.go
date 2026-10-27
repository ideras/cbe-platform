package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"exam-platform/internal/domain"
)

func (t *sessionTransaction) RecordEvent(ctx context.Context, event domain.Event) (int64, error) {
	var detail any
	if event.Detail != nil {
		detail = string(event.Detail)
	}
	result, err := t.tx.ExecContext(ctx, `INSERT INTO event_log(session_id, event_type, occurred_at, detail) VALUES (?, ?, ?, ?)`, event.SessionID, event.Type, timestamp(event.OccurredAt), detail)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (t *sessionTransaction) LatestFocusEvent(ctx context.Context, sessionID string) (domain.Event, error) {
	var event domain.Event
	var occurred string
	var detail sql.NullString
	err := t.tx.QueryRowContext(ctx, `SELECT id, session_id, event_type, occurred_at, detail FROM event_log
		WHERE session_id = ? AND event_type IN ('focus_lost', 'focus_regained', 'app_started', 'app_closed', 'network_connected', 'network_gap', 'network_reconnected', 'connection_changed')
		ORDER BY id DESC LIMIT 1`, sessionID).Scan(&event.ID, &event.SessionID, &event.Type, &occurred, &detail)
	if errors.Is(err, sql.ErrNoRows) {
		return event, domain.ErrNotFound
	}
	if err != nil {
		return event, err
	}
	if detail.Valid {
		event.Detail = []byte(detail.String)
	}
	event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
	return event, err
}

func (t *sessionTransaction) EverOverdue(ctx context.Context, sessionID string) (bool, error) {
	var overdue bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM event_log WHERE session_id = ? AND event_type = 'network_gap')`, sessionID).Scan(&overdue)
	return overdue, err
}

func (t *sessionTransaction) SetNetworkGap(ctx context.Context, sessionID string, since time.Time) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE sessions SET network_gap_since = ? WHERE session_id = ? AND network_gap_since IS NULL`, timestamp(since), sessionID)
	return err
}

func (t *sessionTransaction) TouchHeartbeat(ctx context.Context, sessionID, hostname, remoteAddr string, sound domain.Sound, now time.Time) error {
	var volume, muted any // nil stores NULL: a heartbeat that does not report the level clears the previous one
	if sound.VolumePercent != nil {
		volume = int64(*sound.VolumePercent)
	}
	if sound.Muted != nil {
		muted = int64(0)
		if *sound.Muted {
			muted = int64(1)
		}
	}
	_, err := t.tx.ExecContext(ctx, `UPDATE sessions SET last_seen = ?, hostname = ?, remote_addr = ?, volume_percent = ?, volume_muted = ?, network_gap_since = NULL WHERE session_id = ?`, timestamp(now), hostname, remoteAddr, volume, muted, sessionID)
	return err
}

func (t *sessionTransaction) StudentsWithoutSession(ctx context.Context, examID string) ([]domain.Student, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT account_number, canvas_id, name, email, sis_user_id FROM students
		WHERE EXISTS(SELECT 1 FROM class_students cs JOIN exams e ON e.class_id = cs.class_id
				WHERE e.exam_id = ?1 AND cs.account_number = students.account_number)
			AND NOT EXISTS(SELECT 1 FROM sessions WHERE sessions.account_number = students.account_number AND exam_id = ?1)
		ORDER BY name, account_number`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	students := []domain.Student{}
	for rows.Next() {
		var student domain.Student
		if err := rows.Scan(&student.AccountNumber, &student.CanvasID, &student.Name, &student.Email, &student.SISUserID); err != nil {
			return nil, err
		}
		students = append(students, student)
	}
	return students, rows.Err()
}
