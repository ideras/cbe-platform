package examdoc

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"exam-platform/shared/calc"
)

// settings is what the front matter says about the exam as a whole.
type settings struct {
	exam   Exam
	points *float64
}

// splitFrontMatter separates the leading "---" block from the Markdown body. lines is how many lines the block used, so
// positions in the body can be reported as lines of the whole file.
func splitFrontMatter(source []byte) (front []string, body []byte, lines int, err error) {
	text := string(source)
	first, rest, found := strings.Cut(text, "\n")
	if strings.TrimRight(first, " \t\r") != "---" {
		return nil, nil, 0, fmt.Errorf("line 1: the file must start with a front matter block opened by ---")
	}
	lines = 1
	for found {
		var line string
		line, rest, found = strings.Cut(rest, "\n")
		lines++
		if strings.TrimRight(line, " \t\r") == "---" {
			return front, []byte(rest), lines, nil
		}
		front = append(front, strings.TrimRight(line, "\r"))
	}
	return nil, nil, 0, fmt.Errorf("line 1: the front matter block is not closed by a --- line")
}

// parseFrontMatter reads "key: value" lines. Values may be quoted; # starts a comment line.
func parseFrontMatter(lines []string, report func(int, string, ...any)) settings {
	var s settings
	s.exam.Navigation = FreeNavigation
	seen := map[string]bool{}
	for i, raw := range lines {
		line := i + 2
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			report(line, "front matter lines must look like key: value")
			continue
		}
		if seen[key] {
			report(line, "%s is set twice", key)
			continue
		}
		seen[key] = true
		if unquoted, err := unquote(value); err != nil {
			report(line, "%s: %v", key, err)
			continue
		} else {
			value = unquoted
		}
		switch key {
		case "title":
			s.exam.Title = value
		case "duration":
			if seconds, ok := wholeSeconds(value); ok {
				s.exam.DurationSeconds = seconds
			} else {
				report(line, "duration must be a positive length of time in whole seconds, such as 80m or 1h20m")
			}
		case "grace":
			if seconds, ok := wholeSeconds(value); ok {
				s.exam.GraceSeconds = &seconds
			} else {
				report(line, "grace must be a positive length of time in whole seconds, such as 2m or 30s")
			}
		case "focus_threshold":
			report(line, "focus_threshold is set in the proctor's Settings now")
		case "min_volume":
			volume, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
			if err != nil || volume < 0 || volume > 100 {
				report(line, "min_volume must be a whole number from 0 to 100")
				continue
			}
			s.exam.MinVolumePercent = &volume
		case "opens_at", "closes_at":
			at, err := time.Parse(time.RFC3339, value)
			if err != nil {
				report(line, "%s must be a date and time with its offset, such as 2026-10-20T08:00:00-06:00", key)
				continue
			}
			if key == "opens_at" {
				s.exam.OpensAt = &at
			} else {
				s.exam.ClosesAt = &at
			}
		case "navigation":
			if value != FreeNavigation && value != ForwardOnly {
				report(line, "navigation must be %s or %s", FreeNavigation, ForwardOnly)
				continue
			}
			s.exam.Navigation = value
		case "points":
			points, err := strconv.ParseFloat(value, 64)
			if err != nil || !(points > 0) || points > maxPoints {
				report(line, "points must be a positive number")
				continue
			}
			s.points = &points
		case "pin":
			report(line, "the PIN does not belong in the exam file; the proctor enters it when importing the exam")
		case "calculator":
			calculator, problems := parseCalculatorValue(value)
			for _, problem := range problems {
				report(line, "calculator: %s", problem)
			}
			if len(problems) == 0 {
				s.exam.Tools = &calc.Tools{Version: 1, Calculator: calculator}
			}
		default:
			report(line, "unknown front matter key %q", key)
		}
	}
	if strings.TrimSpace(s.exam.Title) == "" {
		report(1, "the front matter must set a title")
	}
	if !seen["duration"] {
		report(1, "the front matter must set a duration, such as duration: 80m")
	}
	return s
}

func unquote(value string) (string, error) {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
		if value[len(value)-1] != value[0] {
			return "", fmt.Errorf("the quoted value is not closed")
		}
		if value[0] == '\'' {
			return value[1 : len(value)-1], nil
		}
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("the quoted value is not valid")
		}
		return unquoted, nil
	}
	return value, nil
}

