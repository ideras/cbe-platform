package domain

import (
	"errors"
	"time"

	"exam-platform/shared/calc"
)

// ErrClassExists means a class with this name (compared ignoring case) is already stored.
var ErrClassExists = errors.New("a class with this name already exists")

// ErrClassHasExams means the class still has exams, so it cannot be deleted.
var ErrClassHasExams = errors.New("the class still has exams")

// ErrExamInUse means the exam is published or students have taken it, so it cannot be deleted.
var ErrExamInUse = errors.New("the exam is published or students have taken it")

// DeletedExam is the exam that was removed.
type DeletedExam struct {
	ID    string
	Title string
}

// DeletedClass says what removing a class took with it. Pruned counts the people removed because they belonged to no other
// class and had never logged in; students with a session stay, so their history keeps its names.
type DeletedClass struct {
	ID       string
	Name     string
	Students int
	Pruned   int
}

// RosterReplacement is what replacing a class roster changed: students new to the class, students no longer in it, and
// people removed from the server because they were left in no class and had never logged in.
type RosterReplacement struct {
	Added   int
	Removed int
	Pruned  int
}

// Class is a named roster stored on the server.
type Class struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// ClassExam is an exam of a class, as the class list names the one students are being admitted to.
type ClassExam struct {
	ExamID string
	Title  string
}

type ClassSummary struct {
	Class
	StudentCount int
	ExamCount    int
	// PublishedExam is the exam students log in to, when it belongs to this class.
	PublishedExam *ClassExam
}

type Student struct {
	AccountNumber string
	CanvasID      string
	Name          string
	Email         string
	SISUserID     string
}

// Navigation says whether students may move freely between the exercises of an exam.
type Navigation string

const (
	FreeNavigation Navigation = "free"
	// ForwardOnly means a student who leaves an exercise cannot return to it; the server keeps their answers to it.
	ForwardOnly Navigation = "forward_only"
)

// Answer slot kinds.
const (
	BlankSlot = "blank"
	TextSlot  = "text"
	CodeSlot  = "code"
)

// ExamContent is what students see, compiled from the exam's Markdown by shared/examdoc. Every HTML string is a fragment
// in which each answer slot appears as an element with a data-slot attribute naming it.
type ExamContent struct {
	Instructions string     `json:"instructions,omitempty"`
	Exercises    []Exercise `json:"exercises"`
}

// Exercise is one page of an exam; its body is the context its items share.
type Exercise struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Points *float64 `json:"points,omitempty"`
	Body   string   `json:"body,omitempty"`
	Slots  []Slot   `json:"slots,omitempty"`
	Items  []Item   `json:"items,omitempty"`
}

// Item is a part of an exercise, such as 1.a.
type Item struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Points *float64 `json:"points,omitempty"`
	Body   string   `json:"body,omitempty"`
	Slots  []Slot   `json:"slots,omitempty"`
}

// Slot is one answer field; its ID is the key of the student's answer.
type Slot struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Language string `json:"language,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	Starter  string `json:"starter,omitempty"`
}

// ExerciseSlots returns the slots of an exercise, its own and its items', in reading order.
func (e Exercise) ExerciseSlots() []Slot {
	slots := append([]Slot(nil), e.Slots...)
	for _, item := range e.Items {
		slots = append(slots, item.Slots...)
	}
	return slots
}

// SlotExercises maps every slot of the exam to the index of its exercise.
func (c ExamContent) SlotExercises() map[string]int {
	exercises := map[string]int{}
	for i, exercise := range c.Exercises {
		for _, slot := range exercise.ExerciseSlots() {
			exercises[slot.ID] = i
		}
	}
	return exercises
}

type Exam struct {
	ID string
	// ClassID is the class the exam was imported into; only its students can log in to it.
	ClassID         string
	Title           string
	PIN             string
	DurationSeconds int64
	GraceSeconds    int64
	// MinVolumePercent is the lowest system volume the student application keeps during the exam; 0 leaves it alone.
	MinVolumePercent int
	Navigation       Navigation
	// Policy is the settings frozen at the exam's latest publish; the zero policy means the exam was never published.
	Policy Policy
	// Tools is what the exam file's calculator key declared, compiled and validated (D8); nil means no tools.
	Tools   *calc.Tools
	Content ExamContent
	// SourceMarkdown is the exam file's Markdown as imported, kept so the proctor can download filled-in copies. nil when
	// nobody kept it (an exam imported through the API, say).
	SourceMarkdown *string
	CreatedAt      time.Time
	OpensAt        *time.Time
	ClosesAt       *time.Time
}

// ExamDetail is one exam as the proctor reads it to render students' submissions: its content and, when stored, the
// Markdown it was written in.
type ExamDetail struct {
	ID         string
	ClassID    string
	ClassName  string
	Title      string
	Navigation Navigation
	// Tools is what the exam file's calculator key declared, as stored; nil when the exam has none.
	Tools          *calc.Tools
	Content        ExamContent
	SourceMarkdown *string
}

// ExamSummary is what administrators see when listing exams: never the PIN or the exam's text.
type ExamSummary struct {
	ID               string
	ClassID          string
	ClassName        string
	Title            string
	ExerciseCount    int
	Navigation       Navigation
	DurationSeconds  int64
	GraceSeconds     int64
	MinVolumePercent int
	// Tools is what the exam file declared for the calculator, as stored; nil when the file names none.
	Tools     *calc.Tools
	CreatedAt time.Time
	OpensAt   *time.Time
	ClosesAt  *time.Time
	// ActiveSessions are sessions still active or in grace; TotalSessions includes finished ones.
	ActiveSessions int
	TotalSessions  int
	// LockedSessions are the running sessions a proctor has to unlock.
	LockedSessions int
}

type Answers map[string]string

type Version struct {
	ID        int64
	SessionID string
	Sequence  int64
	Answers   Answers
	SavedAt   time.Time
}

type SessionStatus string

const (
	Active         SessionStatus = "active"
	Grace          SessionStatus = "grace"
	Submitted      SessionStatus = "submitted"
	ForceSubmitted SessionStatus = "force_submitted"
)
