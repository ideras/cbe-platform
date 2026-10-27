package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	"exam-platform/internal/service"
)

func TestPresenceMigrationUpgradesExistingFoundation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	foundation, err := migrations.ReadFile("migrations/001_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(foundation)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY NOT NULL); INSERT INTO schema_migrations VALUES ('001_foundation.sql')`); err != nil {
		t.Fatal(err)
	}
	legacy := &Store{db: db}
	seed(t, legacy)
	version, err := appendTest(t.Context(), legacy, sessionID, map[string]string{"q1": "preserved"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET last_seen=? WHERE session_id=?`, timestamp(testNow), sessionID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if err := current.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		session, err := tx.SessionByID(t.Context(), sessionID)
		if err != nil {
			return err
		}
		if session.LastSeen == nil || !session.LastSeen.Equal(testNow) || session.NetworkGapSince != nil || session.Hostname != "" || session.RemoteAddr != "" {
			t.Error("migration lost last-seen or inferred connectivity")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	latest, err := current.LatestVersion(t.Context(), sessionID)
	if err != nil || latest.ID != version.ID || latest.Answers["q1"] != "preserved" {
		t.Fatal("migration changed answer history")
	}
	var count int
	if err := current.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != migrationCount(t) {
		t.Fatal("migration versions not tracked")
	}
}
