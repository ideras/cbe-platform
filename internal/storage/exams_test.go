package storage

import (
	"errors"
	"testing"
	"time"

	"exam-platform/internal/domain"
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

func TestDeleteExamRefusesPublishedAndTakenExamsAndLeavesNoTrace(t *testing.T) {
	s := openTest(t)
	seed(t, s) // examID is in seedClass and student 0012 has a session in it
	ctx := t.Context()
	const spare = "00000000-0000-4000-8000-0000000000a1"
	addExam(t, s, spare, seedClass, testNow.Add(time.Hour))

	if _, err := s.DeleteExam(ctx, examID); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting an exam students took = %v", err)
	}
	publish(t, s, spare, testNow)
	if _, err := s.DeleteExam(ctx, spare); !errors.Is(err, domain.ErrExamInUse) {
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
	deleted, err := s.DeleteExam(ctx, spare)
	if err != nil || deleted != (domain.DeletedExam{ID: spare, Title: "Exam 1"}) {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	var exams, marks int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM exams), (SELECT count(*) FROM screenshot_retention)`).Scan(&exams, &marks); err != nil || exams != 1 || marks != 0 {
		t.Fatalf("exams = %d, retention marks = %d, %v", exams, marks, err)
	}
	if _, err := s.DeleteExam(ctx, spare); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
	if _, err := s.DeleteExam(ctx, examID); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("the exam students took became deletable: %v", err)
	}
}
