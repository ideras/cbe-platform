package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"exam-platform/internal/domain"
)

// LoginGate reads the logins of a session from its event log: the latest accepted one, the ones refused since, and a release
// no login has used. All three questions are answered by the index on (session, event type, id).
func (t *sessionTransaction) LoginGate(ctx context.Context, id string) (domain.LoginGate, error) {
	var gate domain.LoginGate
	var lastLogin int64
	var at sql.NullString
	err := t.tx.QueryRowContext(ctx, `SELECT id, occurred_at FROM event_log WHERE session_id = ? AND event_type = 'login' ORDER BY id DESC LIMIT 1`, id).Scan(&lastLogin, &at)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return gate, err
	}
	if err == nil {
		if gate.LastLoginAt, err = nullableTime(at); err != nil {
			return gate, fmt.Errorf("read login time: %w", err)
		}
	}
	// Whether the application announced its closing: the latest of the events that open or close it says so. Only the holder
	// of the session's token can send that event.
	var latest sql.NullString
	err = t.tx.QueryRowContext(ctx, `SELECT event_type FROM event_log WHERE session_id = ? AND event_type IN ('login', 'app_started', 'app_closed') ORDER BY id DESC LIMIT 1`, id).Scan(&latest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return gate, err
	}
	gate.AppClosed = latest.Valid && latest.String == "app_closed"
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM event_log WHERE session_id = ? AND event_type = 'login_refused' AND id > ?`, id, lastLogin).Scan(&gate.Refusals); err != nil {
		return gate, err
	}
	if gate.Refusals > 0 {
		var detail sql.NullString
		if err := t.tx.QueryRowContext(ctx, `SELECT occurred_at, detail FROM event_log WHERE session_id = ? AND event_type = 'login_refused' AND id > ? ORDER BY id DESC LIMIT 1`, id, lastLogin).Scan(&at, &detail); err != nil {
			return gate, err
		}
		if gate.LastRefusedAt, err = nullableTime(at); err != nil {
			return gate, fmt.Errorf("read refusal time: %w", err)
		}
		if detail.Valid {
			var fields struct {
				RemoteAddr string `json:"remote_addr"`
			}
			// An unreadable detail only loses the address; the refusal itself still counts.
			if json.Unmarshal([]byte(detail.String), &fields) == nil {
				gate.LastRefusedAddr = fields.RemoteAddr
			}
		}
	}
	err = t.tx.QueryRowContext(ctx, `SELECT occurred_at FROM event_log WHERE session_id = ? AND event_type = 'login_released' AND id > ? ORDER BY id DESC LIMIT 1`, id, lastLogin).Scan(&at)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return gate, err
	}
	if err == nil {
		if gate.ReleasedAt, err = nullableTime(at); err != nil {
			return gate, fmt.Errorf("read release time: %w", err)
		}
	}
	return gate, nil
}

// RotateToken gives the session a new token. The old one no longer identifies it.
func (t *sessionTransaction) RotateToken(ctx context.Context, id, token string) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE sessions SET token = ? WHERE session_id = ?`, token, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrNotFound
	}
	return nil
}
