package storage

import (
	"errors"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

const classA = "00000000-0000-4000-8000-0000000000c1"
const classB = "00000000-0000-4000-8000-0000000000c2"

func newClass(id, name string) domain.Class {
	return domain.Class{ID: id, Name: name, CreatedAt: testNow}
}

func TestClassIsStoredWithItsStudentsAndSharesPeopleWithOtherClasses(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0002", Name: "Grace", Email: "g@x.invalid"}, {AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	// 0001 also takes a second class; the person is stored once and the newest roster's data wins.
	if err := s.CreateClass(ctx, newClass(classB, "History 11B"), []domain.Student{{AccountNumber: "0001", Name: "Ada Lovelace"}, {AccountNumber: "0003", Name: "Alan"}}); err != nil {
		t.Fatal(err)
	}
	a, err := s.ClassStudents(ctx, classA)
	if err != nil || len(a) != 2 || a[0].AccountNumber != "0001" || a[0].Name != "Ada Lovelace" || a[1].AccountNumber != "0002" {
		t.Fatalf("class A = %+v, %v", a, err)
	}
	b, err := s.ClassStudents(ctx, classB)
	if err != nil || len(b) != 2 {
		t.Fatalf("class B = %+v, %v", b, err)
	}
	var people int
	if err := s.db.QueryRow(`SELECT count(*) FROM students`).Scan(&people); err != nil || people != 3 {
		t.Fatalf("people = %d, %v", people, err)
	}
	list, err := s.ListClasses(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "History 11B" || list[1].Name != "Mathematics 10A" || list[0].StudentCount != 2 || list[0].ExamCount != 0 || list[0].PublishedExam != nil || !list[0].CreatedAt.Equal(testNow) {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := s.ClassStudents(ctx, "00000000-0000-4000-8000-0000000000ff"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown class = %v", err)
	}
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		for account, want := range map[string]bool{"0001": true, "0002": true, "0003": false} {
			if got, err := tx.StudentInClass(ctx, classA, account); err != nil || got != want {
				t.Errorf("%s in class A = %v, %v", account, got, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClassNamesAreUniqueIgnoringCaseAndFailedCreationChangesNothing(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateClass(ctx, newClass(classB, "  mathematics 10a"[2:]), []domain.Student{{AccountNumber: "0009", Name: "Other"}}); !errors.Is(err, domain.ErrClassExists) {
		t.Fatalf("duplicate name = %v", err)
	}
	// A bad student row rolls back the class, the earlier rows of the same roster and updates to known people.
	if err := s.CreateClass(ctx, newClass(classB, "History 11B"), []domain.Student{{AccountNumber: "0001", Name: "Renamed"}, {AccountNumber: "0007", Name: "Seven"}, {AccountNumber: "  "}}); err == nil {
		t.Fatal("a roster with a blank account was accepted")
	}
	if err := s.CreateClass(ctx, newClass(classB, "Empty"), nil); err == nil {
		t.Fatal("an empty class was accepted")
	}
	list, err := s.ListClasses(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("classes after failures = %+v, %v", list, err)
	}
	var name string
	var people int
	if err := s.db.QueryRow(`SELECT (SELECT name FROM students WHERE account_number = '0001'), (SELECT count(*) FROM students)`).Scan(&name, &people); err != nil || name != "Ada" || people != 1 {
		t.Fatalf("failed creation leaked: %q %d %v", name, people, err)
	}
}

// addExam imports a bare exam into a class.
func addExam(t *testing.T, s *Store, id, class string, created time.Time) {
	t.Helper()
	if err := s.CreateExam(t.Context(), domain.Exam{ID: id, ClassID: class, Title: "Exam " + id[len(id)-1:], PIN: "p", DurationSeconds: 60, GraceSeconds: 30, CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
}

func publish(t *testing.T, s *Store, exam string, at time.Time) {
	t.Helper()
	if err := s.WithPublishTransaction(t.Context(), func(tx service.PublishTransaction) error { return tx.SetCurrentExam(t.Context(), exam, at) }); err != nil {
		t.Fatal(err)
	}
}

func TestTheClassListCountsExamsAndNamesThePublishedOne(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	const empty = "00000000-0000-4000-8000-0000000000c3"
	const a1, a2, b1 = "00000000-0000-4000-8000-0000000000a1", "00000000-0000-4000-8000-0000000000a2", "00000000-0000-4000-8000-0000000000b1"
	for id, name := range map[string]string{classA: "Mathematics 10A", classB: "History 11B", empty: "Art 9C"} {
		if err := s.CreateClass(ctx, newClass(id, name), []domain.Student{{AccountNumber: "9" + id[len(id)-1:], Name: "Someone"}}); err != nil {
			t.Fatal(err)
		}
	}
	addExam(t, s, a1, classA, testNow)
	addExam(t, s, a2, classA, testNow.Add(time.Minute))
	addExam(t, s, b1, classB, testNow.Add(2*time.Minute))
	summary := func() map[string]domain.ClassSummary {
		list, err := s.ListClasses(ctx)
		if err != nil || len(list) != 3 {
			t.Fatalf("list = %+v, %v", list, err)
		}
		byName := map[string]domain.ClassSummary{}
		for _, c := range list {
			byName[c.Name] = c
		}
		return byName
	}
	byName := summary()
	for name, count := range map[string]int{"Mathematics 10A": 2, "History 11B": 1, "Art 9C": 0} {
		if got := byName[name]; got.ExamCount != count || got.PublishedExam != nil {
			t.Fatalf("%s before publishing = %+v", name, got)
		}
	}
	if err := s.WithPublishTransaction(ctx, func(tx service.PublishTransaction) error {
		if err := tx.SetCurrentExam(ctx, "00000000-0000-4000-8000-0000000000ff", testNow); err == nil {
			t.Error("published an exam that does not exist")
		}
		exam, class, err := tx.CurrentExam(ctx)
		if err != nil || exam != "" || class != "" {
			t.Errorf("a failed publication left %q/%q, %v", exam, class, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	publish(t, s, a2, testNow)
	byName = summary()
	if got := byName["Mathematics 10A"].PublishedExam; got == nil || got.ExamID != a2 || got.Title != "Exam 2" || byName["History 11B"].PublishedExam != nil || byName["Art 9C"].PublishedExam != nil {
		t.Fatalf("after publishing an exam of class A: %+v", byName)
	}
	// The class admitted is the class of the exam; publishing another class's exam replaces it.
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		exam, class, err := tx.CurrentExam(ctx)
		if err != nil || exam != a2 || class != classA {
			t.Errorf("current = %q/%q, %v", exam, class, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if admitting, err := s.ClassAdmitting(ctx, classA); err != nil || !admitting {
		t.Fatalf("class A admitting = %v, %v", admitting, err)
	}
	publish(t, s, b1, testNow.Add(time.Hour))
	byName = summary()
	if got := byName["History 11B"].PublishedExam; got == nil || got.ExamID != b1 || byName["Mathematics 10A"].PublishedExam != nil || byName["Mathematics 10A"].ExamCount != 2 {
		t.Fatalf("after admission moved to class B: %+v", byName)
	}
	if admitting, err := s.ClassAdmitting(ctx, classA); err != nil || admitting {
		t.Fatalf("class A admitting after the move = %v, %v", admitting, err)
	}
}

func TestStudentsNotLoggedInAndTheRosterFlagFollowTheClassOfTheExam(t *testing.T) {
	s := openTest(t)
	seed(t, s) // the exam is in seedClass with 0012 (who has a session in it) and 0034
	ctx := t.Context()
	if _, err := s.ReplaceClassRoster(ctx, seedClass, []domain.Student{{AccountNumber: "0012", Name: "Ada"}, {AccountNumber: "0034", Name: "Grace"}, {AccountNumber: "0056", Name: "Alan"}}); err != nil {
		t.Fatal(err)
	}
	// Another class never matters to this exam, whether or not it is the one being admitted.
	if err := s.CreateClass(ctx, newClass(classB, "History 11B"), []domain.Student{{AccountNumber: "0078", Name: "Edsger"}}); err != nil {
		t.Fatal(err)
	}
	missing := func() string {
		var accounts []string
		if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
			students, err := tx.StudentsWithoutSession(ctx, examID)
			for _, student := range students {
				accounts = append(accounts, student.AccountNumber)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return strings.Join(accounts, ",")
	}
	flag := func() bool {
		var flag bool
		if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
			record, err := tx.ProctorSession(ctx, sessionID)
			flag = record.RosterActive
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return flag
	}
	if got := missing(); got != "0034,0056" && got != "0056,0034" {
		t.Fatalf("missing from the exam's class = %q (0012 has a session; 0078 is in another class)", got)
	}
	if !flag() {
		t.Fatal("a student of the exam's class is flagged as off the roster")
	}
	// Leaving the class takes the student off the exam's roster, but their session stays.
	if _, err := s.ReplaceClassRoster(ctx, seedClass, []domain.Student{{AccountNumber: "0034", Name: "Grace"}, {AccountNumber: "0056", Name: "Alan"}}); err != nil {
		t.Fatal(err)
	}
	if flag() {
		t.Fatal("a student who left the class is still flagged as on the roster")
	}
	if got := missing(); got != "0034,0056" && got != "0056,0034" {
		t.Fatalf("missing after the student with a session left = %q", got)
	}
	publish(t, s, examID, testNow)
	addExam(t, s, "00000000-0000-4000-8000-0000000000b1", classB, testNow)
	publish(t, s, "00000000-0000-4000-8000-0000000000b1", testNow.Add(time.Minute))
	if got := missing(); got != "0034,0056" && got != "0056,0034" {
		t.Fatalf("missing after admission moved to another class = %q", got)
	}
}

func TestRenameClassKeepsNamesUniqueIgnoringCase(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	for id, name := range map[string]string{classA: "Mathematics 10A", classB: "History 11B"} {
		if err := s.CreateClass(ctx, newClass(id, name), []domain.Student{{AccountNumber: "9" + id[len(id)-1:], Name: "Someone"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RenameClass(ctx, classA, "Mathematics 10B"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameClass(ctx, classA, "MATHEMATICS 10b"); err != nil {
		t.Fatalf("changing only the case of its own name: %v", err)
	}
	if err := s.RenameClass(ctx, classA, "history 11b"); !errors.Is(err, domain.ErrClassExists) {
		t.Fatalf("a name taken by another class = %v", err)
	}
	if err := s.RenameClass(ctx, "00000000-0000-4000-8000-0000000000ff", "Anything"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown class = %v", err)
	}
	list, err := s.ListClasses(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "History 11B" || list[1].Name != "MATHEMATICS 10b" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestDeleteClassIsRefusedWhileItHasExamsAndOtherwiseKeepsHistoryAndSharedPeople(t *testing.T) {
	s := openTest(t)
	seed(t, s) // seedClass holds 0012 (who has a session in the exam) and 0034, and the exam
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0012", Name: "Ada"}, {AccountNumber: "0034", Name: "Grace"}, {AccountNumber: "0056", Name: "Alan", Email: "alan@x.invalid"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateClass(ctx, newClass(classB, "History 11B"), []domain.Student{{AccountNumber: "0034", Name: "Grace"}}); err != nil {
		t.Fatal(err)
	}
	// An exam keeps its class from import on, whether or not anyone took it or it is published.
	spare := "00000000-0000-4000-8000-0000000000a1"
	addExam(t, s, spare, classB, testNow)
	for name, class := range map[string]string{"an exam students took": seedClass, "an exam nobody took": classB} {
		if _, err := s.DeleteClass(ctx, class); !errors.Is(err, domain.ErrClassHasExams) {
			t.Fatalf("deleting a class with %s = %v", name, err)
		}
	}
	publish(t, s, spare, testNow)
	if _, err := s.DeleteClass(ctx, classB); !errors.Is(err, domain.ErrClassHasExams) {
		t.Fatalf("deleting the class students are admitted from = %v", err)
	}
	if list, err := s.ListClasses(ctx); err != nil || len(list) != 3 {
		t.Fatalf("a refused deletion changed the classes: %+v, %v", list, err)
	}

	deleted, err := s.DeleteClass(ctx, classA)
	if err != nil || deleted.Name != "Mathematics 10A" || deleted.Students != 3 || deleted.Pruned != 1 {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	var people, sessions, gone int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM students), (SELECT count(*) FROM sessions), (SELECT count(*) FROM students WHERE account_number = '0056')`).Scan(&people, &sessions, &gone); err != nil {
		t.Fatal(err)
	}
	if people != 2 || sessions != 1 || gone != 0 {
		t.Fatalf("people = %d (0012 and 0034 are in the seeded class, 0056 pruned), sessions = %d, 0056 = %d", people, sessions, gone)
	}
	if _, err := s.DeleteClass(ctx, classA); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
	// Once its exams are deleted a class can go too.
	publishNothing(t, s)
	if _, err := s.DeleteExam(ctx, spare); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteClass(ctx, classB); err != nil || deleted.Name != "History 11B" || deleted.Students != 1 || deleted.Pruned != 0 {
		t.Fatalf("deleting a class whose exams are gone = %+v, %v", deleted, err)
	}
	if list, err := s.ListClasses(ctx); err != nil || len(list) != 1 || list[0].Name != "Seeded class" || list[0].ExamCount != 1 {
		t.Fatalf("classes after deleting = %+v, %v", list, err)
	}
}

func publishNothing(t *testing.T, s *Store) {
	t.Helper()
	if err := s.WithPublishTransaction(t.Context(), func(tx service.PublishTransaction) error { return tx.ClearCurrentExam(t.Context()) }); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceClassRosterChangesOnlyThatClass(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0012", Name: "Ada"}, {AccountNumber: "0034", Name: "Grace"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateClass(ctx, newClass(classB, "History 11B"), []domain.Student{{AccountNumber: "0034", Name: "Grace"}}); err != nil {
		t.Fatal(err)
	}
	change, err := s.ReplaceClassRoster(ctx, classA, []domain.Student{{AccountNumber: "0034", Name: "Grace Hopper"}, {AccountNumber: "0099", Name: "Linus"}})
	if err != nil || change != (domain.RosterReplacement{Added: 1, Removed: 1, Pruned: 0}) {
		t.Fatalf("change = %+v, %v (0012 left the class but keeps its session record)", change, err)
	}
	a, _ := s.ClassStudents(ctx, classA)
	if len(a) != 2 || a[0].AccountNumber != "0034" || a[0].Name != "Grace Hopper" || a[1].AccountNumber != "0099" {
		t.Fatalf("class A = %+v", a)
	}
	if b, _ := s.ClassStudents(ctx, classB); len(b) != 1 || b[0].Name != "Grace Hopper" {
		t.Fatalf("the person shared with class B should show the newest data and stay in it: %+v", b)
	}
	// 0099 has no session and no other class, so leaving takes them off the server.
	change, err = s.ReplaceClassRoster(ctx, classA, []domain.Student{{AccountNumber: "0034", Name: "Grace Hopper"}})
	if err != nil || change != (domain.RosterReplacement{Added: 0, Removed: 1, Pruned: 1}) {
		t.Fatalf("second change = %+v, %v", change, err)
	}
	var left int
	if err := s.db.QueryRow(`SELECT count(*) FROM students WHERE account_number = '0099'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("a student in no class with no session stayed: %d, %v", left, err)
	}
	// The same roster again changes nothing.
	if change, err := s.ReplaceClassRoster(ctx, classA, []domain.Student{{AccountNumber: "0034", Name: "Grace Hopper"}}); err != nil || change != (domain.RosterReplacement{}) {
		t.Fatalf("repeat = %+v, %v", change, err)
	}
}

func TestReplaceClassRosterFailuresChangeNothing(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if err := s.CreateClass(ctx, newClass(classA, "Mathematics 10A"), []domain.Student{{AccountNumber: "0001", Name: "Ada"}, {AccountNumber: "0002", Name: "Bob"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceClassRoster(ctx, classA, []domain.Student{{AccountNumber: "0009", Name: "New"}, {AccountNumber: "  "}}); err == nil {
		t.Fatal("a roster with a blank account was applied")
	}
	if _, err := s.ReplaceClassRoster(ctx, classA, nil); err == nil {
		t.Fatal("an empty roster was applied")
	}
	if _, err := s.ReplaceClassRoster(ctx, "00000000-0000-4000-8000-0000000000ff", []domain.Student{{AccountNumber: "0009", Name: "New"}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown class = %v", err)
	}
	a, err := s.ClassStudents(ctx, classA)
	if err != nil || len(a) != 2 || a[0].AccountNumber != "0001" || a[1].AccountNumber != "0002" {
		t.Fatalf("a failed replacement changed the class: %+v, %v", a, err)
	}
	var people int
	if err := s.db.QueryRow(`SELECT count(*) FROM students`).Scan(&people); err != nil || people != 2 {
		t.Fatalf("a failed replacement stored a person: %d, %v", people, err)
	}
	if admitting, err := s.ClassAdmitting(ctx, classA); err != nil || admitting {
		t.Fatalf("admitting = %v, %v", admitting, err)
	}
}
