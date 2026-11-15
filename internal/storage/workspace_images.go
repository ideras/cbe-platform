package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"exam-platform/internal/workspace"
)

// CreateImageBuild queues a build of an exam's image bundle, unless one is waiting or going for the exam.
func (s Workspaces) CreateImageBuild(ctx context.Context, examID, bundleHash string, rebuild bool, now time.Time) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_image_builds WHERE exam_id = ? AND state IN ('queued', 'running'))`, examID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return workspace.ErrBuildBusy
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO workspace_image_builds(exam_id, bundle_hash, state, rebuild, queued_at) VALUES (?, ?, 'queued', ?, ?)`, examID, bundleHash, rebuild, timestamp(now))
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	return id, err
}

func (s Workspaces) StartImageBuild(ctx context.Context, id int64, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_image_builds SET state = 'running', started_at = ? WHERE build_id = ? AND state = 'queued'`, timestamp(now), id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return workspace.ErrNotFound
		}
		return nil
	})
}

func (s Workspaces) FinishImageBuild(ctx context.Context, record workspace.ImageBuildRecord) error {
	warnings, _ := json.Marshal(append([]string{}, record.Warnings...))
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_image_builds SET state = ?, failure = ?, message = ?, cached = ?, hash = ?, image_id = ?, base_name = ?, base_digest = ?,
				warnings = ?, log = ?, duration_ms = ?, finished_at = ? WHERE build_id = ? AND state IN ('queued', 'running')`,
			record.State, record.Failure, record.Message, record.Cached, record.Hash, record.ImageID, record.BaseName, record.BaseDigest, string(warnings), record.Log, record.DurationMS,
			timestamp(*record.FinishedAt), record.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return workspace.ErrNotFound
		}
		return nil
	})
}

func (s Workspaces) InterruptImageBuilds(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workspace_image_builds SET state = 'failed', failure = ?, message = ?, finished_at = ? WHERE state IN ('queued', 'running')`,
			workspace.BuildFailInterrupted, "the exam server stopped before this build ended; build the image again", timestamp(now))
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		count = int(n)
		return nil
	})
	return count, err
}

const imageBuildColumns = `build_id, exam_id, bundle_hash, state, rebuild, failure, message, cached, hash, image_id, base_name, base_digest, warnings, duration_ms, queued_at, started_at, finished_at`

// scanImageBuild reads a row of imageBuildColumns, and then the log column when withLog.
func scanImageBuild(row scanner, withLog bool) (workspace.ImageBuildRecord, error) {
	var b workspace.ImageBuildRecord
	var warnings, queued string
	var started, finished sql.NullString
	dest := []any{&b.ID, &b.ExamID, &b.BundleHash, &b.State, &b.Rebuild, &b.Failure, &b.Message, &b.Cached, &b.Hash, &b.ImageID, &b.BaseName, &b.BaseDigest, &warnings, &b.DurationMS, &queued, &started, &finished}
	if withLog {
		dest = append(dest, &b.Log)
	}
	err := row.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return b, workspace.ErrNotFound
	}
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal([]byte(warnings), &b.Warnings); err != nil {
		return b, err
	}
	if b.QueuedAt, err = parseTime(queued); err != nil {
		return b, err
	}
	if b.StartedAt, err = nullTime(started); err != nil {
		return b, err
	}
	if b.FinishedAt, err = nullTime(finished); err != nil {
		return b, err
	}
	return b, nil
}

func (s Workspaces) ImageBuild(ctx context.Context, id int64) (workspace.ImageBuildRecord, error) {
	return scanImageBuild(s.db.QueryRowContext(ctx, `SELECT `+imageBuildColumns+`, log FROM workspace_image_builds WHERE build_id = ?`, id), true)
}

func (s Workspaces) ImageBuilds(ctx context.Context, examID string, limit int) ([]workspace.ImageBuildRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+imageBuildColumns+` FROM workspace_image_builds WHERE exam_id = ? ORDER BY build_id DESC LIMIT ?`, examID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []workspace.ImageBuildRecord{}
	for rows.Next() {
		b, err := scanImageBuild(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s Workspaces) LatestImageBuild(ctx context.Context, examID, bundleHash string) (workspace.ImageBuildRecord, error) {
	return scanImageBuild(s.db.QueryRowContext(ctx, `SELECT `+imageBuildColumns+` FROM workspace_image_builds WHERE exam_id = ? AND bundle_hash = ? AND state = 'ok' ORDER BY build_id DESC LIMIT 1`, examID, bundleHash), false)
}

// ImageSummaries is two queries however many exams there are: the exams, and the latest build of each.
func (s Workspaces) ImageSummaries(ctx context.Context) ([]workspace.ImageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.exam_id, w.image, w.image_hash, COALESCE(json_array_length(w.document_json, '$.content.todos'), 0),
			EXISTS(SELECT 1 FROM workspace_image_builds b WHERE b.exam_id = w.exam_id AND b.bundle_hash = w.image_hash AND b.state = 'ok')
		FROM workspace_exams w ORDER BY w.created_at DESC`)
	if err != nil {
		return nil, err
	}
	var out []workspace.ImageSummary
	index := map[string]int{}
	for rows.Next() {
		var summary workspace.ImageSummary
		if err := rows.Scan(&summary.ExamID, &summary.Image, &summary.ImageHash, &summary.Todos, &summary.HasGood); err != nil {
			rows.Close()
			return nil, err
		}
		index[summary.ExamID] = len(out)
		out = append(out, summary)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// The latest build of each exam: the rows of the newest build_id per exam.
	latest, err := s.db.QueryContext(ctx, `SELECT `+imageBuildColumns+` FROM workspace_image_builds WHERE build_id IN (SELECT MAX(build_id) FROM workspace_image_builds GROUP BY exam_id)`)
	if err != nil {
		return nil, err
	}
	defer latest.Close()
	for latest.Next() {
		build, err := scanImageBuild(latest, false)
		if err != nil {
			return nil, err
		}
		if i, found := index[build.ExamID]; found {
			out[i].Latest = &build
		}
	}
	return out, latest.Err()
}

// UpdateImageBuildLog keeps the log of a running build up to date in the database, so that a server that stops in the middle leaves it there.
func (s Workspaces) UpdateImageBuildLog(ctx context.Context, id int64, log string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workspace_image_builds SET log = ? WHERE build_id = ? AND state = 'running'`, log, id)
	return err
}
