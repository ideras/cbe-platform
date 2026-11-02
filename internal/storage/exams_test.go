package storage

import (
	"errors"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestCreateExamNeedsAnExistingClassAndKeepsIt(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	for name, class := range map[string]string{"an unknown class": classB, "no class": ""} {
		exam := domain.Exam{ID: examID, ClassID: class, Title: "t", PIN: "p", DurationSeconds: 60, GraceSeconds: 30, CreatedAt: testNow}
		if err := s.CreateExam(ctx, exam); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("an exam in %s = %v", name, err)
		}
	}
	if _, err := s.Exam(ctx, examID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a refused exam was stored: %v", err)
	}
	addExam(t, s, examID, classA, testNow)
	if exam, err := s.Exam(ctx, examID); err != nil || exam.ClassID != classA {
		t.Fatalf("exam = %+v, %v", exam, err)
	}
}

func TestDeleteExamRefusesPublishedExamsAndOnesStudentsAreWorkingInAndLeavesNoTrace(t *testing.T) {
	s := openTest(t)
	seed(t, s) // examID is in seedClass and student 0012 has an active session in it
	ctx := t.Context()
	const spare = "00000000-0000-4000-8000-0000000000a1"
	addExam(t, s, spare, seedClass, testNow.Add(time.Hour))

	// Nobody may be working in the exam, whatever number the caller confirms.
	for _, confirmed := range []int{0, 1} {
		if _, err := s.DeleteExam(ctx, examID, confirmed); !errors.Is(err, domain.ErrExamInUse) {
			t.Fatalf("deleting an exam a student is working in (confirmed %d) = %v", confirmed, err)
		}
	}
	publish(t, s, spare, testNow)
	if _, err := s.DeleteExam(ctx, spare, 0); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting the published exam = %v", err)
	}
	if _, err := s.Exam(ctx, spare); err != nil {
		t.Fatalf("a refused deletion removed the exam: %v", err)
	}

	publishNothing(t, s)
	// Purging the screenshots of an exam nobody took still leaves a retention mark behind.
	if _, err := s.db.Exec(`INSERT INTO screenshot_retention(exam_id, purged_at) VALUES (?, ?)`, spare, timestamp(testNow)); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteExam(ctx, spare, 0)
	if err != nil || deleted != (domain.DeletedExam{ID: spare, Title: "Exam 1"}) {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	var exams, marks int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM exams), (SELECT count(*) FROM screenshot_retention)`).Scan(&exams, &marks); err != nil || exams != 1 || marks != 0 {
		t.Fatalf("exams = %d, retention marks = %d, %v", exams, marks, err)
	}
	if _, err := s.DeleteExam(ctx, spare, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
	if _, err := s.DeleteExam(ctx, examID, 0); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("the exam a student is working in became deletable: %v", err)
	}
}

// finish gives a session its final version and marks it submitted, as a student's submission does, and gives it a focus-loss
// event with the given screenshot files.
func finish(t *testing.T, s *Store, session string, files ...string) {
	t.Helper()
	version, err := appendTest(t.Context(), s, session, domain.Answers{"q1": "an answer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET status = 'submitted', submitted_at = ?, final_version_id = ? WHERE session_id = ?`, timestamp(testNow), version.ID, session); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO event_log(session_id, event_type, occurred_at) VALUES (?, 'focus_lost', ?)`, session, timestamp(testNow)); err != nil {
		t.Fatal(err)
	}
	for i, name := range files {
		if _, err := s.db.Exec(`INSERT INTO screenshots(session_id, event_log_id, capture_index, file_path, captured_at) VALUES (?, (SELECT max(id) FROM event_log WHERE session_id = ?), ?, ?, ?)`,
			session, session, i, name, timestamp(testNow)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeleteExamTakesItsSubmissionsEventsAndScreenshotRecordsWithItAndNothingElse(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	ctx := t.Context()
	const other = "00000000-0000-4000-8000-0000000000a1"
	const otherSession = "00000000-0000-4000-8000-0000000000a2"
	addExam(t, s, other, seedClass, testNow)
	if _, err := s.db.Exec(`INSERT INTO sessions(session_id, account_number, exam_id, token, started_at, deadline_at, grace_until, status)
		VALUES (?, '0034', ?, 'other-token', ?, ?, ?, 'active')`, otherSession, other, timestamp(testNow), timestamp(testNow.Add(time.Minute)), timestamp(testNow.Add(90*time.Second))); err != nil {
		t.Fatal(err)
	}
	addSession(t, s, "00000000-0000-4000-8000-000000000003", "0034")
	ours := sessionID + "/1-0.jpg"
	oursToo := sessionID + "/1-1.jpg"
	theirs := otherSession + "/1-0.jpg"
	finish(t, s, sessionID, ours, oursToo)
	finish(t, s, "00000000-0000-4000-8000-000000000003")
	finish(t, s, otherSession, theirs)
	if _, err := s.db.Exec(`INSERT INTO screenshot_retention(exam_id, purged_at) VALUES (?, ?)`, examID, timestamp(testNow)); err != nil {
		t.Fatal(err)
	}

	// The count has to be the one the caller was told: none, or a wrong one, deletes nothing.
	for _, confirmed := range []int{0, 1, 3} {
		if _, err := s.DeleteExam(ctx, examID, confirmed); !errors.Is(err, domain.ErrSubmissionsChanged) {
			t.Fatalf("deleting an exam with 2 submissions after confirming %d = %v", confirmed, err)
		}
	}
	count := func(query string) int {
		t.Helper()
		var n int
		if err := s.db.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT count(*) FROM sessions`) != 3 || count(`SELECT count(*) FROM screenshots`) != 3 || count(`SELECT count(*) FROM deleted_screenshot_files`) != 0 {
		t.Fatal("a refused deletion changed something")
	}
	// A student who is still working keeps the exam from being deleted.
	if _, err := s.db.Exec(`UPDATE sessions SET status = 'grace', submitted_at = NULL, final_version_id = NULL WHERE session_id = '00000000-0000-4000-8000-000000000003'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteExam(ctx, examID, 2); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting an exam while a student is in grace = %v", err)
	}
	finish(t, s, "00000000-0000-4000-8000-000000000003")

	deleted, err := s.DeleteExam(ctx, examID, 2)
	if err != nil || deleted != (domain.DeletedExam{ID: examID, Title: "Prueba", Sessions: 2, Screenshots: 2}) {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	// The exam and everything of it is gone; the other exam, the class and the people are not.
	for table, want := range map[string]int{"exams": 1, "sessions": 1, "submission_versions": 1, "event_log": 1, "screenshots": 1, "screenshot_retention": 0, "students": 2, "classes": 1, "class_students": 2} {
		if got := count(`SELECT count(*) FROM ` + table); got != want {
			t.Errorf("%s has %d rows, want %d", table, got, want)
		}
	}
	if count(`SELECT count(*) FROM sessions WHERE exam_id = '`+other+`'`) != 1 {
		t.Error("the other exam's session went with it")
	}
	// The files are for the screenshot service to remove, and the list names exactly this exam's.
	var listed []string
	err = s.WithScreenshotTransaction(ctx, func(tx service.ScreenshotTransaction) error {
		var err error
		listed, err = tx.DeletedFiles(ctx)
		return err
	})
	if err != nil || len(listed) != 2 || listed[0] != ours || listed[1] != oursToo {
		t.Fatalf("files to remove = %v, %v", listed, err)
	}
	if _, err := s.DeleteExam(ctx, examID, 2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
	// The permission to delete answer versions ended with the transaction; the guard is back for everything else.
	if count(`SELECT count(*) FROM exam_being_deleted`) != 0 {
		t.Error("the exam is still listed as being deleted")
	}
	if _, err := s.db.Exec(`DELETE FROM submission_versions`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("deleting an answer version outside an exam deletion = %v", err)
	}
	broken, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer broken.Close()
	if broken.Next() {
		t.Error("the deletion left a broken reference")
	}
}
