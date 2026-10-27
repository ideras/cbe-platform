package storage

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"exam-platform/internal/domain"
)

func joinedSessionColumns() string { return "s." + strings.ReplaceAll(sessionColumns, ", ", ", s.") }

type proctorScanner struct {
	row    scanner
	record *domain.ProctorRecord
}

func (s proctorScanner) Scan(values ...any) error {
	return s.row.Scan(append(values, &s.record.StudentName, &s.record.RosterActive)...)
}
func scanProctor(row scanner) (domain.ProctorRecord, error) {
	var record domain.ProctorRecord
	session, err := scanSession(proctorScanner{row, &record})
	record.Session = session
	return record, err
}

// rosterFlag is true while the student is still in the class the session's exam belongs to.
const rosterFlag = `EXISTS(SELECT 1 FROM class_students cs JOIN exams e ON e.class_id = cs.class_id WHERE e.exam_id = s.exam_id AND cs.account_number = s.account_number)`

func (t *sessionTransaction) ProctorSessions(ctx context.Context, examID string) ([]domain.ProctorRecord, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT `+joinedSessionColumns()+`, p.name, `+rosterFlag+` FROM sessions s JOIN students p ON p.account_number=s.account_number WHERE s.exam_id=? ORDER BY p.name,p.account_number,s.session_id`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []domain.ProctorRecord{}
	for rows.Next() {
		record, err := scanProctor(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
func (t *sessionTransaction) VersionMetadata(ctx context.Context, sessionID string, id int64) (domain.Version, error) {
	return scanVersionMetadata(t.tx.QueryRowContext(ctx, `SELECT id,session_id,version_seq,saved_at FROM submission_versions WHERE session_id=? AND id=?`, sessionID, id))
}

func (t *sessionTransaction) ProctorSession(ctx context.Context, id string) (domain.ProctorRecord, error) {
	return scanProctor(t.tx.QueryRowContext(ctx, `SELECT `+joinedSessionColumns()+`, p.name,`+rosterFlag+` FROM sessions s JOIN students p ON p.account_number=s.account_number WHERE s.session_id=?`, id))
}
func scanEvent(row scanner) (domain.Event, error) {
	var event domain.Event
	var occurred string
	var detail sql.NullString
	err := row.Scan(&event.ID, &event.SessionID, &event.Type, &occurred, &detail)
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
func (t *sessionTransaction) LatestReportedFocus(ctx context.Context, id string) (domain.Event, error) {
	return scanEvent(t.tx.QueryRowContext(ctx, `SELECT id,session_id,event_type,occurred_at,detail FROM event_log WHERE session_id=? AND event_type IN ('focus_lost','focus_regained') ORDER BY id DESC LIMIT 1`, id))
}
func (t *sessionTransaction) FocusInterrupted(ctx context.Context, id string, after int64) (bool, error) {
	var interrupted bool
	err := t.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM event_log WHERE session_id=? AND id>? AND event_type IN ('focus_regained','app_started','app_closed','network_connected','network_reconnected','connection_changed'))`, id, after).Scan(&interrupted)
	return interrupted, err
}

func (t *sessionTransaction) FinalizationEvent(ctx context.Context, id string) (domain.Event, error) {
	return scanEvent(t.tx.QueryRowContext(ctx, `SELECT id,session_id,event_type,occurred_at,detail FROM event_log WHERE session_id=? AND event_type IN ('submitted','force_submitted') ORDER BY id DESC LIMIT 1`, id))
}
func (t *sessionTransaction) HistoryWatermark(ctx context.Context, id string) (domain.TimelineWatermark, error) {
	var mark domain.TimelineWatermark
	err := t.tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT MAX(id) FROM event_log WHERE session_id=?),0),COALESCE((SELECT MAX(id) FROM submission_versions WHERE session_id=?),0)`, id, id).Scan(&mark.EventID, &mark.VersionID)
	return mark, err
}

// SQLite timestamps have variable fractions; fixed-width keys preserve nanosecond ordering.
func timelineKey(column string) string {
	return `substr(` + column + `,1,19)||'.'||substr(rtrim(substr(` + column + `,21),'Z')||'000000000',1,9)||'Z'`
}
func branchAfter(after domain.TimelinePosition, kind string) int64 {
	if kind < after.Kind {
		return math.MaxInt64
	}
	if kind > after.Kind {
		return 0
	}
	return after.ID
}
func (t *sessionTransaction) HistoryPage(ctx context.Context, id string, after domain.TimelinePosition, mark domain.TimelineWatermark, limit int) ([]domain.HistoryRecord, error) {
	records := []domain.HistoryRecord{}
	key := timelineKey("saved_at")
	rows, err := t.tx.QueryContext(ctx, `SELECT id,session_id,version_seq,saved_at FROM submission_versions WHERE session_id=? AND id<=? AND (`+key+`,id)>(?,?) ORDER BY `+key+`,id LIMIT ?`, id, mark.VersionID, after.Time, branchAfter(after, "answer_version"), limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		version, err := scanVersionMetadata(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		records = append(records, domain.HistoryRecord{Version: &version})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	key = timelineKey("e.occurred_at")
	rows, err = t.tx.QueryContext(ctx, `SELECT e.id,e.session_id,e.event_type,e.occurred_at,e.detail FROM event_log e WHERE e.session_id=? AND e.id<=? AND (`+key+`,e.id)>(?,?) ORDER BY `+key+`,e.id LIMIT ?`, id, mark.EventID, after.Time, branchAfter(after, "event"), limit)
	if err != nil {
		return nil, err
	}
	events := map[int64]*domain.HistoryRecord{}
	first := len(records)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		records = append(records, domain.HistoryRecord{Event: &event, Screenshots: []domain.ScreenshotMetadata{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	low, high := int64(0), int64(0)
	for i := first; i < len(records); i++ {
		events[records[i].Event.ID] = &records[i]
		if low == 0 || records[i].Event.ID < low {
			low = records[i].Event.ID
		}
		high = max(high, records[i].Event.ID)
	}
	if len(events) > 0 {
		// A focus loss holds a series of captures; they are read separately so each event stays one history record.
		images, err := t.tx.QueryContext(ctx, `SELECT `+screenshotColumns+` FROM screenshots p JOIN sessions s ON s.session_id=p.session_id WHERE p.session_id=? AND p.event_log_id BETWEEN ? AND ? AND p.state='ready' ORDER BY p.event_log_id,p.capture_index`, id, low, high)
		if err != nil {
			return nil, err
		}
		defer images.Close()
		for images.Next() {
			image, err := scanScreenshot(images)
			if err != nil {
				return nil, err
			}
			if record := events[image.EventLogID]; record != nil {
				record.Screenshots = append(record.Screenshots, image.ScreenshotMetadata)
			}
		}
		if err := images.Err(); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(records, func(a, b domain.HistoryRecord) int { return domain.CompareTimeline(a.Position(), b.Position()) })
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}
