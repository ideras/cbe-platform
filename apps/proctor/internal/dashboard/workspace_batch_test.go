package dashboard

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testMarkdownReport = "# Evaluation\n\n```go\npackage demo\n```\n"

func batchTestRoster(names ...string) Roster {
	r := Roster{ExamID: examID, ServerTime: at, Sessions: []Session{}, Missing: []Student{{"9999", "Never Joined"}}}
	for i, name := range names {
		session := row()
		session.ID = fmt.Sprintf("%08d-0000-4000-8000-000000000000", i+1)
		session.Student = Student{Account: fmt.Sprintf("%04d", i+1), Name: name}
		r.Sessions = append(r.Sessions, session)
	}
	return r
}

func batchTestService(t *testing.T, serve http.HandlerFunc, picker func(FolderDialog) (string, error)) (*Service, context.CancelFunc) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		serve(w, r)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Service{opts: Options{ChooseFolder: picker}, client: server.Client(), connection: ctx, token: secret, link: 1,
		view: View{Connected: true, Loaded: true, ExamID: examID, Server: server.URL, Workspace: &WorkspaceRoster{}}}, cancel
}

type batchTestEntry struct {
	name, data string
	mode       os.FileMode
}

func batchTestZip(t *testing.T, entries ...batchTestEntry) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		out, err := z.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(out, entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func batchCheckFile(t *testing.T, directory, path, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(path)))
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", path, data, err, want)
	}
}

func batchCheckAbsent(t *testing.T, directory, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
		t.Errorf("%q must not have been written: %v", name, err)
	}
}

func TestWorkspaceBatchFilesExportsLatestAndWholeRoster(t *testing.T) {
	r := batchTestRoster("María José Núñez", "Aná Dí az", "ANA DI\u0301 AZ", "No Submission", "Bad Archive", "No Files", "Unsafe/Name")
	archive := batchTestZip(t, batchTestEntry{"src/main.go", "package demo\n", 0o755}, batchTestEntry{"assets/image.bin", "\x00\x01\xff", 0o644})
	unsafeArchive := batchTestZip(t, batchTestEntry{"nested/../../escape", "do not export", 0o644})
	empty := batchTestZip(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "ana_di_az-0003-FILES"), 0o700); err != nil {
		t.Fatal(err)
	}
	picked := 0
	s, _ := batchTestService(t, func(w http.ResponseWriter, req *http.Request) {
		switch p := req.URL.Path; {
		case p == "/admin/sessions" && req.URL.Query().Get("exam_id") == examID:
			reply(w, r)
		case strings.HasPrefix(p, "/admin/workspace/session/"):
			for i, session := range r.Sessions[:6] {
				if i == 2 || p != "/admin/workspace/session/"+session.ID {
					continue
				}
				// The later manual submission is the one Student Review downloads, not the earlier final.
				submissions := []WorkspaceSubmission{{ID: 101, Final: true}, {ID: int64(i + 1)}}
				if i == 3 {
					submissions = []WorkspaceSubmission{}
				}
				reply(w, map[string]any{"session_id": session.ID, "exam_id": examID, "submissions": submissions})
				return
			}
			t.Errorf("unexpected session URL: %s", req.URL)
			http.NotFound(w, req)
		case p == "/admin/workspace/submission/1/zip" || p == "/admin/workspace/submission/2/zip":
			w.Write(archive)
		case p == "/admin/workspace/submission/5/zip":
			w.Write(unsafeArchive)
		case p == "/admin/workspace/submission/6/zip":
			w.Write(empty)
		default:
			t.Errorf("unexpected URL: %s", req.URL)
			http.NotFound(w, req)
		}
	}, func(dialog FolderDialog) (string, error) {
		picked++
		if dialog.Title != "Download all student files" {
			t.Errorf("wrong picker: %+v", dialog)
		}
		return dir, nil
	})
	// Simulate an old UI roster (or a filter). Only the fresh exam-wide roster should drive the batch.
	s.view.Roster = batchTestRoster("Stale Record")
	out, err := s.DownloadWorkspaceBatch(examID, s.view.Server, "files", false)
	if err != nil || picked != 1 || out.Written != 2 || out.Skipped != 4 || out.Failed != 2 || len(out.Issues) != 6 || out.Folder != dir {
		t.Fatalf("result = %+v, %v, pickers: %d", out, err, picked)
	}
	for i, reason := range []string{"already exists", "No recorded submission", "unsafe path", "no files", "safe destination", "No session"} {
		if !strings.Contains(out.Issues[i].Reason, reason) || out.Issues[i].Student.Name == "" {
			t.Errorf("issue %d = %+v, expected %q", i, out.Issues[i], reason)
		}
	}
	if progress := s.WorkspaceDownloadProgress(); progress.Done != 8 || progress.Total != 8 {
		t.Errorf("progress = %+v", progress)
	}
	batchCheckFile(t, dir, "Maria_Jose_Nunez-files/src/main.go", "package demo\n")
	batchCheckFile(t, dir, "Maria_Jose_Nunez-files/assets/image.bin", "\x00\x01\xff")
	batchCheckFile(t, dir, "Ana_Di_az-0002-files/src/main.go", "package demo\n")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("expected only two new folders: %v, %v", entries, err)
	}
	entries, err = os.ReadDir(filepath.Join(dir, "ana_di_az-0003-FILES"))
	if err != nil || len(entries) != 0 {
		t.Errorf("existing folder was modified: %v, %v", entries, err)
	}
}

