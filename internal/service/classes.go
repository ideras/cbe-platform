package service

import (
	"context"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"exam-platform/internal/clock"
	"exam-platform/internal/domain"
	"exam-platform/internal/roster"
)

// MaxRosterWarnings bounds the warnings returned to a caller; WarningCount still reports every one.
const MaxRosterWarnings = 100

// MaxClassNameRunes bounds a class name.
const MaxClassNameRunes = 120

// MaxListedStudents bounds the list returned to a caller; Total still says how many students the class has.
const MaxListedStudents = 5000

type ClassRepository interface {
	CreateClass(context.Context, domain.Class, []domain.Student) error
	ListClasses(context.Context) ([]domain.ClassSummary, error)
	Class(context.Context, string) (domain.Class, error)
	ClassStudents(context.Context, string) ([]domain.Student, error)
	RenameClass(ctx context.Context, id, name string) error
	DeleteClass(context.Context, string) (domain.DeletedClass, error)
	ReplaceClassRoster(ctx context.Context, id string, students []domain.Student) (domain.RosterReplacement, error)
	ClassAdmitting(context.Context, string) (bool, error)
}

// Classes keeps the classes the proctor teaches, each with its roster and the exams imported into it.
type Classes struct {
	repository ClassRepository
	clock      clock.Clock
}

func NewClasses(repository ClassRepository, clock clock.Clock) *Classes {
	return &Classes{repository: repository, clock: clock}
}

type RosterWarning struct {
	Row           int    `json:"row"`
	AccountNumber string `json:"account_number,omitempty"`
	Message       string `json:"message"`
}

// ClassExamEntry names an exam of a class.
type ClassExamEntry struct {
	ExamID string `json:"exam_id"`
	Title  string `json:"title"`
}

type ClassEntry struct {
	ID           string `json:"class_id"`
	Name         string `json:"name"`
	StudentCount int    `json:"student_count"`
	// ExamCount is how many exams were imported into the class.
	ExamCount int       `json:"exam_count"`
	CreatedAt time.Time `json:"created_at"`
	// PublishedExam is the exam students are being admitted to, when it belongs to this class.
	PublishedExam *ClassExamEntry `json:"published_exam"`
}

type ClassList struct {
	Classes []ClassEntry `json:"classes"`
}

// StudentEntry is all the proctor dashboard shows of a student: the account and the full name, never the e-mail address or
// the other roster columns.
type StudentEntry struct {
	AccountNumber string `json:"account_number"`
	Name          string `json:"student_name"`
}

type ClassRoster struct {
	ID       string         `json:"class_id"`
	Name     string         `json:"name"`
	Total    int            `json:"total"`
	Students []StudentEntry `json:"students"`
}

// ClassReport describes a roster document and, once applied, the class created from it.
type ClassReport struct {
	Students     int             `json:"students"`
	WarningCount int             `json:"warning_count"`
	Warnings     []RosterWarning `json:"warnings"`
	Applied      bool            `json:"applied"`
	Class        *ClassEntry     `json:"class"`
}

// RosterChange compares a roster file with the roster of a class and, once applied, says what changed.
type RosterChange struct {
	Students     int             `json:"students"`
	Added        int             `json:"added"`
	Removed      int             `json:"removed"`
	WarningCount int             `json:"warning_count"`
	Warnings     []RosterWarning `json:"warnings"`
	Applied      bool            `json:"applied"`
	// Admitting is true while this is the class students are being admitted from, so removed students cannot log in at all.
	Admitting bool `json:"admitting"`
	// Pruned is how many removed students were taken off the server too: they were in no other class and had never logged in.
	Pruned int `json:"pruned"`
}

// DeletedClass says what deleting a class took with it.
type DeletedClass struct {
	ID       string `json:"class_id"`
	Name     string `json:"name"`
	Students int    `json:"students"`
	Pruned   int    `json:"pruned"`
}

func entry(summary domain.ClassSummary) ClassEntry {
	result := ClassEntry{ID: summary.ID, Name: summary.Name, StudentCount: summary.StudentCount, ExamCount: summary.ExamCount, CreatedAt: summary.CreatedAt}
	if exam := summary.PublishedExam; exam != nil {
		result.PublishedExam = &ClassExamEntry{ExamID: exam.ExamID, Title: exam.Title}
	}
	return result
}

// validClassName trims the name and reports what is wrong with it.
func validClassName(name string) (string, string) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return name, "class name is required"
	case !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl):
		return name, "class name must be plain text"
	case utf8.RuneCountInString(name) > MaxClassNameRunes:
		return name, "class name is longer than 120 characters"
	}
	return name, ""
}

// Create checks a roster CSV and, with apply set, stores it as a new class. Without apply nothing is written, so the
// proctor can see what the file holds first; the name is then optional but still checked when given. A malformed
// document or name is a *ValidationError; a name already taken is domain.ErrClassExists.
func (s *Classes) Create(ctx context.Context, name string, input io.Reader, apply bool) (ClassReport, error) {
	var invalid ValidationError
	name, problem := validClassName(name)
	if problem != "" && (apply || name != "") {
		invalid.add("%s", problem)
	}
	parsed, err := roster.Parse(input)
	if err != nil {
		invalid.add("%s", err)
	}
	if len(invalid.Problems) > 0 {
		return ClassReport{}, &invalid
	}
	if name != "" {
		existing, err := s.repository.ListClasses(ctx)
		if err != nil {
			return ClassReport{}, err
		}
		for _, class := range existing {
			if strings.EqualFold(class.Name, name) {
				return ClassReport{}, domain.ErrClassExists
			}
		}
	}
	report := ClassReport{Students: len(parsed.Students), WarningCount: len(parsed.Warnings), Warnings: []RosterWarning{}}
	for i, warning := range parsed.Warnings {
		if i == MaxRosterWarnings {
			break
		}
		report.Warnings = append(report.Warnings, RosterWarning{Row: warning.Row, AccountNumber: warning.AccountNumber, Message: warning.Message})
	}
	if !apply {
		return report, nil
	}
	id, err := randomUUID()
	if err != nil {
		return ClassReport{}, err
	}
	class := domain.Class{ID: id, Name: name, CreatedAt: s.clock.Now().UTC()}
	if err := s.repository.CreateClass(ctx, class, parsed.Students); err != nil {
		return ClassReport{}, err
	}
	created := entry(domain.ClassSummary{Class: class, StudentCount: len(parsed.Students)})
	report.Applied, report.Class = true, &created
	return report, nil
}

