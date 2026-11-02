package examdoc

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Answer states of an answers-only export. They describe what the saved snapshot holds, not whether the answer is right.
const (
	// StateMissing: the snapshot has no key for the slot. It is not claimed that the student left the field blank.
	StateMissing = "missing"
	// StateEmpty: the key is present and its value is "".
	StateEmpty = "empty"
	// StateWhitespaceOnly: the value is nonempty and holds only whitespace.
	StateWhitespaceOnly = "whitespace-only"
	// StateStarterUnchanged: the value is exactly the slot's nonempty starter text.
	StateStarterUnchanged = "starter-unchanged"
	// StateAnswered: any other present value.
	StateAnswered = "answered"
	// StateUnmapped is the state of a saved value whose key matches no slot of the exam.
	StateUnmapped = "unmapped"
)

// Kinds of AnswersOnlyWarning.
const (
	// WarnMissing: an expected slot has no key in the snapshot.
	WarnMissing = "missing"
	// WarnUnmapped: the snapshot has a key that matches no slot of the exam.
	WarnUnmapped = "unmapped"
)

// AnswersOnlyWarning is something the instructor should look at in one student's answers-only file. It names a field, never
// an answer.
type AnswersOnlyWarning struct {
	Kind  string
	Field string
}

// slotIDPattern is what the exporter accepts as a slot ID. The compiler writes IDs like 1a.regex; anything else (a quote, a
// backtick, a line break) could end the info string of the fence that carries the ID, so the export refuses it.
var slotIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,200}$`)

// exportLanguagePattern is the compiler's rule for a code answer's language.
var exportLanguagePattern = regexp.MustCompile(`^[a-z0-9+#-]{1,32}$`)

// ClassifyAnswer says what the saved snapshot holds for a slot. It uses the two-value map lookup, so a missing key and an
// empty value are different states, and it decides on the exact stored string, never on a trimmed copy of it.
func ClassifyAnswer(slot Slot, answers map[string]string) string {
	value, present := answers[slot.ID]
	switch {
	case !present:
		return StateMissing
	case value == "":
		return StateEmpty
	// An unchanged starter is decided before the whitespace test, in case the starter is itself only whitespace.
	case slot.Starter != "" && value == slot.Starter:
		return StateStarterUnchanged
	case strings.TrimSpace(value) == "":
		return StateWhitespaceOnly
	default:
		return StateAnswered
	}
}

// AnswersOnly renders one student's saved answers without the exam: the fields in reading order, each with its stable ID,
// kind, answer state and a fenced block holding the saved string exactly as received. header is the front matter block
// (without a trailing blank line) and title the text of the document heading. Both are written by the caller; title is
// escaped here.
//
// The block is framed so the saved string can be recovered exactly: the opening fence line ends with one LF, the saved string
// follows unchanged, and one more LF (used only as framing, and always written, even when the string already ends in a newline)
// comes before the closing fence line. Remove exactly that one LF to get the saved string back.
//
// A saved key that matches no slot is written after the fields, in a section of its own, and reported as a warning together
// with every slot the snapshot has no key for. The content must have unique, usable slot IDs; otherwise nothing is rendered.
func AnswersOnly(content Content, answers map[string]string, header, title string) ([]byte, []AnswersOnlyWarning, error) {
	known := map[string]bool{}
	for _, exercise := range content.Exercises {
		for _, slot := range exercise.Slots {
			if err := checkExportSlot(slot, known); err != nil {
				return nil, nil, err
			}
		}
		for _, item := range exercise.Items {
			for _, slot := range item.Slots {
				if err := checkExportSlot(slot, known); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	var out strings.Builder
	var warnings []AnswersOnlyWarning
	if strings.TrimSpace(header) != "" {
		out.WriteString(strings.TrimRight(header, "\r\n"))
		out.WriteString("\n\n")
	}
	out.WriteString("# " + markdownText(title) + "\n\n")
	out.WriteString("Saved responses only. Consult the original exam for the questions.\n")
	field := func(slot Slot) {
		state := ClassifyAnswer(slot, answers)
		if state == StateMissing {
			warnings = append(warnings, AnswersOnlyWarning{Kind: WarnMissing, Field: slot.ID})
		}
		writeAnswersOnlyField(&out, slot, state, answers[slot.ID])
	}
	for _, exercise := range content.Exercises {
		out.WriteString("\n## " + exportLabel(exercise.ID, exercise.Title) + "\n")
		for _, slot := range exercise.Slots {
			field(slot)
		}
		for _, item := range exercise.Items {
			out.WriteString("\n### " + exportLabel(item.ID, item.Title) + "\n")
			for _, slot := range item.Slots {
				field(slot)
			}
		}
	}
	unmapped := make([]string, 0)
	for key := range answers {
		if !known[key] {
			unmapped = append(unmapped, key)
		}
	}
	sort.Strings(unmapped)
	if len(unmapped) > 0 {
		out.WriteString("\n## Unmapped saved answers\n\n")
		out.WriteString("These saved values belong to no answer field of the exam. They are kept as saved, with no question, language or grade guessed for them.\n")
		for i, key := range unmapped {
			warnings = append(warnings, AnswersOnlyWarning{Kind: WarnUnmapped, Field: key})
			out.WriteString("\n**Saved key:** " + codeSpan(strconv.Quote(key)) + "  \n")
			out.WriteString("**Answer state:** `" + StateUnmapped + "`\n\n")
			out.WriteString(fencedBlock(`title="unmapped:`+strconv.Itoa(i+1)+`"`, "text", answers[key]))
		}
	}
	return []byte(out.String()), warnings, nil
}

// checkExportSlot refuses a slot the export cannot place without confusing one answer with another.
func checkExportSlot(slot Slot, known map[string]bool) error {
	switch {
	case slot.ID == "":
		return errors.New("the exam has an answer field without an ID")
	case !slotIDPattern.MatchString(slot.ID):
		return fmt.Errorf("the exam has an answer field whose ID %s cannot be used in a file", strconv.Quote(shorten(slot.ID, 60)))
	case known[slot.ID]:
		return fmt.Errorf("the exam has two answer fields with the ID %q", slot.ID)
	}
	known[slot.ID] = true
	return nil
}

func writeAnswersOnlyField(out *strings.Builder, slot Slot, state, value string) {
	language := "text"
	if slot.Kind == Code && exportLanguagePattern.MatchString(slot.Language) {
		language = slot.Language
	}
	out.WriteString("\n**Field:** " + codeSpan(slot.ID) + "  \n")
	out.WriteString("**Kind:** " + codeSpan(exportKind(slot.Kind)) + "  \n")
	out.WriteString("**Answer state:** `" + state + "`\n\n")
	switch state {
	case StateMissing:
		out.WriteString("No saved value for this field.\n\n")
	case StateEmpty:
		out.WriteString("No answer.\n\n")
	case StateWhitespaceOnly:
		out.WriteString("The saved value contains only whitespace, kept as saved.\n\n")
	case StateStarterUnchanged:
		out.WriteString("The saved value is the unchanged starter text.\n\n")
	}
	out.WriteString(fencedBlock(`title="`+slot.ID+`"`, language, value))
}

// exportKind is a slot kind as the file names it; an unknown kind is written as "unknown" instead of as arbitrary text.
func exportKind(kind string) string {
	switch kind {
	case Blank, Text, Code:
		return kind
	}
	return "unknown"
}

// fencedBlock is value inside a backtick fence longer than any run of backticks in it, framed as AnswersOnly describes. info
// follows the language on the opening fence line and must already be safe there.
func fencedBlock(info, language, value string) string {
	fence := strings.Repeat("`", max(3, maxBacktickRun(value)+1))
	return fence + language + " " + info + "\n" + value + "\n" + fence + "\n"
}

// exportLabel is the heading text of an exercise or an item: its ID and its title, both as plain text.
func exportLabel(id, title string) string {
	id, title = markdownText(id), markdownText(title)
	switch {
	case id != "" && title != "":
		return id + " — " + title
	case id != "":
		return id
	case title != "":
		return title
	}
	return "Untitled"
}

// markdownText makes a title or a name plain text in Markdown: one line, with every ASCII punctuation character
// backslash-escaped, which CommonMark reads as that literal character. Nothing in it can start a tag, an image, a link
// (including a bare URL), an entity or a heading.
func markdownText(value string) string {
	var out strings.Builder
	space := false
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == ' ' || r < 0x20 || r == 0x7f:
			space = out.Len() > 0
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		if r < 0x80 && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// codeSpan writes a value as an inline code span whose fence cannot be ended by a backtick inside it.
func codeSpan(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, value)
	fence := strings.Repeat("`", maxBacktickRun(value)+1)
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		value = " " + value + " "
	}
	return fence + value + fence
}

func shorten(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
