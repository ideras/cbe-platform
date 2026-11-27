package dashboard

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// renderStudentEvaluation exports only the TODO rows shown in Student Review, in exam order.
// Include the feedback shown when a TODO is expanded, but not submitted files or run-wide output.
func renderStudentEvaluation(exam, student string, todos []GradingTodo, run Grading, ticked map[string]bool) []byte {
	var out strings.Builder
	out.WriteString("# Evaluation\n\n")
	if exam != "" {
		fmt.Fprintf(&out, "**Exam:** %s  \n", evaluationText(exam))
	}
	status := "Advisory — not confirmed"
	if run.Confirmed {
		status = "Confirmed"
	}
	fmt.Fprintf(&out, "**Student:** %s  \n**Submission:** #%d  \n**Total score:** %s (%s)\n", evaluationText(student), run.SubmissionID, evaluationScore(run.Points, run.Max), status)
	out.WriteString("\n## Grader feedback\n")
	if len(todos) == 0 {
		out.WriteString("\nThis exam has no TODO items to report.\n")
		return []byte(out.String())
	}
	for _, row := range studentEvaluationRows(todos, run, ticked) {
		id, title := evaluationText(row.todo.ID), evaluationText(row.todo.Text)
		if title == "" {
			title = id
		}
		score := "—"
		if row.earned != nil {
			score = evaluationScore(row.earned, row.todo.Points)
		}
		fmt.Fprintf(&out, "\n### %s\n\nID: **%s**  \nResult: **%s**  \nPoints: **%s**  \nMarked as done: **%s**\n\nOutput feedback:\n", title, id, row.verdict, score, row.tick)
		if strings.TrimSpace(row.feedback) == "" {
			out.WriteString("\nNo feedback was reported.\n")
			continue
		}
		writeEvaluationFeedback(&out, row.feedback)
	}
	return []byte(out.String())
}

type evaluationRow struct {
	todo                    GradingTodo
	earned                  *float64
	verdict, tick, feedback string
}

func studentEvaluationRows(todos []GradingTodo, run Grading, ticked map[string]bool) []evaluationRow {
	results := make(map[string]GradingResult, len(run.Results))
	for _, result := range run.Results {
		if result.Known {
			results[result.TodoID] = result
		}
	}
	rows := make([]evaluationRow, 0, len(todos))
	for _, todo := range todos {
		row := evaluationRow{todo: todo, verdict: "No result", tick: "Not available"}
		if result, found := results[todo.ID]; found {
			row.earned, row.feedback = &result.Points, result.Log
			row.verdict = "Not passed"
			if result.Passed {
				row.verdict = "Passed"
			}
		}
		if ticked != nil {
			row.tick = "No"
			if ticked[todo.ID] {
				row.tick = "Yes"
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func writeEvaluationFeedback(out *strings.Builder, log string) {
	log = strings.ReplaceAll(strings.ReplaceAll(log, "\r\n", "\n"), "\r", "\n")
	// Use a fence longer than any backtick run in the feedback so it stays literal Markdown text.
	longest, count := 0, 0
	for _, c := range log {
		if c == '`' {
			count++
			longest = max(longest, count)
		} else {
			count = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	fmt.Fprintf(out, "\n%stext\n%s\n%s\n", fence, strings.TrimSuffix(log, "\n"), fence)
}

func evaluationScore(earned, maximum *float64) string {
	number := func(value *float64) string {
		if value == nil {
			return "—"
		}
		// Round to two decimal places, like the Review table; omit trailing zeroes.
		rounded := *value
		if rounded < math.MaxFloat64/100 {
			rounded = math.Round(rounded*100) / 100
		}
		return strconv.FormatFloat(rounded, 'g', -1, 64)
	}
	if maximum != nil && *maximum > 0 {
		return number(earned) + " / " + number(maximum)
	}
	return number(earned)
}

var evaluationEscapes = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", "#", `\#`,
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "~", `\~`,
)

func evaluationText(s string) string {
	// Keep headings and field values on one line, without Markdown links or raw HTML.
	return evaluationEscapes.Replace(strings.Join(strings.Fields(s), " "))
}
