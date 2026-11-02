package dashboard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"exam-platform/shared/examdoc"
)

// The answers-only download format. A student's file holds identity and provenance in its front matter, then every answer field
// of the exam with its stable ID, kind, answer state and the saved string, and nothing of the exam's text (examdoc.AnswersOnly
// draws that line). It is a faithful record, not a corrected answer, a model solution or an evaluation.

// exportOutcome is what one finished student's file came to in this run.
type exportOutcome struct {
	// written is true when the file was written in this run. Only then does the answers-only index link it.
	written bool
	// failure says why the file was not written; empty when it was.
	failure string
	// warnings are the fields of a written file that need the instructor's attention.
	warnings []examdoc.AnswersOnlyWarning
}

// renderAnswersOnly is the student's answers-only file: the saved answers of their final version under the exam's field labels.
// The warnings name fields without a saved value and saved keys that match no field. It reads the stored Content and never the
// exam's Markdown, so a source that is missing, malformed or out of step with the stored exam changes nothing.
func renderAnswersOnly(detail examDetail, sub submission, now time.Time) ([]byte, []examdoc.AnswersOnlyWarning, error) {
	if sub.Version == nil || sub.SubmittedAt == nil {
		return nil, nil, errors.New("The server's submission is incomplete.")
	}
	answers := sub.Version.Answers
	if answers == nil {
		answers = map[string]string{}
	}
	data, warnings, err := examdoc.AnswersOnly(detail.Content, answers, answersOnlyHeader(detail, sub, now), detail.Title+" — "+sub.StudentName)
	if err != nil {
		return nil, nil, errors.New("The exam's answer fields cannot be exported: " + err.Error() + ".")
	}
	return data, warnings, nil
}

// answersOnlyHeader is the front matter of a student's answers-only file. It is built apart from submissionHeader so nothing
// the complete file says for the instructor (locks, what the proctor typed when unlocking or force-submitting) can reach a file
// a student may be given. answer_version is the sequence number of the saved answers, not a revision of the exam.
func answersOnlyHeader(detail examDetail, sub submission, now time.Time) string {
	// The reason a proctor typed is the proctor's note; the file keeps only how the session ended.
	sub.OperatorReason = ""
	var out strings.Builder
	out.WriteString("---\n")
	fmt.Fprintf(&out, "export_format: %s\n", yamlText(FormatAnswersOnly))
	fmt.Fprintf(&out, "export_schema: %d\n", answersOnlySchema)
	fmt.Fprintf(&out, "exam: %s\n", yamlText(detail.Title))
	fmt.Fprintf(&out, "exam_id: %s\n", yamlText(detail.ExamID))
	fmt.Fprintf(&out, "class: %s\n", yamlText(detail.ClassName))
	fmt.Fprintf(&out, "student: %s\n", yamlText(sub.StudentName))
	fmt.Fprintf(&out, "account_number: %s\n", yamlText(sub.AccountNumber))
	fmt.Fprintf(&out, "session_id: %s\n", yamlText(sub.SessionID))
	fmt.Fprintf(&out, "status: %s\n", yamlText(statusPhrase(sub)))
	fmt.Fprintf(&out, "submitted_at: %s\n", yamlText(localStamp(*sub.SubmittedAt)))
	fmt.Fprintf(&out, "answer_version: %d\n", sub.Version.Sequence)
	fmt.Fprintf(&out, "downloaded_at: %s\n", yamlText(localStamp(now)))
	out.WriteString("---")
	return out.String()
}

// answersOnlyIndexHeader is the front matter of the answers-only _index.md, which says what kind of export the folder holds.
func answersOnlyIndexHeader(detail examDetail, now time.Time) string {
	return "---\n" +
		"export_format: " + yamlText(FormatAnswersOnly) + "\n" +
		"export_schema: " + strconv.Itoa(answersOnlySchema) + "\n" +
		"exam: " + yamlText(detail.Title) + "\n" +
		"exam_id: " + yamlText(detail.ExamID) + "\n" +
		"class: " + yamlText(detail.ClassName) + "\n" +
		"downloaded_at: " + yamlText(localStamp(now)) + "\n" +
		"---"
}

// renderAnswersOnlyIndex is the instructor's checklist of an answers-only download. Unlike the complete one it links a
// student's file only when this run wrote it, says what happened to each finished student's file, and lists the fields that
// need a look.
func renderAnswersOnlyIndex(detail examDetail, roster Roster, names map[string]string, outcomes map[string]exportOutcome, now time.Time) ([]byte, error) {
	return buildIndex(detail, roster, names, true, outcomes, now)
}

