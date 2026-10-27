package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

const csvHeader = "canvas_id,account_number,name,email,sis_user_id\n"

func newClasses(f *fixture) *service.Classes { return service.NewClasses(f.store, f.clock) }

func TestCreateClassReportsBeforeStoringAndThenListsIt(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	const document = csvHeader + "1,0099,Linus,l@x.invalid,S3\n2,0100,Ada B,a@x.invalid,S4\n3,,Nobody,n@x.invalid,S5\n"

	report, err := classes.Create(t.Context(), "", strings.NewReader(document), false) // the name may wait until the file looks right
	if err != nil || report.Applied || report.Class != nil || report.Students != 2 || report.WarningCount != 1 || len(report.Warnings) != 1 {
		t.Fatalf("dry run = %+v, %v", report, err)
	}
	if list, _ := classes.List(t.Context()); len(list.Classes) != 1 {
		t.Fatalf("a dry run stored a class: %+v", list)
	}

	report, err = classes.Create(t.Context(), "  History 11B ", strings.NewReader(document), true)
	if err != nil || !report.Applied || report.Class == nil || report.Class.Name != "History 11B" || report.Class.StudentCount != 2 || report.Class.ExamCount != 0 || report.Class.PublishedExam != nil || !report.Class.CreatedAt.Equal(epoch) {
		t.Fatalf("create = %+v, %v", report, err)
	}
	list, err := classes.List(t.Context())
	if err != nil || len(list.Classes) != 2 || list.Classes[0].Name != "History 11B" || list.Classes[1].Name != "Test class" || list.Classes[1].ExamCount != 1 || list.Classes[0].PublishedExam != nil ||
		list.Classes[1].PublishedExam == nil || list.Classes[1].PublishedExam.ExamID != testExamID || list.Classes[1].PublishedExam.Title != "Test" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	roster, err := classes.Students(t.Context(), report.Class.ID)
	if err != nil || roster.Name != "History 11B" || roster.Total != 2 || len(roster.Students) != 2 || roster.Students[0] != (service.StudentEntry{AccountNumber: "0099", Name: "Linus"}) {
		t.Fatalf("students = %+v, %v", roster, err)
	}
	if strings.Contains(fmt.Sprint(roster), "x.invalid") {
		t.Fatal("an e-mail address was listed")
	}
	if _, err := classes.Students(t.Context(), "bad"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("bad id = %v", err)
	}
	if _, err := classes.Students(t.Context(), "00000000-0000-4000-8000-0000000000ff"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown class = %v", err)
	}
}

func TestClassNamesAreCheckedAndUniqueIgnoringCase(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	document := csvHeader + "1,0099,Linus,l@x.invalid,S3\n"
	for _, apply := range []bool{false, true} {
		if _, err := classes.Create(t.Context(), "test CLASS", strings.NewReader(document), apply); !errors.Is(err, domain.ErrClassExists) {
			t.Errorf("duplicate name (apply=%v) = %v", apply, err)
		}
	}
	var invalid *service.ValidationError
	for name, class := range map[string]string{"empty": "  ", "control": "Math\x00ematics", "newline": "Math\nematics", "long": strings.Repeat("é", service.MaxClassNameRunes+1)} {
		if _, err := classes.Create(t.Context(), class, strings.NewReader(document), true); !errors.As(err, &invalid) || !errors.Is(err, service.ErrInvalidInput) {
			t.Errorf("%s name = %v", name, err)
		}
	}
	if _, err := classes.Create(t.Context(), strings.Repeat("é", service.MaxClassNameRunes), strings.NewReader(document), true); err != nil {
		t.Fatalf("a name of exactly the limit was refused: %v", err)
	}
	// A bad name and a bad file are both reported at once.
	_, err := classes.Create(t.Context(), "", strings.NewReader("account_number,name\n1,A\n"), true)
	if !errors.As(err, &invalid) || len(invalid.Problems) != 2 {
		t.Fatalf("problems = %v", err)
	}
}

func TestClassRostersAreValidatedAndBounded(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	var invalid *service.ValidationError
	for name, document := range map[string]string{
		"empty":         "",
		"missing":       "account_number,name\n1,A\n",
		"header only":   csvHeader,
		"no usable row": csvHeader + "1,,A,a,s\n",
		"ragged":        csvHeader + "1,2\n",
	} {
		if _, err := classes.Create(t.Context(), "Valid name", strings.NewReader(document), true); !errors.As(err, &invalid) || !errors.Is(err, service.ErrInvalidInput) {
			t.Errorf("%s accepted or not a validation error: %v", name, err)
		}
	}
	if list, _ := classes.List(t.Context()); len(list.Classes) != 1 {
		t.Fatalf("a rejected roster created a class: %+v", list)
	}
	var many strings.Builder
	many.WriteString(csvHeader + "1,0012,Ada,a,s\n")
	for i := 0; i < 150; i++ {
		many.WriteString("9,,Blank,b,s\n")
	}
	report, err := classes.Create(t.Context(), "", strings.NewReader(many.String()), false)
	if err != nil || report.WarningCount != 150 || len(report.Warnings) != service.MaxRosterWarnings {
		t.Fatalf("warnings = %d/%d, %v", len(report.Warnings), report.WarningCount, err)
	}

	many.Reset()
	many.WriteString(csvHeader)
	for i := 0; i < service.MaxListedStudents+25; i++ {
		fmt.Fprintf(&many, "%d,%06d,Student %d,s%d@example.invalid,S%d\n", i, i, i, i, i)
	}
	created, err := classes.Create(t.Context(), "Large", strings.NewReader(many.String()), true)
	if err != nil {
		t.Fatal(err)
	}
	roster, err := classes.Students(t.Context(), created.Class.ID)
	if err != nil || roster.Total != service.MaxListedStudents+25 || len(roster.Students) != service.MaxListedStudents || roster.Students[0].AccountNumber != "000000" {
		t.Fatalf("bounded list = total %d, %d listed, %v", roster.Total, len(roster.Students), err)
	}
}

