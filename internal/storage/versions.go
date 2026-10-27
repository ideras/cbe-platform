package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"exam-platform/internal/domain"
)

// The lifecycle service must check server time in this same transaction before appending.
func (s *Store) appendVersion(ctx context.Context, tx *sql.Tx, sessionID string, answers domain.Answers, now time.Time) (domain.Version, error) {
	if answers == nil {
		answers = domain.Answers{}
	}
	encoded, err := json.Marshal(answers)
	if err != nil {
		return domain.Version{}, err
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_seq), 0) + 1 FROM submission_versions WHERE session_id = ?", sessionID).Scan(&seq); err != nil {
		return domain.Version{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO submission_versions(session_id, version_seq, answers_json, saved_at) VALUES (?, ?, jsonb(?), ?)`, sessionID, seq, string(encoded), timestamp(now))
	if err != nil {
		return domain.Version{}, err
	}
	id, err := result.LastInsertId()
	return domain.Version{ID: id, SessionID: sessionID, Sequence: seq, Answers: answers, SavedAt: now.UTC()}, err
}

const versionColumns = `id, session_id, version_seq, json(answers_json), saved_at`

func scanVersionMetadata(row scanner) (domain.Version, error) {
	var version domain.Version
	var saved string
	err := row.Scan(&version.ID, &version.SessionID, &version.Sequence, &saved)
	if errors.Is(err, sql.ErrNoRows) {
		return version, domain.ErrNotFound
	}
	if err != nil {
		return version, err
	}
	version.SavedAt, err = time.Parse(time.RFC3339Nano, saved)
	return version, err
}

func (s *Store) LatestVersion(ctx context.Context, sessionID string) (domain.Version, error) {
	return scanVersion(s.db.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM submission_versions WHERE session_id = ? ORDER BY version_seq DESC LIMIT 1`, sessionID))
}

func scanVersion(row scanner) (domain.Version, error) {
	var version domain.Version
	var answers, saved string
	err := row.Scan(&version.ID, &version.SessionID, &version.Sequence, &answers, &saved)
	if errors.Is(err, sql.ErrNoRows) {
		return version, domain.ErrNotFound
	}
	if err != nil {
		return version, err
	}
	if err := json.Unmarshal([]byte(answers), &version.Answers); err != nil {
		return version, fmt.Errorf("decode answers: %w", err)
	}
	version.SavedAt, err = time.Parse(time.RFC3339Nano, saved)
	return version, err
}
