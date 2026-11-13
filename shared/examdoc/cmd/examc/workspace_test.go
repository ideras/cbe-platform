package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exam-platform/shared/examdoc"
)

const example = "../../../../testdata/workspace/lab-listas"

// captureStderr is captureStdout for the standard error: the problems of a package are written there.
func captureStderr(t *testing.T, call func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = writer
	done := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- data
	}()
	defer func() { os.Stderr = saved }()
	call()
	writer.Close()
	os.Stderr = saved
	return string(<-done)
}

// A folder that holds workspace.md is a workspace package, and so is the path of the workspace.md in it.
func TestRunSummarizesAWorkspacePackage(t *testing.T) {
	for _, path := range []string{example, filepath.Join(example, "workspace.md")} {
		output := captureStdout(t, func() {
			if code := run(path, false, ""); code != 0 {
				t.Fatalf("run(%s) = %d", path, code)
			}
		})
		for _, want := range []string{"Laboratorio 2 – Listas enlazadas", "workspace exam, 120 min, image cbe/cpp20, terminal on", "run: make run · check: make check",
			"resources: 1 cpu, 1024 MiB memory, 100 MiB disk", "t1   Implement push_front in src/list.cpp [10 pts]", "total: 40 points", "starter bundle: 5 files", "grading bundle: 2 files", "sha256:"} {
			if !strings.Contains(output, want) {
				t.Errorf("the summary of %s lacks %q:\n%s", path, want, output)
			}
		}
		if strings.Contains(output, "HIDDEN-CASES-MARKER") || strings.Contains(output, "hidden_cases") {
			t.Errorf("the summary names the grading files:\n%s", output)
		}
	}
}

func TestRunPrintsTheWorkspaceDocumentAsJSON(t *testing.T) {
	output := captureStdout(t, func() {
		if code := run(example, true, ""); code != 0 {
			t.Fatalf("run = %d", code)
		}
	})
	golden, err := os.ReadFile("../../../../testdata/workspace/lab-listas.json")
	if err != nil {
		t.Fatal(err)
	}
	if output != string(golden) {
		t.Fatalf("-json differs from testdata/workspace/lab-listas.json:\n%s", output)
	}
	var document examdoc.WorkspaceExam
	if err := json.Unmarshal([]byte(output), &document); err != nil || document.Kind != examdoc.KindWorkspace {
		t.Fatalf("output is not a workspace document: %v", err)
	}
}

func TestRunWritesTheBundlesOnRequest(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundles", "lab")
	captureStdout(t, func() {
		if code := runWith(example, true, "", out); code != 0 {
			t.Fatalf("runWith = %d", code)
		}
	})
	_, bundles, err := examdoc.CompileWorkspaceDir(example)
	if err != nil {
		t.Fatal(err)
	}
	starter, err := os.ReadFile(filepath.Join(out, "starter.tar"))
	if err != nil || !bytes.Equal(starter, bundles.Starter.Data) {
		t.Errorf("starter.tar: %v", err)
	}
	grading, err := os.ReadFile(filepath.Join(out, "grading.tar"))
	if err != nil || !bytes.Equal(grading, bundles.Grading.Data) {
		t.Errorf("grading.tar: %v", err)
	}

	// A package without a grading folder writes no grading.tar.
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workspace.md", "---\ntitle: Lab\nduration: 1h\nimage: cbe/cpp20\n---\n[ ] Do it\n")
	write("project/main.c", "int main(void) { return 0; }\n")
	out = filepath.Join(t.TempDir(), "out")
	captureStdout(t, func() {
		if code := runWith(dir, false, "", out); code != 0 {
			t.Fatalf("runWith = %d", code)
		}
	})
	if _, err := os.Stat(filepath.Join(out, "starter.tar")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(out, "grading.tar")); !os.IsNotExist(err) {
		t.Errorf("grading.tar: %v", err)
	}
}

func TestRunNamesTheProblemsOfAWorkspacePackage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "workspace.md"), []byte("---\ntitle: Lab\nduration: 1h\nimage: cbe/cpp20\nterminal: maybe\n---\n[ ] a {#x}\n[ ] b {#x}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var code int
	stderr := captureStderr(t, func() { code = run(dir, false, "") })
	if code != 1 {
		t.Errorf("run = %d, want 1", code)
	}
	for _, want := range []string{"cannot be imported:", "line 5: terminal must be on or off", `line 8: TODO id "x" is already used`, "project/ is missing"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestOptionsThatDoNotFitThePathAreRefused(t *testing.T) {
	var code int
	if stderr := captureStderr(t, func() { code = run(example, false, "answers.json") }); code != 2 || !strings.Contains(stderr, "-fill is for question exams") {
		t.Errorf("-fill on a package: %d, %q", code, stderr)
	}
	exam := filepath.Join(t.TempDir(), "exam.md")
	if err := os.WriteFile(exam, []byte("---\ntitle: Quiz\nduration: 30m\n---\n## A\n{{x}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stderr := captureStderr(t, func() { code = runWith(exam, false, "", t.TempDir()) }); code != 2 || !strings.Contains(stderr, "-bundles is for workspace packages") {
		t.Errorf("-bundles on a question exam: %d, %q", code, stderr)
	}
	// A question exam file still compiles as it always did.
	if output := captureStdout(t, func() { code = run(exam, false, "") }); code != 0 || !strings.Contains(output, "Quiz") {
		t.Errorf("question exam: %d, %q", code, output)
	}
}
