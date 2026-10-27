package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// A database from before migration 014 had sessions without a copy of the settings, and finished sessions that kept their
// lock. The upgrade gives every session and the published exam the settings, and ends the locks of finished sessions.
func TestTheUnlockMigrationGivesEverySessionItsSettingsAndEndsFinishedLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-014.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "014" {
			break
		}
		script, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?)`, entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &Store{db: db}
	seed(t, legacy)
	const finishedID = "00000000-0000-4000-8000-000000000003"
	addSession(t, legacy, finishedID, "0034")
	final, err := appendTest(t.Context(), legacy, finishedID, domain.Answers{"q1": "last saved"})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO current_exam(singleton, exam_id, published_at) VALUES (1, '` + examID + `', '2026-01-02T03:04:05.000000000Z')`,
		// The running session is locked, and the finished one kept the lock it had when its time ran out.
		`UPDATE sessions SET locked_at = '2026-01-02T03:04:05.000000000Z', lock_reason = 'away'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE sessions SET status = 'submitted', submitted_at = ?, final_version_id = ? WHERE session_id = ?`, timestamp(testNow), final.ID, finishedID); err != nil {
		t.Fatal(err)
	}
	db.Close()

	current, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	sessions := map[string]domain.Session{}
	var exam domain.Exam
	err = current.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		for _, id := range []string{sessionID, finishedID} {
			session, err := tx.SessionByID(t.Context(), id)
			if err != nil {
				return err
			}
			sessions[id] = session
		}
		exam, err = tx.Exam(t.Context(), examID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defaults := domain.DefaultPolicy()
	for id, session := range sessions {
		if session.Policy != defaults {
			t.Fatalf("session %s follows %+v, want the defaults %+v", id, session.Policy, defaults)
		}
	}
	if exam.Policy != defaults {
		t.Fatalf("the published exam keeps %+v, want the defaults", exam.Policy)
	}
	if running := sessions[sessionID]; running.LockedAt == nil || !running.LockedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) || running.LockReason != "away" {
		t.Fatalf("the running session lost its lock: %+v", running)
	}
	if finished := sessions[finishedID]; finished.LockedAt != nil || finished.LockReason != "" {
		t.Fatalf("the finished session is still locked: %+v", finished)
	}
}