func batchTestReportMetadata(w http.ResponseWriter, req *http.Request, session Session, submissions []WorkspaceSubmission, runs []Grading) bool {
	path := "/admin/workspace/session/" + session.ID
	switch req.URL.Path {
	case path:
		reply(w, map[string]any{"session_id": session.ID, "exam_id": session.ExamID, "submissions": submissions, "done": map[string]bool{"parse": true}})
	case path + "/gradings":
		reply(w, WorkspaceGradings{SessionID: session.ID, Todos: []GradingTodo{{ID: "parse", Text: "Parse the input"}}, Gradings: runs})
	default:
		return false
	}
	return true
}

func batchTestCompletedMetadata(w http.ResponseWriter, req *http.Request, session Session) bool {
	return batchTestReportMetadata(w, req, session, []WorkspaceSubmission{{ID: 1}}, []Grading{{ID: 1, SubmissionID: 1, State: "done", Results: []GradingResult{{TodoID: "parse", Known: true, Passed: true, Log: "r1 ✓ · r2 ✓"}}}})
}

func batchCheckEvaluation(t *testing.T, directory, name string, snippets ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range append([]string{"# Evaluation\n", "## Grader feedback", "### Parse the input\n\nID: **parse**"}, snippets...) {
		if !strings.Contains(string(data), want) {
			t.Errorf("%s missing %q: %s", name, want, data)
		}
	}
}

func TestWorkspaceBatchEvaluationsDoNotOverwriteByDefault(t *testing.T) {
	r := batchTestRoster("Mari\u0301a Jose\u0301 Nu\u0301n\u0303ez", "Already There", "Interrupted")
	dir := t.TempDir()
	const existingName = "Already_There-report.md"
	if err := os.WriteFile(filepath.Join(dir, existingName), []byte("my grading notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	picked := 0
	s, _ := batchTestService(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/admin/sessions" && req.URL.Query().Get("exam_id") == examID {
			reply(w, r)
			return
		}
		if req.URL.Path == "/admin/workspace/session/"+r.Sessions[2].ID+"/gradings" {
			// The response promised a length but ended after the first bytes.
			w.Header().Set("Content-Length", "400")
			io.WriteString(w, `{"gradings":`)
			return
		}
		if batchTestCompletedMetadata(w, req, r.Sessions[0]) || batchTestCompletedMetadata(w, req, r.Sessions[2]) {
			return
		}
		t.Errorf("unexpected request %s", req.URL)
		http.NotFound(w, req)
	}, func(dialog FolderDialog) (string, error) {
		picked++
		if dialog.Title != "Download all evaluation reports" {
			t.Errorf("wrong picker: %+v", dialog)
		}
		return dir, nil
	})
	out, err := s.DownloadWorkspaceBatch(examID, s.view.Server, "reports", false)
	if err != nil || picked != 1 || out.Written != 1 || out.Skipped != 2 || out.Failed != 1 {
		t.Fatalf("result = %+v, %v, pickers: %d", out, err, picked)
	}
	batchCheckEvaluation(t, dir, "Maria_Jose_Nunez-report.md", "Result: **Passed**  \nPoints: **0**  \nMarked as done: **Yes**", "Output feedback:\n\n```text\nr1 ✓ · r2 ✓\n```\n")
	batchCheckFile(t, dir, existingName, "my grading notes")
	for _, name := range []string{"Interrupted-report.md", "Never_Joined-report.md"} {
		batchCheckAbsent(t, dir, name)
	}
}

