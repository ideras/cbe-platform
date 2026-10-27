package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/domain"
)

var testNow = time.Date(2026, 1, 2, 3, 4, 5, 123, time.UTC)

var testPoints = 2.5

// testContent has text that must round-trip exactly through JSONB.
var testContent = domain.ExamContent{Instructions: "<p>Lea 世界</p>", Exercises: []domain.Exercise{{
	ID: "1", Title: "¿Qué imprime? 🧪", Points: &testPoints, Body: `<p>x</p><div class="slot" data-slot="1.q1"></div>`,
	Slots: []domain.Slot{{ID: "1.q1", Kind: domain.CodeSlot, Language: "go", Lines: 4, Starter: "fmt.Println(\"hi\")\n"}},
	Items: []domain.Item{{ID: "1a", Title: "a", Body: `<span class="slot" data-slot="1a.x"></span>`, Slots: []domain.Slot{{ID: "1a.x", Kind: domain.BlankSlot}}}},
}}}

const examID = "00000000-0000-4000-8000-000000000001"
const sessionID = "00000000-0000-4000-8000-000000000002"

// seedClass is the class seed puts the seeded exam and both seeded students in.
const seedClass = "00000000-0000-4000-8000-0000000000c0"

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "exam.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

// seed stores two students, an exam and a session of the first student. On the current schema the students form seedClass,
// which the exam belongs to.
func seed(t *testing.T, s *Store) {
	t.Helper()
	ctx := t.Context()
	// Plain inserts work on every schema generation, including the legacy ones the upgrade tests start from.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO students(account_number, canvas_id, name, email, sis_user_id) VALUES ('0012', '', 'Ada', '', ''), ('0034', '', 'Grace', '', '')`); err != nil {
		t.Fatal(err)
	}
	exam := domain.Exam{ID: examID, ClassID: seedClass, Title: "Prueba", PIN: "1234", DurationSeconds: 60, GraceSeconds: 30, Content: testContent, CreatedAt: testNow}
	var hasContent int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('exams') WHERE name = 'content_json'`).Scan(&hasContent); err != nil {
		t.Fatal(err)
	}
	if hasContent == 0 {
		// Upgrade tests seed schemas that predate the column, as the earlier application would have.
		if _, err := s.db.ExecContext(ctx, `INSERT INTO exams(exam_id, title, pin, duration_seconds, grace_seconds, questions_json, created_at) VALUES (?, ?, ?, ?, ?, jsonb(?), ?)`,
			exam.ID, exam.Title, exam.PIN, exam.DurationSeconds, exam.GraceSeconds, `[{"id":"q1","prompt":"p"}]`, timestamp(exam.CreatedAt)); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := s.CreateClass(ctx, newClass(seedClass, "Seeded class"), []domain.Student{{AccountNumber: "0012", Name: "Ada"}, {AccountNumber: "0034", Name: "Grace"}}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateExam(ctx, exam); err != nil {
			t.Fatal(err)
		}
	}
	addSession(t, s, sessionID, "0012")
}

func addSession(t *testing.T, s *Store, id, account string) {
	t.Helper()
	_, err := s.db.ExecContext(t.Context(), `INSERT INTO sessions(session_id, account_number, exam_id, token, started_at, deadline_at, grace_until, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active')`, id, account, examID, "test-token-"+id, timestamp(testNow), timestamp(testNow.Add(time.Minute)), timestamp(testNow.Add(90*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
}

func appendTest(ctx context.Context, s *Store, session string, answers domain.Answers) (domain.Version, error) {
	var version domain.Version
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		version, err = s.appendVersion(ctx, tx, session, answers, testNow)
		return err
	})
	return version, err
}

func TestMigrationRestartAndJSONB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	answers := domain.Answers{"q1": "你好 🌎\nprint(\"sí\")\t\\end"}
	if _, err := appendTest(t.Context(), s, sessionID, answers); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	exam, err := s.Exam(t.Context(), examID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exam.Content, testContent) || exam.Navigation != domain.FreeNavigation || !exam.CreatedAt.Equal(testNow) {
		t.Fatalf("exam = %+v", exam)
	}
	version, err := s.LatestVersion(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Answers["q1"] != answers["q1"] || version.Sequence != 1 || !version.SavedAt.Equal(testNow) {
		t.Fatalf("version = %+v", version)
	}
	var migrations int
	if err := s.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&migrations); err != nil || migrations != migrationCount(t) {
		t.Fatalf("migrations = %d, %v", migrations, err)
	}
	var kind string
	if err := s.db.QueryRow("SELECT typeof(content_json) FROM exams").Scan(&kind); err != nil || kind != "blob" {
		t.Fatalf("JSONB type = %q, %v", kind, err)
	}
	if err := s.db.QueryRow("SELECT typeof(answers_json) FROM submission_versions").Scan(&kind); err != nil || kind != "blob" {
		t.Fatalf("answer JSONB type = %q, %v", kind, err)
	}
	t.Logf("SQLite %s", mustVersion(t, s))
}

