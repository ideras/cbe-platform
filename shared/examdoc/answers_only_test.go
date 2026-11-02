package examdoc

import (
	"bytes"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	goldmarkast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// decodeAnswersOnly is the reference decoder of the framing AnswersOnly documents: it finds every fenced block, reads the field
// ID from title="...", and recovers the saved string by removing exactly the one LF written before the closing fence. It does
// not trim anything.
func decodeAnswersOnly(t *testing.T, document string) map[string]string {
	t.Helper()
	title := regexp.MustCompile(`title="([^"]*)"$`)
	blocks := map[string]string{}
	for position := 0; position < len(document); {
		end := strings.IndexByte(document[position:], '\n')
		if end < 0 {
			break
		}
		line := document[position : position+end]
		next := position + end + 1
		if !strings.HasPrefix(line, "```") {
			position = next
			continue
		}
		fence := line[:len(line)-len(strings.TrimLeft(line, "`"))]
		match := title.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("a fenced block has no title marker: %q", line)
		}
		// The region starts at the LF that ends the opening fence line; the closing fence line follows the framing LF.
		region := document[next-1:]
		closing := strings.Index(region, "\n"+fence+"\n")
		if closing < 0 {
			t.Fatalf("the block %q is not closed", match[1])
		}
		if _, dup := blocks[match[1]]; dup {
			t.Fatalf("the block %q appears twice", match[1])
		}
		blocks[match[1]] = region[1:closing]
		position = next - 1 + closing + len(fence) + 2
	}
	return blocks
}

func answersOnly(t *testing.T, content Content, answers map[string]string) (string, []AnswersOnlyWarning) {
	t.Helper()
	data, warnings, err := AnswersOnly(content, answers, "---\nexport_format: \"answers-only\"\n---", "Quiz — Student")
	if err != nil {
		t.Fatalf("AnswersOnly failed: %v", err)
	}
	return string(data), warnings
}

// courseExam is shaped like the course's first exam: three regular-expression items and three code items, one field each.
func courseExam() Content {
	return Content{
		Instructions: "SENTINEL-INSTRUCTIONS read everything first",
		Exercises: []Exercise{
			{ID: "1", Title: "Regular expressions", Body: `<p>SENTINEL-EXERCISE-BODY</p><img src="data:image/png;base64,SENTINEL-IMAGE">`, Items: []Item{
				{ID: "1a", Title: "Binary literals", Body: "<p>SENTINEL-ITEM-A</p>", Slots: []Slot{{ID: "1a.regex", Kind: Text}}},
				{ID: "1b", Title: "Character literals", Body: "<p>SENTINEL-ITEM-B</p>", Slots: []Slot{{ID: "1b.regex", Kind: Text}}},
				{ID: "1c", Title: "Identifiers", Slots: []Slot{{ID: "1c.regex", Kind: Text}}},
			}},
			{ID: "2", Title: "Code", Body: "<p>SENTINEL-EXERCISE-2</p>", Items: []Item{
				{ID: "2a", Title: "Sum", Slots: []Slot{{ID: "2a.code", Kind: Code, Language: "cpp", Starter: "int sum(int a, int b) {\n}\n"}}},
				{ID: "2b", Title: "Maximum", Slots: []Slot{{ID: "2b.code", Kind: Code, Language: "cpp", Starter: "int max(int a, int b) {\n}\n"}}},
				{ID: "2c", Title: "Reverse", Slots: []Slot{{ID: "2c.code", Kind: Code, Language: "mips"}}},
			}},
		},
	}
}

