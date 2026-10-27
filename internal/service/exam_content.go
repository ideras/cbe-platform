package service

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"exam-platform/internal/domain"
)

// Limits of an exam's content. The HTML is produced by shared/examdoc; the server checks its structure, and the student
// application only shows it after removing anything but the few elements the compiler writes.
const (
	maxExercises     = 100
	maxItems         = 52
	maxSlots         = 1000
	maxFragmentBytes = 256 << 10
	maxStarterBytes  = 64 << 10
	maxContentBytes  = 1 << 20
	maxPoints        = 10000
	maxSlotLines     = 100
)

// slotAttribute starts an answer slot's placeholder. Text never contains it: the compiler escapes quotes in text as &quot;.
const slotAttribute = `data-slot="`

var (
	contentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	slotIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	languagePattern  = regexp.MustCompile(`^[a-z0-9+#-]{1,32}$`)
)

// validateContent reports everything wrong with an exam's content, naming exercises and items by position and ID.
func validateContent(content domain.ExamContent, problems *ValidationError) {
	if len(content.Exercises) == 0 || len(content.Exercises) > maxExercises {
		problems.add("content must contain between 1 and %d exercises", maxExercises)
	}
	if !utf8.ValidString(content.Instructions) || len(content.Instructions) > maxFragmentBytes {
		problems.add("instructions must be UTF-8 text of at most %d bytes", maxFragmentBytes)
	} else if strings.Contains(content.Instructions, slotAttribute) {
		problems.add("instructions cannot hold answer slots")
	}
	ids := map[string]bool{}
	slotIDs := map[string]bool{}
	size := len(content.Instructions)
	for i, exercise := range content.Exercises {
		label := fmt.Sprintf("exercise %d", i+1)
		if exercise.ID != "" {
			label += fmt.Sprintf(" (%s)", exercise.ID)
		}
		size += validatePart(label, exercise.ID, exercise.Title, exercise.Points, exercise.Body, exercise.Slots, ids, slotIDs, problems)
		if len(exercise.Items) > maxItems {
			problems.add("%s: has more than %d items", label, maxItems)
		}
		slots := len(exercise.Slots)
		for j, item := range exercise.Items {
			itemLabel := fmt.Sprintf("%s item %d", label, j+1)
			if item.ID != "" {
				itemLabel += fmt.Sprintf(" (%s)", item.ID)
			}
			size += validatePart(itemLabel, item.ID, item.Title, item.Points, item.Body, item.Slots, ids, slotIDs, problems)
			slots += len(item.Slots)
		}
		if slots == 0 {
			problems.add("%s: has no answer slot", label)
		}
	}
	if len(slotIDs) > maxSlots {
		problems.add("content has more than %d answer slots", maxSlots)
	}
	if size > maxContentBytes {
		problems.add("content is larger than 1 MiB combined")
	}
}

// validatePart checks an exercise or item and its slots, and returns how many bytes of text it holds.
func validatePart(label, id, title string, points *float64, body string, slots []domain.Slot, ids, slotIDs map[string]bool, problems *ValidationError) int {
	switch {
	case !contentIDPattern.MatchString(id):
		problems.add("%s: id is required and may only use letters, digits, _ and -, at most 64 characters", label)
	case ids[id]:
		problems.add("%s: id is already used by another exercise or item", label)
	}
	ids[id] = true
	if !validText(title, 512) {
		problems.add("%s: title is required and must be at most 512 bytes of text", label)
	}
	if points != nil && (!(*points > 0) || *points > maxPoints || math.IsInf(*points, 0)) {
		problems.add("%s: points must be a positive number up to %d", label, maxPoints)
	}
	if !utf8.ValidString(body) || len(body) > maxFragmentBytes {
		problems.add("%s: body must be UTF-8 text of at most %d bytes", label, maxFragmentBytes)
	}
	size := len(id) + len(title) + len(body)
	for k, slot := range slots {
		slotLabel := fmt.Sprintf("%s slot %d", label, k+1)
		if slot.ID != "" {
			slotLabel = fmt.Sprintf("%s slot %s", label, slot.ID)
		}
		switch {
		case !slotIDPattern.MatchString(slot.ID):
			problems.add("%s: id is required and may only use letters, digits, _, . and -, at most 128 characters", slotLabel)
		case slotIDs[slot.ID]:
			problems.add("%s: id is already used by another slot", slotLabel)
		case strings.Count(body, slotAttribute+slot.ID+`"`) != 1:
			problems.add("%s: must appear exactly once in the body", slotLabel)
		}
		slotIDs[slot.ID] = true
		switch slot.Kind {
		case domain.BlankSlot:
			if slot.Language != "" || slot.Lines != 0 || slot.Starter != "" {
				problems.add("%s: a blank takes no language, lines or starter", slotLabel)
			}
		case domain.TextSlot, domain.CodeSlot:
			if slot.Lines < 0 || slot.Lines > maxSlotLines {
				problems.add("%s: lines must be between 0 and %d", slotLabel, maxSlotLines)
			}
			if slot.Language != "" && (slot.Kind != domain.CodeSlot || !languagePattern.MatchString(slot.Language)) {
				problems.add("%s: language is a short lowercase name and only applies to code", slotLabel)
			}
			if !utf8.ValidString(slot.Starter) || len(slot.Starter) > maxStarterBytes {
				problems.add("%s: starter must be UTF-8 text of at most %d bytes", slotLabel, maxStarterBytes)
			}
		default:
			problems.add("%s: kind must be blank, text or code", slotLabel)
		}
		size += len(slot.ID) + len(slot.Starter)
	}
	if strings.Count(body, slotAttribute) != len(slots) {
		problems.add("%s: body holds answer slots that are not listed", label)
	}
	return size
}

// startingAnswers are the answers of a session with no saved version: each text or code slot holds its starter.
func startingAnswers(content domain.ExamContent) domain.Answers {
	answers := domain.Answers{}
	for _, exercise := range content.Exercises {
		for _, slot := range exercise.ExerciseSlots() {
			answers[slot.ID] = slot.Starter
		}
	}
	return answers
}

// lockAnswers keeps a forward-only student's answers to every exercise but the current one as they were last saved, so
// a closed exercise cannot change and a later one cannot be answered early. latest is nil before the first save.
func lockAnswers(exam domain.Exam, current int, answers domain.Answers, latest *domain.Version) domain.Answers {
	if exam.Navigation != domain.ForwardOnly {
		return answers
	}
	saved := startingAnswers(exam.Content)
	if latest != nil {
		saved = latest.Answers
	}
	locked := make(domain.Answers, len(answers))
	for id, exercise := range exam.Content.SlotExercises() {
		text, sent := answers[id]
		if exercise != current || !sent {
			text = saved[id]
		}
		locked[id] = text
	}
	return locked
}
