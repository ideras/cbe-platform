package examdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the compiled sample exam")

// The sample exam is the reference for the format; its compiled form is what the server's tests import.
func TestSampleExamMatchesItsCompiledDocument(t *testing.T) {
	source, err := os.ReadFile("../../testdata/exam.md")
	if err != nil {
		t.Fatal(err)
	}
	exam, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	var compiled bytes.Buffer
	encoder := json.NewEncoder(&compiled)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(exam); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("../../testdata/exam.json", compiled.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile("../../testdata/exam.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, compiled.Bytes()) {
		t.Fatal("testdata/exam.json is not the compiled testdata/exam.md; run go test ./... -update in shared/examdoc")
	}
	if exam.Navigation != ForwardOnly || exam.DurationSeconds != 4800 || *exam.GraceSeconds != 120 || exam.PIN != "" || exam.Content.SlotCount() != 17 {
		t.Fatalf("unexpected settings: %+v", exam)
	}
	if total, ok := exam.Content.Points(); !ok || total != 100 {
		t.Fatalf("points = %v, %v", total, ok)
	}
}

const header = "---\ntitle: Quiz\nduration: 30m\n---\n"

func compile(t *testing.T, body string) Exam {
	t.Helper()
	exam, err := Compile([]byte(header + body))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	return exam
}

func TestStructureAndDefaults(t *testing.T) {
	exam := compile(t, "Read carefully.\n\n## First\n\nShared context.\n\n### Part one {points=2}\n\nValue: {{x}}\n\n### Part two {#custom points=3.5}\n\n```answer\nstarter\n```\n\n## Second {#q2}\n\n```answer {#prog lang=c lines=20}\nint main() {}\n```\n")
	content := exam.Content
	if exam.Navigation != FreeNavigation || exam.Title != "Quiz" || exam.DurationSeconds != 1800 || exam.GraceSeconds != nil {
		t.Fatalf("settings = %+v", exam)
	}
	if content.Instructions != "<p>Read carefully.</p>" || len(content.Exercises) != 2 {
		t.Fatalf("content = %+v", content)
	}
	first := content.Exercises[0]
	if first.ID != "1" || first.Title != "First" || first.Points != nil || first.Body != "<p>Shared context.</p>" || len(first.Items) != 2 {
		t.Fatalf("first = %+v", first)
	}
	if item := first.Items[0]; item.ID != "1a" || *item.Points != 2 || item.Slots[0] != (Slot{ID: "1a.x", Kind: Blank}) || item.Body != `<p>Value: <span class="slot" data-slot="1a.x"></span></p>` {
		t.Fatalf("first item = %+v", item)
	}
	if item := first.Items[1]; item.ID != "custom" || *item.Points != 3.5 || item.Slots[0] != (Slot{ID: "custom.answer", Kind: Text, Lines: defaultTextRows, Starter: "starter\n"}) || item.Body != `<div class="slot" data-slot="custom.answer"></div>` {
		t.Fatalf("second item = %+v", item)
	}
	second := content.Exercises[1]
	if second.ID != "q2" || len(second.Items) != 0 || second.Slots[0] != (Slot{ID: "q2.prog", Kind: Code, Language: "c", Lines: 20, Starter: "int main() {}\n"}) {
		t.Fatalf("second = %+v", second)
	}
}

func TestSlotsComeInReadingOrderWhereverTheyAre(t *testing.T) {
	exam := compile(t, "## One\n\n```answer {#first}\n```\n\n| a | b |\n|---|---|\n| {{cell}} | x |\n\n1. Item {{inlist}}\n\n   ```answer {#nested kind=code}\n   indented\n   ```\n")
	var ids []string
	for _, slot := range exam.Content.Exercises[0].Slots {
		ids = append(ids, slot.ID)
	}
	if strings.Join(ids, " ") != "1.first 1.cell 1.inlist 1.nested" {
		t.Fatalf("slots = %v", ids)
	}
	if nested := exam.Content.Exercises[0].Slots[3]; nested.Kind != Code || nested.Starter != "indented\n" {
		t.Fatalf("nested = %+v", nested)
	}
}

func TestTextIsEscapedAndCodeKeepsItsLanguage(t *testing.T) {
	exam := compile(t, "## One\n\nIs `a < b` & `<i>`? {{x}}\n\n```c\nif (a < b) {}\n```\n\n{{y}} uses a {single} brace.\n")
	body := exam.Content.Exercises[0].Body
	for _, want := range []string{"<code>a &lt; b</code> &amp; <code>&lt;i&gt;</code>", `<pre><code class="language-c">if (a &lt; b) {}`, "uses a {single} brace."} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
}

func TestFrontMatter(t *testing.T) {
	exam, err := Compile([]byte("---\n# comment\ntitle: \"Final: part 2\"\nduration: 1h20m\ngrace: 45s\nmin_volume: 0\nopens_at: 2026-10-20T08:00:00-06:00\ncloses_at: 2026-10-20T12:00:00-06:00\nnavigation: forward_only\npoints: 5\n---\n## A {points=5}\n{{x}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if exam.Title != "Final: part 2" || exam.DurationSeconds != 4800 || *exam.GraceSeconds != 45 || *exam.MinVolumePercent != 0 || exam.OpensAt.Hour() != 8 || !exam.ClosesAt.After(*exam.OpensAt) || exam.Navigation != ForwardOnly {
		t.Fatalf("exam = %+v", exam)
	}
}

func TestProblemsNameTheirLines(t *testing.T) {
	for name, test := range map[string]struct{ source, want string }{
		"no front matter":      {"## A\n{{x}}\n", "line 1: the file must start with a front matter block"},
		"unclosed front":       {"---\ntitle: x\n", "not closed"},
		"pin in file":          {"---\ntitle: x\nduration: 1m\npin: 1234\n---\n## A\n{{x}}\n", "line 4: the PIN does not belong in the exam file"},
		"unknown key":          {"---\ntitle: x\nduration: 1m\ncolour: red\n---\n## A\n{{x}}\n", `line 4: unknown front matter key "colour"`},
		"no title":             {"---\nduration: 1m\n---\n## A\n{{x}}\n", "must set a title"},
		"no duration":          {"---\ntitle: x\n---\n## A\n{{x}}\n", "must set a duration"},
		"bad duration":         {"---\ntitle: x\nduration: 90\n---\n## A\n{{x}}\n", "line 3: duration must be"},
		"bad navigation":       {"---\ntitle: x\nduration: 1m\nnavigation: back\n---\n## A\n{{x}}\n", "navigation must be free or forward_only"},
		"moved threshold":      {"---\ntitle: x\nduration: 1m\nfocus_threshold: 8s\n---\n## A\n{{x}}\n", "line 4: focus_threshold is set in the proctor's Settings now"},
		"no exercises":         {header + "Only instructions.\n", "the exam has no exercises"},
		"title heading":        {header + "# Exam\n## A\n{{x}}\n", "line 5: use ## for an exercise"},
		"link":                 {header + "## A\n[site](http://example.com) {{x}}\n", "line 6: links are not allowed"},
		"autolink":             {header + "## A\n<http://example.com> {{x}}\n", "links are not allowed"},
		"image":                {header + "## A\n![d](d.png) {{x}}\n", "images are not supported"},
		"html":                 {header + "## A\n<b>bold</b> {{x}}\n", "line 6: HTML is not allowed"},
		"html block":           {header + "## A\n{{x}}\n\n<div>\nx\n</div>\n", "line 8: HTML is not allowed"},
		"slot in instructions": {header + "Name: {{name}}\n## A\n{{x}}\n", "line 5: answer fields cannot be in the general instructions"},
		"slot in heading":      {header + "## A {{x}}\n{{y}}\n", "answer fields cannot be in headings"},
		"unclosed blank":       {header + "## A\n{{x\n", "line 6: {{ opens an answer field that is not closed"},
		"bad blank name":       {header + "## A\n{{two words}}\n", `answer field name "two words"`},
		"duplicate slot":       {header + "## A\n{{x}} {{x}}\n", `line 6: answer field "1.x" is already used`},
		"duplicate answer":     {header + "## A\n```answer\n```\n```answer\n```\n", `line 8: answer field "1.answer" is already used`},
		"duplicate id":         {header + "## A {#same}\n{{x}}\n## B {#same}\n{{y}}\n", `line 7: id "same" is already used`},
		"bad id":               {header + "## A {#a.b}\n{{x}}\n", `id "a.b" may only use`},
		"empty item":           {header + "## A\n### a\nNothing to answer.\n### b\n{{x}}\n", `line 6: item "a" has no answer field`},
		"empty exercise":       {header + "## A\nNothing.\n## B\n{{x}}\n", `line 5: exercise "A" has no answer field`},
		"item points":          {header + "## A {points=10}\n### a {points=3}\n{{x}}\n### b {points=4}\n{{y}}\n", `line 5: the items of exercise "A" add up to 7 points, not 10`},
		"total points":         {"---\ntitle: x\nduration: 1m\npoints: 100\n---\n## A {points=10}\n{{x}}\n", "the exercises add up to 10 points but the front matter says 100"},
		"unknown attribute":    {header + "## A {weight=3}\n{{x}}\n", `unknown heading attribute "weight"`},
		"negative points":      {header + "## A {points=-1}\n{{x}}\n", "points must be a positive number"},
		"unreadable attribute": {header + "## A {points=}\n{{x}}\n", "the attributes at the end of the heading could not be read"},
		"answer option":        {header + "## A\n```answer {#x rows=3}\n```\n", `unknown answer option "rows"`},
		"answer syntax":        {header + "## A\n```answer #x\n```\n", "the answer block options could not be read"},
		"answer kind":          {header + "## A\n```answer {kind=essay}\n```\n", "kind must be text or code"},
		"lang on text":         {header + "## A\n```answer {kind=text lang=c}\n```\n", "lang only applies to code answers"},
		"lines":                {header + "## A\n```answer {lines=1000}\n```\n", "lines must be a whole number"},
		"nested heading":       {header + "## A\n{{x}}\n> ### quoted\n", "headings must start at the beginning of a line"},
		"not utf8":             {header + "## A\n\xff{{x}}\n", "not UTF-8"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile([]byte(test.source))
			var problems Problems
			if !errors.As(err, &problems) {
				t.Fatalf("err = %v, want problems", err)
			}
			if !strings.Contains(problems.Error(), test.want) {
				t.Fatalf("problems:\n%s\nwant %q", problems.Error(), test.want)
			}
		})
	}
}

func TestProblemsAreBounded(t *testing.T) {
	var source strings.Builder
	source.WriteString(header)
	for range 80 {
		source.WriteString("## A {#x}\n{{x}}\n")
	}
	_, err := Compile([]byte(source.String()))
	var problems Problems
	if !errors.As(err, &problems) || len(problems) != maxProblems+1 || problems[maxProblems] != "more problems were found; fix these first" {
		t.Fatalf("problems = %d", len(problems))
	}
}