// fakeClassRepository counts writes to prove malformed input never reaches storage.
type fakeClassRepository struct{ creations int }

func (r *fakeClassRepository) CreateClass(context.Context, domain.Class, []domain.Student) error {
	r.creations++
	return nil
}
func (r *fakeClassRepository) ListClasses(context.Context) ([]domain.ClassSummary, error) {
	return nil, nil
}
func (r *fakeClassRepository) Class(context.Context, string) (domain.Class, error) {
	return domain.Class{}, domain.ErrNotFound
}
func (r *fakeClassRepository) ClassStudents(context.Context, string) ([]domain.Student, error) {
	return nil, nil
}
func (r *fakeClassRepository) RenameClass(context.Context, string, string) error { return nil }
func (r *fakeClassRepository) DeleteClass(context.Context, string) (domain.DeletedClass, error) {
	return domain.DeletedClass{}, nil
}
func (r *fakeClassRepository) ReplaceClassRoster(context.Context, string, []domain.Student) (domain.RosterReplacement, error) {
	return domain.RosterReplacement{}, nil
}
func (r *fakeClassRepository) ClassAdmitting(context.Context, string) (bool, error) {
	return false, nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestMalformedRosterDoesNotTouchRepository(t *testing.T) {
	repo := &fakeClassRepository{}
	classes := service.NewClasses(repo, fixedClock{now: epoch})
	_, err := classes.Create(t.Context(), "Mathematics 10A", strings.NewReader(csvHeader+"1,0012,A,a@example.invalid,S\n2,0034,B\n"), true)
	if err == nil || repo.creations != 0 {
		t.Fatalf("error = %v; creations = %d", err, repo.creations)
	}
	var reader io.Reader = strings.NewReader(csvHeader + "1,0012,A,a@example.invalid,S\n")
	if _, err := classes.Create(t.Context(), "Mathematics 10A", reader, true); err != nil || repo.creations != 1 {
		t.Fatalf("a good roster: %v; creations = %d", err, repo.creations)
	}
}

func TestRenameChecksTheNameAndKeepsItUnique(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	created, err := classes.Create(t.Context(), "History 11B", strings.NewReader(csvHeader+"1,0099,Linus,l@x.invalid,S3\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := classes.Rename(t.Context(), created.Class.ID, "  History 11C  ")
	if err != nil || renamed.Name != "History 11C" || renamed.ID != created.Class.ID || renamed.StudentCount != 1 {
		t.Fatalf("renamed = %+v, %v", renamed, err)
	}
	if renamed, err := classes.Rename(t.Context(), testClassID, "TEST class"); err != nil || renamed.Name != "TEST class" || renamed.ExamCount != 1 || renamed.PublishedExam == nil {
		t.Fatalf("changing only the case = %+v, %v", renamed, err)
	}
	if _, err := classes.Rename(t.Context(), created.Class.ID, "test CLASS"); !errors.Is(err, domain.ErrClassExists) {
		t.Fatalf("a name taken by another class = %v", err)
	}
	var invalid *service.ValidationError
	for name, bad := range map[string]string{"empty": " ", "control": "A\x00B", "long": strings.Repeat("é", service.MaxClassNameRunes+1)} {
		if _, err := classes.Rename(t.Context(), created.Class.ID, bad); !errors.As(err, &invalid) || !errors.Is(err, service.ErrInvalidInput) {
			t.Errorf("%s name = %v", name, err)
		}
	}
	if _, err := classes.Rename(t.Context(), "bad", "Fine"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("bad id = %v", err)
	}
	if _, err := classes.Rename(t.Context(), "00000000-0000-4000-8000-0000000000ff", "Fine"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown class = %v", err)
	}
}

func TestDeleteRefusesAClassWithExamsAndKeepsStudentsWhoAreInOtherClasses(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	f.login(t, "0012") // Ada has a session; Grace has none
	// An exam stays in its class for good: published or not, the class cannot go while it has one.
	for _, closed := range []bool{false, true} {
		if closed {
			if _, err := service.NewPublisher(f.store, f.clock).Unpublish(t.Context(), testExamID, true); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := classes.Delete(t.Context(), testClassID); !errors.Is(err, domain.ErrClassHasExams) {
			t.Fatalf("deleting a class with an exam (admission closed: %v) = %v", closed, err)
		}
	}
	if list, _ := classes.List(t.Context()); len(list.Classes) != 1 || list.Classes[0].ExamCount != 1 {
		t.Fatalf("a refused deletion removed the class: %+v", list)
	}
	// A class without exams can be deleted. People who are in no other class and never logged in go with it; Ada is still
	// in the first class and has a session.
	other, err := classes.Create(t.Context(), "History 11B", strings.NewReader(csvHeader+"1,0012,Ada,a@x.invalid,S1\n2,0077,Linus,l@x.invalid,S3\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := classes.Delete(t.Context(), other.Class.ID)
	if err != nil || deleted.Name != "History 11B" || deleted.Students != 2 || deleted.Pruned != 1 {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	if f.count(t, `SELECT count(*) FROM sessions`) != 1 || f.count(t, `SELECT count(*) FROM students`) != 2 {
		t.Fatal("deleting a class must keep sessions and the people who are in other classes")
	}
	if _, err := classes.Delete(t.Context(), "bad"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("bad id = %v", err)
	}
	if _, err := classes.Delete(t.Context(), other.Class.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
}

func TestReplacingARosterReportsFirstAndThenLetsOnlyTheNewStudentsIn(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	const document = csvHeader + "1,0034,Grace B,g@x.invalid,S2\n2,0099,Linus,l@x.invalid,S3\n3,,Nobody,n@x.invalid,S4\n"

	report, err := classes.ReplaceRoster(t.Context(), testClassID, strings.NewReader(document), false)
	if err != nil || report.Applied || report.Students != 2 || report.Added != 1 || report.Removed != 1 || report.WarningCount != 1 || len(report.Warnings) != 1 || !report.Admitting {
		t.Fatalf("dry run = %+v, %v", report, err)
	}
	if _, err := f.sessions.Login(t.Context(), "0012", "exam-secret"); err != nil {
		t.Fatalf("a dry run removed a student: %v", err)
	}
	report, err = classes.ReplaceRoster(t.Context(), testClassID, strings.NewReader(document), true)
	if err != nil || !report.Applied || report.Added != 1 || report.Removed != 1 || report.Pruned != 0 {
		t.Fatalf("apply = %+v, %v (Ada has a session from the login above, so she stays on the server)", report, err)
	}
	if _, err := f.sessions.Login(t.Context(), "0099", "exam-secret"); err != nil {
		t.Fatalf("an added student cannot log in: %v", err)
	}
	if _, err := f.sessions.Login(t.Context(), "0012", "exam-secret"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("a removed student can still log in: %v", err)
	}
	roster, err := classes.Students(t.Context(), testClassID)
	if err != nil || roster.Total != 2 || roster.Students[0] != (service.StudentEntry{AccountNumber: "0034", Name: "Grace B"}) {
		t.Fatalf("roster = %+v, %v", roster, err)
	}
	if again, err := classes.ReplaceRoster(t.Context(), testClassID, strings.NewReader(document), true); err != nil || again.Added != 0 || again.Removed != 0 {
		t.Fatalf("applying the same roster again = %+v, %v", again, err)
	}
}

func TestAClassThatIsNotAdmittedSaysSoWhenItsRosterChanges(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	other, err := classes.Create(t.Context(), "History 11B", strings.NewReader(csvHeader+"1,0077,Linus,l@x.invalid,S3\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	report, err := classes.ReplaceRoster(t.Context(), other.Class.ID, strings.NewReader(csvHeader+"1,0088,Edsger,e@x.invalid,S4\n"), true)
	if err != nil || report.Admitting || report.Added != 1 || report.Removed != 1 || report.Pruned != 1 {
		t.Fatalf("report = %+v, %v (Linus left the only class he was in and had never logged in)", report, err)
	}
}

func TestReplacingARosterRejectsBadInput(t *testing.T) {
	f := newFixture(t, 30)
	classes := newClasses(f)
	var invalid *service.ValidationError
	for name, document := range map[string]string{"empty": "", "missing column": "account_number,name\n1,A\n", "header only": csvHeader, "no usable row": csvHeader + "1,,A,a,s\n"} {
		if _, err := classes.ReplaceRoster(t.Context(), testClassID, strings.NewReader(document), true); !errors.As(err, &invalid) || !errors.Is(err, service.ErrInvalidInput) {
			t.Errorf("%s accepted or not a validation error: %v", name, err)
		}
	}
	if _, err := classes.ReplaceRoster(t.Context(), "bad", strings.NewReader(csvHeader+"1,1,A,a,s\n"), true); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("bad id = %v", err)
	}
	if _, err := classes.ReplaceRoster(t.Context(), "00000000-0000-4000-8000-0000000000ff", strings.NewReader(csvHeader+"1,1,A,a,s\n"), true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown class = %v", err)
	}
	if roster, _ := classes.Students(t.Context(), testClassID); roster.Total != 2 {
		t.Fatalf("a rejected roster changed the class: %+v", roster)
	}
}
