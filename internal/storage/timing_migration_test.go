package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"exam-platform/internal/service"
)

func TestTimingMigrationDefaultsAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-016.db")
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
		if entry.Name() >= "016" {
			break
		}
		script, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?)`, entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	seed(t, &Store{db: db})
	db.Close()
	end := testNow.Add(time.Hour)
	for attempt := 0; attempt < 2; attempt++ {
		store, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		err = store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
			exam, err := tx.Exam(t.Context(), examID)
			if err != nil {
				return err
			}
			session, err := tx.SessionByID(t.Context(), sessionID)
			if err != nil {
				return err
			}
			if attempt == 0 {
				if exam.ExtraGraceSeconds != 0 || exam.EndsAt != nil || session.EndsAt != nil {
					t.Fatal("legacy defaults changed")
				}
				if err := tx.SetExamEndTime(t.Context(), examID, &end); err != nil {
					return err
				}
				return tx.AddExamGrace(t.Context(), examID, 300)
			}
			if exam.ExtraGraceSeconds != 300 || exam.EndsAt == nil || !exam.EndsAt.Equal(end) || session.EndsAt == nil || !session.EndsAt.Equal(end) {
				t.Fatal("timing did not survive restart")
			}
			return nil
		})
		store.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
