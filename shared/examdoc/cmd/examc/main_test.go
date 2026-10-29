package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exam-platform/shared/calc"
	"exam-platform/shared/examdoc"
	"exam-platform/shared/examimage"
)

// The summary names the angle setting only when scientific mode is on: no other mode has trigonometric functions,
// so for them the setting has no effect.
func TestCalculatorSummary(t *testing.T) {
	on := func(modes []string, angle string, disabled ...string) *calc.Tools {
		return &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: modes, AngleMode: angle, DisabledFunctions: disabled}}
	}
	for _, test := range []struct {
		tools *calc.Tools
		want  string
	}{
		{nil, ""},
		{&calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: false}}, ""},
		{on(nil, ""), "calculator: scientific · selectable angle"},
		{on([]string{calc.ModeScientific, calc.ModeNeural}, calc.AngleRadians, "softmax", "matmul"), "calculator: scientific, neural · radians · 2 functions disabled"},
		{on([]string{calc.ModeScientific}, calc.AngleDegrees), "calculator: scientific · degrees"},
		{on([]string{calc.ModeProgrammer}, ""), "calculator: programmer"},
		{on([]string{calc.ModeProgrammer}, calc.AngleRadians, "ieee754"), "calculator: programmer · 1 function disabled"},
		{on([]string{calc.ModeNeural}, calc.AngleDegrees), "calculator: neural"},
	} {
		if got := calculatorSummary(test.tools); got != test.want {
			t.Errorf("summary = %q, want %q", got, test.want)
		}
	}
}

// The pictures are found next to the exam file, whatever the working directory is, and -json carries them embedded.
func TestRunReadsPicturesNextToTheExam(t *testing.T) {
	var square bytes.Buffer
	if err := png.Encode(&square, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img", "square.png"), square.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	source := "---\ntitle: Quiz\nduration: 30m\n---\n## A\n\n![A grey square](img/square.png)\n\n{{x}}\n"
	examPath := filepath.Join(dir, "quiz.md")
	if err := os.WriteFile(examPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	output := captureStdout(t, func() {
		if code := run(examPath, true, ""); code != 0 {
			t.Fatalf("run = %d", code)
		}
	})
	var exam examdoc.Exam
	if err := json.Unmarshal([]byte(output), &exam); err != nil {
		t.Fatalf("output is not JSON: %v\n%.300s", err, output)
	}
	want := examimage.HTML(examimage.PNG, square.Bytes(), "A grey square")
	if body := exam.Content.Exercises[0].Body; !strings.Contains(body, want) {
		t.Errorf("the body lacks the embedded picture:\n%.300s", body)
	}
	if strings.Contains(output, `\u003c`) {
		t.Errorf("-json escaped the <img> element:\n%.300s", output)
	}

	// A picture that is not there is a problem, and the exit code says so.
	if err := os.Remove(filepath.Join(dir, "img", "square.png")); err != nil {
		t.Fatal(err)
	}
	if code := run(examPath, false, ""); code != 1 {
		t.Errorf("run with a missing picture = %d, want 1", code)
	}
}

func captureStdout(t *testing.T, call func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = writer
	done := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- data
	}()
	defer func() { os.Stdout = saved }()
	call()
	writer.Close()
	os.Stdout = saved
	return string(<-done)
}
