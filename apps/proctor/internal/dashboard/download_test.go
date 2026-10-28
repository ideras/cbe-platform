package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/shared/examdoc"
)

const (
	finishedSession  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	workingSession   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	finishingSession = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

// downloadServer is a fake exam server for the download routes: one exam with a stored source, its sessions and their
// submissions.
type downloadServer struct {
	mu                sync.Mutex
	detail            examDetail
	roster            Roster
	subs              map[string]submission
	failing           map[string]bool
	holdSubmission    chan struct{}
	submissionStarted chan struct{}
}

func (d *downloadServer) handler(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	detail, roster, subs, failing := d.detail, d.roster, d.subs, d.failing
	hold, started := d.holdSubmission, d.submissionStarted
	d.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/admin/exams":
		reply(w, catalog())
	case r.Method == "GET" && r.URL.Path == "/admin/classes":
		reply(w, classList())
	case r.Method == "GET" && r.URL.Path == "/admin/sessions":
		reply(w, roster)
	case r.Method == "GET" && r.URL.Path == "/admin/exam/"+examID:
		reply(w, detail)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/admin/session/") && strings.HasSuffix(r.URL.Path, "/submission"):
		if hold != nil {
			if started != nil {
				close(started)
			}
			<-hold
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/session/"), "/submission")
		if failing[id] {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		sub, ok := subs[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(w, sub)
	default:
		http.NotFound(w, r)
	}
}

type downloadFixture struct {
	*Service
	server       *downloadServer
	chosenFolder string
	dialogs      int
	opened       []string
	openErr      error
}

func newDownloadFixture(t *testing.T) *downloadFixture {
	t.Helper()
	source := "---\ntitle: Exam one\nduration: 10m\npoints: 10\n---\n\n## Exercise {points=10}\n\nValue: {{a}}\n\n```answer {#code lang=mips}\n```\n"
	exam, err := examdoc.Compile([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	now := at
	submitted := now
	finished := Session{ID: finishedSession, ExamID: examID, Student: Student{"0012", "José Ñúñez Martínez"}, Status: "submitted", PersistedStatus: "submitted",
		Final: &FinalSummary{VersionID: 1, Sequence: 14, SavedAt: now, SubmittedAt: submitted, Reason: "explicit_submit"}}
	working := Session{ID: workingSession, ExamID: examID, Student: Student{"0013", "Student Working"}, Status: "active", PersistedStatus: "active"}
	finishing := Session{ID: finishingSession, ExamID: examID, Student: Student{"0014", "Student Finishing"}, Status: "grace", PersistedStatus: "grace", FinalizationPending: true}
	fake := &downloadServer{
		detail: examDetail{ExamID: examID, Title: "Exam one", ClassID: classID, ClassName: "Mathematics 10A", Navigation: "free", Content: exam.Content, SourceMarkdown: &source},
		roster: Roster{ExamID: examID, ServerTime: now, Sessions: []Session{finished, working, finishing}, Missing: []Student{{"0003", "Never There"}}},
		subs: map[string]submission{finishedSession: {
			SessionID: finishedSession, ExamID: examID, AccountNumber: "0012", StudentName: "José Ñúñez Martínez", Status: "submitted", Final: true, Reason: "explicit_submit", SubmittedAt: &submitted,
			Version: &submissionVersion{VersionID: 1, Sequence: 14, SavedAt: now, Answers: map[string]string{"1.a": "16", "1.code": "mult $s0, $s1"}},
		}},
		failing: map[string]bool{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "raw credential rejected "+secret, 401)
			return
		}
		fake.handler(w, r)
	}))
	t.Cleanup(server.Close)
	f := &downloadFixture{server: fake}
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.ExamID = server.URL, secret, examID
	opts.Poll, opts.Timeout = 15*time.Millisecond, time.Second
	opts.ChooseFolder = func(FolderDialog) (string, error) {
		f.dialogs++
		return f.chosenFolder, nil
	}
	opts.OpenFolder = func(path string) error {
		f.opened = append(f.opened, path)
		return f.openErr
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	f.Service = s
	if _, err := s.Connect(server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().CatalogLoaded })
	return f
}

func (f *downloadFixture) setRoster(roster Roster) {
	f.server.mu.Lock()
	f.server.roster = roster
	f.server.mu.Unlock()
}

func (f *downloadFixture) failSubmission(id string) {
	f.server.mu.Lock()
	f.server.failing[id] = true
	f.server.mu.Unlock()
}

func TestPrepareDownloadCountsEveryone(t *testing.T) {
	f := newDownloadFixture(t)
	preview, err := f.PrepareDownload(examID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExamID != examID || preview.Title != "Exam one" || preview.ClassName != "Mathematics 10A" || preview.FolderName != "Mathematics_10A-Exam_one" {
		t.Fatalf("preview = %+v", preview)
	}
	if preview.Finished != 1 || preview.Working != 1 || preview.Finishing != 1 || preview.NeverLoggedIn != 1 || !preview.SourceAvailable {
		t.Fatalf("counts = %+v", preview)
	}
}

func (f *downloadFixture) choose(t *testing.T) string {
	t.Helper()
	path, err := f.ChooseDownloadFolder()
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("the folder dialog was cancelled")
	}
	return path
}

const testFolderName = "Mathematics_10A-Exam_one"

func TestDownloadWritesPrivateFilesAndTheIndex(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	parent := f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(parent, testFolderName)
	if result.Folder != folder || result.NeedsConfirmation || len(result.Files) != 1 || result.Files[0] != "Jose_Nunez_Martinez.md" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Skipped) != 2 || result.Skipped[0].Status != "working" || result.Skipped[1].Status != "finishing" || len(result.Failed) != 0 {
		t.Fatalf("skipped = %+v, failed = %+v", result.Skipped, result.Failed)
	}
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("folder mode = %v, %v", info.Mode().Perm(), err)
	}
	path := filepath.Join(folder, "Jose_Nunez_Martinez.md")
	info, err = os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("file mode = %v, %v", info.Mode().Perm(), err)
	}
	content, _ := os.ReadFile(path)
	for _, want := range []string{
		`student: "José Ñúñez Martínez"`,
		`account_number: "0012"`,
		`status: "submitted by the student"`,
		"answer_version: 14",
		"times_locked: 0",
		"Value: `16`",
		"```mips title=\"1.code\"\nmult $s0, $s1\n```",
		"## Exercise {points=10}",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("the student's file does not contain %q:\n%s", want, content)
		}
	}
	index, err := os.Stat(filepath.Join(folder, indexFileName))
	if err != nil || (runtime.GOOS != "windows" && index.Mode().Perm() != 0o600) {
		t.Fatalf("index mode = %v, %v", index.Mode().Perm(), err)
	}
	indexContent, _ := os.ReadFile(filepath.Join(folder, indexFileName))
	for _, want := range []string{
		"| Student | Account | Status | Submitted | Locks | File |",
		"| José Ñúñez Martínez | 0012 | submitted |",
		"[Jose_Nunez_Martinez.md](Jose_Nunez_Martinez.md)",
		"| Student Working | 0013 | still working |",
		"| Student Finishing | 0014 | still finishing |",
		"| Never There | 0003 | never logged in |",
	} {
		if !strings.Contains(string(indexContent), want) {
			t.Errorf("_index.md does not contain %q:\n%s", want, indexContent)
		}
	}
	if err := f.OpenDownloadFolder(); err != nil || len(f.opened) != 1 || f.opened[0] != folder {
		t.Fatalf("open folder = %v, %v", f.opened, err)
	}
	progress := f.DownloadProgress()
	if progress.Running || progress.Done != 1 || progress.Total != 1 || progress.Current != "" {
		t.Fatalf("progress after the download = %+v", progress)
	}
}

