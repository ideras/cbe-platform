// Command examc checks an exam written in Markdown and prints the exam document the server takes.
//
//	examc exam.md                    check the file and print a summary
//	examc -json exam.md              print the compiled exam document (without a PIN)
//	examc -fill answers.json exam.md print the exam filled in with the answers, as a student's file looks
//
// The PIN is never read from the command line, where other users of the computer could see it; the proctor application
// asks for it when importing the exam.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"exam-platform/shared/calc"
	"exam-platform/shared/examdoc"
)

func main() {
	asJSON := flag.Bool("json", false, "print the compiled exam document instead of a summary")
	fill := flag.String("fill", "", "print the exam with the answers of this JSON file (slot ID to answer) filled in")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: examc [-json | -fill answers.json] exam.md")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 || *asJSON && *fill != "" {
		flag.Usage()
		os.Exit(2)
	}
	os.Exit(run(flag.Arg(0), *asJSON, *fill))
}

func run(path string, asJSON bool, fillPath string) int {
	source, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if fillPath != "" {
		return runFill(source, path, fillPath)
	}
	exam, err := examdoc.Compile(source)
	var problems examdoc.Problems
	if errors.As(err, &problems) {
		fmt.Fprintf(os.Stderr, "%s cannot be imported:\n", path)
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "  "+problem)
		}
		return 1
	}
	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(exam); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Printf("%s\n", exam.Title)
	fmt.Printf("  %d min, navigation %s, %d answer fields\n", exam.DurationSeconds/60, exam.Navigation, exam.Content.SlotCount())
	if summary := calculatorSummary(exam.Tools); summary != "" {
		fmt.Printf("  %s\n", summary)
	}
	for _, exercise := range exam.Content.Exercises {
		fmt.Printf("  %-4s %s%s\n", exercise.ID, exercise.Title, points(exercise.Points))
		for _, slot := range exercise.Slots {
			fmt.Printf("         %s (%s)\n", slot.ID, slot.Kind)
		}
		for _, item := range exercise.Items {
			fmt.Printf("    %-4s %s%s\n", item.ID, item.Title, points(item.Points))
			for _, slot := range item.Slots {
				fmt.Printf("           %s (%s)\n", slot.ID, slot.Kind)
			}
		}
	}
	if total, ok := exam.Content.Points(); ok {
		fmt.Printf("  total: %g points\n", total)
	}
	return 0
}

// runFill prints the exam with a student's answers in it, to see the format by hand. The original front matter is kept;
// the dashboard replaces it with the submission's.
func runFill(source []byte, examPath, answersPath string) int {
	data, err := os.ReadFile(answersPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var answers map[string]string
	if err := json.Unmarshal(data, &answers); err != nil {
		fmt.Fprintf(os.Stderr, "%s: not a JSON object of slot ID to answer: %v\n", answersPath, err)
		return 1
	}
	if answers == nil {
		answers = map[string]string{}
	}
	filled, err := examdoc.Fill(source, answers, frontMatter(source))
	var problems examdoc.Problems
	if errors.As(err, &problems) {
		fmt.Fprintf(os.Stderr, "%s cannot be filled in:\n", examPath)
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "  "+problem)
		}
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := os.Stdout.Write(filled); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// frontMatter returns the leading --- block of the file, delimiters included, as a string without a trailing newline.
func frontMatter(source []byte) string {
	source = bytes.TrimPrefix(source, []byte("\xef\xbb\xbf"))
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	return ""
}

// calculatorSummary describes the exam's calculator for the summary, or "" when there is none. It shows the
// effective configuration, the way GET /exam sends it to the student application. The angle setting is named only
// when the exam enables scientific mode: it affects the trigonometric functions alone, which no other mode has.
func calculatorSummary(tools *calc.Tools) string {
	if tools == nil || !tools.On() {
		return ""
	}
	effective := tools.Effective()
	c := effective.Calculator
	parts := []string{"calculator: " + strings.Join(c.Modes, ", ")}
	if calc.ModeEnabled(effective, calc.ModeScientific) {
		switch c.AngleMode {
		case calc.AngleDegrees:
			parts = append(parts, "degrees")
		case calc.AngleRadians:
			parts = append(parts, "radians")
		default:
			parts = append(parts, "selectable angle")
		}
	}
	if n := len(c.DisabledFunctions); n > 0 {
		plural := ""
		if n > 1 {
			plural = "s"
		}
		parts = append(parts, fmt.Sprintf("%d function%s disabled", n, plural))
	}
	return strings.Join(parts, " · ")
}

func points(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf(" [%g pts]", *value)
}
