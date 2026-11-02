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

// DeleteExam removes an exam and, with it, every session students had in it: their answers, their events and their screenshot
// records. It is one transaction, so an exam is either gone with all of that or untouched. The exam is refused (domain.ErrExamInUse)
// while it is the one students are admitted to or while any student is still working in it or being finalized.
//
// An exam that students took is deleted only for a caller who says how many sessions it has: submissions is that number, and
// anything else is domain.ErrSubmissionsChanged, so nobody deletes answers they have not been told about. 0 means "nobody took
// it". The screenshot files cannot be removed inside the transaction; their paths are listed in deleted_screenshot_files, which
// the screenshot service empties.
func (s *Store) DeleteExam(ctx context.Context, id string, submissions int) (domain.DeletedExam, error) {
	deleted := domain.DeletedExam{ID: id}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT title FROM exams WHERE exam_id = ?`, id).Scan(&deleted.Title); errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		var published bool
		var open int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM current_exam WHERE exam_id = ?1),
			(SELECT count(*) FROM sessions WHERE exam_id = ?1 AND status IN ('active', 'grace')),
			(SELECT count(*) FROM sessions WHERE exam_id = ?1)`, id).Scan(&published, &open, &deleted.Sessions); err != nil {
			return err
		}
		if published || open > 0 {
			return domain.ErrExamInUse
		}
		if deleted.Sessions != submissions {
			return domain.ErrSubmissionsChanged
		}
		// A session names its final version and a version names its session, so neither can go first: the references are
		// checked when the transaction commits, by when both are gone.
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		// Answer versions are append-only except for this: the guard on them lets them go while their exam is listed here, and
		// the list is emptied below, so the permission cannot outlive the transaction.
		if _, err := tx.ExecContext(ctx, `INSERT INTO exam_being_deleted(exam_id) VALUES (?)`, id); err != nil {
			return err
		}
		mine := `(SELECT session_id FROM sessions WHERE exam_id = ?)`
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deleted_screenshot_files(file_path) SELECT file_path FROM screenshots WHERE session_id IN `+mine, id); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM screenshots WHERE session_id IN `+mine, id).Scan(&deleted.Screenshots); err != nil {
			return err
		}
		// Purging the screenshots of an exam leaves a retention mark behind, with or without sessions.
		for _, statement := range []string{
			`DELETE FROM screenshots WHERE session_id IN ` + mine,
			`DELETE FROM event_log WHERE session_id IN ` + mine,
			`DELETE FROM submission_versions WHERE session_id IN ` + mine,
			`DELETE FROM sessions WHERE exam_id = ?`,
			`DELETE FROM screenshot_retention WHERE exam_id = ?`,
			`DELETE FROM exam_being_deleted WHERE exam_id = ?`,
			`DELETE FROM exams WHERE exam_id = ?`,
		} {
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

const examColumns = `exam_id, class_id, title, pin, duration_seconds, grace_seconds, json(content_json), created_at, opens_at, closes_at, min_volume_percent, navigation, policy_json, tools_json, ends_at, extra_grace_seconds`

func (s *Store) Exam(ctx context.Context, id string) (domain.Exam, error) {
	return scanExam(s.db.QueryRowContext(ctx, `SELECT `+examColumns+` FROM exams WHERE exam_id = ?`, id))
}

// ExamDetail reads an exam for the proctor, with its class name and the Markdown source when one was stored. It never reads
// the PIN.
func (s *Store) ExamDetail(ctx context.Context, id string) (domain.ExamDetail, error) {
	var detail domain.ExamDetail
	var content string
	var source, tools, ends sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT e.exam_id, e.class_id, c.name, e.title, e.navigation, json(e.content_json), e.source_markdown, e.tools_json, e.ends_at
		FROM exams e JOIN classes c ON c.class_id = e.class_id WHERE e.exam_id = ?`, id).
		Scan(&detail.ID, &detail.ClassID, &detail.ClassName, &detail.Title, &detail.Navigation, &content, &source, &tools, &ends)
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
	if detail.EndsAt, err = nullableTime(ends); err != nil {
		return detail, err
	}
	if source.Valid {
		detail.SourceMarkdown = &source.String
	}
	return detail, nil
}

// ExamPIN reads the PIN of one exam and nothing else, for the one administrator call that shows it to the proctor who is about
// to share it with the class. No listing and no other read returns the PIN.
func (s *Store) ExamPIN(ctx context.Context, id string) (string, error) {
	var pin string
	err := s.db.QueryRowContext(ctx, `SELECT pin FROM exams WHERE exam_id = ?`, id).Scan(&pin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return pin, nil
}

func scanExam(row scanner) (domain.Exam, error) {
	var exam domain.Exam
	var content, created string
	var policy, tools sql.NullString
	var opens, closes, ends sql.NullString
	err := row.Scan(&exam.ID, &exam.ClassID, &exam.Title, &exam.PIN, &exam.DurationSeconds, &exam.GraceSeconds, &content, &created, &opens, &closes, &exam.MinVolumePercent, &exam.Navigation, &policy, &tools, &ends, &exam.ExtraGraceSeconds)
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
	}{{opens, &exam.OpensAt}, {closes, &exam.ClosesAt}, {ends, &exam.EndsAt}} {
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
