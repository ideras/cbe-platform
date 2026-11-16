package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"exam-platform/internal/workspace"
)

func (s Workspaces) CreateGrading(ctx context.Context, session string, submission int64, now time.Time) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO workspace_gradings(session_id, submission_id, state, queued_at) VALUES (?, ?, 'queued', ?)`, session, submission, timestamp(now))
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	return id, err
}

func (s Workspaces) StartGrading(ctx context.Context, id int64, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_gradings SET state = 'running', started_at = ? WHERE grading_id = ? AND state = 'queued'`, timestamp(now), id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return workspace.ErrNotFound
		}
		return nil
	})
}

func (s Workspaces) FinishGrading(ctx context.Context, record workspace.GradingRecord) error {
	skipped, _ := json.Marshal(append([]string{}, record.Skipped...))
	var code any
	if record.ExitCode != nil {
		code = *record.ExitCode
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_gradings SET state = ?, failure = ?, failure_text = ?, finished_at = ?, exit_code = ?, timed_out = ?, duration_ms = ?,
				stdout = ?, stderr = ?, stdout_truncated = ?, stderr_truncated = ?, skipped = ?, points = ?, max_points = ? WHERE grading_id = ? AND state IN ('queued', 'running')`,
			record.State, record.Failure, record.FailureText, timestamp(*record.FinishedAt), code, record.TimedOut, record.DurationMS,
			record.Stdout, record.Stderr, record.StdoutTruncated, record.StderrTruncated, string(skipped), record.Points, record.Max, record.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return workspace.ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM workspace_grading_results WHERE grading_id = ?`, record.ID); err != nil {
			return err
		}
		for _, item := range record.Results {
			if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_grading_results(grading_id, todo_id, known, passed, points, log) VALUES (?, ?, ?, ?, ?, ?)`,
				record.ID, item.TodoID, item.Known, item.Passed, item.Points, item.Log); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s Workspaces) ConfirmGrading(ctx context.Context, id int64, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var session, state string
		if err := tx.QueryRowContext(ctx, `SELECT session_id, state FROM workspace_gradings WHERE grading_id = ?`, id).Scan(&session, &state); errors.Is(err, sql.ErrNoRows) {
			return workspace.ErrNotFound
		} else if err != nil {
			return err
		}
		if state != workspace.GradingDone {
			return workspace.ErrNotGraded
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workspace_gradings SET confirmed_at = NULL WHERE session_id = ?`, session); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE workspace_gradings SET confirmed_at = ? WHERE grading_id = ?`, timestamp(now), id)
		return err
	})
}

func (s Workspaces) UnconfirmGrading(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_gradings SET confirmed_at = NULL WHERE grading_id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return workspace.ErrNotFound
		}
		return nil
	})
}

func (s Workspaces) InterruptGradings(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_gradings SET state = 'failed', failure = ?, failure_text = ?, finished_at = ? WHERE state IN ('queued', 'running')`,
			workspace.FailInterrupted, "the exam server stopped before this run ended; run it again", timestamp(now))
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		count = int(n)
		return nil
	})
	return count, err
}

const gradingColumns = `grading_id, session_id, submission_id, state, failure, failure_text, queued_at, started_at, finished_at, exit_code, timed_out, duration_ms,
	stdout_truncated, stderr_truncated, skipped, points, max_points, confirmed_at`

// scanGrading reads a row of gradingColumns, and then the two output columns when withOutput.
func scanGrading(row scanner, withOutput bool) (workspace.GradingRecord, error) {
	var g workspace.GradingRecord
	var queued, skipped string
	var started, finished, confirmed sql.NullString
	var code sql.NullInt64
	var points, max sql.NullFloat64
	dest := []any{&g.ID, &g.SessionID, &g.SubmissionID, &g.State, &g.Failure, &g.FailureText, &queued, &started, &finished, &code, &g.TimedOut, &g.DurationMS,
		&g.StdoutTruncated, &g.StderrTruncated, &skipped, &points, &max, &confirmed}
	if withOutput {
		dest = append(dest, &g.Stdout, &g.Stderr)
	}
	if err := row.Scan(dest...); errors.Is(err, sql.ErrNoRows) {
		return g, workspace.ErrNotFound
	} else if err != nil {
		return g, err
	}
	var err error
	if g.QueuedAt, err = parseTime(queued); err != nil {
		return g, err
	}
	if g.StartedAt, err = nullTime(started); err != nil {
		return g, err
	}
	if g.FinishedAt, err = nullTime(finished); err != nil {
		return g, err
	}
	if g.ConfirmedAt, err = nullTime(confirmed); err != nil {
		return g, err
	}
	if code.Valid {
		value := int(code.Int64)
		g.ExitCode = &value
	}
	if points.Valid {
		g.Points = &points.Float64
	}
	if max.Valid {
		g.Max = &max.Float64
	}
	_ = json.Unmarshal([]byte(skipped), &g.Skipped)
	return g, nil
}

