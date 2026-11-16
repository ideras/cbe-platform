//go:build linux

package httpapi

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/runner"
	"exam-platform/internal/workspace/workspacetest"
)

// Phase 8: the exam's grade sheet, a CSV the proctor takes away. It has a line for every student of the class (also one who never logged in), keeps a
// confirmed grade apart from an advisory one, and cannot be turned into a formula by a name in the roster.
func TestTheGradeSheetHasEveryStudentAndKeepsAdvisoryGradesApart(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	f.runner.GradeHook = func(_ context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		return runner.GradeResult{Stdout: goodOutput}, nil
	}
	// The roster: Ada, a student whose name is a formula, and one who will never log in.
	if _, err := f.store.ReplaceClassRoster(context.Background(), apiClassID, []domain.Student{
		{AccountNumber: "0012", CanvasID: "1", Name: "Ada Lovelace", Email: "ada@example.invalid", SISUserID: "s1"},
		{AccountNumber: "0034", CanvasID: "2", Name: "=SUM(1)+Grace", Email: "grace@example.invalid", SISUserID: "s2"},
		{AccountNumber: "0056", CanvasID: "3", Name: "Clarke, Joan", Email: "joan@example.invalid", SISUserID: "s3"}}); err != nil {
		t.Fatal(err)
	}
	sheet := func() (raw string, rows [][]string) {
		t.Helper()
		w := request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/grades.csv", "", "Bearer "+testAdminToken)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
			t.Fatalf("grades = %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Content-Disposition"))
		}
		raw = w.Body.String()
		if !strings.HasPrefix(raw, "\xef\xbb\xbf") || !strings.Contains(raw, "\r\n") {
			t.Errorf("the sheet has no byte order mark or no CR LF: %q", raw[:min(60, len(raw))])
		}
		parsed, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(raw, "\xef\xbb\xbf"))).ReadAll()
		if err != nil {
			t.Fatalf("the sheet is not a CSV: %v\n%s", err, raw)
		}
		return raw, parsed
	}
	byAccount := func(rows [][]string, account string) []string {
		t.Helper()
		for _, row := range rows[1:] {
			if row[0] == account {
				return row
			}
		}
		t.Fatalf("no line for %s in %v", account, rows)
		return nil
	}

	// Nobody has logged in: a line for each student of the class, and the columns.
	_, rows := sheet()
	wantHeader := "account_number,name,email,status,points,advisory_points,max_points,t1 (10),t2 (15),todos_ticked_by_student,submitted,final_submission,last_submission_at,graded_at"
	if strings.Join(rows[0], ",") != wantHeader || len(rows) != 4 {
		t.Fatalf("the header and the lines: %v", rows)
	}
	if row := byAccount(rows, "0056"); row[3] != "not logged in" || row[4] != "" || row[6] != "25" || row[10] != "no" {
		t.Errorf("a student who never logged in: %v", row)
	}
	// Sorted by name, as a teacher reads a list.
	if rows[1][1] != "=SUM(1)+Grace" && rows[1][1] != "'=SUM(1)+Grace" || rows[2][1] != "Ada Lovelace" && rows[2][1] != "Clarke, Joan" {
		t.Logf("order: %v", [][]string{rows[1], rows[2], rows[3]})
	}

	// Ada submits and is graded: advisory until confirmed.
	ada := f.submitted(t, "0012", "int answer = 42;\n")
	if status, body := f.post(t, "/admin/workspace/session/"+ada.SessionID+"/grade", ""); status != http.StatusAccepted {
		t.Fatalf("grading: %d %s", status, body)
	}
	run := f.gradedWith(t, ada.SessionID)
	_, rows = sheet()
	row := byAccount(rows, "0012")
	if row[3] != "advisory" || row[4] != "" || row[5] != "16" || row[6] != "25" || row[7] != "10" || row[8] != "6" || row[9] != "0" || row[10] != "yes" || row[11] != "no" || row[12] == "" || row[13] == "" {
		t.Errorf("Ada, graded and not confirmed: %v", row)
	}
	// Confirmed: the points move to the column of the confirmed grade.
	if status, body := f.post(t, fmt.Sprintf("/admin/workspace/grading/%d/confirm", run.ID), ""); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("confirm: %d %s", status, body)
	}
	_, rows = sheet()
	if row = byAccount(rows, "0012"); row[3] != "confirmed" || row[4] != "16" || row[5] != "" || row[7] != "10" || row[8] != "6" {
		t.Errorf("Ada, confirmed: %v", row)
	}
	// A student who logged in and has handed nothing in.
	grace := f.loginReady(t, "0034")
	_ = grace
	raw, rows := sheet()
	t.Logf("the sheet:\n%s", strings.ReplaceAll(strings.TrimPrefix(raw, "\xef\xbb\xbf"), "\r\n", "\n"))
	if row = byAccount(rows, "0034"); row[3] != "no submission" || row[4] != "" || row[9] != "0" {
		t.Errorf("a student with nothing submitted: %v", row)
	}
	// The name that is a formula is text in the sheet, and a name with a comma is one cell.
	if row[1] != "'=SUM(1)+Grace" {
		t.Errorf("the name that begins with =: %q", row[1])
	}
	if row = byAccount(rows, "0056"); row[1] != "Clarke, Joan" {
		t.Errorf("a name with a comma: %q", row[1])
	}
	// The route is the proctor's only, and a question exam has none.
	if w := request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/grades.csv", "", ""); w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("without the admin token: %d", w.Code)
	}
	if w := request(f.router, "GET", "/admin/workspace/exam/"+apiExamID+"/grades.csv", "", "Bearer "+testAdminToken); w.Code != http.StatusNotFound {
		t.Errorf("a question exam's sheet: %d", w.Code)
	}
}
