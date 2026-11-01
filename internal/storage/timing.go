package storage

import (
	"context"
	"database/sql"
	"time"
)

// Both writes share the caller's transaction, so new logins and running sessions
// can never observe different caps.
func setExamEndTime(ctx context.Context, tx *sql.Tx, id string, end *time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE exams SET ends_at = ? WHERE exam_id = ?`, optionalTimestamp(end), id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET ends_at = ? WHERE exam_id = ? AND status IN ('active', 'grace')`, optionalTimestamp(end), id)
	return err
}
func (t *sessionTransaction) SetExamEndTime(ctx context.Context, id string, end *time.Time) error {
	return setExamEndTime(ctx, t.tx, id, end)
}
func (t *publishTransaction) SetExamEndTime(ctx context.Context, id string, end *time.Time) error {
	return setExamEndTime(ctx, t.tx, id, end)
}
func (t *sessionTransaction) SetGraceUntil(ctx context.Context, id string, grace time.Time) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE sessions SET grace_until = ? WHERE session_id = ? AND status IN ('active', 'grace')`, timestamp(grace), id)
	return err
}
func (t *sessionTransaction) AddExamGrace(ctx context.Context, id string, seconds int64) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE exams SET extra_grace_seconds = extra_grace_seconds + ? WHERE exam_id = ?`, seconds, id)
	return err
}