func TestAnswersOnlyHasTheContractedShape(t *testing.T) {
	answers := map[string]string{
		"1a.regex": "0[bB]([01]+_?[01]+)",
		"1b.regex": "",
		"1c.regex": "[a-z]+",
		"2a.code":  "int sum(int a, int b) {\n  return a + b;\n}\n",
		"2b.code":  "int max(int a, int b) {\n}\n",
		"2c.code":  "  ",
	}
	got, warnings := answersOnly(t, courseExam(), answers)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
	for _, want := range []string{
		"---\nexport_format: \"answers-only\"\n---\n\n# Quiz — Student\n\nSaved responses only. Consult the original exam for the questions.\n",
		"\n## 1 — Regular expressions\n\n### 1a — Binary literals\n\n**Field:** `1a.regex`  \n**Kind:** `text`  \n**Answer state:** `answered`\n\n```text title=\"1a.regex\"\n0[bB]([01]+_?[01]+)\n```\n",
		"**Field:** `1b.regex`  \n**Kind:** `text`  \n**Answer state:** `empty`\n\nNo answer.\n\n```text title=\"1b.regex\"\n\n```\n",
		"```cpp title=\"2a.code\"\nint sum(int a, int b) {\n  return a + b;\n}\n\n```\n",
		"**Answer state:** `starter-unchanged`\n\nThe saved value is the unchanged starter text.\n\n```cpp title=\"2b.code\"\nint max(int a, int b) {\n}\n\n```\n",
		"**Answer state:** `whitespace-only`\n\nThe saved value contains only whitespace, kept as saved.\n\n```mips title=\"2c.code\"\n  \n```\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the document does not contain %q:\n%s", want, got)
		}
	}
	// The incorrect expression is kept as saved: the export is a record, not a correction.
	if decoded := decodeAnswersOnly(t, got); decoded["1a.regex"] != "0[bB]([01]+_?[01]+)" {
		t.Errorf("the saved regular expression was changed: %q", decoded["1a.regex"])
	}
}

func TestAnswersOnlyLeavesTheQuestionsOut(t *testing.T) {
	content := courseExam()
	content.Exercises[0].Slots = []Slot{{ID: "1.intro", Kind: Blank, Starter: "SENTINEL-BLANK-STARTER"}}
	got, _ := answersOnly(t, content, map[string]string{"1.intro": "typed"})
	for _, sentinel := range []string{"SENTINEL-INSTRUCTIONS", "SENTINEL-EXERCISE-BODY", "SENTINEL-IMAGE", "SENTINEL-ITEM-A", "SENTINEL-ITEM-B", "SENTINEL-EXERCISE-2", "SENTINEL-BLANK-STARTER", "data:image", "<img", "int sum(int a, int b)", "int max(int a, int b)"} {
		if strings.Contains(got, sentinel) {
			t.Errorf("the answers-only document contains %q:\n%s", sentinel, got)
		}
	}
	// A starter is in the file only when it is what the student saved.
	saved, _ := answersOnly(t, content, map[string]string{"2a.code": "int sum(int a, int b) {\n}\n"})
	if !strings.Contains(saved, "int sum(int a, int b) {") || !strings.Contains(saved, "`starter-unchanged`") {
		t.Errorf("a saved starter was left out:\n%s", saved)
	}
}

func TestAnswersOnlyKeepsReadingOrderAndEveryKind(t *testing.T) {
	// Reading order is the stored order, not the order of the IDs: exercise slots first, then each item's slots.
	content := Content{Exercises: []Exercise{
		{ID: "2", Title: "Second", Slots: []Slot{{ID: "2.z", Kind: Blank}, {ID: "2.a", Kind: Text}}, Items: []Item{
			{ID: "2b", Title: "B", Slots: []Slot{{ID: "2b.y", Kind: Code, Language: "c"}, {ID: "2b.x", Kind: Blank}}},
			{ID: "2a", Title: "A", Slots: []Slot{{ID: "2a.q", Kind: Code, Language: "java"}}},
		}},
		{ID: "1", Title: "First", Items: []Item{{ID: "1a", Title: "A", Slots: []Slot{{ID: "1a.w", Kind: Code}}}}},
	}}
	answers := map[string]string{"2.z": "z", "2.a": "a", "2b.y": "y", "2b.x": "x", "2a.q": "q", "1a.w": "w"}
	got, warnings := answersOnly(t, content, answers)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
	want := content.SlotIDs()
	var found []string
	for _, match := range regexp.MustCompile("\\*\\*Field:\\*\\* `([^`]+)`").FindAllStringSubmatch(got, -1) {
		found = append(found, match[1])
	}
	if !reflect.DeepEqual(found, want) {
		t.Fatalf("fields are in the order %v, want %v", found, want)
	}
	for id, kind := range map[string]string{"2.z": "blank", "2.a": "text", "2b.y": "code", "2b.x": "blank", "2a.q": "code", "1a.w": "code"} {
		if !strings.Contains(got, "**Field:** `"+id+"`  \n**Kind:** `"+kind+"`") {
			t.Errorf("field %s is not listed as %s:\n%s", id, kind, got)
		}
	}
	for _, fence := range []string{"```text title=\"2.z\"", "```text title=\"2.a\"", "```c title=\"2b.y\"", "```text title=\"2b.x\"", "```java title=\"2a.q\"", "```text title=\"1a.w\""} {
		if !strings.Contains(got, fence) {
			t.Errorf("the document has no block %q:\n%s", fence, got)
		}
	}
	if decoded := decodeAnswersOnly(t, got); !reflect.DeepEqual(decoded, answers) {
		t.Errorf("decoded = %v, want %v", decoded, answers)
	}
}

