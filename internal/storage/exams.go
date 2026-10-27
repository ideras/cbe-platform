package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/shared/calc"
)

func timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func optionalTimestamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return timestamp(*t)
}

// CreateExam accepts a server-assigned ID; admission and content validation belong to the service. The exam goes into
// exam.ClassID, which must name a stored class (domain.ErrNotFound otherwise).
func (s *Store) CreateExam(ctx context.Context, exam domain.Exam) error {
	content, err := json.Marshal(exam.Content)
	if err != nil {
		return err
	}
	var tools any
	if exam.Tools != nil {
		raw, err := json.Marshal(exam.Tools)
		if err != nil {
			return err
		}
		tools = string(raw)
	}
	navigation := exam.Navigation
	if navigation == "" {
		navigation = domain.FreeNavigation
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM classes WHERE class_id = ?)`, exam.ClassID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
		// A database the tests seed with an earlier schema has no tools column yet; the exam is stored without
		// its tools there, exactly as exams written before the column existed already are.
		insert, values := examInsert(exam, navigation, content, tools)
		if tools != nil {
			var hasTools int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('exams') WHERE name = 'tools_json'`).Scan(&hasTools); err != nil {
				return err
			}
			if hasTools == 0 {
				insert, values = examInsert(exam, navigation, content, nil)
			}
		}
		_, err := tx.ExecContext(ctx, insert, values...)
		return err
	})
}

// examInsert builds the INSERT of an exam and its arguments; tools may be dropped when the schema predates it.
func examInsert(exam domain.Exam, navigation domain.Navigation, content []byte, tools any) (string, []any) {
	columns, marks, values := "exam_id, class_id, title, pin, duration_seconds, grace_seconds, content_json, created_at, opens_at, closes_at, min_volume_percent, navigation, source_markdown", "?, ?, ?, ?, ?, ?, jsonb(?), ?, ?, ?, ?, ?, ?",
		[]any{exam.ID, exam.ClassID, exam.Title, exam.PIN, exam.DurationSeconds, exam.GraceSeconds, string(content), timestamp(exam.CreatedAt), optionalTimestamp(exam.OpensAt), optionalTimestamp(exam.ClosesAt), exam.MinVolumePercent, navigation, exam.SourceMarkdown}
	if tools != nil {
		columns += ", tools_json"
		marks += ", ?"
		values = append(values, tools)
	}
	return `INSERT INTO exams (` + columns + `) VALUES (` + marks + `)`, values
}

// DeleteExam removes an exam nobody has taken and that is not the one students are being admitted to. An exam that has
// sessions is never removed, because its answers and history belong to it.
func (s *Store) DeleteExam(ctx context.Context, id string) (domain.DeletedExam, error) {
	deleted := domain.DeletedExam{ID: id}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT title FROM exams WHERE exam_id = ?`, id).Scan(&deleted.Title); errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		var inUse bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM current_exam WHERE exam_id = ?1) OR EXISTS(SELECT 1 FROM sessions WHERE exam_id = ?1)`, id).Scan(&inUse); err != nil {
			return err
		}
		if inUse {
			return domain.ErrExamInUse
		}
		// Purging screenshots of an exam nobody took still leaves a retention mark behind.
		for _, statement := range []string{`DELETE FROM screenshot_retention WHERE exam_id = ?`, `DELETE FROM exams WHERE exam_id = ?`} {
			if _, err := tx.ExecContext(ctx, statement, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.DeletedExam{}, err
	}
	return deleted, nil
}

const examColumns = `exam_id, class_id, title, pin, duration_seconds, grace_seconds, json(content_json), created_at, opens_at, closes_at, min_volume_percent, navigation, policy_json, tools_json`

func (s *Store) Exam(ctx context.Context, id string) (domain.Exam, error) {
	return scanExam(s.db.QueryRowContext(ctx, `SELECT `+examColumns+` FROM exams WHERE exam_id = ?`, id))
}

// ExamDetail reads an exam for the proctor, with its class name and the Markdown source when one was stored. It never reads
// the PIN.
func (s *Store) ExamDetail(ctx context.Context, id string) (domain.ExamDetail, error) {
	var detail domain.ExamDetail
	var content string
	var source, tools sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT e.exam_id, e.class_id, c.name, e.title, e.navigation, json(e.content_json), e.source_markdown, e.tools_json
		FROM exams e JOIN classes c ON c.class_id = e.class_id WHERE e.exam_id = ?`, id).
		Scan(&detail.ID, &detail.ClassID, &detail.ClassName, &detail.Title, &detail.Navigation, &content, &source, &tools)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, domain.ErrNotFound
	}
	if err != nil {
		return detail, err
	}
	if err := json.Unmarshal([]byte(content), &detail.Content); err != nil {
		return detail, fmt.Errorf("decode exam content: %w", err)
	}
	if tools.Valid && tools.String != "" {
		detail.Tools = &calc.Tools{}
		if err := json.Unmarshal([]byte(tools.String), detail.Tools); err != nil {
			return detail, fmt.Errorf("decode exam tools: %w", err)
		}
	}
	if source.Valid {
		detail.SourceMarkdown = &source.String
	}
	return detail, nil
}

func scanExam(row scanner) (domain.Exam, error) {
	var exam domain.Exam
	var content, created string
	var policy, tools sql.NullString
	var opens, closes sql.NullString
	err := row.Scan(&exam.ID, &exam.ClassID, &exam.Title, &exam.PIN, &exam.DurationSeconds, &exam.GraceSeconds, &content, &created, &opens, &closes, &exam.MinVolumePercent, &exam.Navigation, &policy, &tools)
	if errors.Is(err, sql.ErrNoRows) {
		return exam, domain.ErrNotFound
	}
	if err != nil {
		return exam, err
	}
	if err := json.Unmarshal([]byte(content), &exam.Content); err != nil {
		return exam, fmt.Errorf("decode exam content: %w", err)
	}
	if tools.Valid && tools.String != "" {
		exam.Tools = &calc.Tools{}
		if err := json.Unmarshal([]byte(tools.String), exam.Tools); err != nil {
			return exam, fmt.Errorf("decode exam tools: %w", err)
		}
	}
	if policy.Valid && policy.String != "" {
		if err := json.Unmarshal([]byte(policy.String), &exam.Policy); err != nil {
			return exam, err
		}
	}
	if exam.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return exam, err
	}
	for _, field := range []struct {
		src sql.NullString
		dst **time.Time
	}{{opens, &exam.OpensAt}, {closes, &exam.ClosesAt}} {
		if field.src.Valid {
			t, err := time.Parse(time.RFC3339Nano, field.src.String)
			if err != nil {
				return exam, err
			}
			*field.dst = &t
		}
	}
	return exam, nil
}