// wholeSeconds turns a duration like 80m into whole seconds.
func wholeSeconds(value string) (int64, bool) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return 0, false
	}
	return int64(duration / time.Second), true
}

// parseCalculatorValue reads the calculator key's value (D9): the bare key, or comma-separated setting=value
// pairs whose list values sit in brackets, so a list's own commas do not split the pairs. It returns one
// problem per mistake, for the caller to report with the key's line.
func parseCalculatorValue(value string) (*calc.Calculator, []string) {
	if strings.TrimSpace(value) == "" {
		// The bare key turns the calculator on with the defaults.
		return &calc.Calculator{Enabled: true}, nil
	}
	var problems []string
	result := &calc.Calculator{Enabled: true}
	seen := map[string]bool{}
	for _, pair := range splitSettings(value) {
		setting, rest, found := strings.Cut(pair, "=")
		setting, rest = strings.TrimSpace(setting), strings.TrimSpace(rest)
		if !found || setting == "" || rest == "" {
			problems = append(problems, fmt.Sprintf("%q is not a setting=value pair such as angle=rad", strings.TrimSpace(pair)))
			continue
		}
		if seen[setting] {
			problems = append(problems, fmt.Sprintf("%s is set twice", setting))
			continue
		}
		seen[setting] = true
		switch setting {
		case "mode":
			before := len(problems)
			names, ok := listValue(rest)
			if !ok {
				problems = append(problems, fmt.Sprintf("mode=%s is not a list such as mode=[scientific, neural] or a single mode", rest))
				continue
			}
			if len(names) == 0 {
				problems = append(problems, "the mode list is empty; use mode=[scientific] or leave mode out for scientific alone")
				continue
			}
			for _, name := range names {
				if !calc.ModeKnown(name) {
					problems = append(problems, fmt.Sprintf("mode=[…] names no mode %q", name))
				}
			}
			if len(problems) == before {
				result.Modes = names
			}
		case "angle":
			if !calc.AngleModeKnown(rest) {
				problems = append(problems, fmt.Sprintf("angle must be deg, rad or user, not %q", rest))
				continue
			}
			result.AngleMode = rest
		case "disabled":
			before := len(problems)
			names, ok := listValue(rest)
			if !ok {
				problems = append(problems, fmt.Sprintf("disabled=%s is not a list such as disabled=[softmax] or a single name", rest))
				continue
			}
			if len(names) == 0 {
				problems = append(problems, "the disabled list is empty; leave disabled out when nothing is disabled")
				continue
			}
			for _, name := range names {
				if !calc.RegistryHas(name) {
					problems = append(problems, fmt.Sprintf("disabled=[…] names nothing in the registry: %q", name))
				}
			}
			if len(problems) == before {
				result.DisabledFunctions = names
			}
		default:
			problems = append(problems, fmt.Sprintf("unknown setting %q; the settings are mode, angle and disabled", setting))
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return result, nil
}

// splitSettings splits a calculator value on the commas that separate its setting=value pairs, skipping the
// commas inside a bracketed list.
func splitSettings(value string) []string {
	var pairs []string
	depth, start := 0, 0
	for i, r := range value {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case r == ',' && depth == 0:
			pairs = append(pairs, value[start:i])
			start = i + 1
		}
	}
	pairs = append(pairs, value[start:])
	// Drop pieces that are only whitespace, so a trailing comma is not an error of its own.
	return slices.DeleteFunc(pairs, func(pair string) bool { return strings.TrimSpace(pair) == "" })
}

// listValue reads a setting's value: a single bare token, or a bracketed, comma-separated list. ok is false when
// the brackets do not close.
func listValue(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") {
		return []string{value}, true
	}
	if !strings.HasSuffix(value, "]") {
		return nil, false
	}
	var names []string
	for _, name := range strings.Split(value[1:len(value)-1], ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names, true
}

// lineOf converts a byte offset in the body into a line of the file.
func lineOf(body []byte, offset, frontLines int) int {
	offset = min(max(offset, 0), len(body))
	return frontLines + bytes.Count(body[:offset], []byte("\n")) + 1
}
