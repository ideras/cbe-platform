package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func backupsOf(t *testing.T, path string) []string {
	t.Helper()
	found, err := filepath.Glob(path + ".backup-*")
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestUpgradeCopiesTheOldDatabaseFirstAndPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	legacy := openLegacyThrough(t, path, "007_publishing.sql")
	seed(t, legacy) // two students, an exam and a session, in the schema as it was
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	backup := s.UpgradeBackup()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if backup == "" || !strings.HasPrefix(backup, path+".backup-") || !strings.HasSuffix(backup, "-before-008_classes") {
		t.Fatalf("backup = %q", backup)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("the copy holds names and PINs and must be private: %v, %v", info, err)
		}
	}
	// The copy is the database as it was: still the old schema, with the data and the old roster flag.
	old, err := sql.Open("sqlite", backup)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	var migrationsApplied, students, flags, classes int
	if err := old.QueryRow(`SELECT (SELECT count(*) FROM schema_migrations), (SELECT count(*) FROM students), (SELECT count(*) FROM pragma_table_info('students') WHERE name = 'roster_active'), (SELECT count(*) FROM sqlite_master WHERE name = 'classes')`).Scan(&migrationsApplied, &students, &flags, &classes); err != nil {
		t.Fatal(err)
	}
	if migrationsApplied != 7 || students != 2 || flags != 1 || classes != 0 {
		t.Fatalf("backup: %d migrations, %d students, roster_active columns %d, classes tables %d", migrationsApplied, students, flags, classes)
	}
	// The database itself moved on.
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var now int
	if err := s.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&now); err != nil || now != migrationCount(t) {
		t.Fatalf("migrations after the upgrade = %d, %v", now, err)
	}
	if again := s.UpgradeBackup(); again != "" {
		t.Fatalf("an up-to-date database was copied again: %s", again)
	}
	if n := len(backupsOf(t, path)); n != 1 {
		t.Fatalf("%d backups after reopening, want the one from the upgrade", n)
	}
}

func TestANewDatabaseNeedsNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.UpgradeBackup() != "" || len(backupsOf(t, path)) != 0 {
		t.Fatalf("a new database was copied: %q", s.UpgradeBackup())
	}
}

func TestAnUpgradeThatCannotBeBackedUpDoesNotHappen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	legacy := openLegacyThrough(t, path, "007_publishing.sql")
	seed(t, legacy)
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}
	// Something already sits where the copy would go (the same second, the same upgrade): never overwrite it, never go on.
	fixed := time.Date(2026, 10, 17, 18, 50, 49, 0, time.UTC)
	upgradeClock = func() time.Time { return fixed }
	t.Cleanup(func() { upgradeClock = func() time.Time { return time.Now().UTC() } })
	taken := path + ".backup-20261017T185049Z-before-008_classes"
	if err := os.WriteFile(taken, []byte("an earlier copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(t.Context(), path); err == nil {
		s.Close()
		t.Fatal("the database was upgraded without a backup")
	} else if !strings.Contains(err.Error(), "back up the database before upgrading") {
		t.Fatalf("error = %v", err)
	}
	if data, err := os.ReadFile(taken); err != nil || string(data) != "an earlier copy" {
		t.Fatalf("an existing file was overwritten: %q, %v", data, err)
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var applied int
	if err := check.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != 7 {
		t.Fatalf("the database changed although the backup failed: %d migrations, %v", applied, err)
	}
}
