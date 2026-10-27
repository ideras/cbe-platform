package storage

import (
	"context"
	"database/sql"
	"errors"
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"time"
)

func (t *sessionTransaction) ScreenshotStoreID(ctx context.Context) (string, error) {
	var id string
	err := t.tx.QueryRowContext(ctx, `SELECT store_id FROM screenshot_store WHERE id=1`).Scan(&id)
	return id, err
}
func (s *Store) WithScreenshotTransaction(ctx context.Context, fn func(service.ScreenshotTransaction) error) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return fn(&sessionTransaction{tx: tx, store: s}) })
}
func (t *sessionTransaction) FocusEvent(ctx context.Context, session string, id int64) (domain.Event, error) {
	return scanEvent(t.tx.QueryRowContext(ctx, `SELECT id,session_id,event_type,occurred_at,detail FROM event_log WHERE session_id=? AND id=?`, session, id))
}
func (t *sessionTransaction) FocusBoundary(ctx context.Context, session string, after int64) (domain.Event, error) {
	return scanEvent(t.tx.QueryRowContext(ctx, `SELECT id,session_id,event_type,occurred_at,detail FROM event_log WHERE session_id=? AND id>? AND event_type IN ('focus_lost','focus_regained','app_started','app_closed','network_gap','network_connected','network_reconnected','connection_changed','submitted','force_submitted') ORDER BY id LIMIT 1`, session, after))
}

const screenshotColumns = `p.id,p.session_id,p.event_log_id,p.capture_index,p.content_type,p.file_path,p.captured_at,p.width,p.height,p.byte_size,p.sha256,p.client_captured_at,p.state,s.exam_id`

func scanScreenshot(row scanner) (domain.Screenshot, error) {
	var r domain.Screenshot
	var at string
	var client sql.NullString
	err := row.Scan(&r.ID, &r.SessionID, &r.EventLogID, &r.CaptureIndex, &r.ContentType, &r.Path, &at, &r.Width, &r.Height, &r.Bytes, &r.SHA256, &client, &r.State, &r.ExamID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, domain.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.CapturedAt, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return r, err
	}
	r.ClientCapturedAt, err = nullableTime(client)
	return r, err
}
func (t *sessionTransaction) ScreenshotByCapture(ctx context.Context, session string, event int64, index int) (domain.Screenshot, error) {
	return scanScreenshot(t.tx.QueryRowContext(ctx, `SELECT `+screenshotColumns+` FROM screenshots p JOIN sessions s ON s.session_id=p.session_id WHERE p.session_id=? AND p.event_log_id=? AND p.capture_index=?`, session, event, index))
}

// SessionScreenshotCount counts every screenshot a session has stored, including ones being deleted.
func (t *sessionTransaction) SessionScreenshotCount(ctx context.Context, session string) (int, error) {
	var count int
	err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM screenshots WHERE session_id=?`, session).Scan(&count)
	return count, err
}
func (t *sessionTransaction) ScreenshotByID(ctx context.Context, id int64) (domain.Screenshot, error) {
	return scanScreenshot(t.tx.QueryRowContext(ctx, `SELECT `+screenshotColumns+` FROM screenshots p JOIN sessions s ON s.session_id=p.session_id WHERE p.id=?`, id))
}
func (t *sessionTransaction) ScreenshotRows(ctx context.Context, exam string) ([]domain.Screenshot, error) {
	query := `SELECT ` + screenshotColumns + ` FROM screenshots p JOIN sessions s ON s.session_id=p.session_id`
	var args []any
	if exam != "" {
		query += ` WHERE s.exam_id=?`
		args = append(args, exam)
	}
	query += ` ORDER BY p.id`
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Screenshot{}
	for rows.Next() {
		r, err := scanScreenshot(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (t *sessionTransaction) InsertScreenshot(ctx context.Context, r domain.Screenshot) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO screenshots(session_id,event_log_id,capture_index,content_type,file_path,captured_at,width,height,byte_size,sha256,client_captured_at,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,'ready')`, r.SessionID, r.EventLogID, r.CaptureIndex, r.ContentType, r.Path, timestamp(r.CapturedAt), r.Width, r.Height, r.Bytes, r.SHA256, optionalTime(r.ClientCapturedAt))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return timestamp(*t)
}
func (t *sessionTransaction) DeleteScreenshot(ctx context.Context, id int64) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM screenshots WHERE id=?`, id)
	return err
}
func (t *sessionTransaction) ScreenshotsPurged(ctx context.Context, exam string) (bool, error) {
	var purged bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM screenshot_retention WHERE exam_id=?)`, exam).Scan(&purged)
	return purged, err
}
func (t *sessionTransaction) MarkScreenshotPurge(ctx context.Context, exam string, now time.Time) error {
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO screenshot_retention(exam_id,purged_at) VALUES(?,?) ON CONFLICT(exam_id) DO NOTHING`, exam, timestamp(now)); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(ctx, `UPDATE screenshots SET state='deleting' WHERE session_id IN (SELECT session_id FROM sessions WHERE exam_id=?)`, exam)
	return err
}
