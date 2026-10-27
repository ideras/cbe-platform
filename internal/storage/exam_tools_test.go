package storage

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"exam-platform/internal/domain"

	"exam-platform/internal/service"
	"exam-platform/shared/calc"
)

// An exam's tools round-trip through storage exactly as they were validated, and the exam catalog lists them.
func TestExamToolsRoundTripThroughStorage(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	tools := &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific, calc.ModeProgrammer}, AngleMode: calc.AngleRadians, DisabledFunctions: []string{"softmax", "ieee754"}}}
	exam := domain.Exam{ID: examID, ClassID: classA, Title: "t", PIN: "p", DurationSeconds: 60, GraceSeconds: 30, Tools: tools, CreatedAt: testNow}
	if err := s.CreateExam(ctx, exam); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Exam(ctx, examID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.Tools, tools) {
		t.Fatalf("stored tools = %+v, want %+v", stored.Tools, tools)
	}
	var listed *calc.Tools
	if err := s.WithPublishTransaction(ctx, func(tx service.PublishTransaction) error {
		exams, err := tx.ListExams(ctx, testNow, 10)
		if err != nil {
			return err
		}
		listed = exams[0].Tools
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(listed, tools) {
		t.Fatalf("catalog tools = %+v, want %+v", listed, tools)
	}
}

// An exam without tools reads back with none, the way every exam stored before the column existed does.
func TestExamWithoutToolsStaysWithoutTools(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	addExam(t, s, examID, classA, testNow)
	stored, err := s.Exam(ctx, examID)
	if err != nil || stored.Tools != nil {
		t.Fatalf("exam = %+v, %v", stored.Tools, err)
	}
}

// A development database that already has every earlier migration applied gains the column and keeps its exams.
func TestTheToolsMigrationKeepsExistingExams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-015.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateClass(t.Context(), newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	addExam(t, s, examID, classA, testNow)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopening applies nothing new (015 was already applied) but must leave the exam readable.
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err := reopened.Exam(t.Context(), examID)
	if err != nil {
		t.Fatalf("the upgraded database lost the exam: %v", err)
	}
	if stored.Tools != nil {
		t.Fatalf("the upgrade invented tools: %+v", stored.Tools)
	}
	// The upgraded database takes a new exam's tools.
	tools := &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeNeural}}}
	const spare = "00000000-0000-4000-8000-0000000000a1"
	if err := reopened.CreateExam(t.Context(), domain.Exam{ID: spare, ClassID: classA, Title: "t2", PIN: "p", DurationSeconds: 60, GraceSeconds: 30, Tools: tools, CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	stored, err = reopened.Exam(t.Context(), spare)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Tools == nil || !slices.Equal(stored.Tools.Calculator.Modes, []string{calc.ModeNeural}) {
		t.Fatalf("tools on the upgraded database = %+v", stored.Tools)
	}
}
