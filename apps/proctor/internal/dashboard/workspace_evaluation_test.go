package dashboard

import (
	"strings"
	"testing"
)

func TestStudentEvaluationMarkdownKeepsOnlyStudentFacingTODOs(t *testing.T) {
	todos := []GradingTodo{
		{ID: "parse", Text: "Parse a|b\n and handle <empty>", Points: new(5.0)},
		{ID: "errors", Text: "Report [bad inputs](example)", Points: new(4.0)},
		{ID: "tree", Text: "Build a tree", Points: new(3.0)},
		{ID: "format", Text: "Format output"},
		{ID: "file", Text: "Read files", Points: new(2.0)},
	}
	run := Grading{SubmissionID: 42, Confirmed: true, Points: new(6.256), Max: new(14.0), Stdout: "hidden stdout", Stderr: "hidden stderr", Skipped: []string{"private/path"}, Results: []GradingResult{
		{TodoID: "tree", Known: true, Points: 0},
		{TodoID: "parse", Known: true, Passed: true, Points: 5},
		{TodoID: "errors", Known: true, Points: 1.256, Log: "r1 ✓ · r2 ✗ (unexpected token)\r\n\r\n```\r\n<script>example</script>"},
		{TodoID: "format", Known: true, Passed: true},
		{TodoID: "hidden", Known: false, Points: 800},
	}}
	got := string(renderStudentEvaluation("Compiler Lab", "Sam & Lee", todos, run, map[string]bool{"parse": true, "errors": true}))
	want := "# Evaluation\n\n" +
		"**Exam:** Compiler Lab  \n**Student:** Sam &amp; Lee  \n**Submission:** #42  \n**Total score:** 6.26 / 14 (Confirmed)\n\n" +
		"## Grader feedback\n\n### Parse a|b and handle &lt;empty&gt;\n\n" +
		"ID: **parse**  \nResult: **Passed**  \nPoints: **5 / 5**  \nMarked as done: **Yes**\n\nOutput feedback:\n\nNo feedback was reported.\n" +
		"\n### Report \\[bad inputs\\](example)\n\nID: **errors**  \nResult: **Not passed**  \nPoints: **1.26 / 4**  \nMarked as done: **Yes**\n\nOutput feedback:\n" +
		"\n````text\nr1 ✓ · r2 ✗ (unexpected token)\n\n```\n<script>example</script>\n````\n" +
		"\n### Build a tree\n\nID: **tree**  \nResult: **Not passed**  \nPoints: **0 / 3**  \nMarked as done: **No**\n\nOutput feedback:\n\nNo feedback was reported.\n" +
		"\n### Format output\n\nID: **format**  \nResult: **Passed**  \nPoints: **0**  \nMarked as done: **No**\n\nOutput feedback:\n\nNo feedback was reported.\n" +
		"\n### Read files\n\nID: **file**  \nResult: **No result**  \nPoints: **—**  \nMarked as done: **No**\n\nOutput feedback:\n\nNo feedback was reported.\n"
	if got != want {
		t.Fatalf("report =\n%s\nwant\n%s", got, want)
	}
}

func TestStudentEvaluationMarkdownWithoutTODOs(t *testing.T) {
	got := string(renderStudentEvaluation("", "Sam", nil, Grading{SubmissionID: 1}, nil))
	if !strings.Contains(got, "## Grader feedback\n\nThis exam has no TODO items to report.") || strings.Contains(got, "### ") {
		t.Fatalf("expected an explicit empty report, got %q", got)
	}
}