// answersOnlyWarnings are the lines the result shows when written files have fields that need a look. The detail, with account
// numbers and field IDs, is in _index.md.
func answersOnlyWarnings(roster Roster, outcomes map[string]exportOutcome) []string {
	var missing, unmapped []string
	for _, session := range roster.Sessions {
		outcome := outcomes[session.Account]
		if !outcome.written {
			continue
		}
		hasMissing, hasUnmapped := false, false
		for _, warning := range outcome.warnings {
			hasMissing = hasMissing || warning.Kind == examdoc.WarnMissing
			hasUnmapped = hasUnmapped || warning.Kind == examdoc.WarnUnmapped
		}
		if hasMissing {
			missing = append(missing, session.Name)
		}
		if hasUnmapped {
			unmapped = append(unmapped, session.Name)
		}
	}
	var lines []string
	if len(missing) > 0 {
		lines = append(lines, fmt.Sprintf("%s answer fields with no saved value (%s). They are marked missing in the file and listed in _index.md.", filesOf(len(missing)), nameList(missing)))
	}
	if len(unmapped) > 0 {
		lines = append(lines, fmt.Sprintf("%s saved answers that match no field of the exam (%s). They are kept in an Unmapped saved answers section and listed in _index.md.", filesOf(len(unmapped)), nameList(unmapped)))
	}
	return lines
}

func filesOf(n int) string {
	if n == 1 {
		return "1 student's file has"
	}
	return fmt.Sprintf("%d students' files have", n)
}

// nameList names up to five students, then says how many more.
func nameList(names []string) string {
	const shown = 5
	if len(names) <= shown {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:shown], ", "), len(names)-shown)
}

// exportNotes is a written file's warnings as two table cells: the fields with no saved value, and the saved keys that match no
// field. The key of a saved answer is quoted and clipped; an answer itself never appears.
func exportNotes(warnings []examdoc.AnswersOnlyWarning) (missing, unmapped string) {
	var missingIDs, unmappedKeys []string
	for _, warning := range warnings {
		switch warning.Kind {
		case examdoc.WarnMissing:
			missingIDs = append(missingIDs, warning.Field)
		case examdoc.WarnUnmapped:
			unmappedKeys = append(unmappedKeys, strconv.Quote(clip(warning.Field, 60)))
		}
	}
	return limitedList(missingIDs), limitedList(unmappedKeys)
}

// limitedList joins items, stopping after forty so one student cannot make the checklist unreadable.
func limitedList(items []string) string {
	const shown = 40
	if len(items) <= shown {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(items[:shown], ", "), len(items)-shown)
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// existingExportFormat says which format the _index.md of an existing download folder identifies: FormatAnswersOnly or
// FormatComplete, where an index without format metadata is a complete download from before the formats existed. It is empty
// when the folder has no _index.md that can be read. It only reads the first lines of a regular file.
func existingExportFormat(folder string) string {
	path := filepath.Join(folder, indexFileName)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	buffer := make([]byte, 2048)
	n, _ := io.ReadFull(file, buffer)
	lines := strings.Split(strings.ReplaceAll(string(buffer[:n]), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return FormatComplete
	}
	for _, line := range lines[1:] {
		if line == "---" {
			break
		}
		if value, ok := strings.CutPrefix(line, "export_format:"); ok {
			if strings.Trim(strings.TrimSpace(value), `"'`) == FormatAnswersOnly {
				return FormatAnswersOnly
			}
			return FormatComplete
		}
	}
	return FormatComplete
}

// yamlText quotes a front matter value for the answers-only files. Unlike yamlString it keeps every character: a line break,
// a tab or any character YAML does not print is written as an escape, so a name cannot add a line or an invalid character to
// the front matter.
func yamlText(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '\\':
			out.WriteString(`\\`)
		case r == '"':
			out.WriteString(`\"`)
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\r':
			out.WriteString(`\r`)
		case r == '\t':
			out.WriteString(`\t`)
		case r == ' ' || unicode.IsPrint(r) && r != unicode.ReplacementChar:
			out.WriteRune(r)
		case r > 0xFFFF:
			fmt.Fprintf(&out, `\U%08X`, r)
		default:
			fmt.Fprintf(&out, `\u%04X`, r)
		}
	}
	out.WriteByte('"')
	return out.String()
}
