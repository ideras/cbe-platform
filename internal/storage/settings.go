package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"exam-platform/internal/domain"
)

// execer is a statement runner; a transaction and a database both are one.
type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// readSettings reads the stored monitoring settings. No row means the zero policy and no update time: no proctor has
// saved any settings, and the server's built-in defaults apply.
func readSettings(ctx context.Context, q queryRower) (domain.Policy, time.Time, error) {
	var raw, updated string
	err := q.QueryRowContext(ctx, `SELECT policy_json, updated_at FROM settings WHERE singleton = 1`).Scan(&raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Policy{}, time.Time{}, nil
	}
	if err != nil {
		return domain.Policy{}, time.Time{}, err
	}
	var policy domain.Policy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return domain.Policy{}, time.Time{}, err
	}
	at, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.Policy{}, time.Time{}, err
	}
	return policy, at, nil
}

// writeSettings stores the monitoring settings as the only row of the table.
func writeSettings(ctx context.Context, ex execer, policy domain.Policy, now time.Time) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = ex.ExecContext(ctx, `INSERT INTO settings(singleton, policy_json, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET policy_json = excluded.policy_json, updated_at = excluded.updated_at`, string(raw), timestamp(now))
	return err
}

// averageScreenshotBytes is the mean size of the stored screenshots, or nil when there are none. The mean is a fraction, so
// it is rounded to the nearest whole byte.
func averageScreenshotBytes(ctx context.Context, q queryRower) (*int64, error) {
	var average *int64
	var value sql.NullFloat64
	err := q.QueryRowContext(ctx, `SELECT AVG(byte_size) FROM screenshots WHERE state = 'ready'`).Scan(&value)
	if err != nil {
		return nil, err
	}
	if value.Valid {
		size := int64(value.Float64 + 0.5)
		average = &size
	}
	return average, nil
}

// Settings reads the monitoring settings of the server.
func (t *sessionTransaction) Settings(ctx context.Context) (domain.Policy, time.Time, error) {
	return readSettings(ctx, t.tx)
}

// SaveSettings stores the monitoring settings of the server.
func (t *sessionTransaction) SaveSettings(ctx context.Context, policy domain.Policy, now time.Time) error {
	return writeSettings(ctx, t.tx, policy, now)
}

// AverageScreenshotBytes is the mean size of the stored screenshots, or nil when there are none.
func (t *sessionTransaction) AverageScreenshotBytes(ctx context.Context) (*int64, error) {
	return averageScreenshotBytes(ctx, t.tx)
}

// Settings reads the monitoring settings of the server.
func (t *publishTransaction) Settings(ctx context.Context) (domain.Policy, time.Time, error) {
	return readSettings(ctx, t.tx)
}

// SetExamPolicy freezes the monitoring settings in an exam, at its publish.
func (t *publishTransaction) SetExamPolicy(ctx context.Context, examID string, policy domain.Policy) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `UPDATE exams SET policy_json = ? WHERE exam_id = ?`, string(raw), examID)
	return err
}