func TestWorkspaceBatchOverwriteReplacesOnlyCompleteMatchingReports(t *testing.T) {
	dir := t.TempDir()
	r := batchTestRoster("Replace Me", "Keep On Failure", "A Folder", "New Report")
	const existing, incomplete, folder = "replace_me-REPORT.md", "Keep_On_Failure-report.md", "A_Folder-report.md"
	for _, name := range []string{existing, incomplete, "unrelated.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, folder), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, folder, "notes.md"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := batchTestService(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/admin/sessions" {
			reply(w, r)
			return
		}
		for i, student := range r.Sessions {
			if i == 1 && req.URL.Path == "/admin/workspace/session/"+student.ID+"/gradings" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if batchTestCompletedMetadata(w, req, student) {
				return
			}
		}
		t.Errorf("unexpected request %s", req.URL)
		http.NotFound(w, req)
	}, func(FolderDialog) (string, error) { return dir, nil })
	if _, err := s.DownloadWorkspaceBatch(examID, s.view.Server, "files", true); err == nil {
		t.Fatal("accepted overwriting student file exports")
	}
	result, err := s.DownloadWorkspaceBatch(examID, s.view.Server, "reports", true)
	if err != nil || result.Written != 2 || result.Skipped != 1 || result.Failed != 2 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	batchCheckEvaluation(t, dir, existing)
	batchCheckEvaluation(t, dir, "New_Report-report.md")
	batchCheckFile(t, dir, incomplete, "original")
	batchCheckFile(t, dir, "unrelated.md", "original")
	batchCheckFile(t, dir, folder+"/notes.md", "keep me")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 5 {
		t.Fatalf("unexpected destination entries: %v, %v", entries, err)
	}
}

