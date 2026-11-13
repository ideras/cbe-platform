package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/workspace"
)

// Workspaces is the database of workspace exams: the Store, with the methods the workspace package needs. They share names with the
// Store's own (Exam, CreateExam), so they live on this type.
type Workspaces struct{ *Store }

var _ workspace.Repository = Workspaces{}

// Workspaces returns the workspace view of the store.
func (s *Store) Workspaces() Workspaces { return Workspaces{s} }

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func nullTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	t, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateExam stores a workspace exam in its class. The exams row is the same as any exam's, with no exercises and kind "workspace", so
// that classes, PINs, publishing, sessions, timing and deletion work for it unchanged.
func (s Workspaces) CreateExam(ctx context.Context, exam workspace.ExamRecord) error {
	var tools any
	if exam.Tools != nil {
		raw, err := json.Marshal(exam.Tools)
		if err != nil {
			return err
		}
		tools = string(raw)
	}
	document, err := json.Marshal(exam.Document)
	if err != nil {
		return err
	}
	content, _ := json.Marshal(domain.ExamContent{Exercises: []domain.Exercise{}})
	base := domain.Exam{ID: exam.ExamID, ClassID: exam.ClassID, Title: exam.Title, PIN: exam.PIN, DurationSeconds: exam.DurationSeconds, GraceSeconds: exam.GraceSeconds,
		MinVolumePercent: exam.MinVolumePercent, CreatedAt: exam.CreatedAt, OpensAt: exam.OpensAt, ClosesAt: exam.ClosesAt}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM classes WHERE class_id = ?)`, exam.ClassID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
		insert, values := examInsert(base, domain.FreeNavigation, content, tools)
		if _, err := tx.ExecContext(ctx, insert, values...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE exams SET kind = 'workspace' WHERE exam_id = ?`, exam.ExamID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO workspace_exams(exam_id, document_json, image, starter_hash, grading_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			exam.ExamID, string(document), exam.Document.Workspace.Image, exam.StarterHash, exam.GradingHash, timestamp(exam.CreatedAt))
		return err
	})
}

// Exam reads an imported workspace exam.
func (s Workspaces) Exam(ctx context.Context, examID string) (workspace.ExamRecord, error) {
	var record workspace.ExamRecord
	var document, created string
	var opens, closes sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT e.exam_id, e.class_id, e.title, e.pin, e.duration_seconds, e.grace_seconds, e.min_volume_percent, e.opens_at, e.closes_at,
			w.document_json, w.starter_hash, w.grading_hash, w.created_at FROM exams e JOIN workspace_exams w ON w.exam_id = e.exam_id WHERE e.exam_id = ?`, examID).
		Scan(&record.ExamID, &record.ClassID, &record.Title, &record.PIN, &record.DurationSeconds, &record.GraceSeconds, &record.MinVolumePercent, &opens, &closes,
			&document, &record.StarterHash, &record.GradingHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return record, workspace.ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal([]byte(document), &record.Document); err != nil {
		return record, fmt.Errorf("decode workspace document: %w", err)
	}
	if record.CreatedAt, err = parseTime(created); err != nil {
		return record, err
	}
	if record.OpensAt, err = nullTime(opens); err != nil {
		return record, err
	}
	if record.ClosesAt, err = nullTime(closes); err != nil {
		return record, err
	}
	return record, nil
}

const workspaceSessionSelect = `SELECT s.session_id, s.exam_id, s.account_number, s.status, s.deadline_at, s.grace_until, s.ends_at,
		COALESCE(w.state, 'new'), COALESCE(w.agent_restarts, 0), w.ready_at, w.frozen_at, w.last_snapshot_at, COALESCE(w.failure, '')
	FROM sessions s JOIN workspace_exams we ON we.exam_id = s.exam_id LEFT JOIN workspace_sessions w ON w.session_id = s.session_id`

func scanWorkspaceSession(row scanner) (workspace.SessionInfo, error) {
	var info workspace.SessionInfo
	var deadline, grace string
	var ends, ready, frozen, snapshot sql.NullString
	err := row.Scan(&info.SessionID, &info.ExamID, &info.Account, &info.Status, &deadline, &grace, &ends, &info.State, &info.AgentRestarts, &ready, &frozen, &snapshot, &info.Failure)
	if errors.Is(err, sql.ErrNoRows) {
		return info, workspace.ErrNotFound
	}
	if err != nil {
		return info, err
	}
	if info.DeadlineAt, err = parseTime(deadline); err != nil {
		return info, err
	}
	if info.GraceUntil, err = parseTime(grace); err != nil {
		return info, err
	}
	if info.EndsAt, err = nullTime(ends); err != nil {
		return info, err
	}
	if info.ReadyAt, err = nullTime(ready); err != nil {
		return info, err
	}
	if info.FrozenAt, err = nullTime(frozen); err != nil {
		return info, err
	}
	info.LastSnapshotAt, err = nullTime(snapshot)
	return info, err
}

// Session reads a session of a workspace exam.
func (s Workspaces) Session(ctx context.Context, sessionID string) (workspace.SessionInfo, error) {
	return scanWorkspaceSession(s.db.QueryRowContext(ctx, workspaceSessionSelect+` WHERE s.session_id = ?`, sessionID))
}

// EnsureSession makes the workspace row of a session, if it has none, and returns the session.
func (s Workspaces) EnsureSession(ctx context.Context, sessionID string, now time.Time) (workspace.SessionInfo, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO workspace_sessions(session_id, exam_id, created_at)
			SELECT s.session_id, s.exam_id, ? FROM sessions s JOIN workspace_exams we ON we.exam_id = s.exam_id WHERE s.session_id = ?
			ON CONFLICT(session_id) DO NOTHING`, timestamp(now), sessionID)
		return err
	})
	if err != nil {
		return workspace.SessionInfo{}, err
	}
	return s.Session(ctx, sessionID)
}

