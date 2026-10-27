package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"exam-platform/internal/domain"
)

func TestExamVolumeIsStoredAndBounded(t *testing.T) {
	s := openTest(t)
	if err := s.CreateClass(t.Context(), newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	for i, volume := range []int{0, 35, 100} {
		exam := domain.Exam{ID: fmt.Sprintf("00000000-0000-4000-8000-0000000000a%d", i), ClassID: classA, Title: "t", PIN: "p", DurationSeconds: 60, GraceSeconds: 30, MinVolumePercent: volume, CreatedAt: testNow}
		if err := s.CreateExam(t.Context(), exam); err != nil {
			t.Fatal(err)
		}
		got, err := s.Exam(t.Context(), exam.ID)
		if err != nil || got.MinVolumePercent != volume {
			t.Fatalf("volume %d stored as %d, %v", volume, got.MinVolumePercent, err)
		}
	}
	for _, statement := range []string{`UPDATE exams SET min_volume_percent = 101`, `UPDATE exams SET min_volume_percent = -1`} {
		if _, err := s.db.ExecContext(t.Context(), statement); err == nil {
			t.Errorf("accepted %q", statement)
		}
	}
}

func TestExamVolumeMigrationGivesExistingExamsTheDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-volume.db")
	legacy := openLegacyThrough(t, path, "005_sound.sql")
	seed(t, legacy) // pre-006 shape: no volume column
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	exam, err := current.Exam(t.Context(), examID)
	if err != nil || exam.MinVolumePercent != 80 {
		t.Fatalf("existing exam volume = %d, %v; want the default 80", exam.MinVolumePercent, err)
	}
}

// openLegacyThrough applies the embedded migrations up to and including last, then returns an unmigrated-further store.
func openLegacyThrough(t *testing.T, path, last string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
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
		if name == last {
			break
		}
	}
	return &Store{db: db}
}