func TestReplaceBatchReportRefusesSymlink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	original := filepath.Join(outside, "original.md")
	if err := os.WriteFile(original, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	const name = "Student-report.md"
	if err := os.Symlink(original, filepath.Join(dir, name)); err != nil {
		t.Skipf("cannot create test link: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := replaceBatchReport(t.Context(), root, name, []byte(testMarkdownReport)); err == nil {
		t.Fatal("accepted replacement of a symbolic link")
	}
	if link, err := root.Readlink(name); err != nil || link != original {
		t.Fatalf("link = %q, %v", link, err)
	}
	batchCheckFile(t, outside, "original.md", "original")
}

func TestWorkspaceBatchReportsRequireCompletedGradingOfLatestSubmission(t *testing.T) {
	r := batchTestRoster("Advisory", "Confirmed", "Never Graded", "Queued", "Running", "Failed", "Graded Older Save", "No Submission", "New Save During Report")
	const latest = 2
	done := Grading{ID: 10, SubmissionID: latest, State: "done"}
	runs := [][]Grading{
		{{ID: 12, SubmissionID: latest, State: "failed"}, done, {ID: 9, SubmissionID: latest, State: "done", Points: new(1.0)}},
		{{ID: 11, SubmissionID: latest, State: "done", Points: new(2.0)}, {ID: 10, SubmissionID: latest, State: "done", Confirmed: true, Points: new(3.0)}},
		{},
		{{ID: 10, SubmissionID: latest, State: "queued"}},
		{{ID: 10, SubmissionID: latest, State: "running"}},
		{{ID: 10, SubmissionID: latest, State: "failed"}},
		{{ID: 10, SubmissionID: 1, State: "done", Confirmed: true}},
		nil,
		{done},
	}
	dir := t.TempDir()
	var newSave atomic.Bool
	s, _ := batchTestService(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/admin/sessions" {
			reply(w, r)
			return
		}
		for i, student := range r.Sessions {
			submissions := []WorkspaceSubmission{{ID: 1}, {ID: latest}}
			if i == 7 {
				submissions = []WorkspaceSubmission{}
				if strings.HasSuffix(req.URL.Path, student.ID+"/gradings") {
					t.Error("grading was checked for a student without a submission")
				}
			}
			if i == 8 && newSave.Load() {
				submissions = append(submissions, WorkspaceSubmission{ID: 3})
			}
			if i == 8 && strings.HasSuffix(req.URL.Path, student.ID+"/gradings") {
				newSave.Store(true)
			}
			if batchTestReportMetadata(w, req, student, submissions, runs[i]) {
				return
			}
		}
		t.Errorf("unexpected request %s", req.URL)
		http.NotFound(w, req)
	}, func(FolderDialog) (string, error) { return dir, nil })
	result, err := s.DownloadWorkspaceBatch(examID, s.view.Server, "reports", false)
	if err != nil || result.Written != 2 || result.Skipped != 8 || result.Failed != 0 || len(result.Issues) != 8 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	for i, issue := range result.Issues[:7] {
		want := errBatchNotGraded
		if i == 5 {
			want = errBatchNoSubmission
		} else if i == 6 {
			want = errBatchNewSubmission
		}
		if issue.Reason != want.Error() {
			t.Errorf("%s: %s; want %s", issue.Student.Name, issue.Reason, want)
		}
	}
	for i, student := range r.Sessions {
		name := strings.ReplaceAll(student.Name, " ", "_") + "-report.md"
		switch i {
		case 0:
			batchCheckEvaluation(t, dir, name, "**Total score:** — (Advisory — not confirmed)")
		case 1:
			batchCheckEvaluation(t, dir, name, "**Total score:** 3 (Confirmed)")
		default:
			batchCheckAbsent(t, dir, name)
		}
	}
}

func TestWorkspaceBatchPinsExamAndRejectsDuplicates(t *testing.T) {
	dir := t.TempDir()
	entered, release := make(chan struct{}), make(chan struct{})
	r := batchTestRoster("One Student")
	s, _ := batchTestService(t, func(w http.ResponseWriter, req *http.Request) {
		if batchTestCompletedMetadata(w, req, r.Sessions[0]) {
			return
		}
		switch {
		case req.URL.Path == "/admin/sessions" && req.URL.Query().Get("exam_id") == examID:
			reply(w, r)
		default:
			t.Errorf("unexpected URL: %s", req.URL)
			http.NotFound(w, req)
		}
	}, func(FolderDialog) (string, error) { close(entered); <-release; return dir, nil })
	server := s.view.Server
	finished := make(chan error, 1)
	go func() {
		result, err := s.DownloadWorkspaceBatch(examID, server, "reports", false)
		if err == nil && result.Written != 1 {
			err = fmt.Errorf("unexpected result %+v", result)
		}
		finished <- err
	}()
	<-entered
	_, duplicateErr := s.DownloadWorkspaceBatch(examID, server, "files", false)
	s.mu.Lock()
	s.view.ExamID = otherExam
	s.mu.Unlock()
	close(release)
	if duplicateErr == nil {
		t.Error("duplicate batch was accepted")
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	batchCheckEvaluation(t, dir, "One_Student-report.md")
}

func TestWorkspaceBatchCancelOrSwitchConnection(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			dir := t.TempDir()
			var requests atomic.Int64
			var changeLink func()
			s, cancel := batchTestService(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.NotFound(w, r) }, func(FolderDialog) (string, error) {
				if cancelled {
					return "", nil
				}
				changeLink()
				return dir, nil
			})
			server := s.view.Server
			changeLink = func() {
				cancel()
				s.mu.Lock()
				s.link++
				s.view.Server = "http://127.0.0.1:1"
				s.mu.Unlock()
			}
			res, err := s.DownloadWorkspaceBatch(examID, server, "reports", false)
			if res.Cancelled != cancelled || (err == nil) != cancelled || res.Written != 0 || requests.Load() != 0 {
				t.Fatalf("result = %+v, %v, requests = %d", res, err, requests.Load())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Errorf("unexpected output: %v, %v", entries, err)
			}
		})
	}
}