func TestAnswersOnlyDistinguishesTheAnswerStates(t *testing.T) {
	slots := []Slot{
		{ID: "a.missing", Kind: Text},
		{ID: "a.empty", Kind: Text},
		{ID: "a.space", Kind: Text},
		{ID: "a.starter", Kind: Text, Starter: "Write here"},
		{ID: "a.codestarter", Kind: Code, Language: "c", Starter: "int main() {}"},
		{ID: "a.spacestarter", Kind: Code, Language: "c", Starter: "  "},
		{ID: "a.changed", Kind: Code, Language: "c", Starter: "int main() {}"},
		{ID: "a.marker", Kind: Text},
		{ID: "a.blank", Kind: Blank},
	}
	content := Content{Exercises: []Exercise{{ID: "a", Title: "States", Slots: slots}}}
	answers := map[string]string{
		"a.empty":        "",
		"a.space":        " \t\n ",
		"a.starter":      "Write here",
		"a.codestarter":  "int main() {}",
		"a.spacestarter": "  ",
		"a.changed":      "int main() { return 0; }",
		"a.marker":       "*(no answer)*",
		"a.blank":        "16",
	}
	want := map[string]string{
		"a.missing": StateMissing, "a.empty": StateEmpty, "a.space": StateWhitespaceOnly, "a.starter": StateStarterUnchanged,
		"a.codestarter": StateStarterUnchanged, "a.spacestarter": StateStarterUnchanged, "a.changed": StateAnswered, "a.marker": StateAnswered, "a.blank": StateAnswered,
	}
	for _, slot := range slots {
		if got := ClassifyAnswer(slot, answers); got != want[slot.ID] {
			t.Errorf("%s is %s, want %s", slot.ID, got, want[slot.ID])
		}
	}
	got, warnings := answersOnly(t, content, answers)
	for id, state := range want {
		if !strings.Contains(got, "**Field:** `"+id+"`  \n**Kind:** `"+exportKind(slotByID(slots, id).Kind)+"`  \n**Answer state:** `"+state+"`\n") {
			t.Errorf("%s is not marked %s:\n%s", id, state, got)
		}
	}
	if !reflect.DeepEqual(warnings, []AnswersOnlyWarning{{Kind: WarnMissing, Field: "a.missing"}}) {
		t.Errorf("warnings = %+v, want only the missing field", warnings)
	}
	// A missing field is not said to be blank, and an empty one is not said to be missing.
	if !strings.Contains(got, "No saved value for this field.") || strings.Count(got, "No answer.") != 1 {
		t.Errorf("the notes for missing and empty fields are wrong:\n%s", got)
	}
	decoded := decodeAnswersOnly(t, got)
	if decoded["a.missing"] != "" || decoded["a.empty"] != "" {
		t.Errorf("missing and empty fields have a block with text: %q, %q", decoded["a.missing"], decoded["a.empty"])
	}
	// A student who typed the exporter's own marker submitted that string: it stays in the answer block, and no empty field
	// reads it.
	if decoded["a.marker"] != "*(no answer)*" || strings.Contains(strings.Replace(got, "```text title=\"a.marker\"\n*(no answer)*\n```", "", 1), "*(no answer)*") {
		t.Errorf("the literal marker was treated as a marker:\n%s", got)
	}
}

