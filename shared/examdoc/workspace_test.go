package examdoc

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// The example package is the reference for the workspace format, as testdata/exam.md is for question exams. Its compiled document is
// what the plan's section 5.4 shows; the server's tests will import it.
func TestExampleWorkspaceMatchesItsCompiledDocument(t *testing.T) {
	exam, _, err := CompileWorkspaceDir("../../testdata/workspace/lab-listas")
	if err != nil {
		t.Fatal(err)
	}
	compiled := encode(t, exam)
	if *update {
		if err := os.WriteFile("../../testdata/workspace/lab-listas.json", compiled, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile("../../testdata/workspace/lab-listas.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, compiled) {
		t.Fatalf("testdata/workspace/lab-listas.json is not the compiled testdata/workspace/lab-listas; run go test ./... -update in shared/examdoc\n%s", compiled)
	}

	// Section 5.4 of the plan, key by key.
	var document map[string]any
	if err := json.Unmarshal(compiled, &document); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"kind", "title", "duration_seconds", "grace_seconds", "workspace", "content", "starter_bundle", "grading_bundle"} {
		if _, found := document[key]; !found {
			t.Errorf("the document has no %q", key)
		}
	}
	workspace := document["workspace"].(map[string]any)
	for _, key := range []string{"image", "terminal", "run", "check", "submit", "ignore", "resources"} {
		if _, found := workspace[key]; !found {
			t.Errorf("workspace has no %q", key)
		}
	}
	resources := workspace["resources"].(map[string]any)
	if resources["cpu"] != 1.0 || resources["memory_mb"] != 1024.0 || resources["disk_mb"] != 100.0 {
		t.Errorf("resources = %v", resources)
	}
	content := document["content"].(map[string]any)
	todos := content["todos"].([]any)
	first := todos[0].(map[string]any)
	if document["kind"] != "workspace" || document["duration_seconds"] != 7200.0 || document["grace_seconds"] != 120.0 ||
		len(todos) != 3 || first["id"] != "t1" || first["points"] != 10.0 ||
		first["text_html"] != "Implement <code>push_front</code> in <code>src/list.cpp</code>" || !strings.HasPrefix(document["starter_bundle"].(string), "sha256:") {
		t.Errorf("document = %s", compiled)
	}
	if total, ok := exam.Content.Points(); !ok || total != 40 {
		t.Errorf("points = %v, %v", total, ok)
	}
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// A question exam's document says nothing about its kind, so the documents of exams that exist do not change, and a document
// without a kind is a question exam.
func TestQuestionDocumentsCarryNoKind(t *testing.T) {
	exam, err := Compile([]byte(header + "## A\n{{x}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	document := encode(t, exam)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(document, &top); err != nil {
		t.Fatal(err)
	}
	if _, has := top["kind"]; has {
		t.Fatalf("a question document has a kind:\n%s", document)
	}
	for document, want := range map[string]string{`{"title":"x"}`: KindQuestion, `{"kind":"question"}`: KindQuestion, `{"kind":""}`: KindQuestion, `{"kind":"workspace"}`: KindWorkspace} {
		if got, err := DocumentKind([]byte(document)); err != nil || got != want {
			t.Errorf("DocumentKind(%s) = %q, %v; want %q", document, got, err, want)
		}
	}
	for _, document := range []string{`{"kind":"quiz"}`, `{"kind":7}`, `[]`, `nope`} {
		if kind, err := DocumentKind([]byte(document)); err == nil {
			t.Errorf("DocumentKind(%s) = %q, want an error", document, kind)
		}
	}
	if got, _ := DocumentKind(document); got != KindQuestion {
		t.Errorf("the compiled sample is %q", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Helpers: a valid package in memory, and the same with something wrong in it.

const okFront = "title: Lab\nduration: 1h\nimage: cbe/cpp20\n"

// pkg is a valid package whose workspace.md has the given front matter lines (after the three required ones when okFront is
// part of them) and body. Front matter line 1 is "---", so the first line of front is line 2 of the file; the body starts on the
// line after the closing "---".
func pkg(front, body string, extra ...fstest.MapFS) fstest.MapFS {
	files := fstest.MapFS{
		"workspace.md":     {Data: []byte("---\n" + front + "---\n" + body)},
		"project/Makefile": {Data: []byte("all:\n")},
	}
	for _, more := range extra {
		for name, file := range more {
			files[name] = file
		}
	}
	return files
}

// firstBodyLine is the line the body starts on when the front matter is okFront plus extra lines.
func firstBodyLine(extraLines int) int { return 3 + extraLines + 3 }

func compileWorkspace(t *testing.T, files fs.FS) (WorkspaceExam, WorkspaceBundles) {
	t.Helper()
	exam, bundles, err := CompileWorkspaceFS(files)
	if err != nil {
		t.Fatalf("compile failed:\n%v", err)
	}
	return exam, bundles
}

func workspaceProblems(t *testing.T, files fs.FS) Problems {
	t.Helper()
	_, _, err := CompileWorkspaceFS(files)
	var problems Problems
	if !errors.As(err, &problems) {
		t.Fatalf("err = %v, want problems", err)
	}
	return problems
}

func wantProblem(t *testing.T, files fs.FS, want string) {
	t.Helper()
	if problems := workspaceProblems(t, files); !strings.Contains(problems.Error(), want) {
		t.Fatalf("problems:\n%s\nwant %q", problems.Error(), want)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// What a good package compiles to.

func TestWorkspaceDefaults(t *testing.T) {
	exam, bundles := compileWorkspace(t, pkg(okFront, "Do it.\n\n[ ] First\n[ ] Second\n"))
	workspace := exam.Workspace
	if exam.Kind != KindWorkspace || exam.Title != "Lab" || exam.DurationSeconds != 3600 || exam.GraceSeconds != nil || workspace.Image != "cbe/cpp20" ||
		!workspace.Terminal || workspace.Run != "" || workspace.Check != "" || workspace.Submit != nil || workspace.SnapshotSeconds != nil ||
		workspace.Resources != (Resources{CPU: 1, MemoryMB: 1024, DiskMB: 100}) {
		t.Fatalf("exam = %+v", exam)
	}
	if got := strings.Join(workspace.Ignore, " "); got != "build/** **/*.o **/*.out **/__pycache__/** .cache/**" {
		t.Errorf("default ignore = %q", got)
	}
	if exam.Content.Instructions != "<p>Do it.</p>" || len(exam.Content.Todos) != 2 || exam.Content.Todos[0].ID != "t1" || exam.Content.Todos[1].ID != "t2" ||
		exam.Content.Todos[0].Points != nil || exam.Content.Todos[1].TextHTML != "Second" {
		t.Errorf("content = %+v", exam.Content)
	}
	if exam.GradingBundle != "" || bundles.Grading != nil || exam.StarterBundle != bundles.Starter.Hash || bundles.Starter.Files != 1 {
		t.Errorf("bundles = %+v, document hashes %q %q", bundles, exam.StarterBundle, exam.GradingBundle)
	}
}

func TestWorkspaceFrontMatterSettings(t *testing.T) {
	front := okFront + `# a comment
grace: 45s
min_volume: 0
opens_at: 2026-10-20T08:00:00-06:00
closes_at: 2026-10-20T12:00:00-06:00
terminal: off
run: make run
check: "make check"
submit: src/**
ignore: []
resources: cpu=0.5, memory=512m, disk=1g
snapshot: 30s
calculator: mode=[scientific], angle=rad
points: 5
`
	exam, _ := compileWorkspace(t, pkg(front, "[ ] Only {points=5}\n"))
	workspace := exam.Workspace
	if *exam.GraceSeconds != 45 || *exam.MinVolumePercent != 0 || exam.OpensAt.Hour() != 8 || !exam.ClosesAt.After(*exam.OpensAt) ||
		workspace.Terminal || workspace.Run != "make run" || workspace.Check != "make check" || len(workspace.Submit) != 1 || workspace.Submit[0] != "src/**" ||
		workspace.Ignore == nil || len(workspace.Ignore) != 0 || workspace.Resources != (Resources{CPU: 0.5, MemoryMB: 512, DiskMB: 1024}) ||
		*workspace.SnapshotSeconds != 30 || exam.Tools == nil || !exam.Tools.On() {
		t.Fatalf("exam = %+v", exam)
	}
	// An empty ignore list is written as [], never as null.
	if !strings.Contains(string(encode(t, exam)), `"ignore": []`) {
		t.Errorf("ignore is not written as []:\n%s", encode(t, exam))
	}
}

func TestTodoItems(t *testing.T) {
	body := "Intro **text**.\n\n- [ ] Task `a` {#alpha points=2.5}\n- [x] Task *b*, already ticked\n* [X] Task c <> & d\n[ ] Bare form {points=1}\n\n```\n[ ] not a TODO: it is in a code block\n```\n\nAfter.\n"
	exam, _ := compileWorkspace(t, pkg(okFront, body))
	todos := exam.Content.Todos
	want := []Todo{
		{ID: "alpha", TextHTML: "Task <code>a</code>", Points: ptr(2.5)},
		{ID: "t2", TextHTML: "Task <em>b</em>, already ticked"},
		{ID: "t3", TextHTML: "Task c &lt;&gt; &amp; d"},
		{ID: "t4", TextHTML: "Bare form", Points: ptr(1.0)},
	}
	if len(todos) != len(want) {
		t.Fatalf("todos = %+v", todos)
	}
	for i := range want {
		if todos[i].ID != want[i].ID || todos[i].TextHTML != want[i].TextHTML || (todos[i].Points == nil) != (want[i].Points == nil) || todos[i].Points != nil && *todos[i].Points != *want[i].Points {
			t.Errorf("todo %d = %+v, want %+v", i+1, todos[i], want[i])
		}
	}
	// The instructions keep the prose and the code block and lose the list. The checkbox state is not kept anywhere.
	instructions := exam.Content.Instructions
	if !strings.Contains(instructions, "<p>Intro <strong>text</strong>.</p>") || !strings.Contains(instructions, "[ ] not a TODO: it is in a code block") || !strings.Contains(instructions, "<p>After.</p>") ||
		strings.Contains(instructions, "Task") || strings.Contains(instructions, "checkbox") {
		t.Errorf("instructions = %q", instructions)
	}
}

func ptr(v float64) *float64 { return &v }

func TestTodoPointsAddUpOnlyWhenEveryItemHasThem(t *testing.T) {
	compileWorkspace(t, pkg(okFront+"points: 25\n", "[ ] a {points=10}\n[ ] b {points=15}\n"))
	compileWorkspace(t, pkg(okFront+"points: 99\n", "[ ] a {points=10}\n[ ] b\n"))
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Each rule of section 5, with a failing input and the message it gives.

func TestWorkspaceProblemsNameTheirLines(t *testing.T) {
	body := firstBodyLine(0)
	extra := func(lines string) string { return okFront + lines }
	for name, test := range map[string]struct {
		files fs.FS
		want  string
	}{
		// 5.1 front matter: the keys shared with question exams, with the same rules
		"no front matter":      {fstest.MapFS{"workspace.md": {Data: []byte("[ ] a\n")}, "project/x": {}}, "line 1: the file must start with a front matter block"},
		"unclosed front":       {fstest.MapFS{"workspace.md": {Data: []byte("---\ntitle: x\n")}, "project/x": {}}, "not closed"},
		"no title":             {pkg("duration: 1h\nimage: cbe/cpp20\n", "[ ] a\n"), "line 1: the front matter must set a title"},
		"no duration":          {pkg("title: x\nimage: cbe/cpp20\n", "[ ] a\n"), "line 1: the front matter must set a duration"},
		"bad duration":         {pkg("title: x\nduration: 90\nimage: cbe/cpp20\n", "[ ] a\n"), "line 3: duration must be a positive length of time"},
		"bad grace":            {pkg(extra("grace: soon\n"), "[ ] a\n"), "line 5: grace must be a positive length of time"},
		"bad min_volume":       {pkg(extra("min_volume: loud\n"), "[ ] a\n"), "line 5: min_volume must be a whole number from 0 to 100"},
		"bad opens_at":         {pkg(extra("opens_at: tomorrow\n"), "[ ] a\n"), "line 5: opens_at must be a date and time with its offset"},
		"bad closes_at":        {pkg(extra("closes_at: 2026-10-20\n"), "[ ] a\n"), "line 5: closes_at must be a date and time with its offset"},
		"bad calculator":       {pkg(extra("calculator: angle=turns\n"), "[ ] a\n"), "line 5: calculator: angle must be deg, rad or user"},
		"bad points":           {pkg(extra("points: lots\n"), "[ ] a\n"), "line 5: points must be a positive number"},
		"duplicate key":        {pkg(extra("run: make\nrun: make run\n"), "[ ] a\n"), "line 6: run is set twice"},
		"unquoted pair":        {pkg(extra("run make\n"), "[ ] a\n"), "line 5: front matter lines must look like key: value"},
		"unknown key":          {pkg(extra("colour: red\n"), "[ ] a\n"), `line 5: unknown front matter key "colour"`},
		"pin in file":          {pkg(extra("pin: 1234\n"), "[ ] a\n"), "line 5: the PIN does not belong in the exam file"},
		"focus threshold":      {pkg(extra("focus_threshold: 8s\n"), "[ ] a\n"), "line 5: focus_threshold is set in the proctor's Settings now"},
		"navigation":           {pkg(extra("navigation: free\n"), "[ ] a\n"), "line 5: navigation is not allowed in a workspace exam"},
		"no image":             {pkg("title: x\nduration: 1h\n", "[ ] a\n"), "line 1: the front matter must set an image"},
		"empty image":          {pkg("title: x\nduration: 1h\nimage:\n", "[ ] a\n"), "line 4: image needs the name of a course image"},
		"image with tag":       {pkg("title: x\nduration: 1h\nimage: cbe/cpp20:latest\n", "[ ] a\n"), "line 4: image must be the name of a course image"},
		"image uppercase":      {pkg("title: x\nduration: 1h\nimage: CBE/cpp20\n", "[ ] a\n"), "line 4: image must be the name of a course image"},
		"image with digest":    {pkg("title: x\nduration: 1h\nimage: cbe/cpp20@sha256:00\n", "[ ] a\n"), "line 4: image must be the name of a course image"},
		"bad terminal":         {pkg(extra("terminal: maybe\n"), "[ ] a\n"), "line 5: terminal must be on or off"},
		"empty run":            {pkg(extra("run:\n"), "[ ] a\n"), "line 5: run needs a command"},
		"run with newline":     {pkg(extra("run: \"make\\nrun\"\n"), "[ ] a\n"), "line 5: run must be one line of plain text"},
		"long check":           {pkg(extra("check: "+strings.Repeat("x", 1001)+"\n"), "[ ] a\n"), "line 5: check must be at most 1000 bytes"},
		"submit absolute":      {pkg(extra("submit: [/etc/**]\n"), "[ ] a\n"), `line 5: submit pattern "/etc/**" starts with /`},
		"submit parent":        {pkg(extra("submit: [../x]\n"), "[ ] a\n"), `line 5: submit pattern "../x" has an empty, "." or ".." segment`},
		"submit backslash":     {pkg(extra("submit: [src\\a]\n"), "[ ] a\n"), "line 5: submit pattern"},
		"submit slashes":       {pkg(extra("submit: [src//a]\n"), "[ ] a\n"), `has an empty, "." or ".." segment`},
		"submit folder":        {pkg(extra("submit: [src/]\n"), "[ ] a\n"), "ends with /"},
		"submit stars":         {pkg(extra("submit: [src**]\n"), "[ ] a\n"), "uses ** inside a segment"},
		"submit many stars":    {pkg(extra("submit: [**/**/**/a]\n"), "[ ] a\n"), "uses ** more than twice"},
		"submit bad class":     {pkg(extra("submit: [src/[a]\n"), "[ ] a\n"), "is not a valid pattern"},
		"submit empty":         {pkg(extra("submit: []\n"), "[ ] a\n"), "line 5: submit is an empty list"},
		"submit unclosed":      {pkg(extra("submit: [src/**\n"), "[ ] a\n"), "line 5: submit=[src/** is not a list"},
		"ignore pattern":       {pkg(extra("ignore: [a, /b]\n"), "[ ] a\n"), `line 5: ignore pattern "/b" starts with /`},
		"too many patterns":    {pkg(extra("ignore: ["+strings.TrimSuffix(strings.Repeat("a, ", 51), ", ")+"]\n"), "[ ] a\n"), "line 5: ignore has more than 50 patterns"},
		"resources empty":      {pkg(extra("resources:\n"), "[ ] a\n"), "line 5: resources: needs settings"},
		"resources unknown":    {pkg(extra("resources: gpu=1\n"), "[ ] a\n"), `line 5: resources: unknown setting "gpu"`},
		"resources twice":      {pkg(extra("resources: cpu=1, cpu=2\n"), "[ ] a\n"), "line 5: resources: cpu is set twice"},
		"resources pair":       {pkg(extra("resources: cpu\n"), "[ ] a\n"), `line 5: resources: "cpu" is not a setting=value pair`},
		"resources cpu":        {pkg(extra("resources: cpu=lots\n"), "[ ] a\n"), "line 5: resources: cpu must be a number of cores"},
		"resources cpu small":  {pkg(extra("resources: cpu=0\n"), "[ ] a\n"), "line 5: resources: cpu must be a number of cores"},
		"resources cpu big":    {pkg(extra("resources: cpu=65\n"), "[ ] a\n"), "line 5: resources: cpu must be a number of cores"},
		"resources cpu format": {pkg(extra("resources: cpu=1e1\n"), "[ ] a\n"), "line 5: resources: cpu must be a number of cores"},
		"resources memory":     {pkg(extra("resources: memory=lots\n"), "[ ] a\n"), "line 5: resources: memory must be a size in m or g from 64m to 64g"},
		"resources memory low": {pkg(extra("resources: memory=32m\n"), "[ ] a\n"), "line 5: resources: memory must be a size in m or g from 64m to 64g"},
		"resources disk":       {pkg(extra("resources: disk=100\n"), "[ ] a\n"), "line 5: resources: disk must be a size in m or g from 1m to 100g"},
		"snapshot":             {pkg(extra("snapshot: often\n"), "[ ] a\n"), "line 5: snapshot must be a length of time from 10s to 10m"},
		"snapshot low":         {pkg(extra("snapshot: 5s\n"), "[ ] a\n"), "line 5: snapshot must be a length of time from 10s to 10m"},
		"snapshot high":        {pkg(extra("snapshot: 11m\n"), "[ ] a\n"), "line 5: snapshot must be a length of time from 10s to 10m"},

		// 5.1 body rules: the question-exam rules, plus what a workspace exam may not hold
		"no todos":        {pkg(okFront, "Only instructions.\n"), fmt.Sprintf("line %d: the exam has no TODO items", body)},
		"link":            {pkg(okFront, "Read [the docs](http://example.com).\n\n[ ] a\n"), fmt.Sprintf("line %d: links are not allowed", body)},
		"autolink":        {pkg(okFront, "<http://example.com>\n\n[ ] a\n"), fmt.Sprintf("line %d: links are not allowed", body)},
		"html":            {pkg(okFront, "<b>bold</b>\n\n[ ] a\n"), fmt.Sprintf("line %d: HTML is not allowed", body)},
		"html block":      {pkg(okFront, "<div>\nx\n</div>\n\n[ ] a\n"), fmt.Sprintf("line %d: HTML is not allowed", body)},
		"title heading":   {pkg(okFront, "# Lab\n\n[ ] a\n"), fmt.Sprintf("line %d: use ## and ### for headings", body)},
		"blank field":     {pkg(okFront, "Name: {{name}}\n\n[ ] a\n"), fmt.Sprintf("line %d: answer fields ({{name}}) are not allowed in a workspace exam", body)},
		"answer block":    {pkg(okFront, "```answer\n```\n\n[ ] a\n"), fmt.Sprintf("line %d: answer blocks are not allowed in a workspace exam", body)},
		"missing picture": {pkg(okFront, "![x](missing.png)\n\n[ ] a\n"), fmt.Sprintf(`line %d: image "missing.png": the file does not exist next to the exam`, body)},
		"picture in heading": {pkg(okFront, "## ![x](a.png)\n\n[ ] a\n", fstest.MapFS{"a.png": {Data: tinyPNG(t)}}),
			fmt.Sprintf("line %d: images cannot be in headings", body)},
		"picture from grading": {pkg(okFront, "![x](grading/a.png)\n\n[ ] a\n", fstest.MapFS{"grading/a.png": {Data: tinyPNG(t)}, "grading/grade": {Data: []byte("#!/bin/sh\n")}}),
			fmt.Sprintf(`line %d: image "grading/a.png": pictures cannot come from the grading folder`, body)},
		"picture reaching up": {pkg(okFront, "![x](../a.png)\n\n[ ] a\n"), fmt.Sprintf(`line %d: image "../a.png"`, body)},
		"picture absolute":    {pkg(okFront, "![x](/etc/a.png)\n\n[ ] a\n"), fmt.Sprintf(`line %d: image "/etc/a.png": absolute paths are not allowed`, body)},
		"picture on the web":  {pkg(okFront, "![x](https://example.com/a.png)\n\n[ ] a\n"), fmt.Sprintf(`line %d: image "https://example.com/a.png": only files next to the exam can be used`, body)},

		// 5.1 TODO items
		"empty todo":          {pkg(okFront, "[ ]\n"), fmt.Sprintf("line %d: the TODO item has no text", body)},
		"duplicate todo id":   {pkg(okFront, "[ ] a {#same}\n[ ] b {#same}\n"), fmt.Sprintf(`line %d: TODO id "same" is already used`, body+1)},
		"default id taken":    {pkg(okFront, "[ ] a {#t2}\n[ ] b\n"), fmt.Sprintf(`line %d: TODO id "t2" is already used`, body+1)},
		"bad todo id":         {pkg(okFront, "[ ] a {#a.b}\n"), fmt.Sprintf(`line %d: TODO id "a.b" may only use`, body)},
		"unknown attribute":   {pkg(okFront, "[ ] a {weight=3}\n"), fmt.Sprintf(`line %d: unknown TODO attribute "weight"`, body)},
		"negative points":     {pkg(okFront, "[ ] a {points=-1}\n"), fmt.Sprintf("line %d: points must be a positive number", body)},
		"unreadable todo":     {pkg(okFront, "[ ] a {points=}\n"), fmt.Sprintf("line %d: the attributes at the end of the TODO item could not be read", body)},
		"block in todo":       {pkg(okFront, "[ ] # a heading\n"), fmt.Sprintf("line %d: a TODO item is one line of text without a heading", body)},
		"list in todo":        {pkg(okFront, "[ ] 1. a list\n"), fmt.Sprintf("line %d: a TODO item is one line of text without a heading", body)},
		"link in todo":        {pkg(okFront, "[ ] see [it](http://example.com)\n"), fmt.Sprintf("line %d: links are not allowed", body)},
		"html in todo":        {pkg(okFront, "[ ] a <b>b</b>\n"), fmt.Sprintf("line %d: HTML is not allowed", body)},
		"blank in todo":       {pkg(okFront, "[ ] fill {{x}}\n"), fmt.Sprintf("line %d: answer fields ({{name}}) are not allowed", body)},
		"picture in todo":     {pkg(okFront, "[ ] ![x](a.png)\n"), fmt.Sprintf("line %d: images cannot be in a TODO item", body)},
		"long todo":           {pkg(okFront, "[ ] "+strings.Repeat("x", 2100)+"\n"), fmt.Sprintf("line %d: the TODO item is longer than 2048 bytes", body)},
		"indented todo":       {pkg(okFront, "Steps:\n  [ ] a\n"), fmt.Sprintf("line %d: a TODO item must start its line", body+1)},
		"nested todo":         {pkg(okFront, "- list\n  - [ ] a\n"), fmt.Sprintf("line %d: a TODO item must start its line", body+1)},
		"quoted todo":         {pkg(okFront, "> [ ] a\n"), fmt.Sprintf("line %d: a TODO item must start its line", body)},
		"numbered todo":       {pkg(okFront, "1. [ ] a\n"), fmt.Sprintf("line %d: a TODO item must start its line", body)},
		"todo points total":   {pkg(okFront+"points: 100\n", "[ ] a {points=10}\n[ ] b {points=15}\n"), "line 1: the TODO items add up to 25 points but the front matter says 100"},
		"too many todos":      {pkg(okFront, strings.Repeat("[ ] a\n", 101)), fmt.Sprintf("line %d: the exam has more than 100 TODO items", body+100)},
		"todo after fence":    {pkg(okFront, "```\n```\n  [ ] a\n"), fmt.Sprintf("line %d: a TODO item must start its line", body+2)},
		"not utf8":            {fstest.MapFS{"workspace.md": {Data: []byte("---\n" + okFront + "---\n\xff[ ] a\n")}, "project/x": {}}, "workspace.md: the file is not UTF-8 text"},
		"workspace too large": {fstest.MapFS{"workspace.md": {Data: append([]byte("---\n"+okFront+"---\n"), bytes.Repeat([]byte("x"), 4<<20)...)}, "project/x": {}}, "workspace.md: the file is larger than 4 MiB"},

		// 5 the package
		"no workspace.md":    {fstest.MapFS{"project/x": {}}, "workspace.md is missing"},
		"workspace.md dir":   {fstest.MapFS{"workspace.md/x": {}, "project/x": {}}, "workspace.md must be a regular file"},
		"no project":         {fstest.MapFS{"workspace.md": {Data: []byte("---\n" + okFront + "---\n[ ] a\n")}}, "project/ is missing"},
		"project is a file":  {pkg(okFront, "[ ] a\n", fstest.MapFS{"project": {Data: []byte("x")}}), "project must be a folder"},
		"unknown entry":      {pkg(okFront, "[ ] a\n", fstest.MapFS{"notes.txt": {Data: []byte("x")}}), "notes.txt: a workspace package holds only workspace.md, project/, grading/, image/ and the pictures the instructions use"},
		"image folder":       {pkg(okFront, "[ ] a\n", fstest.MapFS{"image/Containerfile": {Data: []byte("FROM x\n")}}), "image/: per-exam image customization is not supported yet"},
		"grading is a file":  {pkg(okFront, "[ ] a\n", fstest.MapFS{"grading": {Data: []byte("x")}}), "grading must be a folder"},
		"no grade":           {pkg(okFront, "[ ] a\n", fstest.MapFS{"grading/tests.txt": {Data: []byte("x")}}), "grading/grade: missing"},
		"grade not run":      {pkg(okFront, "[ ] a\n", fstest.MapFS{"grading/grade": {Data: []byte("echo {}\n"), Mode: 0o644}}), "grading/grade: is not executable"},
		"grade is a link":    {pkg(okFront, "[ ] a\n", fstest.MapFS{"grading/real": {Data: []byte("#!/bin/sh\n")}, "grading/grade": {Data: []byte("real"), Mode: fs.ModeSymlink | 0o777}}), "grading/grade: must be a regular file"},
		"grade is a folder":  {pkg(okFront, "[ ] a\n", fstest.MapFS{"grading/grade/x": {Data: []byte("#!/bin/sh\n")}}), "grading/grade: must be a regular file"},
		"grading not a link": {pkg(okFront, "[ ] a\n", fstest.MapFS{"grading": {Data: []byte("x"), Mode: fs.ModeSymlink | 0o777}}), "grading must be a folder"},

		// 5.2 project/: the limits and what a bundle may not hold
		"too many files":      {pkg(okFront, "[ ] a\n", manyFiles(2000)), "project/ holds more than 2000 files"},
		"too big":             {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/big": {Data: make([]byte, 21<<20)}}), "project/ holds more than 20 MiB of files"},
		"link absolute":       {pkg(okFront, "[ ] a\n", link("project/passwd", "/etc/passwd")), `project/passwd: the symbolic link points to the absolute path "/etc/passwd", outside the folder`},
		"link above":          {pkg(okFront, "[ ] a\n", link("project/up", "../workspace.md")), `project/up: the symbolic link points to "../workspace.md", outside the folder`},
		"link above deep":     {pkg(okFront, "[ ] a\n", link("project/a/b/up", "../../../x")), `project/a/b/up: the symbolic link points to "../../../x", outside the folder`},
		"link through a name": {pkg(okFront, "[ ] a\n", link("project/sneaky", "a/../../x")), `project/sneaky: the symbolic link has the target "a/../../x", which uses .. after a name`},
		"link drive":          {pkg(okFront, "[ ] a\n", link("project/drive", "C:/Windows")), `project/drive: the symbolic link points to the absolute path "C:/Windows", outside the folder`},
		"link empty":          {pkg(okFront, "[ ] a\n", link("project/empty", "")), "project/empty: the symbolic link has an empty target"},
		"link backslash":      {pkg(okFront, "[ ] a\n", link("project/win", `a\b`)), "project/win: the symbolic link has a target with \\"},
		"device file":         {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/null": {Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666}}), "project/null: it is a device file"},
		"named pipe":          {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/pipe": {Mode: fs.ModeNamedPipe | 0o644}}), "project/pipe: it is a named pipe"},
		"socket":              {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/sock": {Mode: fs.ModeSocket | 0o644}}), "project/sock: it is a socket"},
		"control character":   {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/a\x01b": {Data: []byte("x")}}), "the name has control characters"},
		"name not utf8":       {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/\xffname": {Data: []byte("x")}}), "the name is not valid UTF-8 text"},
		"long name":           {pkg(okFront, "[ ] a\n", fstest.MapFS{"project/" + strings.Repeat("a", 256): {Data: []byte("x")}}), "a name in the path is longer than 255 bytes"},
		"grading too many":    {pkg(okFront, "[ ] a\n", manyFilesIn("grading", 2001), fstest.MapFS{"grading/grade": {Data: []byte("#!/bin/sh\n")}}), "grading/ holds more than 2000 files"},
	} {
		t.Run(name, func(t *testing.T) { wantProblem(t, test.files, test.want) })
	}
}

func manyFiles(n int) fstest.MapFS { return manyFilesIn("project", n) }

func manyFilesIn(dir string, n int) fstest.MapFS {
	files := fstest.MapFS{}
	for i := 0; i <= n; i++ {
		files[fmt.Sprintf("%s/f%04d", dir, i)] = &fstest.MapFile{Data: []byte("x")}
	}
	return files
}

func link(name, target string) fstest.MapFS {
	return fstest.MapFS{name: {Data: []byte(target), Mode: fs.ModeSymlink | 0o777}}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	// A 1x1 PNG.
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x00\x00\x00\x00:~\x9bU\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")
}

func TestWorkspaceProblemsAreBounded(t *testing.T) {
	var body strings.Builder
	for range 80 {
		body.WriteString("[ ] a {#same}\n")
	}
	problems := workspaceProblems(t, pkg(okFront, body.String()))
	if len(problems) != maxProblems+1 || problems[maxProblems] != "more problems were found; fix these first" {
		t.Fatalf("problems = %d", len(problems))
	}
}

func TestWorkspaceProblemsComeTogether(t *testing.T) {
	problems := workspaceProblems(t, fstest.MapFS{
		"workspace.md":        {Data: []byte("---\ntitle: x\nduration: 1h\nimage: Bad Image\nterminal: maybe\n---\nSee [x](http://a.b)\n\n[ ] a {#same}\n[ ] b {#same}\n")},
		"project/link":        {Data: []byte("/etc/shadow"), Mode: fs.ModeSymlink | 0o777},
		"grading/a.txt":       {Data: []byte("x")},
		"README.md":           {Data: []byte("x")},
		"image/Containerfile": {Data: []byte("x")},
	}).Error()
	for _, want := range []string{"line 4: image must be", "line 5: terminal must be on or off", "line 7: links are not allowed", `line 10: TODO id "same"`,
		"project/link: the symbolic link", "grading/grade: missing", "README.md: a workspace package holds only", "image/: per-exam image customization"} {
		if !strings.Contains(problems, want) {
			t.Errorf("missing %q in:\n%s", want, problems)
		}
	}
}

func TestHiddenFilesAndJunkInThePackageRootAreIgnored(t *testing.T) {
	compileWorkspace(t, pkg(okFront, "[ ] a\n", fstest.MapFS{".git/config": {Data: []byte("x")}, ".DS_Store": {Data: []byte("x")}, "Thumbs.db": {Data: []byte("x")}, "desktop.ini": {Data: []byte("x")}}))
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The bundles.

func readTar(t *testing.T, data []byte) []*tar.Header {
	t.Helper()
	var headers []*tar.Header
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return headers
		}
		if err != nil {
			t.Fatal(err)
		}
		headers = append(headers, header)
	}
}

func TestBundleIsADeterministicSortedTar(t *testing.T) {
	files := fstest.MapFS{
		"project/src/b.cpp":   {Data: []byte("b"), Mode: 0o600, ModTime: time.Now()},
		"project/src/a.cpp":   {Data: []byte("a"), Mode: 0o664, ModTime: time.Unix(5, 0)},
		"project/Makefile":    {Data: []byte("all:\n"), Mode: 0o640},
		"project/build.sh":    {Data: []byte("#!/bin/sh\n"), Mode: 0o644},
		"project/tool":        {Data: []byte("\x7fELF\x02"), Mode: 0o644},
		"project/run":         {Data: []byte("data"), Mode: 0o755},
		"project/empty":       {Mode: fs.ModeDir | 0o700},
		"project/a.txt":       {Data: []byte("text")},
		"project/src/link":    {Data: []byte("a.cpp"), Mode: fs.ModeSymlink | 0o777},
		"project/src/up":      {Data: []byte("../Makefile"), Mode: fs.ModeSymlink | 0o777},
		"project/src/sub/dir": {Data: []byte("../../src"), Mode: fs.ModeSymlink | 0o777},
		"workspace.md":        {Data: []byte("---\n" + okFront + "---\n[ ] a\n")},
	}
	_, first := compileWorkspace(t, files)
	// Different times and permission bits on the files that are not programs, and a different order of creation, change nothing.
	again := fstest.MapFS{}
	for name, file := range files {
		copied := *file
		copied.ModTime = time.Unix(1_700_000_000, 0)
		if copied.Mode&fs.ModeType == 0 && copied.Mode&0o111 == 0 {
			copied.Mode = 0o600
		}
		again[name] = &copied
	}
	_, second := compileWorkspace(t, again)
	if first.Starter.Hash != second.Starter.Hash || !bytes.Equal(first.Starter.Data, second.Starter.Data) {
		t.Fatalf("the bundle depends on times or permissions: %s != %s", first.Starter.Hash, second.Starter.Hash)
	}

	headers := readTar(t, first.Starter.Data)
	var names []string
	modes := map[string]int64{}
	for _, header := range headers {
		names = append(names, header.Name)
		modes[header.Name] = header.Mode
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" || !header.ModTime.Equal(bundleTime) {
			t.Errorf("%s: owner %d:%d %q:%q time %v", header.Name, header.Uid, header.Gid, header.Uname, header.Gname, header.ModTime)
		}
	}
	wantNames := "Makefile a.txt build.sh empty/ run src/ src/a.cpp src/b.cpp src/link src/sub/ src/sub/dir src/up tool"
	if got := strings.Join(names, " "); got != wantNames {
		t.Fatalf("entries = %s\nwant      %s", got, wantNames)
	}
	for name, want := range map[string]int64{"Makefile": 0o644, "a.txt": 0o644, "build.sh": 0o755, "run": 0o755, "tool": 0o755, "src/a.cpp": 0o644, "src/": 0o755, "empty/": 0o755, "src/link": 0o777} {
		if modes[name] != want {
			t.Errorf("%s has mode %o, want %o", name, modes[name], want)
		}
	}
	for _, header := range headers {
		if header.Name == "src/link" && (header.Typeflag != tar.TypeSymlink || header.Linkname != "a.cpp") {
			t.Errorf("link = %+v", header)
		}
	}
	if first.Starter.Files != 10 || first.Starter.Size != int64(len("b")+len("a")+len("all:\n")+len("#!/bin/sh\n")+len("\x7fELF\x02")+len("data")+len("text")) {
		t.Errorf("counted %d files, %d bytes", first.Starter.Files, first.Starter.Size)
	}
	if !strings.HasPrefix(first.Starter.Hash, "sha256:") || len(first.Starter.Hash) != len("sha256:")+64 {
		t.Errorf("hash = %q", first.Starter.Hash)
	}
}

func TestAnEmptyProjectIsAValidStarter(t *testing.T) {
	_, bundles := compileWorkspace(t, fstest.MapFS{"workspace.md": {Data: []byte("---\n" + okFront + "---\n[ ] a\n")}, "project": {Mode: fs.ModeDir | 0o755}})
	if len(readTar(t, bundles.Starter.Data)) != 0 || bundles.Starter.Files != 0 || bundles.Starter.Hash == "" {
		t.Fatalf("bundle = %+v", bundles.Starter)
	}
}

func TestAGradeScriptNeedsNoExecutableBitWhenItStartsWithAShebang(t *testing.T) {
	files := pkg(okFront, "[ ] a\n", fstest.MapFS{"grading/grade": {Data: []byte("#!/bin/sh\necho {}\n"), Mode: 0o644}, "grading/data.txt": {Data: []byte("x")}})
	exam, bundles := compileWorkspace(t, files)
	if bundles.Grading == nil || exam.GradingBundle != bundles.Grading.Hash {
		t.Fatalf("grading = %+v, document %q", bundles.Grading, exam.GradingBundle)
	}
	for _, header := range readTar(t, bundles.Grading.Data) {
		if header.Name == "grade" && header.Mode != 0o755 {
			t.Errorf("grade has mode %o", header.Mode)
		}
	}
	// An executable bit is enough too, whatever the file holds.
	compileWorkspace(t, pkg(okFront, "[ ] a\n", fstest.MapFS{"grading/grade": {Data: []byte("binary"), Mode: 0o755}}))
}

func TestAnInsideLinkIsKept(t *testing.T) {
	for _, target := range []string{"a.txt", "./a.txt", "sub/../x"[:0] + "dir", "../a.txt", "dir/", "."} {
		files := pkg(okFront, "[ ] a\n", fstest.MapFS{"project/a.txt": {Data: []byte("x")}, "project/dir/link": {Data: []byte(target), Mode: fs.ModeSymlink | 0o777}})
		if _, _, err := CompileWorkspaceFS(files); err != nil {
			t.Errorf("target %q: %v", target, err)
		}
	}
}

// Nothing of the grading folder reaches the document a student's application is built from: not its files, not their names, not
// even a picture the instructions might ask for.
func TestGradingNeverAppearsInTheStudentDocument(t *testing.T) {
	exam, bundles, err := CompileWorkspaceDir("../../testdata/workspace/lab-listas")
	if err != nil {
		t.Fatal(err)
	}
	document := string(encode(t, exam))
	for _, secret := range []string{"HIDDEN-CASES-MARKER-4f1c9a", "hidden_cases", "grading/", "4/4 tests passed", "insert_sorted 5 1 3", "printf"} {
		if strings.Contains(document, secret) {
			t.Errorf("the compiled document contains %q", secret)
		}
	}
	if bundles.Grading == nil || !bytes.Contains(bundles.Grading.Data, []byte("HIDDEN-CASES-MARKER-4f1c9a")) {
		t.Fatal("the grading bundle should hold the hidden files")
	}
	if bytes.Contains(bundles.Starter.Data, []byte("HIDDEN-CASES-MARKER-4f1c9a")) || bytes.Contains(bundles.Starter.Data, []byte("hidden_cases")) {
		t.Error("the starter bundle contains grading files")
	}
	if exam.GradingBundle != bundles.Grading.Hash || exam.StarterBundle == exam.GradingBundle {
		t.Errorf("hashes: %q %q", exam.StarterBundle, exam.GradingBundle)
	}
}

func TestPicturesNextToWorkspaceMdAreEmbedded(t *testing.T) {
	exam, _ := compileWorkspace(t, pkg(okFront, "![A grey pixel](img/a.png)\n\n[ ] a\n", fstest.MapFS{"img/a.png": {Data: tinyPNG(t)}}))
	if !strings.Contains(exam.Content.Instructions, `<img src="data:image/png;base64,`) || !strings.Contains(exam.Content.Instructions, `alt="A grey pixel"`) {
		t.Fatalf("instructions = %q", exam.Content.Instructions)
	}
}

// Heading attributes belong to question exams: in a workspace exam the braces stay text.
func TestHeadingsAreOrdinary(t *testing.T) {
	exam, _ := compileWorkspace(t, pkg(okFront, "## Setup {#setup}\n\n### Build\n\nText.\n\n[ ] a\n"))
	if !strings.Contains(exam.Content.Instructions, "<h2>Setup {#setup}</h2>") || !strings.Contains(exam.Content.Instructions, "<h3>Build</h3>") {
		t.Fatalf("instructions = %q", exam.Content.Instructions)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Globs.

func TestValidGlob(t *testing.T) {
	for _, good := range []string{"src/**", "**/*.o", "Makefile", "a/b/c.txt", "build/**", ".cache/**", "**/__pycache__/**", "src/[ab]?.c*", "*.cpp", "a**b"[:0] + "x/**/y"} {
		if reason := validGlob(good); reason != "" {
			t.Errorf("validGlob(%q) = %q", good, reason)
		}
	}
	for pattern, want := range map[string]string{
		"": "is empty", "/a": "starts with /", "a/": "ends with /", "a//b": "empty", "./a": "empty", "a/../b": "empty", "a\\b": `uses \`, "a**": "inside a segment",
		"**/**/**": "more than twice", "[": "not a valid pattern", "a\x00b": "characters", strings.Repeat("a", 201): "longer than 200",
	} {
		if reason := validGlob(pattern); !strings.Contains(reason, want) {
			t.Errorf("validGlob(%q) = %q, want %q", pattern, reason, want)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	for _, test := range []struct {
		pattern, name string
		want          bool
	}{
		{"build/**", "build/a.o", true}, {"build/**", "build/x/y/z", true}, {"build/**", "build", false}, {"build/**", "src/build/a", false},
		{"**/*.o", "a.o", true}, {"**/*.o", "src/a.o", true}, {"**/*.o", "src/deep/er/a.o", true}, {"**/*.o", "a.cpp", false}, {"**/*.o", "a.o/b", false},
		{"**/__pycache__/**", "__pycache__/x.pyc", true}, {"**/__pycache__/**", "a/b/__pycache__/x.pyc", true}, {"**/__pycache__/**", "a/__pycache__", false},
		{".cache/**", ".cache/x", true}, {"src/**", "src/a.cpp", true}, {"src/**", "tests/a.cpp", false},
		{"Makefile", "Makefile", true}, {"Makefile", "src/Makefile", false}, {"*.cpp", "a.cpp", true}, {"*.cpp", "src/a.cpp", false},
		{"src/?.c", "src/a.c", true}, {"src/?.c", "src/ab.c", false}, {"src/[ab].c", "src/b.c", true}, {"src/[ab].c", "src/c.c", false},
		{"a/**/b", "a/b", true}, {"a/**/b", "a/x/y/b", true}, {"a/**/b", "a/x/c", false}, {"**", "a", true}, {"**", "a/b", true},
	} {
		if got := GlobMatch(test.pattern, test.name); got != test.want {
			t.Errorf("GlobMatch(%q, %q) = %v, want %v", test.pattern, test.name, got, test.want)
		}
	}
	for _, name := range []string{"build/a.out", "src/main.o", "x/__pycache__/m.pyc", ".cache/q"} {
		matched := false
		for _, pattern := range DefaultIgnore() {
			matched = matched || GlobMatch(pattern, name)
		}
		if !matched {
			t.Errorf("the default ignore list does not cover %s", name)
		}
	}
	// The default list is a copy: changing it changes nothing for the next exam.
	list := DefaultIgnore()
	list[0] = "changed"
	if DefaultIgnore()[0] != "build/**" {
		t.Error("DefaultIgnore returns its own slice")
	}
}