func TestWorkspaceBatchConnectionChangeKeepsOnlyCompletedReports(t *testing.T) {
	dir := t.TempDir()
	r := batchTestRoster("Completed", "Interrupted", "Not Started")
	disconnect := make(chan context.CancelFunc, 1)
	s, cancel := batchTestService(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/admin/workspace/session/"+r.Sessions[1].ID+"/gradings" &&
			(batchTestCompletedMetadata(w, req, r.Sessions[0]) || batchTestCompletedMetadata(w, req, r.Sessions[1])) {
			return
		}
		switch req.URL.Path {
		case "/admin/sessions":
			reply(w, r)
		case "/admin/workspace/session/" + r.Sessions[1].ID + "/gradings":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"gradings":`)
			w.(http.Flusher).Flush()
			(<-disconnect)()
		default:
			t.Errorf("unexpected URL after disconnect: %s", req.URL)
			http.NotFound(w, req)
		}
	}, func(FolderDialog) (string, error) { return dir, nil })
	disconnect <- cancel
	result, err := s.DownloadWorkspaceBatch(examID, s.view.Server, "reports", false)
	if err != nil || result.Written != 1 || result.Failed != 2 || result.Skipped != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	for _, issue := range result.Issues[:2] {
		if issue.Status != "failed" || issue.Reason != errBatchConnection.Error() {
			t.Errorf("unexpected issue: %+v", issue)
		}
	}
	batchCheckEvaluation(t, dir, "Completed-report.md")
	batchCheckAbsent(t, dir, "Interrupted-report.md")
	batchCheckAbsent(t, dir, "Not_Started-report.md")
}

func TestWorkspaceArchiveRefusesUnsafeEntriesAndCleansIncompleteStudent(t *testing.T) {
	tests := []struct {
		name    string
		entries []batchTestEntry
		corrupt bool
	}{
		{"backslash", []batchTestEntry{{`dir\file`, "text", 0o644}}, false},
		{"absolute", []batchTestEntry{{"/root/file", "text", 0o644}}, false},
		{"traversal", []batchTestEntry{{"dir/../file", "text", 0o644}}, false},
		{"reserved", []batchTestEntry{{"dir/COM¹.txt", "text", 0o644}}, false},
		{"symlink", []batchTestEntry{{"dir/file", "../out", os.ModeSymlink}}, false},
		{"case folded directory", []batchTestEntry{{"DIR/one", "text", 0o644}, {"Dir/two", "text", 0o644}}, false},
		{"file directory conflict", []batchTestEntry{{"dir/file", "text", 0o644}, {"dir", "text", 0o644}}, false},
		{"duplicate file", []batchTestEntry{{"src/main", "text", 0o644}, {"src/main", "text", 0o644}}, false},
		{"broken second file", []batchTestEntry{{"src/first", "text", 0o644}, {"src/second", "text", 0o644}}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			data := batchTestZip(t, test.entries...)
			archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil && err != zip.ErrInsecurePath {
				t.Fatal(err)
			}
			if test.corrupt {
				archive.File[1].CRC32++
			}
			if err := exportWorkspaceArchive(t.Context(), root, "Student-files", archive.File); err == nil {
				t.Fatal("unsafe or unfinished archive accepted")
			}
			batchCheckAbsent(t, dir, "Student-files")
		})
	}
}

func TestWorkspaceArchiveDoesNotFollowExistingDestinationLink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "Student-files")); err != nil {
		t.Skipf("cannot create test link: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data := batchTestZip(t, batchTestEntry{"src/main", "text", 0o644})
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := exportWorkspaceArchive(t.Context(), root, "Student-files", archive.File); err != errBatchExists {
		t.Fatalf("expected destination collision, got %v", err)
	}
	batchCheckAbsent(t, outside, "src")
}
