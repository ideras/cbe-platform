package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"exam-platform/internal/workspace"
)

// RecordEvent writes a record in a session's history. Workspace events go to the same table as the monitoring's, so the proctor reads
// one history; the detail is JSON.
func (s Workspaces) RecordEvent(ctx context.Context, sessionID, eventType string, at time.Time, detail any) error {
	var encoded any
	if detail != nil {
		raw, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		encoded = string(raw)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO event_log(session_id, event_type, occurred_at, detail) VALUES (?, ?, ?, ?)`, sessionID, eventType, timestamp(at), encoded)
		return err
	})
}

func (s Workspaces) RecordSubmission(ctx context.Context, submission workspace.SubmissionRecord) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO workspace_submissions(session_id, snapshot_id, kind, submitted_at, manifest_hash, files, bytes) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			submission.SessionID, submission.SnapshotID, submission.Kind, timestamp(submission.SubmittedAt), submission.ManifestHash, submission.Files, submission.Bytes)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	return id, err
}

const submissionColumns = `submission_id, session_id, snapshot_id, kind, submitted_at, manifest_hash, files, bytes`

func scanSubmission(row scanner) (workspace.SubmissionRecord, error) {
	var submission workspace.SubmissionRecord
	var submitted string
	err := row.Scan(&submission.ID, &submission.SessionID, &submission.SnapshotID, &submission.Kind, &submitted, &submission.ManifestHash, &submission.Files, &submission.Bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return submission, workspace.ErrNotFound
	}
	if err != nil {
		return submission, err
	}
	submission.SubmittedAt, err = parseTime(submitted)
	return submission, err
}

func (s Workspaces) Submission(ctx context.Context, id int64) (workspace.SubmissionRecord, error) {
	return scanSubmission(s.db.QueryRowContext(ctx, `SELECT `+submissionColumns+` FROM workspace_submissions WHERE submission_id = ?`, id))
}

func (s Workspaces) Submissions(ctx context.Context, sessionID string) ([]workspace.SubmissionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+submissionColumns+` FROM workspace_submissions WHERE session_id = ? ORDER BY submission_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.SubmissionRecord
	for rows.Next() {
		submission, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, submission)
	}
	return out, rows.Err()
}

func (s Workspaces) RecordTerminal(ctx context.Context, terminal workspace.TerminalRecord) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO workspace_terminals(session_id, term, kind, started_at, cast_file) VALUES (?, ?, ?, ?, ?)`,
			terminal.SessionID, terminal.Term, terminal.Kind, timestamp(terminal.StartedAt), terminal.CastFile)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	return id, err
}

func (s Workspaces) EndTerminal(ctx context.Context, id int64, endedAt time.Time, exitCode *int, bytes int64, truncated bool) error {
	var code any
	if exitCode != nil {
		code = *exitCode
	}
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_terminals SET ended_at = ?, exit_code = ?, bytes = ?, truncated = ? WHERE terminal_id = ?`, timestamp(endedAt), code, bytes, truncated, id)
	return err
}

const terminalColumns = `terminal_id, session_id, term, kind, started_at, ended_at, exit_code, bytes, truncated, cast_file`

func scanTerminal(row scanner) (workspace.TerminalRecord, error) {
	var terminal workspace.TerminalRecord
	var started string
	var ended sql.NullString
	var code sql.NullInt64
	err := row.Scan(&terminal.ID, &terminal.SessionID, &terminal.Term, &terminal.Kind, &started, &ended, &code, &terminal.Bytes, &terminal.Truncated, &terminal.CastFile)
	if errors.Is(err, sql.ErrNoRows) {
		return terminal, workspace.ErrNotFound
	}
	if err != nil {
		return terminal, err
	}
	if terminal.StartedAt, err = parseTime(started); err != nil {
		return terminal, err
	}
	if terminal.EndedAt, err = nullTime(ended); err != nil {
		return terminal, err
	}
	if code.Valid {
		value := int(code.Int64)
		terminal.ExitCode = &value
	}
	return terminal, nil
}

func (s Workspaces) Terminal(ctx context.Context, id int64) (workspace.TerminalRecord, error) {
	return scanTerminal(s.db.QueryRowContext(ctx, `SELECT `+terminalColumns+` FROM workspace_terminals WHERE terminal_id = ?`, id))
}

func (s Workspaces) Terminals(ctx context.Context, sessionID string) ([]workspace.TerminalRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+terminalColumns+` FROM workspace_terminals WHERE session_id = ? ORDER BY terminal_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.TerminalRecord
	for rows.Next() {
		terminal, err := scanTerminal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, terminal)
	}
	return out, rows.Err()
}

func (s Workspaces) Roster(ctx context.Context, examID string) ([]workspace.RosterRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.session_id, s.account_number, w.state, w.agent_restarts, w.last_snapshot_at, w.todo_json,
			(SELECT count(*) FROM workspace_submissions u WHERE u.session_id = s.session_id),
			(SELECT submitted_at FROM workspace_submissions u WHERE u.session_id = s.session_id ORDER BY submission_id DESC LIMIT 1),
			COALESCE((SELECT kind FROM workspace_submissions u WHERE u.session_id = s.session_id ORDER BY submission_id DESC LIMIT 1), ''),
			(SELECT max(e.occurred_at) FROM event_log e WHERE e.session_id = s.session_id AND (e.event_type GLOB 'workspace.*' OR e.event_type GLOB 'file.*' OR e.event_type GLOB 'pty.*' OR e.event_type GLOB 'run.*' OR e.event_type = 'todo.set' OR e.event_type = 'paste.blocked'))
		FROM sessions s JOIN workspace_sessions w ON w.session_id = s.session_id WHERE s.exam_id = ? ORDER BY s.account_number`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.RosterRow
	for rows.Next() {
		var row workspace.RosterRow
		var snapshot, submitted, last sql.NullString
		var todo, kind string
		if err := rows.Scan(&row.SessionID, &row.Account, &row.State, &row.AgentRestarts, &snapshot, &todo, &row.Submissions, &submitted, &kind, &last); err != nil {
			return nil, err
		}
		if row.LastSnapshotAt, err = nullTime(snapshot); err != nil {
			return nil, err
		}
		if row.LastSubmission, err = nullTime(submitted); err != nil {
			return nil, err
		}
		if row.LastEvent, err = nullTime(last); err != nil {
			return nil, err
		}
		row.Final = kind == workspace.SubmissionFinal
		row.Todo = map[string]bool{}
		_ = json.Unmarshal([]byte(todo), &row.Todo)
		out = append(out, row)
	}
	return out, rows.Err()
}
