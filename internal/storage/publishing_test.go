package storage

import (
	"path/filepath"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestCurrentExamSelectionIsDurableAndReferencesARealExam(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publishing.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	if err := s.WithPublishTransaction(t.Context(), func(tx service.PublishTransaction) error {
		if err := tx.SetCurrentExam(t.Context(), "00000000-0000-4000-8000-0000000000ff", testNow); err == nil {
			t.Error("selected an exam that does not exist")
		}
		exam, class, err := tx.CurrentExam(t.Context())
		if err != nil || exam != "" || class != "" {
			t.Errorf("failed selection left %q/%q, %v", exam, class, err)
		}
		return tx.SetCurrentExam(t.Context(), examID, testNow)
	}); err != nil {
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
	if err := s.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		exam, class, err := tx.CurrentExam(t.Context())
		if err != nil || exam != examID || class != seedClass {
			t.Errorf("selection after restart = %q/%q, %v (the class is the exam's)", exam, class, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithPublishTransaction(t.Context(), func(tx service.PublishTransaction) error {
		if err := tx.SetCurrentExam(t.Context(), examID, testNow.Add(time.Minute)); err != nil {
			return err
		}
		return tx.ClearCurrentExam(t.Context())
	}); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT count(*) FROM current_exam`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after clearing = %d, %v", rows, err)
	}
}

func TestListExamsCountsExercisesAndWorkingSessions(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	if err := s.CreateClass(t.Context(), newClass(classB, "History 11B"), []domain.Student{{AccountNumber: "0078", Name: "Edsger"}}); err != nil {
		t.Fatal(err)
	}
	exam := domain.Exam{ID: "00000000-0000-4000-8000-0000000000a1", ClassID: classB, Title: "Newer", PIN: "p", DurationSeconds: 60, GraceSeconds: 30, MinVolumePercent: 10,
		Navigation: domain.ForwardOnly, Content: domain.ExamContent{Exercises: []domain.Exercise{{ID: "a"}, {ID: "b"}, {ID: "c"}}}, CreatedAt: testNow.Add(time.Hour)}
	if err := s.CreateExam(t.Context(), exam); err != nil {
		t.Fatal(err)
	}
	if err := s.WithPublishTransaction(t.Context(), func(tx service.PublishTransaction) error {
		exams, err := tx.ListExams(t.Context(), testNow, 10)
		if err != nil {
			return err
		}
		if len(exams) != 2 || exams[0].ID != exam.ID || exams[0].ExerciseCount != 3 || exams[0].Navigation != domain.ForwardOnly || exams[1].Navigation != domain.FreeNavigation || exams[0].MinVolumePercent != 10 || exams[0].TotalSessions != 0 {
			t.Errorf("newest exam = %+v", exams)
		}
		if exams[0].ClassID != classB || exams[0].ClassName != "History 11B" || exams[1].ClassID != seedClass || exams[1].ClassName != "Seeded class" {
			t.Errorf("exams are not named with their classes: %+v", exams)
		}
		if exams[1].ID != examID || exams[1].TotalSessions != 1 || exams[1].ActiveSessions != 1 {
			t.Errorf("seeded exam = %+v", exams[1])
		}
		if limited, err := tx.ListExams(t.Context(), testNow, 1); err != nil || len(limited) != 1 {
			t.Errorf("limit ignored: %d, %v", len(limited), err)
		}
		if n, err := tx.ActiveSessions(t.Context(), examID, testNow.Add(24*time.Hour)); err != nil || n != 0 {
			t.Errorf("sessions long past grace still active: %d, %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
