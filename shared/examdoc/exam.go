// Package examdoc compiles an exam written in Markdown into the exam document the server stores.
//
// The Markdown is only the authoring format: the server never parses it. Prose is rendered to HTML here, answer fields
// become placeholders in that HTML, and the structure (exercises, items, answer slots) becomes JSON. docs/exam-format.md
// describes the format.
package examdoc

import (
	"time"

	"exam-platform/shared/calc"
)

// Navigation says whether students may move freely between exercises.
const (
	FreeNavigation = "free"
	// ForwardOnly means a student who leaves an exercise cannot return to it. The server enforces it.
	ForwardOnly = "forward_only"
)

// Slot kinds.
const (
	// Blank is a one-line field inside the text, such as a table cell to fill in.
	Blank = "blank"
	// Text is a multi-line free-text answer.
	Text = "text"
	// Code is a code answer edited in a code editor.
	Code = "code"
)

// Exam is a compiled exam: the body of the server's POST /admin/class/{id}/exam. The PIN is never part of the Markdown file;
// the proctor adds it when importing.
type Exam struct {
	Title            string     `json:"title"`
	PIN              string     `json:"pin,omitempty"`
	DurationSeconds  int64      `json:"duration_seconds"`
	GraceSeconds     *int64     `json:"grace_seconds,omitempty"`
	MinVolumePercent *int       `json:"min_volume_percent,omitempty"`
	OpensAt          *time.Time `json:"opens_at,omitempty"`
	ClosesAt         *time.Time `json:"closes_at,omitempty"`
	Navigation       string     `json:"navigation"`
	// Tools is what the front matter's calculator key declared (D9); nil means the exam has no tools. The
	// values are as written: the defaults live in calc.Effective, which the server applies before the student
	// application sees the config.
	Tools *calc.Tools `json:"tools,omitempty"`
	// SourceMarkdown is the Markdown the exam was compiled from. The server keeps it so the proctor can download the exam
	// with each student's answers filled in; Compile leaves it empty.
	SourceMarkdown string  `json:"source_markdown,omitempty"`
	Content        Content `json:"content"`
}

// Content is what students see. Every HTML string is a fragment; answer slots appear in it as elements carrying a
// data-slot attribute naming the slot, which the student application replaces with an input.
type Content struct {
	// Instructions are the general instructions shown with every exercise; they never hold answer slots.
	Instructions string     `json:"instructions,omitempty"`
	Exercises    []Exercise `json:"exercises"`
}

// Exercise is one page of the exam. Its body is the context shared by all of its items.
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

// Slot is one answer field. Its ID is unique in the exam and is the key of the student's answer.
type Slot struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Language names the language of a code answer, such as "mips" or "c".
	Language string `json:"language,omitempty"`
	// Lines is the visible height of a text or code answer.
	Lines int `json:"lines,omitempty"`
	// Starter is the text a text or code answer starts with.
	Starter string `json:"starter,omitempty"`
}

// SlotCount is the number of answer slots in the exam.
func (c Content) SlotCount() int {
	count := 0
	for _, exercise := range c.Exercises {
		count += len(exercise.Slots)
		for _, item := range exercise.Items {
			count += len(item.Slots)
		}
	}
	return count
}

// SlotIDs lists the ID of every answer slot, in reading order.
func (c Content) SlotIDs() []string {
	ids := make([]string, 0, c.SlotCount())
	for _, exercise := range c.Exercises {
		for _, slot := range exercise.Slots {
			ids = append(ids, slot.ID)
		}
		for _, item := range exercise.Items {
			for _, slot := range item.Slots {
				ids = append(ids, slot.ID)
			}
		}
	}
	return ids
}

// Points is the sum of the exercises' points, and false when an exercise has none.
func (c Content) Points() (float64, bool) {
	total := 0.0
	for _, exercise := range c.Exercises {
		if exercise.Points == nil {
			return 0, false
		}
		total += *exercise.Points
	}
	return total, len(c.Exercises) > 0
}
