package storage

import (
	"database/sql"
	"exam-platform/internal/service"
	"path/filepath"
	"testing"
)

func TestScreenshotMigrationKeepsLegacyMetadataAndStableStoreIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phase7.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_foundation.sql", "002_presence.sql", "003_proctor_reads.sql"} {
		script, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &Store{db: db}
	seed(t, legacy)
	if _, err := appendTest(t.Context(), legacy, sessionID, map[string]string{"q1": "migration-safe"}); err != nil {
		t.Fatal(err)
	}
	event, err := db.Exec(`INSERT INTO event_log(session_id,event_type,occurred_at,detail) VALUES(?,'focus_lost',?,'{}')`, sessionID, timestamp(testNow))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := event.LastInsertId()
	if _, err := db.Exec(`INSERT INTO screenshots(session_id,event_log_id,file_path,captured_at) VALUES(?,?,?,?)`, sessionID, id, sessionID+"/legacy.png", timestamp(testNow)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var storeID string
	for range 2 {
		current, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		err = current.WithScreenshotTransaction(t.Context(), func(tx service.ScreenshotTransaction) error {
			identity, err := tx.ScreenshotStoreID(t.Context())
			if err != nil {
				return err
			}
			if len(identity) != 32 || (storeID != "" && identity != storeID) {
				t.Fatal("store identity changed")
			}
			storeID = identity
			shot, err := tx.ScreenshotByCapture(t.Context(), sessionID, id, 0)
			if err != nil {
				return err
			}
			if shot.State != "ready" || shot.Path != sessionID+"/legacy.png" || shot.Bytes != 0 || shot.ClientCapturedAt != nil {
				t.Fatal("migration altered legacy row")
			}
			version, err := tx.LatestVersion(t.Context(), sessionID)
			if err == nil && version.Answers["q1"] != "migration-safe" {
				t.Fatal("answer history altered")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		current.Close()
	}
}
