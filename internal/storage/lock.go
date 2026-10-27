package storage

import (
	"context"
	"encoding/json"
	"time"

	"exam-platform/internal/domain"
)

// LockSession locks a session that is still running; false means it was locked already or is over.
func (t *sessionTransaction) LockSession(ctx context.Context, id string, at time.Time, reason string) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `UPDATE sessions SET locked_at = ?, lock_reason = ? WHERE session_id = ? AND locked_at IS NULL AND status IN ('active', 'grace')`, timestamp(at), reason, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// UnlockSession lifts the lock of a session, moves its deadline and grace and remembers when it was unlocked; false means
// it was not locked or is over. A session that was in grace goes back to active when its new deadline is still in the
// future.
func (t *sessionTransaction) UnlockSession(ctx context.Context, id string, deadline, grace, now time.Time) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `UPDATE sessions SET locked_at = NULL, lock_reason = NULL, unlocked_at = ?, deadline_at = ?, grace_until = ?,
			status = CASE WHEN status = 'grace' AND ? < ? THEN 'active' ELSE status END
		WHERE session_id = ? AND locked_at IS NOT NULL AND status IN ('active', 'grace')`,
		timestamp(now), timestamp(deadline), timestamp(grace), timestamp(now), timestamp(deadline), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// LatestLockEvent is the record of the session's latest lock.
func (t *sessionTransaction) LatestLockEvent(ctx context.Context, id string) (domain.Event, error) {
	return scanEvent(t.tx.QueryRowContext(ctx, `SELECT id,session_id,event_type,occurred_at,detail FROM event_log WHERE session_id=? AND event_type='locked' ORDER BY id DESC LIMIT 1`, id))
}

// FocusEventAtLock is the focus state the session was in when it was last locked: the latest focus, application or
// network event recorded before the lock.
func (t *sessionTransaction) FocusEventAtLock(ctx context.Context, id string) (domain.Event, error) {
	return scanEvent(t.tx.QueryRowContext(ctx, `SELECT id,session_id,event_type,occurred_at,detail FROM event_log
		WHERE session_id = ? AND event_type IN ('focus_lost', 'focus_regained', 'app_started', 'app_closed', 'network_connected', 'network_gap', 'network_reconnected', 'connection_changed')
			AND id < (SELECT max(id) FROM event_log WHERE session_id = ? AND event_type = 'locked')
		ORDER BY id DESC LIMIT 1`, id, id))
}

// LockHistories reads the locks and unlocks of every session of an exam, in the order they happened, keyed by session.
func (t *sessionTransaction) LockHistories(ctx context.Context, examID string) (map[string][]domain.LockRecord, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT e.id, e.session_id, e.event_type, e.occurred_at, e.detail FROM event_log e
		JOIN sessions s ON s.session_id = e.session_id
		WHERE s.exam_id = ? AND e.event_type IN ('locked', 'unlocked') ORDER BY e.id`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	histories := map[string][]domain.LockRecord{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		var detail struct {
			Reason         string `json:"reason"`
			OperatorReason string `json:"operator_reason"`
			LockedMillis   int64  `json:"locked_ms"`
			ExtendedMillis int64  `json:"extended_ms"`
		}
		if err := json.Unmarshal(event.Detail, &detail); err != nil {
			return nil, err
		}
		histories[event.SessionID] = append(histories[event.SessionID], domain.LockRecord{Type: event.Type, At: event.OccurredAt, Reason: detail.Reason,
			OperatorReason: detail.OperatorReason, LockedMillis: detail.LockedMillis, ExtendedMillis: detail.ExtendedMillis})
	}
	return histories, rows.Err()
}
