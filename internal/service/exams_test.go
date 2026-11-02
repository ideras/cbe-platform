package service_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestAnExamIsDeletedOnlyWhenNobodyIsInItAndItsSubmissionsAreConfirmed(t *testing.T) {
	f := newFixture(t, 30)
	f.addExam(t, secondExamID, "second-pin", 1)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	publisher := service.NewPublisher(f.store, f.clock)

	if _, err := exams.Delete(t.Context(), testExamID, 0); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting the published exam = %v", err)
	}
	login := f.login(t, "0012")
	if _, err := publisher.Unpublish(t.Context(), testExamID, true); err != nil {
		t.Fatal(err)
	}
	// The student is still working: admission is closed, but the session runs on.
	if _, err := exams.Delete(t.Context(), testExamID, 1); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting an exam a student is working in = %v", err)
	}
	if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	// Finished, the exam has one submission and is deleted only by someone who was told so.
	for _, confirmed := range []int{0, 2} {
		if _, err := exams.Delete(t.Context(), testExamID, confirmed); !errors.Is(err, domain.ErrSubmissionsChanged) {
			t.Fatalf("deleting an exam with one submission after confirming %d = %v", confirmed, err)
		}
	}
	if _, err := exams.Delete(t.Context(), testExamID, -1); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("a negative count = %v", err)
	}
	if f.count(t, `SELECT count(*) FROM sessions`) != 1 || f.count(t, `SELECT count(*) FROM submission_versions`) == 0 {
		t.Fatal("a refused deletion removed submissions")
	}

	f.publish(t, secondExamID)
	if _, err := exams.Delete(t.Context(), secondExamID, 0); !errors.Is(err, domain.ErrExamInUse) {
		t.Fatalf("deleting the exam admission moved to = %v", err)
	}
	if _, err := publisher.Unpublish(t.Context(), secondExamID, false); err != nil {
		t.Fatal(err)
	}
	deleted, err := exams.Delete(t.Context(), secondExamID, 0)
	if err != nil || deleted != (service.DeletedExam{ID: secondExamID, Title: "Exam 2"}) {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 1 || f.count(t, `SELECT count(*) FROM sessions`) != 1 {
		t.Fatal("deleting an exam changed something else")
	}
	if _, err := exams.Delete(t.Context(), secondExamID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
	if _, err := exams.Delete(t.Context(), "bad", 0); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("bad id = %v", err)
	}

	taken, err := exams.Delete(t.Context(), testExamID, 1)
	if err != nil || taken != (service.DeletedExam{ID: testExamID, Title: "Test", Sessions: 1}) {
		t.Fatalf("deleted the exam a student took = %+v, %v", taken, err)
	}
	for _, table := range []string{"exams", "sessions", "submission_versions", "event_log", "screenshots"} {
		if n := f.count(t, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s still has %d rows", table, n)
		}
	}
	// The student is still on the class roster, ready for the next exam.
	if f.count(t, `SELECT count(*) FROM class_students`) == 0 {
		t.Error("the roster went with the exam")
	}
}

func TestDeletingAnExamRemovesItsScreenshotFilesAndRetriesTheOnesThatStay(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	shots, root := imageService(t, f)
	exams := service.NewExams(f.store, f.clock, f.sessions).RemovingFilesWith(shots)
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewPublisher(f.store, f.clock).Unpublish(t.Context(), testExamID, true); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, login.SessionID)
	file := filepath.Join(folder, fmtID(loss)+"-0.jpg")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the screenshot was not stored: %v", err)
	}

	deleted, err := exams.Delete(t.Context(), testExamID, 1)
	if err != nil || deleted.Sessions != 1 || deleted.Screenshots != 1 || deleted.FilesPending != 0 {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	// The picture and its folder are gone, and nothing waits to be removed.
	if _, err := os.Stat(folder); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the session's screenshot folder is still there: %v", err)
	}
	if f.count(t, `SELECT count(*) FROM deleted_screenshot_files`) != 0 {
		t.Fatal("a removed file is still listed")
	}
	if _, err := os.Stat(filepath.Join(root, ".exam-platform-owner")); err != nil {
		t.Fatalf("the screenshot root lost its ownership marker: %v", err)
	}
}

func TestAScreenshotFileThatCannotBeRemovedIsCountedAndRemovedLater(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not keep a file from being removed on Windows")
	}
	f := answerFixture(t)
	login := f.login(t, "0012")
	shots, root := imageService(t, f)
	exams := service.NewExams(f.store, f.clock, f.sessions).RemovingFilesWith(shots)
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewPublisher(f.store, f.clock).Unpublish(t.Context(), testExamID, true); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, login.SessionID)
	file := filepath.Join(folder, fmtID(loss)+"-0.jpg")
	// A folder nobody may write to keeps its file, which is what a full disk, a busy file or a permissions mistake looks like.
	if err := os.Chmod(folder, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0700) })
	if os.Geteuid() == 0 {
		t.Skip("root removes files whatever the folder's permissions")
	}

	deleted, err := exams.Delete(t.Context(), testExamID, 1)
	if err != nil || deleted.Sessions != 1 || deleted.Screenshots != 1 || deleted.FilesPending != 1 {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 0 || f.count(t, `SELECT count(*) FROM deleted_screenshot_files`) != 1 {
		t.Fatal("the exam is not deleted, or the file that stayed is not listed")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file that could not be removed is gone? %v", err)
	}

	// Once it can be removed, the next pass of the server's cleanup removes it, whether or not the server restarted.
	if err := os.Chmod(folder, 0700); err != nil {
		t.Fatal(err)
	}
	report, err := shots.Reconcile(t.Context())
	if err != nil || report.RemovedDeleted != 1 {
		t.Fatalf("reconcile = %+v, %v", report, err)
	}
	if _, err := os.Stat(folder); !errors.Is(err, fs.ErrNotExist) || f.count(t, `SELECT count(*) FROM deleted_screenshot_files`) != 0 {
		t.Fatal("the file was not removed, or is still listed")
	}
}

func TestPINIsReadForOneStoredExamAndNoListingCarriesIt(t *testing.T) {
	f := newFixture(t, 0)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	id, err := exams.Create(t.Context(), testClassID, service.CreateExamInput{Title: "Midterm", PIN: "  literal PIN 世界  ", DurationSeconds: 60, Content: examContent([]string{"q1"})})
	if err != nil {
		t.Fatal(err)
	}
	got, err := exams.PIN(t.Context(), id)
	if err != nil || got.ExamID != id || got.PIN != "  literal PIN 世界  " {
		t.Fatalf("PIN = %+v, %v; it must come back exactly as it was typed", got, err)
	}
	if _, err := exams.PIN(t.Context(), "not-an-id"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("a malformed ID = %v, want ErrInvalidInput", err)
	}
	if _, err := exams.PIN(t.Context(), "00000000-0000-4000-8000-0000000000ff"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an unknown exam = %v, want ErrNotFound", err)
	}
	detail, err := exams.Detail(t.Context(), id)
	if err != nil || strings.Contains(fmt.Sprintf("%+v", detail), "literal PIN") {
		t.Fatalf("the exam's detail carries the PIN: %+v, %v", detail, err)
	}
}