func TestDownloadUsesTheEditedFolderName(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	parent := f.choose(t)
	result, err := f.DownloadSubmissions(examID, "Notas del Parcial 1", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Folder != filepath.Join(parent, "Notas del Parcial 1") {
		t.Fatalf("folder = %q", result.Folder)
	}
	if _, err := os.Stat(result.Folder); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRejectsAnUnusableFolderName(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a:b", "CON", "LPT1.txt", "trailing.", " "} {
		if _, err := f.DownloadSubmissions(examID, name, false, true); err == nil {
			t.Errorf("folder name %q was accepted", name)
		}
	}
	if f.Snapshot().Busy {
		t.Fatal("a rejected folder name left an action running")
	}
}

func TestDownloadRefusesToReplaceFilesThenReplacesOnlyItsOwn(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	parent := f.choose(t)
	folder := filepath.Join(parent, testFolderName)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	studentFile := filepath.Join(folder, "Jose_Nunez_Martinez.md")
	if err := os.WriteFile(studentFile, []byte("my grading notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(folder, "my-notes.txt")
	if err := os.WriteFile(own, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil || !refused.NeedsConfirmation || refused.ReplaceCount != 1 || len(refused.Files) != 0 {
		t.Fatalf("refusal = %+v, %v", refused, err)
	}
	if content, _ := os.ReadFile(studentFile); string(content) != "my grading notes" {
		t.Fatal("the refusal overwrote the earlier download")
	}
	if f.dialogs != 1 {
		t.Fatalf("folder dialogs = %d", f.dialogs)
	}
	result, err := f.DownloadSubmissions(examID, testFolderName, true, true)
	if err != nil || result.NeedsConfirmation || len(result.Files) != 1 {
		t.Fatalf("replace = %+v, %v", result, err)
	}
	if f.dialogs != 1 {
		t.Fatalf("the replace question asked for the folder again")
	}
	if content, _ := os.ReadFile(studentFile); string(content) == "my grading notes" {
		t.Fatal("the replace did not write the new file")
	}
	if content, _ := os.ReadFile(own); string(content) != "mine" {
		t.Fatal("the replace deleted a file it did not write")
	}
}

func TestDownloadFallsBackWithoutAStoredSource(t *testing.T) {
	f := newDownloadFixture(t)
	f.server.mu.Lock()
	f.server.detail.SourceMarkdown = nil
	f.server.mu.Unlock()
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil || len(result.Files) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	content, err := os.ReadFile(filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Exercise", "**1.a:** `16`", "**1.code:**", "```mips title=\"1.code\""} {
		if !strings.Contains(string(content), want) {
			t.Errorf("the fallback file does not contain %q:\n%s", want, content)
		}
	}
}

func TestDownloadReportsAFailedSubmission(t *testing.T) {
	f := newDownloadFixture(t)
	f.failSubmission(finishedSession)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 || len(result.Failed) != 1 || result.Failed[0].StudentName != "José Ñúñez Martínez" {
		t.Fatalf("result = %+v", result)
	}
	// Nothing was written for a student, so there is no folder of submissions to open.
	if err := f.OpenDownloadFolder(); err == nil || len(f.opened) != 0 {
		t.Fatalf("a download that wrote no student's file made its folder openable: %v, %v", f.opened, err)
	}
}

// addFinishedSession gives the fake server a second finished student, so one download can write some files and fail others.
func (f *downloadFixture) addFinishedSession(id, account, name string) {
	f.server.mu.Lock()
	defer f.server.mu.Unlock()
	submitted := at
	session := Session{ID: id, ExamID: examID, Student: Student{account, name}, Status: "submitted", PersistedStatus: "submitted",
		Final: &FinalSummary{VersionID: 2, Sequence: 3, SavedAt: at, SubmittedAt: submitted, Reason: "grace_expired"}}
	f.server.roster.Sessions = append(f.server.roster.Sessions, session)
	f.server.subs[id] = submission{SessionID: id, ExamID: examID, AccountNumber: account, StudentName: name, Status: "submitted", Final: true, Reason: "grace_expired", SubmittedAt: &submitted,
		Version: &submissionVersion{VersionID: 2, Sequence: 3, SavedAt: at, Answers: map[string]string{"1.a": "7"}}}
}

func TestAPartialDownloadRemembersItsFolder(t *testing.T) {
	f := newDownloadFixture(t)
	const second = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	f.addFinishedSession(second, "0015", "Second Student")
	f.failSubmission(second)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || len(result.Failed) != 1 || result.Failed[0].AccountNumber != "0015" {
		t.Fatalf("result = %+v", result)
	}
	if err := f.OpenDownloadFolder(); err != nil || len(f.opened) != 1 || f.opened[0] != result.Folder {
		t.Fatalf("open folder = %v, %v", f.opened, err)
	}
}

func TestAFailedOrEmptyDownloadKeepsTheEarlierFolder(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	first, err := f.DownloadSubmissions(examID, "First", false, true)
	if err != nil || len(first.Files) != 1 {
		t.Fatalf("first download = %+v, %v", first, err)
	}
	// Every fetch fails, so the second download writes the index and no student's file.
	f.failSubmission(finishedSession)
	failed, err := f.DownloadSubmissions(examID, "Second", false, true)
	if err != nil || len(failed.Files) != 0 || len(failed.Failed) != 1 {
		t.Fatalf("failed download = %+v, %v", failed, err)
	}
	// Nobody has finished, so the third has nothing to write either.
	f.setRoster(Roster{ExamID: examID, ServerTime: at, Sessions: []Session{{ID: workingSession, ExamID: examID, Student: Student{"0013", "Student Working"}, Status: "active", PersistedStatus: "active"}}})
	empty, err := f.DownloadSubmissions(examID, "Third", false, true)
	if err != nil || len(empty.Files) != 0 || len(empty.Skipped) != 1 {
		t.Fatalf("empty download = %+v, %v", empty, err)
	}
	if err := f.OpenDownloadFolder(); err != nil || len(f.opened) != 1 || f.opened[0] != first.Folder {
		t.Fatalf("open folder = %v (the first download's folder is %s), %v", f.opened, first.Folder, err)
	}
}

func TestTheIndexIsSortedByNameIgnoringAccentsAndCase(t *testing.T) {
	roster := Roster{ExamID: examID, Sessions: []Session{
		{ID: workingSession, Student: Student{"2", "Zamora"}, Status: "active"},
		{ID: finishingSession, Student: Student{"3", "Álvarez"}, Status: "grace", FinalizationPending: true},
	}, Missing: []Student{{"4", "beltrán"}, {"5", "Alonso"}}}
	index, err := renderIndex(examDetail{Title: "Exam", ClassName: "Class"}, roster, map[string]string{}, at)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, name := range []string{"| Alonso |", "| Álvarez |", "| beltrán |", "| Zamora |"} {
		position := strings.Index(string(index), name)
		if position <= last {
			t.Fatalf("%q is out of order in:\n%s", name, index)
		}
		last = position
	}
	if !strings.Contains(string(index), "| Álvarez | 3 | still finishing |") {
		t.Fatalf("a student whose session is being finalized is not marked still finishing:\n%s", index)
	}
}

func TestDownloadProgressWhileRunning(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	hold, started := make(chan struct{}), make(chan struct{})
	f.server.mu.Lock()
	f.server.holdSubmission, f.server.submissionStarted = hold, started
	f.server.mu.Unlock()
	type outcome struct {
		result DownloadResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
		done <- outcome{result, err}
	}()
	<-started
	wait(t, func() bool {
		progress := f.DownloadProgress()
		return progress.Running && progress.Total == 1 && progress.Current == "José Ñúñez Martínez"
	})
	close(hold)
	finished := <-done
	if finished.err != nil || len(finished.result.Files) != 1 {
		t.Fatalf("download = %+v, %v", finished.result, finished.err)
	}
	progress := f.DownloadProgress()
	if progress.Running || progress.Done != 1 || progress.Total != 1 {
		t.Fatalf("progress after the download = %+v", progress)
	}
}

func TestOpenDownloadFolderBeforeAnyDownloadAndAfterDeletion(t *testing.T) {
	f := newDownloadFixture(t)
	if err := f.OpenDownloadFolder(); err == nil || len(f.opened) != 0 {
		t.Fatal("opening before a download was allowed")
	}
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.OpenDownloadFolder(); err != nil || len(f.opened) != 1 || f.opened[0] != result.Folder {
		t.Fatalf("open folder = %v, %v", f.opened, err)
	}
	if err := os.RemoveAll(result.Folder); err != nil {
		t.Fatal(err)
	}
	if err := f.OpenDownloadFolder(); err == nil || len(f.opened) != 1 {
		t.Fatal("a deleted download folder was opened")
	}
}

func TestOpenDownloadFolderNeedsNoServerConnection(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil {
		t.Fatal(err)
	}
	f.Disconnect()
	if err := f.OpenDownloadFolder(); err != nil || len(f.opened) != 1 || f.opened[0] != result.Folder {
		t.Fatalf("open after disconnect = %v, %v", f.opened, err)
	}
}

func TestDownloadRejectsBadInput(t *testing.T) {
	f := newDownloadFixture(t)
	if _, err := f.DownloadSubmissions(examID, testFolderName, false, false); err == nil {
		t.Fatal("a download without confirmation was accepted")
	}
	if _, err := f.DownloadSubmissions("not-a-uuid", testFolderName, false, true); err == nil {
		t.Fatal("an invalid exam ID was accepted")
	}
	if _, err := f.DownloadSubmissions("99999999-9999-4999-8999-999999999999", testFolderName, false, true); err == nil {
		t.Fatal("an exam not on the server's list was accepted")
	}
	if _, err := f.DownloadSubmissions(examID, testFolderName, false, true); err == nil {
		t.Fatal("a download without a chosen folder was accepted")
	}
	if _, err := f.PrepareDownload("not-a-uuid"); err == nil {
		t.Fatal("preparing an invalid exam ID was accepted")
	}
}

// The locks of an exam are in the downloaded files: how many times it was locked, and what the proctor said and gave back.
func TestTheDownloadRecordsTheLocksOfTheExam(t *testing.T) {
	f := newDownloadFixture(t)
	roster := f.server.roster
	roster.Sessions[0].Locks = []LockRecord{
		{Type: "locked", At: at, Reason: "away"},
		{Type: "unlocked", At: at, OperatorReason: "the student came back", LockedMillis: 420000},
		{Type: "locked", At: at, Reason: "no_contact"},
		{Type: "unlocked", At: at, OperatorReason: "network outage", LockedMillis: 450000, ExtendedMillis: 450000},
	}
	f.setRoster(roster)
	f.chosenFolder = t.TempDir()
	parent := f.choose(t)
	if _, err := f.DownloadSubmissions(examID, testFolderName, false, true); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(parent, testFolderName)
	content, err := os.ReadFile(filepath.Join(folder, "Jose_Nunez_Martinez.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"times_locked: 2\nunlocks:\n" +
			"  - reason: \"the student came back\"\n    minutes_given_back: 0\n" +
			"  - reason: \"network outage\"\n    minutes_given_back: 7.5\n",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("the student's file does not contain %q:\n%s", want, content)
		}
	}
	index, err := os.ReadFile(filepath.Join(folder, indexFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "2 (away, no contact)") {
		t.Errorf("_index.md does not show the locks:\n%s", index)
	}
}
