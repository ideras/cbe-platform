package storage

import (
	"errors"
	"path/filepath"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// The roster that used to be global must survive the upgrade as a class, with the exam that was published and its history.
func TestUpgradeTurnsTheGlobalRosterIntoTheFirstClass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	legacy := openLegacyThrough(t, path, "007_publishing.sql")
	seed(t, legacy) // students 0012 and 0034, the exam, and a session for 0012
	other := "00000000-0000-4000-8000-0000000000a1"
	for _, statement := range []string{
		`INSERT INTO students(account_number, canvas_id, name, email, sis_user_id, roster_active) VALUES ('0099', '', 'Dropped', '', '', 0)`,
		`INSERT INTO exams(exam_id, title, pin, duration_seconds, grace_seconds, questions_json, created_at) VALUES ('` + other + `', 'Never used', 'p', 60, 30, jsonb('[]'), '2026-01-01T00:00:00.000000000Z')`,
		`INSERT INTO current_exam(singleton, exam_id, published_at) VALUES (1, '` + examID + `', '2026-01-02T03:04:05.000000000Z')`,
		`INSERT INTO screenshot_retention(exam_id, purged_at) VALUES ('` + other + `', '2026-01-03T00:00:00.000000000Z')`,
	} {
		if _, err := legacy.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	classes, err := s.ListClasses(ctx)
	if err != nil || len(classes) != 1 || classes[0].Name != "Imported roster" || classes[0].StudentCount != 2 {
		t.Fatalf("classes = %+v, %v", classes, err)
	}
	if got := classes[0].PublishedExam; got == nil || got.ExamID != examID || classes[0].ExamCount != 1 {
		t.Fatalf("the published exam did not move to the class: %+v with %d exams (the one nobody used is gone)", got, classes[0].ExamCount)
	}
	members, err := s.ClassStudents(ctx, classes[0].ID)
	if err != nil || len(members) != 2 || members[0].AccountNumber != "0012" || members[1].AccountNumber != "0034" {
		t.Fatalf("members = %+v, %v (a student dropped from the old roster must not come back)", members, err)
	}
	if !validClassID(classes[0].ID) {
		t.Fatalf("class id %q is not a lowercase UUID", classes[0].ID)
	}
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		exam, class, err := tx.CurrentExam(ctx)
		if err != nil || exam != examID || class != classes[0].ID {
			t.Errorf("current = %q/%q, %v", exam, class, err)
		}
		record, err := tx.ProctorSession(ctx, sessionID)
		if err != nil || !record.RosterActive {
			t.Errorf("old session lost its roster flag: %+v, %v", record, err)
		}
		missing, err := tx.StudentsWithoutSession(ctx, examID)
		if err != nil || len(missing) != 1 || missing[0].AccountNumber != "0034" {
			t.Errorf("missing = %+v, %v", missing, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var tables, columns, unused, retention int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM sqlite_master WHERE name = 'exam_classes'), (SELECT count(*) FROM pragma_table_info('students') WHERE name = 'roster_active'),
		(SELECT count(*) FROM exams WHERE exam_id = ?1), (SELECT count(*) FROM screenshot_retention WHERE exam_id = ?1)`, other).Scan(&tables, &columns, &unused, &retention); err != nil || tables != 0 || columns != 0 || unused != 0 || retention != 0 {
		t.Fatalf("exam_classes tables = %d, roster_active columns = %d, unused exams = %d with %d retention marks, %v", tables, columns, unused, retention, err)
	}
	// The old person data is untouched.
	var name string
	if err := s.db.QueryRow(`SELECT name FROM students WHERE account_number = '0099'`).Scan(&name); err != nil || name != "Dropped" {
		t.Fatalf("a historical student was lost: %q, %v", name, err)
	}
	if err := s.CreateClass(ctx, domain.Class{ID: classB, Name: "Another", CreatedAt: testNow}, []domain.Student{{AccountNumber: "0044", Name: "New"}}); err != nil {
		t.Fatalf("the upgraded database does not accept new classes: %v", err)
	}
}

func validClassID(id string) bool {
	if len(id) != 36 || id[14] != '4' || !(id[19] == '8' || id[19] == '9' || id[19] == 'a' || id[19] == 'b') {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Without any roster nobody could log in, so there is nothing to carry over: the publication is dropped, and exams that were
// never taken have no class to go to.
func TestUpgradeWithoutARosterDropsThePublicationAndTheExamsAndInventsNoClass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	legacy := openLegacyThrough(t, path, "007_publishing.sql")
	if _, err := legacy.db.Exec(`INSERT INTO exams(exam_id, title, pin, duration_seconds, grace_seconds, questions_json, created_at) VALUES ('` + examID + `', 'Exam', 'p', 60, 30, jsonb('[]'), '2026-01-01T00:00:00.000000000Z');
		INSERT INTO current_exam(singleton, exam_id, published_at) VALUES (1, '` + examID + `', '2026-01-02T03:04:05.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var classes, publications, exams int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM classes), (SELECT count(*) FROM current_exam), (SELECT count(*) FROM exams)`).Scan(&classes, &publications, &exams); err != nil || classes != 0 || publications != 0 || exams != 0 {
		t.Fatalf("classes = %d, publications = %d, exams = %d, %v", classes, publications, exams, err)
	}
}

// A database from before this release has exams that were published to several classes, or to none.
func TestUpgradeGivesEachExamTheClassItWasLastPublishedTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	legacy := openLegacyThrough(t, path, "010_capture_series.sql")
	const (
		moved     = "00000000-0000-4000-8000-0000000000e1" // published to class A, later to class B
		current   = "00000000-0000-4000-8000-0000000000e2" // being admitted to class A
		taken     = "00000000-0000-4000-8000-0000000000e3" // published to class B once, with a session
		neverUsed = "00000000-0000-4000-8000-0000000000e4"
	)
	statements := []string{
		`INSERT INTO classes(class_id, name, created_at) VALUES ('` + classA + `', 'Mathematics 10A', '2026-01-01T00:00:00.000000000Z'), ('` + classB + `', 'History 11B', '2026-01-01T00:00:00.000000000Z')`,
		`INSERT INTO students(account_number, canvas_id, name, email, sis_user_id) VALUES ('0012', '', 'Ada', '', '')`,
		`INSERT INTO class_students(class_id, account_number) VALUES ('` + classB + `', '0012')`,
	}
	for _, id := range []string{moved, current, taken, neverUsed} {
		statements = append(statements, `INSERT INTO exams(exam_id, title, pin, duration_seconds, grace_seconds, content_json, created_at) VALUES ('`+id+`', 'Exam `+id[len(id)-1:]+`', 'p', 60, 30, jsonb('{"exercises":[]}'), '2026-01-01T00:00:00.000000000Z')`)
	}
	statements = append(statements,
		`INSERT INTO exam_classes(exam_id, class_id, published_at) VALUES
			('`+moved+`', '`+classA+`', '2026-02-01T00:00:00.000000000Z'), ('`+moved+`', '`+classB+`', '2026-03-01T00:00:00.000000000Z'),
			('`+current+`', '`+classA+`', '2026-02-01T00:00:00.000000000Z'), ('`+current+`', '`+classB+`', '2026-03-01T00:00:00.000000000Z'),
			('`+taken+`', '`+classB+`', '2026-02-01T00:00:00.000000000Z')`,
		// The class being admitted wins over a later row of the history.
		`INSERT INTO current_exam(singleton, exam_id, class_id, published_at) VALUES (1, '`+current+`', '`+classA+`', '2026-02-01T00:00:00.000000000Z')`,
		`INSERT INTO sessions(session_id, account_number, exam_id, token, started_at, deadline_at, grace_until, status)
			VALUES ('`+sessionID+`', '0012', '`+taken+`', 't', '2026-02-01T00:00:00.000000000Z', '2026-02-01T00:01:00.000000000Z', '2026-02-01T00:01:30.000000000Z', 'active')`)
	for _, statement := range statements {
		if _, err := legacy.db.Exec(statement); err != nil {
			t.Fatalf("%v\n%s", err, statement)
		}
	}
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	for id, want := range map[string]string{moved: classB, current: classA, taken: classB} {
		if exam, err := s.Exam(ctx, id); err != nil || exam.ClassID != want {
			t.Errorf("exam %s class = %q, %v; want %s", id[len(id)-1:], exam.ClassID, err, want)
		}
	}
	if _, err := s.Exam(ctx, neverUsed); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an exam that was never published should be gone: %v", err)
	}
	exam, class := "", ""
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) (err error) { exam, class, err = tx.CurrentExam(ctx); return }); err != nil || exam != current || class != classA {
		t.Errorf("current exam = %q in %q, %v", exam, class, err)
	}
	var tables int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'exam_classes'`).Scan(&tables); err != nil || tables != 0 {
		t.Errorf("exam_classes still exists: %d, %v", tables, err)
	}
	if _, err := s.db.Exec(`INSERT INTO exams(exam_id, title, pin, duration_seconds, content_json, created_at) VALUES ('x', 't', 'p', 60, jsonb('{"exercises":[]}'), 'now')`); err == nil {
		t.Error("the upgraded database accepts an exam without a class")
	}
}

// If the roster was empty when classes appeared, an exam with sessions has no class to move to; it must not stop the upgrade
// or lose the people who took it.
func TestUpgradeKeepsAnExamThatWasTakenWithoutAnyClassInAClassOfItsOwn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exam.db")
	legacy := openLegacyThrough(t, path, "007_publishing.sql")
	seed(t, legacy) // students 0012 and 0034, the exam, and a session for 0012
	if _, err := legacy.db.Exec(`UPDATE students SET roster_active = 0`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	classes, err := s.ListClasses(ctx)
	if err != nil || len(classes) != 1 || classes[0].Name != "Imported exams" || classes[0].StudentCount != 1 || classes[0].ExamCount != 1 || !validClassID(classes[0].ID) {
		t.Fatalf("classes = %+v, %v (the exam and the one student who took it)", classes, err)
	}
	if exam, err := s.Exam(ctx, examID); err != nil || exam.ClassID != classes[0].ID {
		t.Fatalf("exam = %+v, %v", exam, err)
	}
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		record, err := tx.ProctorSession(ctx, sessionID)
		if err != nil || !record.RosterActive {
			t.Errorf("the student who took the exam lost their place in its class: %+v, %v", record, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
