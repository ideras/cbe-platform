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
	"exam-platform/shared/calc"
)

type publishTransaction struct{ tx *sql.Tx }

func (s *Store) WithPublishTransaction(ctx context.Context, fn func(service.PublishTransaction) error) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return fn(&publishTransaction{tx: tx}) })
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// currentExam returns the published exam and the class it belongs to, or "" for both when admission is closed.
func currentExam(ctx context.Context, q queryRower) (string, string, error) {
	var examID, classID string
	err := q.QueryRowContext(ctx, `SELECT ce.exam_id, e.class_id FROM current_exam ce JOIN exams e ON e.exam_id = ce.exam_id WHERE ce.singleton = 1`).Scan(&examID, &classID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return examID, classID, err
}

func (t *sessionTransaction) CurrentExam(ctx context.Context) (string, string, error) {
	return currentExam(ctx, t.tx)
}

func (t *publishTransaction) CurrentExam(ctx context.Context) (string, string, error) {
	return currentExam(ctx, t.tx)
}

func (t *publishTransaction) Exam(ctx context.Context, id string) (domain.Exam, error) {
	return scanExam(t.tx.QueryRowContext(ctx, `SELECT `+examColumns+` FROM exams WHERE exam_id = ?`, id))
}

// SetCurrentExam publishes examID; students of the exam's class are admitted to it.
func (t *publishTransaction) SetCurrentExam(ctx context.Context, examID string, now time.Time) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO current_exam(singleton, exam_id, published_at) VALUES (1, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET exam_id = excluded.exam_id, published_at = excluded.published_at`, examID, timestamp(now))
	return err
}

func (t *publishTransaction) ClearCurrentExam(ctx context.Context) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM current_exam WHERE singleton = 1`)
	return err
}

func (t *publishTransaction) ActiveSessions(ctx context.Context, examID string, now time.Time) (int, error) {
	var count int
	err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE exam_id = ? AND status IN ('active', 'grace') AND grace_until > ?`, examID, timestamp(now)).Scan(&count)
	return count, err
}

func (t *publishTransaction) ListExams(ctx context.Context, now time.Time, limit int) ([]domain.ExamSummary, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT e.exam_id, e.class_id, c.name, e.title, COALESCE(json_array_length(e.content_json, '$.exercises'), 0), e.navigation, e.duration_seconds, e.grace_seconds,
			e.min_volume_percent, e.created_at, e.opens_at, e.closes_at, e.tools_json,
			(SELECT count(*) FROM sessions s WHERE s.exam_id = e.exam_id AND s.status IN ('active', 'grace') AND s.grace_until > ?),
			(SELECT count(*) FROM sessions s WHERE s.exam_id = e.exam_id),
			(SELECT count(*) FROM sessions s WHERE s.exam_id = e.exam_id AND s.locked_at IS NOT NULL AND s.status IN ('active', 'grace') AND s.grace_until > ?)
		FROM exams e JOIN classes c ON c.class_id = e.class_id ORDER BY e.created_at DESC, e.exam_id LIMIT ?`, timestamp(now), timestamp(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	exams := []domain.ExamSummary{}
	for rows.Next() {
		var exam domain.ExamSummary
		var created string
		var opens, closes, tools sql.NullString
		if err := rows.Scan(&exam.ID, &exam.ClassID, &exam.ClassName, &exam.Title, &exam.ExerciseCount, &exam.Navigation, &exam.DurationSeconds, &exam.GraceSeconds, &exam.MinVolumePercent, &created, &opens, &closes, &tools, &exam.ActiveSessions, &exam.TotalSessions, &exam.LockedSessions); err != nil {
			return nil, err
		}
		if tools.Valid && tools.String != "" {
			exam.Tools = &calc.Tools{}
			if err := json.Unmarshal([]byte(tools.String), exam.Tools); err != nil {
				return nil, fmt.Errorf("decode exam tools: %w", err)
			}
		}
		if exam.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		for _, field := range []struct {
			src sql.NullString
			dst **time.Time
		}{{opens, &exam.OpensAt}, {closes, &exam.ClosesAt}} {
			if field.src.Valid {
				t, err := time.Parse(time.RFC3339Nano, field.src.String)
				if err != nil {
					return nil, err
				}
				*field.dst = &t
			}
		}
		exams = append(exams, exam)
	}
	return exams, rows.Err()
}