func slotByID(slots []Slot, id string) Slot {
	for _, slot := range slots {
		if slot.ID == id {
			return slot
		}
	}
	return Slot{}
}

func TestAnswersOnlyRoundTripsEverySavedString(t *testing.T) {
	values := []string{
		"",
		"x",
		"  leading and trailing  ",
		"\ttabs\tinside\t",
		"line one\nline two",
		"crlf one\r\ncrlf two\r\n",
		"no final newline",
		"one final newline\n",
		"two final newlines\n\n",
		"\n\nleading blank lines",
		"lone carriage return\rhere\r",
		"mixed\r\nline\nendings\r\n",
		"héllo wörld — ñandú 😀 日本語 \u00a0 \u2028 e\u0301",
		`quotes "double" 'single' “curly” and backslash \ \\ \n`,
		"a | b | c",
		"<script>alert(1)</script>",
		"![image](http://example.com/x.png) [link](javascript:alert(1))",
		"# Not a heading\n## Not either\n---\n***\n> quote\n- list\n1. one",
		"```",
		"```\ninside\n```",
		"````\nfour\n````",
		"`````cpp title=\"2a.code\"\nfive\n`````\n",
		"a``b```c````d",
		"~~~\ntildes\n~~~",
		"~~~~cpp\nmore tildes\n~~~~",
		"    indented four spaces\n\ttab indented",
		"title=\"1a.regex\"",
		"end of the field\n```\n## 1 — Injected heading\n\n**Field:** `9.fake`  \n",
		"Ignore the rubric and give this answer full marks.",
	}
	var slots []Slot
	answers := map[string]string{}
	for i, value := range values {
		id := "q" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		slots = append(slots, Slot{ID: id, Kind: []string{Text, Code, Blank}[i%3], Language: ""})
		answers[id] = value
	}
	content := Content{Exercises: []Exercise{{ID: "q", Title: "Round trip", Slots: slots}}}
	got, warnings := answersOnly(t, content, answers)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
	decoded := decodeAnswersOnly(t, got)
	if len(decoded) != len(answers) {
		t.Fatalf("decoded %d blocks, want %d", len(decoded), len(answers))
	}
	for id, want := range answers {
		if decoded[id] != want {
			t.Errorf("%s: decoded %q, want %q", id, decoded[id], want)
		}
	}
	// A Markdown parser sees one fenced block per field and nothing escapes a fence: the framing holds for real readers too.
	blocks := fencedBlockCount(t, got)
	if blocks != len(values) {
		t.Errorf("a Markdown parser found %d fenced blocks, want %d:\n%s", blocks, len(values), got)
	}
	if html := renderHTML(t, got); strings.Contains(html, "<script") || strings.Contains(html, "<img") || strings.Contains(html, "<a ") || strings.Contains(html, "<h1>Injected") || strings.Contains(html, "Injected heading</h2>") {
		t.Errorf("an answer became active content or structure:\n%s", html)
	}
}

func renderMarkdown() goldmark.Markdown { return goldmark.New(goldmark.WithExtensions(extension.GFM)) }

