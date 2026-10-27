package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func reportedSound(t *testing.T, s *Store) domain.Sound {
	t.Helper()
	var sound domain.Sound
	if err := s.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		session, err := tx.SessionByID(t.Context(), sessionID)
		sound = session.Sound
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return sound
}
func touch(t *testing.T, s *Store, sound domain.Sound) {
	t.Helper()
	if err := s.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		return tx.TouchHeartbeat(t.Context(), sessionID, "pc", "192.0.2.1", sound, testNow)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatStoresTheReportedSoundAndClearsItWhenNotReported(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	if got := reportedSound(t, s); got.VolumePercent != nil || got.Muted != nil {
		t.Fatalf("a session without a heartbeat has a level: %+v", got)
	}
	volume, muted := 35, true
	touch(t, s, domain.Sound{VolumePercent: &volume, Muted: &muted})
	if got := reportedSound(t, s); got.VolumePercent == nil || *got.VolumePercent != 35 || got.Muted == nil || !*got.Muted {
		t.Fatalf("level not stored: %+v", got)
	}
	volume, muted = 0, false
	touch(t, s, domain.Sound{VolumePercent: &volume, Muted: &muted})
	if got := reportedSound(t, s); got.VolumePercent == nil || *got.VolumePercent != 0 || got.Muted == nil || *got.Muted {
		t.Fatalf("zero volume or unmuted lost: %+v", got)
	}
	touch(t, s, domain.Sound{})
	if got := reportedSound(t, s); got.VolumePercent != nil || got.Muted != nil {
		t.Fatalf("a stale level survived a heartbeat that did not report one: %+v", got)
	}
}

func TestSoundColumnsRejectImpossibleValues(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	for _, statement := range []string{`UPDATE sessions SET volume_percent = 101`, `UPDATE sessions SET volume_percent = -1`, `UPDATE sessions SET volume_muted = 2`} {
		if _, err := s.db.ExecContext(t.Context(), statement); err == nil {
			t.Errorf("accepted %q", statement)
		}
	}
}

func TestSoundMigrationKeepsExistingSessionsUnreported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-sound.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_foundation.sql", "002_presence.sql", "003_proctor_reads.sql", "004_screenshots.sql"} {
		script, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	seed(t, &Store{db: db})
	if _, err := db.Exec(`UPDATE sessions SET last_seen = ?, hostname = 'old-pc'`, timestamp(testNow)); err != nil {
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
	if got := reportedSound(t, current); got.VolumePercent != nil || got.Muted != nil {
		t.Fatalf("migration invented a level: %+v", got)
	}
	var session domain.Session
	if err := current.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		var err error
		session, err = tx.SessionByID(t.Context(), sessionID)
		return err
	}); err != nil || session.Hostname != "old-pc" || session.LastSeen == nil {
		t.Fatalf("migration lost presence: %+v %v", session, err)
	}
}