// List returns every class by name.
func (s *Classes) List(ctx context.Context) (ClassList, error) {
	summaries, err := s.repository.ListClasses(ctx)
	if err != nil {
		return ClassList{}, err
	}
	list := ClassList{Classes: make([]ClassEntry, 0, len(summaries))}
	for _, summary := range summaries {
		list.Classes = append(list.Classes, entry(summary))
	}
	return list, nil
}

// Students lists a class's roster by account number, at most MaxListedStudents of it.
func (s *Classes) Students(ctx context.Context, classID string) (ClassRoster, error) {
	if !validProctorID(classID) {
		return ClassRoster{}, ErrInvalidInput
	}
	class, err := s.repository.Class(ctx, classID)
	if err != nil {
		return ClassRoster{}, err
	}
	students, err := s.repository.ClassStudents(ctx, classID)
	if err != nil {
		return ClassRoster{}, err
	}
	result := ClassRoster{ID: class.ID, Name: class.Name, Total: len(students), Students: make([]StudentEntry, 0, min(len(students), MaxListedStudents))}
	for _, student := range students[:min(len(students), MaxListedStudents)] {
		result.Students = append(result.Students, StudentEntry{AccountNumber: student.AccountNumber, Name: student.Name})
	}
	return result, nil
}

// find returns the class as the list shows it.
func (s *Classes) find(ctx context.Context, id string) (ClassEntry, error) {
	list, err := s.List(ctx)
	if err != nil {
		return ClassEntry{}, err
	}
	for _, class := range list.Classes {
		if class.ID == id {
			return class, nil
		}
	}
	return ClassEntry{}, domain.ErrNotFound
}

// Rename gives a class a new name, which must be valid and unique ignoring case. A malformed name is a *ValidationError; a
// name taken by another class is domain.ErrClassExists.
func (s *Classes) Rename(ctx context.Context, id, name string) (ClassEntry, error) {
	if !validProctorID(id) {
		return ClassEntry{}, ErrInvalidInput
	}
	name, problem := validClassName(name)
	if problem != "" {
		var invalid ValidationError
		invalid.add("%s", problem)
		return ClassEntry{}, &invalid
	}
	if err := s.repository.RenameClass(ctx, id, name); err != nil {
		return ClassEntry{}, err
	}
	return s.find(ctx, id)
}

// Delete removes a class and its roster. A class that has exams cannot be deleted (domain.ErrClassHasExams); sessions and
// answers are never touched.
func (s *Classes) Delete(ctx context.Context, id string) (DeletedClass, error) {
	if !validProctorID(id) {
		return DeletedClass{}, ErrInvalidInput
	}
	deleted, err := s.repository.DeleteClass(ctx, id)
	if err != nil {
		return DeletedClass{}, err
	}
	return DeletedClass{ID: deleted.ID, Name: deleted.Name, Students: deleted.Students, Pruned: deleted.Pruned}, nil
}

// ReplaceRoster compares a roster CSV with the roster of a class and, with apply set, makes the class hold exactly the students
// of the file. Without apply nothing is written. A malformed document is a *ValidationError.
func (s *Classes) ReplaceRoster(ctx context.Context, id string, input io.Reader, apply bool) (RosterChange, error) {
	if !validProctorID(id) {
		return RosterChange{}, ErrInvalidInput
	}
	parsed, err := roster.Parse(input)
	if err != nil {
		var invalid ValidationError
		invalid.add("%s", err)
		return RosterChange{}, &invalid
	}
	current, err := s.repository.ClassStudents(ctx, id)
	if err != nil {
		return RosterChange{}, err
	}
	admitting, err := s.repository.ClassAdmitting(ctx, id)
	if err != nil {
		return RosterChange{}, err
	}
	known := make(map[string]bool, len(current))
	for _, student := range current {
		known[student.AccountNumber] = true
	}
	change := RosterChange{Students: len(parsed.Students), WarningCount: len(parsed.Warnings), Warnings: []RosterWarning{}, Admitting: admitting}
	for _, student := range parsed.Students {
		if known[student.AccountNumber] {
			delete(known, student.AccountNumber)
		} else {
			change.Added++
		}
	}
	change.Removed = len(known)
	for i, warning := range parsed.Warnings {
		if i == MaxRosterWarnings {
			break
		}
		change.Warnings = append(change.Warnings, RosterWarning{Row: warning.Row, AccountNumber: warning.AccountNumber, Message: warning.Message})
	}
	if apply {
		applied, err := s.repository.ReplaceClassRoster(ctx, id, parsed.Students)
		if err != nil {
			return RosterChange{}, err
		}
		change.Added, change.Removed, change.Pruned, change.Applied = applied.Added, applied.Removed, applied.Pruned, true
	}
	return change, nil
}