// SetState moves a session along its life.
func (s Workspaces) SetState(ctx context.Context, sessionID, state, failure string, now time.Time) error {
	column := ""
	switch state {
	case workspace.StateReady:
		column = ", ready_at = COALESCE(ready_at, ?2)"
	case workspace.StateFrozen:
		column = ", frozen_at = COALESCE(frozen_at, ?2)"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_sessions SET state = ?1, failure = ?3`+column+` WHERE session_id = ?4`, state, timestamp(now), failure, sessionID)
	return err
}

// Sessions lists the workspace sessions the supervisor still watches.
func (s Workspaces) Sessions(ctx context.Context) ([]workspace.SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx, workspaceSessionSelect+` WHERE w.state IN ('provisioning', 'ready', 'frozen') ORDER BY s.session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.SessionInfo
	for rows.Next() {
		info, err := scanWorkspaceSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

func (s Workspaces) AddAgentRestart(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_sessions SET agent_restarts = agent_restarts + 1 WHERE session_id = ?`, sessionID)
	return err
}

func (s Workspaces) RecordSnapshot(ctx context.Context, snapshot workspace.SnapshotRecord) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO workspace_snapshots(session_id, reason, taken_at, manifest_hash, files, bytes, skipped) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			snapshot.SessionID, snapshot.Reason, timestamp(snapshot.TakenAt), snapshot.ManifestHash, snapshot.Files, snapshot.Bytes, snapshot.Skipped)
		if err != nil {
			return err
		}
		if id, err = result.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE workspace_sessions SET last_snapshot_at = ? WHERE session_id = ?`, timestamp(snapshot.TakenAt), snapshot.SessionID)
		return err
	})
	return id, err
}

func scanSnapshot(row scanner) (workspace.SnapshotRecord, error) {
	var snapshot workspace.SnapshotRecord
	var taken string
	err := row.Scan(&snapshot.ID, &snapshot.SessionID, &snapshot.Reason, &taken, &snapshot.ManifestHash, &snapshot.Files, &snapshot.Bytes, &snapshot.Skipped)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, workspace.ErrNotFound
	}
	if err != nil {
		return snapshot, err
	}
	snapshot.TakenAt, err = parseTime(taken)
	return snapshot, err
}

const snapshotColumns = `snapshot_id, session_id, reason, taken_at, manifest_hash, files, bytes, skipped`

func (s Workspaces) Snapshot(ctx context.Context, id int64) (workspace.SnapshotRecord, error) {
	return scanSnapshot(s.db.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM workspace_snapshots WHERE snapshot_id = ?`, id))
}

func (s Workspaces) LatestSnapshot(ctx context.Context, sessionID string) (workspace.SnapshotRecord, error) {
	return scanSnapshot(s.db.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM workspace_snapshots WHERE session_id = ? ORDER BY snapshot_id DESC LIMIT 1`, sessionID))
}

func (s Workspaces) Snapshots(ctx context.Context, sessionID string) ([]workspace.SnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+snapshotColumns+` FROM workspace_snapshots WHERE session_id = ? ORDER BY snapshot_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.SnapshotRecord
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, rows.Err()
}

func (s Workspaces) TouchSnapshot(ctx context.Context, sessionID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_sessions SET last_snapshot_at = ? WHERE session_id = ?`, timestamp(now), sessionID)
	return err
}

func (s Workspaces) Todo(ctx context.Context, sessionID string) (map[string]bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT todo_json FROM workspace_sessions WHERE session_id = ?`, sessionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workspace.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	todo := map[string]bool{}
	return todo, json.Unmarshal([]byte(raw), &todo)
}

func (s Workspaces) SetTodo(ctx context.Context, sessionID string, todo map[string]bool) error {
	raw, _ := json.Marshal(todo)
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_sessions SET todo_json = ? WHERE session_id = ?`, string(raw), sessionID)
	return err
}
