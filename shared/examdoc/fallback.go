package examdoc

import (
	"fmt"
	"strings"
)

// Fallback renders a student's file from an exam's compiled content alone, for an exam whose Markdown source the server does
// not have. The exercises and items become headings and each answer sits under its slot ID, in the same blank and block
// formats Fill uses, so the grader can match every answer with the solution file.
func Fallback(content Content, answers map[string]string, header string) ([]byte, error) {
	var out strings.Builder
	if strings.TrimSpace(header) != "" {
		out.WriteString(strings.TrimRight(header, "\r\n"))
		out.WriteByte('\n')
	}
	for _, exercise := range content.Exercises {
		fmt.Fprintf(&out, "## %s\n\n", exercise.Title)
		writeFallbackSlots(&out, exercise.Slots, answers)
		for _, item := range exercise.Items {
			fmt.Fprintf(&out, "### %s\n\n", item.Title)
			writeFallbackSlots(&out, item.Slots, answers)
		}
	}
	return []byte(out.String()), nil
}

func writeFallbackSlots(out *strings.Builder, slots []Slot, answers map[string]string) {
	for _, slot := range slots {
		answer := answers[slot.ID]
		if slot.Kind == Blank {
			fmt.Fprintf(out, "**%s:** %s\n\n", slot.ID, fillBlank(answer, false))
			continue
		}
		fmt.Fprintf(out, "**%s:**\n\n%s\n\n", slot.ID, fillAnswerBlock(slot, answer, ""))
	}
}
