package service_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestCreateExamValidationAndDefaults(t *testing.T) {
	f := newFixture(t, 0)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	valid := func() service.CreateExamInput {
		return service.CreateExamInput{Title: " Unicode exam ", PIN: "literal PIN", DurationSeconds: 60, Content: examContent([]string{"q1"})}
	}
	for _, mutate := range []func(*service.CreateExamInput){
		func(i *service.CreateExamInput) { i.Title = " " }, func(i *service.CreateExamInput) { i.PIN = "" },
		func(i *service.CreateExamInput) { i.DurationSeconds = 0 }, func(i *service.CreateExamInput) { i.DurationSeconds = math.MaxInt64 },
		func(i *service.CreateExamInput) { g := int64(0); i.GraceSeconds = &g }, func(i *service.CreateExamInput) { g := int64(-1); i.GraceSeconds = &g },
		func(i *service.CreateExamInput) { v := -1; i.MinVolumePercent = &v }, func(i *service.CreateExamInput) { v := 101; i.MinVolumePercent = &v },
		func(i *service.CreateExamInput) { i.Navigation = "sideways" },
		func(i *service.CreateExamInput) { i.Content.Exercises = nil },
		func(i *service.CreateExamInput) {
			i.Content.Exercises = append(i.Content.Exercises, i.Content.Exercises[0])
		},
		func(i *service.CreateExamInput) { i.Content.Exercises[0].ID = " 1" }, func(i *service.CreateExamInput) { i.Content.Exercises[0].ID = "" },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Title = "" },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Body = strings.Repeat("x", (256<<10)+1) },
		func(i *service.CreateExamInput) { p := 0.0; i.Content.Exercises[0].Points = &p },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Slots = nil },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Slots[0].ID = "q 1" },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Slots[0].Kind = "essay" },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Slots[0].Language = "c" },
		func(i *service.CreateExamInput) {
			i.Content.Exercises[0].Slots[0].Starter = strings.Repeat("x", (64<<10)+1)
		},
		func(i *service.CreateExamInput) {
			i.Content.Exercises[0].Slots[0] = domain.Slot{ID: "q1", Kind: domain.BlankSlot, Starter: "x"}
		},
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Body = "<p>no placeholder</p>" },
		func(i *service.CreateExamInput) { i.Content.Exercises[0].Body += `<span data-slot="extra"></span>` },
		func(i *service.CreateExamInput) { i.Content.Instructions = `<span class="slot" data-slot="q1"></span>` },
		func(i *service.CreateExamInput) {
			i.Content.Exercises[0].Items = []domain.Item{{ID: "1a", Title: "a", Body: `<div data-slot="q1"></div>`, Slots: []domain.Slot{{ID: "q1", Kind: domain.TextSlot}}}}
		},
		func(i *service.CreateExamInput) { now := epoch; i.OpensAt = &now; i.ClosesAt = &now },
		func(i *service.CreateExamInput) {
			later, earlier := epoch.Add(time.Second), epoch
			i.OpensAt = &later
			i.ClosesAt = &earlier
		},
	} {
		input := valid()
		mutate(&input)
		if _, err := exams.Create(t.Context(), testClassID, input); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("validation = %v", err)
		}
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 1 {
		t.Fatal("invalid exam persisted")
	}
	id, err := exams.Create(t.Context(), testClassID, valid())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.store.Exam(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MinVolumePercent != service.DefaultMinVolumePercent {
		t.Fatalf("default exam volume = %d", stored.MinVolumePercent)
	}
	if stored.ClassID != testClassID {
		t.Fatalf("the exam went to class %q", stored.ClassID)
	}
	if stored.Title != "Unicode exam" || stored.GraceSeconds != 30 || stored.PIN != "literal PIN" || !stored.CreatedAt.Equal(epoch) || !reflect.DeepEqual(stored.Content, valid().Content) || stored.Navigation != domain.FreeNavigation {
		t.Fatal("exam content/defaults changed")
	}
	input := valid()
	grace := int64(7)
	input.GraceSeconds = &grace
	id, err = exams.Create(t.Context(), testClassID, input)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = f.store.Exam(t.Context(), id)
	if err != nil || stored.GraceSeconds != 7 {
		t.Fatal("custom grace ignored")
	}
}

func TestAnExamIsImportedIntoAnExistingClass(t *testing.T) {
	f := newFixture(t, 30)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	input := service.CreateExamInput{Title: "Final", PIN: "p", DurationSeconds: 60, Content: examContent([]string{"q1"})}
	if _, err := exams.Create(t.Context(), "00000000-0000-4000-8000-0000000000ff", input); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a class that does not exist = %v", err)
	}
	for _, class := range []string{"", "bad", strings.ToUpper(testClassID)} {
		if _, err := exams.Create(t.Context(), class, input); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("class %q = %v", class, err)
		}
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 1 {
		t.Fatal("a refused import stored an exam")
	}
	// Each class gets its own copy, with its own PIN: the same file imported twice is two exams.
	f.addClass(t, "00000000-0000-4000-8000-0000000000c2", "History 11B", domain.Student{AccountNumber: "0077", Name: "Linus"})
	first, err := exams.Create(t.Context(), testClassID, input)
	if err != nil {
		t.Fatal(err)
	}
	input.PIN = "other"
	second, err := exams.Create(t.Context(), "00000000-0000-4000-8000-0000000000c2", input)
	if err != nil || first == second {
		t.Fatalf("second import = %q, %v", second, err)
	}
	if got := f.count(t, `SELECT count(*) FROM exams WHERE class_id = ?`, testClassID); got != 2 {
		t.Fatalf("%d exams in the first class", got)
	}
}

func TestOnlyAnUnpublishedExamNobodyTookCanBeDeleted(t *testing.T) {
	f := newFixture(t, 30)
	f.addExam(t, secondExamID, "second-pin", 1)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	publisher := service.NewPublisher(f.store, f.clock)

	if _, err := exams.Delete(t.Context(), testExamID); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting the published exam = %v", err)
	}
	f.login(t, "0012")
	if _, err := publisher.Unpublish(t.Context(), testExamID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := exams.Delete(t.Context(), testExamID); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting an exam a student took = %v", err)
	}
	f.publish(t, secondExamID)
	if _, err := exams.Delete(t.Context(), secondExamID); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting the exam admission moved to = %v", err)
	}
	if _, err := publisher.Unpublish(t.Context(), secondExamID, false); err != nil {
		t.Fatal(err)
	}
	deleted, err := exams.Delete(t.Context(), secondExamID)
	if err != nil || deleted != (service.DeletedExam{ID: secondExamID, Title: "Exam 2"}) {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 1 || f.count(t, `SELECT count(*) FROM sessions`) != 1 {
		t.Fatal("deleting an exam changed something else")
	}
	if _, err := exams.Delete(t.Context(), secondExamID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
	if _, err := exams.Delete(t.Context(), "bad"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("bad id = %v", err)
	}
}