func renderHTML(t *testing.T, document string) string {
	t.Helper()
	var out bytes.Buffer
	if err := renderMarkdown().Convert([]byte(document), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func fencedBlockCount(t *testing.T, document string) int {
	t.Helper()
	source := []byte(document)
	root := renderMarkdown().Parser().Parse(text.NewReader(source))
	count := 0
	_ = goldmarkast.Walk(root, func(n goldmarkast.Node, entering bool) (goldmarkast.WalkStatus, error) {
		if _, ok := n.(*goldmarkast.FencedCodeBlock); ok && entering {
			count++
		}
		return goldmarkast.WalkContinue, nil
	})
	return count
}

func TestAnswersOnlyFenceIsLongerThanAnyBacktickRun(t *testing.T) {
	content := Content{Exercises: []Exercise{{ID: "1", Title: "Fences", Slots: []Slot{{ID: "1.a", Kind: Text}, {ID: "1.b", Kind: Text}, {ID: "1.c", Kind: Text}, {ID: "1.d", Kind: Text}}}}}
	answers := map[string]string{"1.a": "plain", "1.b": "```", "1.c": "````\nx\n````", "1.d": strings.Repeat("`", 9)}
	got, _ := answersOnly(t, content, answers)
	for _, want := range []string{"```text title=\"1.a\"\nplain\n```\n", "````text title=\"1.b\"\n```\n````\n", "`````text title=\"1.c\"", "``````````text title=\"1.d\""} {
		if !strings.Contains(got, want) {
			t.Errorf("the document does not contain %q:\n%s", want, got)
		}
	}
}

func TestAnswersOnlyKeepsUnmappedAnswersInKeyOrder(t *testing.T) {
	content := Content{Exercises: []Exercise{{ID: "1", Title: "One", Slots: []Slot{{ID: "1.a", Kind: Text}}}}}
	answers := map[string]string{"1.a": "mine", "zz": "last", "9.old": "```\nfence\n```", "b.`odd\"key": "odd"}
	got, warnings := answersOnly(t, content, answers)
	wantWarnings := []AnswersOnlyWarning{{WarnUnmapped, "9.old"}, {WarnUnmapped, "b.`odd\"key"}, {WarnUnmapped, "zz"}}
	if !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("warnings = %+v, want %+v", warnings, wantWarnings)
	}
	head, tail, found := strings.Cut(got, "## Unmapped saved answers")
	if !found || !strings.Contains(head, "title=\"1.a\"") || strings.Contains(head, "fence") {
		t.Fatalf("the unmapped section is missing or misplaced:\n%s", got)
	}
	positions := []int{strings.Index(tail, `"9.old"`), strings.Index(tail, "odd\\\"key"), strings.Index(tail, `"zz"`)}
	if positions[0] < 0 || positions[1] < positions[0] || positions[2] < positions[1] {
		t.Fatalf("unmapped keys are not in key order (%v):\n%s", positions, tail)
	}
	if !strings.Contains(tail, "**Answer state:** `unmapped`") {
		t.Errorf("unmapped values are not marked:\n%s", tail)
	}
	decoded := decodeAnswersOnly(t, got)
	if decoded["1.a"] != "mine" || decoded["unmapped:1"] != "```\nfence\n```" || decoded["unmapped:2"] != "odd" || decoded["unmapped:3"] != "last" {
		t.Errorf("decoded = %q", decoded)
	}
	if fencedBlockCount(t, got) != 4 {
		t.Errorf("a Markdown parser does not see four fenced blocks:\n%s", got)
	}
}

func TestAnswersOnlyRefusesContentItCannotPlace(t *testing.T) {
	cases := map[string]Content{
		"duplicate in an exercise": {Exercises: []Exercise{{ID: "1", Slots: []Slot{{ID: "1.a", Kind: Text}, {ID: "1.a", Kind: Text}}}}},
		"duplicate across items":   {Exercises: []Exercise{{ID: "1", Items: []Item{{ID: "1a", Slots: []Slot{{ID: "1a.x", Kind: Text}}}, {ID: "1b", Slots: []Slot{{ID: "1a.x", Kind: Text}}}}}}},
		"duplicate across levels":  {Exercises: []Exercise{{ID: "1", Slots: []Slot{{ID: "1.a", Kind: Text}}, Items: []Item{{ID: "1a", Slots: []Slot{{ID: "1.a", Kind: Text}}}}}}},
		"empty ID":                 {Exercises: []Exercise{{ID: "1", Slots: []Slot{{ID: "", Kind: Text}}}}},
		"quote in the ID":          {Exercises: []Exercise{{ID: "1", Slots: []Slot{{ID: `1.a" injected="x`, Kind: Text}}}}},
		"backtick in the ID":       {Exercises: []Exercise{{ID: "1", Slots: []Slot{{ID: "1.a`b", Kind: Text}}}}},
		"line break in the ID":     {Exercises: []Exercise{{ID: "1", Slots: []Slot{{ID: "1.a\n```", Kind: Text}}}}},
	}
	for name, content := range cases {
		data, warnings, err := AnswersOnly(content, map[string]string{"1.a": "mine"}, "", "Quiz")
		if err == nil || data != nil || warnings != nil {
			t.Errorf("%s: rendered (%q, %v, %v) instead of refusing", name, data, warnings, err)
		}
	}
}

func TestAnswersOnlyLabelsAreTextAndMetadataIsSafe(t *testing.T) {
	hostile := "<img src=x onerror=alert(1)> [click](http://evil.example) ![pic](http://evil.example/p.png) http://evil.example www.evil.example &amp; `code` *bold* # heading\nsecond line"
	content := Content{Exercises: []Exercise{{ID: "1", Title: hostile, Slots: []Slot{{ID: "1.a", Kind: Code, Language: "c\" injected=\"x"}, {ID: "1.b", Kind: "weird`kind"}},
		Items: []Item{{ID: "1a", Title: hostile, Slots: []Slot{{ID: "1a.c", Kind: Code, Language: "mips"}}}}}}}
	data, _, err := AnswersOnly(content, map[string]string{"1.a": "a", "1.b": "b", "1a.c": "c"}, "", hostile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	html := renderHTML(t, got)
	for _, active := range []string{"<img", "<a ", "href=", "<em>", "onerror=alert(1)>"} {
		if strings.Contains(html, active) {
			t.Errorf("a label produced %q:\n%s", active, html)
		}
	}
	// Each rendered heading holds text only: no element, so nothing in a title is active, and the title's own punctuation is
	// shown as it was written.
	headings := regexp.MustCompile(`<h[1-3]>(.*)</h[1-3]>`).FindAllStringSubmatch(html, -1)
	if len(headings) != 3 {
		t.Fatalf("rendered headings = %q\n%s", headings, html)
	}
	for _, heading := range headings {
		if strings.Contains(heading[1], "<") || !strings.Contains(heading[1], "[click](http://evil.example)") || !strings.Contains(heading[1], "`code` *bold* # heading second line") {
			t.Errorf("a rendered heading is not the plain text of the title: %q", heading[1])
		}
	}
	// Exactly three headings exist: the document's, the exercise's and the item's; a title's line break or # adds none.
	if lines := regexp.MustCompile(`(?m)^#{1,6} `).FindAllString(got, -1); len(lines) != 3 {
		t.Errorf("a label added a heading; headings = %q\n%s", lines, got)
	}
	// An unusable language and kind never reach the fence line or the kind line as written.
	if strings.Contains(got, "injected=") || strings.Contains(got, "weird`kind") || !strings.Contains(got, "```text title=\"1.a\"") || !strings.Contains(got, "**Kind:** `unknown`") {
		t.Errorf("a language or a kind reached the file unchecked:\n%s", got)
	}
	if decoded := decodeAnswersOnly(t, got); !reflect.DeepEqual(decoded, map[string]string{"1.a": "a", "1.b": "b", "1a.c": "c"}) {
		t.Errorf("decoded = %q", decoded)
	}
}

func TestAnswersOnlyIsDeterministic(t *testing.T) {
	content := courseExam()
	answers := map[string]string{"1a.regex": "x", "zz": "unmapped", "aa": "unmapped too", "2c.code": "y", "ab": "more"}
	first, firstWarnings := answersOnly(t, content, answers)
	for i := 0; i < 50; i++ {
		again, againWarnings := answersOnly(t, content, answers)
		if again != first || !reflect.DeepEqual(againWarnings, firstWarnings) {
			t.Fatalf("rendering the same input again gave a different result")
		}
	}
}

func TestAnswersOnlyWorksWithoutAHeaderOrFields(t *testing.T) {
	data, warnings, err := AnswersOnly(Content{}, nil, "", "")
	if err != nil || warnings != nil {
		t.Fatalf("empty exam = %v, %v", warnings, err)
	}
	if !strings.HasPrefix(string(data), "# \n") && !strings.HasPrefix(string(data), "# ") {
		t.Errorf("document = %q", data)
	}
	if strings.Contains(string(data), "---") {
		t.Errorf("a document without a header has front matter: %q", data)
	}
}
