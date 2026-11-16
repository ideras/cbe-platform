package workspace

import (
	"bytes"
	"context"
	"encoding/csv"
	"strconv"
	"strings"
	"time"
)

// The grade sheet (Phase 8): what a teacher takes away of an exam's grades, one line for each student of the class, as a CSV a spreadsheet opens.
//
// A grade is advisory until the proctor confirms it (P6-3), and the sheet keeps the two apart instead of choosing for the teacher: `points` has the
// confirmed grade, `advisory_points` the latest run's points of a student whose grade is not confirmed. The columns of the TODO items have the points of
// that same run. `status` says which it is, and says it too for a student with no grade.

// gradeStatus is the words of the sheet's status column.
func gradeStatus(row GradeSheetRow) string {
	switch {
	case row.SessionID == "":
		return "not logged in"
	case row.Grade == nil && row.Submissions > 0:
		return "not graded"
	case row.Grade == nil:
		return "no submission"
	case row.Grade.State == GradingQueued:
		return "waiting to be graded"
	case row.Grade.State == GradingRunning:
		return "being graded"
	case row.Grade.State == GradingFailed:
		return "grading failed: " + row.Grade.Failure
	case row.Grade.Confirmed:
		return "confirmed"
	default:
		return "advisory"
	}
}

// sheetCell makes a text safe to put in a spreadsheet: a cell that begins with = + - @ or a control character is taken for a formula by Excel and
// Calc, and a name or an e-mail address in a roster is not the proctor's own writing.
func sheetCell(text string) string {
	if text != "" && strings.ContainsRune("=+-@\t\r\n", rune(text[0])) {
		return "'" + text
	}
	return text
}

func number(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// GradesCSV is the exam's grade sheet: UTF-8 with a byte order mark (so that a spreadsheet reads the accents of the names), lines ending in CR LF.
func (s *Service) GradesCSV(ctx context.Context, examID string) ([]byte, error) {
	exam, err := s.Repo.Exam(ctx, examID)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.GradeSheet(ctx, examID)
	if err != nil {
		return nil, err
	}
	todos := exam.Document.Content.Todos
	var examMax *float64
	for _, todo := range todos {
		if todo.Points != nil {
			if examMax == nil {
				examMax = new(float64)
			}
			*examMax += *todo.Points
		}
	}
	var out bytes.Buffer
	out.WriteString("\xef\xbb\xbf")
	writer := csv.NewWriter(&out)
	writer.UseCRLF = true
	header := []string{"account_number", "name", "email", "status", "points", "advisory_points", "max_points"}
	for _, todo := range todos {
		if todo.Points != nil {
			header = append(header, todo.ID+" ("+number(*todo.Points)+")")
		} else {
			header = append(header, todo.ID)
		}
	}
	header = append(header, "todos_ticked_by_student", "submitted", "final_submission", "last_submission_at", "graded_at")
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for _, row := range rows {
		cells := []string{sheetCell(row.Account), sheetCell(row.Name), sheetCell(row.Email), gradeStatus(row), "", "", ""}
		if examMax != nil {
			cells[6] = number(*examMax)
		}
		graded := row.Grade != nil && row.Grade.State == GradingDone
		if graded && row.Grade.Points != nil {
			if row.Grade.Confirmed {
				cells[4] = number(*row.Grade.Points)
			} else {
				cells[5] = number(*row.Grade.Points)
			}
		}
		results := map[string]GradingResult{}
		for _, result := range row.Results {
			if result.Known {
				results[result.TodoID] = result
			}
		}
		for _, todo := range todos {
			cell := ""
			if result, found := results[todo.ID]; graded && found {
				cell = number(result.Points)
			}
			cells = append(cells, cell)
		}
		yes := func(value bool) string {
			if value {
				return "yes"
			}
			return "no"
		}
		last, at := "", ""
		if row.LastSubmission != nil {
			last = row.LastSubmission.UTC().Format(time.RFC3339)
		}
		if graded {
			at = row.Grade.At.UTC().Format(time.RFC3339)
		}
		ticked := ""
		if row.SessionID != "" {
			ticked = strconv.Itoa(row.Ticked)
		}
		cells = append(cells, ticked, yes(row.Submissions > 0), yes(row.Final), last, at)
		if err := writer.Write(cells); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return out.Bytes(), writer.Error()
}
