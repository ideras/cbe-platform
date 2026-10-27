package service_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

const secondExamID = "00000000-0000-4000-8000-000000000002"

// addExam imports an exam into the test class.
func (f *fixture) addExam(t *testing.T, id, pin string, exercises int) {
	f.addExamTo(t, testClassID, id, pin, exercises)
}

func (f *fixture) addExamTo(t *testing.T, classID, id, pin string, exercises int) {
	t.Helper()
	slots := make([][]string, exercises)
	for i := range slots {
		slots[i] = []string{fmt.Sprintf("q%d", i+1)}
	}
	created := epoch.Add(time.Duration(len(id)) * time.Second)
	if id == secondExamID {
		created = epoch.Add(time.Hour)
	}
	if err := f.store.CreateExam(t.Context(), domain.Exam{ID: id, ClassID: classID, Title: "Exam " + id[len(id)-1:], PIN: pin, DurationSeconds: 60, GraceSeconds: 30, MinVolumePercent: 55, Content: examContent(slots...), CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedExamAdmitsStudentsAndReplacingItProtectsActiveOnes(t *testing.T) {
	f := newUnpublishedFixture(t, 30)
	f.addExam(t, secondExamID, "second-pin", 2)
	open := testSessions(f.store, f.clock)
	publisher := service.NewPublisher(f.store, f.clock)

	if _, err := open.Login(t.Context(), "0012", "exam-secret"); !errors.Is(err, service.ErrNoCurrentExam) {
		t.Fatalf("login before publishing = %v", err)
	}
	if _, err := publisher.Publish(t.Context(), testExamID, "", false); err != nil {
		t.Fatal(err)
	}
	first, err := open.Login(t.Context(), "0012", "exam-secret")
	if err != nil {
		t.Fatal(err)
	}

	// A decision based on a stale view must not overwrite the current selection.
	if _, err := publisher.Publish(t.Context(), secondExamID, "", true); !errors.Is(err, service.ErrSelectionChanged) {
		t.Fatalf("stale publish = %v", err)
	}
	var active *service.ActiveSessionsError
	if _, err := publisher.Publish(t.Context(), secondExamID, testExamID, false); !errors.As(err, &active) || active.Count != 1 {
		t.Fatalf("publish over a working student = %v", err)
	}
	if catalog, _ := publisher.Catalog(t.Context()); catalog.CurrentExamID != testExamID || catalog.CurrentClassID != testClassID || catalog.CurrentClassName != "Test class" {
		t.Fatalf("a refused publish changed the selection: %+v", catalog)
	}
	result, err := publisher.Publish(t.Context(), secondExamID, testExamID, true)
	if err != nil || result.ReplacedExamID != testExamID || result.ReplacedClassID != testClassID || result.ActiveSessions != 1 || result.CurrentExamID != secondExamID || result.CurrentClassID != testClassID {
		t.Fatalf("confirmed publish = %+v, %v", result, err)
	}

	// The working student keeps full authority, but can no longer log back in to the old exam.
	if principal, err := open.Authenticate(t.Context(), first.Token); err != nil || principal.Session.ExamID != testExamID {
		t.Fatalf("existing session lost authority: %v", err)
	}
	if _, err := open.Login(t.Context(), "0012", "exam-secret"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("old PIN after replacing the exam = %v", err)
	}
	second, err := open.Login(t.Context(), "0034", "second-pin")
	if err != nil || second.MinVolumePercent != 55 {
		t.Fatalf("login to the published exam = %+v, %v", second, err)
	}
	if f.storedSession(t, second.Token).ExamID != secondExamID {
		t.Fatal("student was admitted to the wrong exam")
	}
}

func TestPublishValidationIdempotenceAndUnpublish(t *testing.T) {
	f := newUnpublishedFixture(t, 30)
	f.addExam(t, secondExamID, "second-pin", 1)
	open := testSessions(f.store, f.clock)
	publisher := service.NewPublisher(f.store, f.clock)

	const missing = "00000000-0000-4000-8000-0000000000ff"
	for name, call := range map[string]func() error{
		"bad exam id": func() error { _, err := publisher.Publish(t.Context(), "not-a-uuid", "", false); return err },
		"no exam":     func() error { _, err := publisher.Publish(t.Context(), "", "", false); return err },
		"bad expected exam": func() error {
			_, err := publisher.Publish(t.Context(), testExamID, "bad", false)
			return err
		},
		"unpublish bad expected": func() error { _, err := publisher.Unpublish(t.Context(), "bad", false); return err },
	} {
		if err := call(); !errors.Is(err, service.ErrInvalidInput) {
			t.Errorf("%s = %v", name, err)
		}
	}
	if _, err := publisher.Publish(t.Context(), missing, "", false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown exam = %v", err)
	}
	if _, err := publisher.Publish(t.Context(), testExamID, "", false); err != nil {
		t.Fatal(err)
	}
	if again, err := publisher.Publish(t.Context(), testExamID, testExamID, false); err != nil || again.ReplacedExamID != "" {
		t.Fatalf("publishing the published exam again = %+v, %v", again, err)
	}

	login := f.login(t, "0012")
	var active *service.ActiveSessionsError
	if _, err := publisher.Unpublish(t.Context(), testExamID, false); !errors.As(err, &active) {
		t.Fatalf("closing admission over a working student = %v", err)
	}
	// Once the student's deadline and grace have passed nobody can still be writing, even before the worker finalizes.
	f.clock.Set(epoch.Add(91 * time.Second))
	if _, err := publisher.Unpublish(t.Context(), testExamID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := open.Login(t.Context(), "0034", "exam-secret"); !errors.Is(err, service.ErrNoCurrentExam) {
		t.Fatalf("login after unpublishing = %v", err)
	}
	if _, err := open.Authenticate(t.Context(), login.Token); err != nil {
		t.Fatal("unpublishing revoked a session")
	}
	if _, err := publisher.Unpublish(t.Context(), "", false); err != nil {
		t.Fatalf("unpublishing nothing = %v", err)
	}
}

func TestOnlyTheStudentsOfTheExamsClassCanLogIn(t *testing.T) {
	const history, historyExam = "00000000-0000-4000-8000-0000000000c2", "00000000-0000-4000-8000-000000000003"
	f := newUnpublishedFixture(t, 30)
	f.addClass(t, history, "History 11B", domain.Student{AccountNumber: "0034", Name: "Grace"}, domain.Student{AccountNumber: "0077", Name: "Linus"}, domain.Student{AccountNumber: "0088", Name: "Edsger"})
	f.addExamTo(t, history, historyExam, "history-pin", 1)
	f.publish(t, testExamID)
	if _, err := f.sessions.Login(t.Context(), "0077", "exam-secret"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("a student of another class logged in with the right PIN: %v", err)
	}
	ada := f.login(t, "0012")

	// Admitting the other class means publishing its exam; Ada is still working, so it needs confirmation.
	publisher := service.NewPublisher(f.store, f.clock)
	var active *service.ActiveSessionsError
	if _, err := publisher.Publish(t.Context(), historyExam, testExamID, false); !errors.As(err, &active) || active.Count != 1 {
		t.Fatalf("publishing another class's exam over a working student = %v", err)
	}
	result, err := publisher.Publish(t.Context(), historyExam, testExamID, true)
	if err != nil || result.ReplacedClassID != testClassID || result.CurrentClassID != history || result.ReplacedExamID != testExamID {
		t.Fatalf("class change = %+v, %v", result, err)
	}
	if catalog, err := publisher.Catalog(t.Context()); err != nil || catalog.CurrentExamID != historyExam || catalog.CurrentClassID != history || catalog.CurrentClassName != "History 11B" {
		t.Fatalf("catalog after the change = %+v, %v", catalog, err)
	}
	if _, err := f.sessions.Login(t.Context(), "0077", "history-pin"); err != nil {
		t.Fatalf("a student of the new class cannot log in: %v", err)
	}
	if _, err := f.sessions.Login(t.Context(), "0012", "history-pin"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("a student of the first class logged in to the other class's exam: %v", err)
	}
	if principal, err := f.sessions.Authenticate(t.Context(), ada.Token); err != nil || principal.Session.ExamID != testExamID {
		t.Fatalf("a running session lost its authority: %v", err)
	}
	// The students not yet logged in are those of the exam's own class, whichever exam is published (listed by name).
	for exam, want := range map[string][]string{testExamID: {"0034"}, historyExam: {"0088", "0034"}} {
		list, err := f.sessions.ProctorSessions(t.Context(), exam)
		var got []string
		for _, student := range list.StudentsWithoutSession {
			got = append(got, student.AccountNumber)
		}
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("without a session in %s = %v, %v; want %v", exam[len(exam)-1:], got, err, want)
		}
	}
}

func TestCatalogListsNewestFirstWithoutSecrets(t *testing.T) {
	f := newFixture(t, 30)
	f.addExam(t, secondExamID, "second-pin", 3)
	f.login(t, "0012")
	publisher := service.NewPublisher(f.store, f.clock)
	catalog, err := publisher.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Exams) != 2 || catalog.Exams[0].ID != secondExamID || catalog.CurrentExamID != testExamID || catalog.CurrentClassName != "Test class" {
		t.Fatalf("catalog = %+v", catalog)
	}
	first, second := catalog.Exams[1], catalog.Exams[0]
	if second.ExerciseCount != 3 || second.Navigation != domain.FreeNavigation || second.MinVolumePercent != 55 || second.DurationSeconds != 60 || second.GraceSeconds != 30 || second.TotalSessions != 0 {
		t.Fatalf("second = %+v", second)
	}
	if first.ID != testExamID || first.ActiveSessions != 1 || first.TotalSessions != 1 || !first.Published || second.Published {
		t.Fatalf("first = %+v", first)
	}
	if first.ClassID != testClassID || first.ClassName != "Test class" || second.ClassID != testClassID || second.ClassName != "Test class" {
		t.Fatalf("exams are not named with their class: %+v", catalog.Exams)
	}
	f.clock.Set(epoch.Add(91 * time.Second))
	if later, _ := publisher.Catalog(t.Context()); later.Exams[1].ActiveSessions != 0 || later.Exams[1].TotalSessions != 1 {
		t.Fatalf("expired session still counted as working: %+v", later.Exams[1])
	}
}

func TestExamValidationListsEveryProblemWithoutEchoingThePIN(t *testing.T) {
	f := newFixture(t, 30)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	volume, grace := 101, int64(0)
	later, earlier := epoch.Add(time.Hour), epoch
	input := service.CreateExamInput{Title: " ", PIN: "", DurationSeconds: 0, GraceSeconds: &grace, MinVolumePercent: &volume, OpensAt: &later, ClosesAt: &earlier,
		Navigation: "back", Content: examContent([]string{"q1"}, []string{"q1"}, nil)}
	input.Content.Exercises[1].ID = "1"
	input.Content.Exercises[2].ID, input.Content.Exercises[2].Title = " x", ""
	_, err := exams.Create(t.Context(), testClassID, input)
	var invalid *service.ValidationError
	if !errors.Is(err, service.ErrInvalidInput) || !errors.As(err, &invalid) {
		t.Fatalf("error = %v", err)
	}
	joined := strings.Join(invalid.Problems, "\n")
	for _, want := range []string{"title is required", "pin is required", "duration_seconds must be", "grace_seconds must be", "min_volume_percent must be between 0 and 100",
		"opens_at must be earlier than closes_at", "navigation must be free or forward_only", "exercise 2 (1): id is already used by another exercise or item",
		"exercise 2 (1) slot q1: id is already used by another slot", "exercise 3 ( x): id is required", "exercise 3 ( x): title is required",
		"exercise 3 ( x): has no answer slot"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 1 {
		t.Fatal("an invalid exam was stored")
	}

	secret := service.CreateExamInput{Title: "", PIN: "super-secret-pin", DurationSeconds: 60, Content: examContent([]string{"q1"})}
	if _, err := exams.Preview(t.Context(), secret); err == nil || strings.Contains(err.Error(), "super-secret-pin") {
		t.Fatalf("PIN leaked into a validation error: %v", err)
	}
	many := service.CreateExamInput{Title: "Many", PIN: "p", DurationSeconds: 60}
	for i := 0; i < 50; i++ {
		many.Content.Exercises = append(many.Content.Exercises, domain.Exercise{ID: "same"})
	}
	_, err = exams.Preview(t.Context(), many)
	if !errors.As(err, &invalid) || len(invalid.Problems) > 21 {
		t.Fatalf("problems are not bounded: %d", len(invalid.Problems))
	}
}

func TestPreviewValidatesWithoutStoring(t *testing.T) {
	f := newFixture(t, 30)
	// The proctor's Settings give exam files without a grace period of their own 45 seconds.
	settings := testPolicy()
	settings.DefaultGrace = 45 * time.Second
	storeSettings(f.store, f.clock, settings)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	// The proctor checks the file before choosing the PIN, so a preview needs none.
	preview, err := exams.Preview(t.Context(), service.CreateExamInput{Title: " Final ", DurationSeconds: 600, Navigation: domain.ForwardOnly, Content: examContent([]string{"a", "b"}, []string{"c"})})
	if err != nil || preview.Title != "Final" || preview.ExerciseCount != 2 || preview.SlotCount != 3 || preview.Navigation != domain.ForwardOnly || preview.GraceSeconds != 45 || preview.MinVolumePercent != service.DefaultMinVolumePercent || preview.DurationSeconds != 600 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if f.count(t, `SELECT count(*) FROM exams`) != 1 {
		t.Fatal("preview stored an exam")
	}
}