func (s Workspaces) resultsOf(ctx context.Context, id int64) ([]workspace.GradingResult, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT todo_id, known, passed, points, log FROM workspace_grading_results WHERE grading_id = ? ORDER BY todo_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.GradingResult
	for rows.Next() {
		var item workspace.GradingResult
		if err := rows.Scan(&item.TodoID, &item.Known, &item.Passed, &item.Points, &item.Log); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s Workspaces) Grading(ctx context.Context, id int64) (workspace.GradingRecord, error) {
	g, err := scanGrading(s.db.QueryRowContext(ctx, `SELECT `+gradingColumns+`, stdout, stderr FROM workspace_gradings WHERE grading_id = ?`, id), true)
	if err != nil {
		return g, err
	}
	g.Results, err = s.resultsOf(ctx, id)
	return g, err
}

func (s Workspaces) Gradings(ctx context.Context, session string) ([]workspace.GradingRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+gradingColumns+` FROM workspace_gradings WHERE session_id = ? ORDER BY grading_id DESC`, session)
	if err != nil {
		return nil, err
	}
	var out []workspace.GradingRecord
	for rows.Next() {
		g, err := scanGrading(rows, false)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		if out[i].Results, err = s.resultsOf(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// gradesOf reads, for every student of an exam who has been graded, the grade that stands: the confirmed grading, or else the latest.
// GradeSheet lists the students of the exam's class, and any who has a session of the exam, with the grade each has as it stands and the TODO
// results of that run.
func (s Workspaces) GradeSheet(ctx context.Context, examID string) ([]workspace.GradeSheetRow, error) {
	grades, err := s.gradesOf(ctx, examID)
	if err != nil {
		return nil, err
	}
	// The rows are read to the end and closed before the results are read: the database has one connection.
	rows, err := s.db.QueryContext(ctx, `SELECT st.account_number, st.name, st.email, COALESCE(s.session_id, ''), COALESCE(w.todo_json, '{}'),
			(SELECT count(*) FROM workspace_submissions u WHERE u.session_id = s.session_id),
			(SELECT submitted_at FROM workspace_submissions u WHERE u.session_id = s.session_id ORDER BY submission_id DESC LIMIT 1),
			COALESCE((SELECT kind FROM workspace_submissions u WHERE u.session_id = s.session_id ORDER BY submission_id DESC LIMIT 1), '')
		FROM students st
		LEFT JOIN sessions s ON s.exam_id = ? AND s.account_number = st.account_number
		LEFT JOIN workspace_sessions w ON w.session_id = s.session_id
		WHERE st.account_number IN (SELECT cs.account_number FROM class_students cs JOIN exams e ON e.class_id = cs.class_id WHERE e.exam_id = ?
			UNION SELECT account_number FROM sessions WHERE exam_id = ?)
		ORDER BY st.name COLLATE NOCASE, st.account_number`, examID, examID, examID)
	if err != nil {
		return nil, err
	}
	var out []workspace.GradeSheetRow
	for rows.Next() {
		var row workspace.GradeSheetRow
		var submitted sql.NullString
		var todo, kind string
		if err := rows.Scan(&row.Account, &row.Name, &row.Email, &row.SessionID, &todo, &row.Submissions, &submitted, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		if row.LastSubmission, err = nullTime(submitted); err != nil {
			rows.Close()
			return nil, err
		}
		row.Final = kind == workspace.SubmissionFinal
		ticks := map[string]bool{}
		_ = json.Unmarshal([]byte(todo), &ticks)
		for _, done := range ticks {
			if done {
				row.Ticked++
			}
		}
		row.Grade = grades[row.SessionID]
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		if g := out[i].Grade; g != nil && g.State == workspace.GradingDone {
			if out[i].Results, err = s.resultsOf(ctx, g.GradingID); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (s Workspaces) gradesOf(ctx context.Context, examID string) (map[string]*workspace.GradeSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id, grading_id, state, failure, points, max_points, confirmed_at, COALESCE(finished_at, started_at, queued_at) FROM (
			SELECT g.*, ROW_NUMBER() OVER (PARTITION BY g.session_id ORDER BY (g.confirmed_at IS NOT NULL) DESC, g.grading_id DESC) AS rank
			FROM workspace_gradings g JOIN sessions s ON s.session_id = g.session_id WHERE s.exam_id = ?) WHERE rank = 1`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*workspace.GradeSummary{}
	for rows.Next() {
		var session, at string
		var grade workspace.GradeSummary
		var points, max sql.NullFloat64
		var confirmed sql.NullString
		if err := rows.Scan(&session, &grade.GradingID, &grade.State, &grade.Failure, &points, &max, &confirmed, &at); err != nil {
			return nil, err
		}
		if points.Valid {
			grade.Points = &points.Float64
		}
		if max.Valid {
			grade.Max = &max.Float64
		}
		grade.Confirmed = confirmed.Valid
		if grade.At, err = parseTime(at); err != nil {
			return nil, err
		}
		out[session] = &grade
	}
	return out, rows.Err()
}