func mustVersion(t *testing.T, s *Store) string {
	t.Helper()
	v, err := s.SQLiteVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConstraints(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	version, err := appendTest(t.Context(), s, sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"missing student":         `INSERT INTO sessions SELECT 'bad', 'missing', exam_id, 'bad-token', started_at, deadline_at, grace_until, NULL, NULL, 'active', NULL FROM sessions LIMIT 1`,
		"duplicate session":       `INSERT INTO sessions SELECT 'duplicate', account_number, exam_id, 'different-token', started_at, deadline_at, grace_until, NULL, NULL, 'active', NULL FROM sessions LIMIT 1`,
		"missing exam":            `UPDATE sessions SET exam_id = 'missing'`,
		"duplicate version":       `INSERT INTO submission_versions(session_id, version_seq, answers_json, saved_at) SELECT session_id, version_seq, answers_json, saved_at FROM submission_versions`,
		"missing session":         `INSERT INTO submission_versions(session_id, version_seq, answers_json, saved_at) VALUES ('missing', 1, jsonb('{}'), 'now')`,
		"mutate answer":           `UPDATE submission_versions SET answers_json = jsonb('{"q1":"changed"}')`,
		"delete answer":           `DELETE FROM submission_versions`,
		"invalid JSONB":           `UPDATE exams SET content_json = 'not JSONB'`,
		"exam without a class":    `INSERT INTO exams(exam_id, title, pin, duration_seconds, content_json, created_at) VALUES ('no-class', 't', 'p', 60, jsonb('{"exercises":[]}'), 'now')`,
		"exam in a missing class": `INSERT INTO exams(exam_id, class_id, title, pin, duration_seconds, content_json, created_at) VALUES ('ghost', 'missing', 't', 'p', 60, jsonb('{"exercises":[]}'), 'now')`,
		"exam taken out of class": `UPDATE exams SET class_id = NULL`,
		"invalid detail":          `INSERT INTO event_log(session_id, event_type, occurred_at, detail) VALUES ('` + sessionID + `', 'focus_lost', 'now', 'not json')`,
		"terminal without final":  `UPDATE sessions SET status = 'submitted', submitted_at = 'now'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.db.ExecContext(t.Context(), statement); err == nil {
				t.Fatal("constraint accepted invalid write")
			}
		})
	}
	other := "00000000-0000-4000-8000-000000000003"
	addSession(t, s, other, "0034")
	if _, err := s.db.Exec(`UPDATE sessions SET status = 'submitted', submitted_at = ?, final_version_id = ? WHERE session_id = ?`, timestamp(testNow), version.ID, other); err == nil {
		t.Fatal("cross-session final version accepted")
	}
	if _, err := s.db.Exec(`UPDATE sessions SET status = 'submitted', submitted_at = ?, final_version_id = ? WHERE session_id = ?`, timestamp(testNow), version.ID, sessionID); err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec(`INSERT INTO event_log(session_id, event_type, occurred_at) VALUES (?, 'focus_lost', ?)`, sessionID, timestamp(testNow))
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO screenshots(session_id, event_log_id, file_path, captured_at) VALUES (?, ?, 'a.png', ?)`, other, eventID, timestamp(testNow)); err == nil {
		t.Fatal("cross-session screenshot accepted")
	}
	if _, err := s.db.Exec(`INSERT INTO screenshots(session_id, event_log_id, file_path, captured_at) VALUES (?, ?, 'a.png', ?)`, sessionID, eventID, timestamp(testNow)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO screenshots(session_id, event_log_id, file_path, captured_at) VALUES (?, ?, 'b.png', ?)`, sessionID, eventID, timestamp(testNow)); err == nil {
		t.Fatal("duplicate incident screenshot accepted")
	}
}

func TestParallelWrites(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	const writers = 64
	var wg sync.WaitGroup
	errors := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			_, err := appendTest(t.Context(), s, sessionID, domain.Answers{"q1": fmt.Sprint(i)})
			errors <- err
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
	var count, minSeq, maxSeq int
	if err := s.db.QueryRow(`SELECT count(*), min(version_seq), max(version_seq) FROM submission_versions`).Scan(&count, &minSeq, &maxSeq); err != nil {
		t.Fatal(err)
	}
	if count != writers || minSeq != 1 || maxSeq != writers {
		t.Fatalf("sequences: %d / %d / %d", count, minSeq, maxSeq)
	}
	if err := s.withTx(t.Context(), func(tx *sql.Tx) error {
		if _, err := s.appendVersion(t.Context(), tx, sessionID, nil, testNow); err != nil {
			return err
		}
		return fmt.Errorf("rollback requested")
	}); err == nil {
		t.Fatal("missing rollback error")
	}
	latest, err := s.LatestVersion(t.Context(), sessionID)
	if err != nil || latest.Sequence != writers {
		t.Fatalf("rollback leaked version: %+v, %v", latest, err)
	}
}

func TestPragmasOnReplacementConnection(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	s.db.SetMaxIdleConns(0)
	for range 3 {
		var foreignKeys, busy int
		var journal, sync string
		if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || busy != 5000 || journal != "wal" || sync != "2" {
			t.Fatalf("pragmas: %d %d %s %s", foreignKeys, busy, journal, sync)
		}
		if _, err := s.db.Exec(`UPDATE sessions SET account_number = 'missing'`); err == nil {
			t.Fatal("replacement connection lost foreign keys")
		}
	}
}

// migrationCount is how many migrations a fully upgraded database has applied.
func migrationCount(t *testing.T) int {
	t.Helper()
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}
