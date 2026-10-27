package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded is one request that reached the fake server's administrator routes.
type recorded struct {
	Method, Target, ContentType, Authorization string
	Body                                       []byte
}

// adminServer is a fake exam server for the exam, roster and publishing routes. Handlers set what it answers.
type adminServer struct {
	mu          sync.Mutex
	requests    []recorded
	current     string
	classRoster *ClassRoster
	reply       func(w http.ResponseWriter, r *http.Request, body []byte) bool
}

func (a *adminServer) seen(method, path string) []recorded {
	a.mu.Lock()
	defer a.mu.Unlock()
	var found []recorded
	for _, r := range a.requests {
		if r.Method == method && strings.HasPrefix(r.Target, path) {
			found = append(found, r)
		}
	}
	return found
}

// setClassRoster makes the server answer every class roster request with roster (nil restores the normal answer).
func (a *adminServer) setClassRoster(roster *ClassRoster) {
	a.mu.Lock()
	a.classRoster = roster
	a.mu.Unlock()
}

func (a *adminServer) setCurrent(id string) {
	a.mu.Lock()
	a.current = id
	a.mu.Unlock()
}

func (a *adminServer) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.Method != "GET" {
		a.mu.Lock()
		a.requests = append(a.requests, recorded{r.Method, r.URL.RequestURI(), r.Header.Get("Content-Type"), r.Header.Get("Authorization"), body})
		a.mu.Unlock()
	}
	a.mu.Lock()
	current, override := a.current, a.classRoster
	a.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/admin/exams":
		c := catalog()
		c.CurrentExamID, c.CurrentClassID, c.CurrentClassName = current, "", ""
		for i := range c.Exams {
			c.Exams[i].Published = c.Exams[i].ID == current
			if c.Exams[i].Published { // the class admitted is the class of the published exam
				c.CurrentClassID, c.CurrentClassName = c.Exams[i].ClassID, c.Exams[i].ClassName
			}
		}
		reply(w, c)
	case r.Method == "GET" && r.URL.Path == "/admin/classes":
		reply(w, classList())
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/admin/class/") && strings.HasSuffix(r.URL.Path, "/students"):
		if override != nil {
			reply(w, *override)
			return
		}
		reply(w, classRoster(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/class/"), "/students")))
	case r.Method == "GET" && r.URL.Path == "/admin/sessions":
		rr := roster()
		rr.ExamID = r.URL.Query().Get("exam_id")
		rr.Sessions[0].ExamID = rr.ExamID
		reply(w, rr)
	case a.reply != nil && a.reply(w, r, body):
	default:
		http.NotFound(w, r)
	}
}

func problemsReply(w http.ResponseWriter, status int, message string, problems ...string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	reply(w, map[string]any{"error": message, "problems": problems})
}

type publishFixture struct {
	*Service
	server *adminServer
	// chosen is the path the fake file dialog returns; empty means the operator cancelled.
	chosen string
	opened int
}

func newPublishFixture(t *testing.T, examSelected string) *publishFixture {
	t.Helper()
	fake := &adminServer{current: examID}
	server := httptestServer(t, fake)
	f := &publishFixture{server: fake}
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.ExamID = server.URL, secret, examSelected
	opts.Poll, opts.Timeout = 15*time.Millisecond, time.Second
	opts.Choose = func(FileDialog) (string, error) {
		f.opened++
		if f.chosen == "bad dialog" {
			return "", errors.New("gtk failure with " + secret)
		}
		return f.chosen, nil
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

func writeTemp(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// examRoute is where an exam is imported into the first class.
const examRoute = "/admin/class/" + classID + "/exam"

const examDocument = "---\ntitle: Midterm\nduration: 10m\nnavigation: forward_only\n---\nRead carefully.\n\n## One {points=4}\n\nAnswer: {{x}}\n\n## Two {points=6}\n\n```answer {#code lang=c}\nint main;\n```\n"

func checkedExam(w http.ResponseWriter) {
	reply(w, map[string]any{"title": "Midterm", "exercise_count": 2, "slot_count": 2, "navigation": "forward_only", "duration_seconds": 600, "grace_seconds": 30, "min_volume_percent": 80, "opens_at": nil, "closes_at": nil})
}

// sentExam decodes an exam document the dashboard sent to the server.
func sentExam(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestConnectWithoutAnExamMonitorsThePublishedOneUntilTheOperatorPicksAnother(t *testing.T) {
	f := newPublishFixture(t, "")
	wait(t, func() bool { v := f.Snapshot(); return v.Loaded && v.ExamID == examID && !v.Stale })
	v := f.Snapshot()
	if v.Catalog.CurrentExamID != examID || v.Catalog.CurrentClassID != classID || len(v.Catalog.Exams) != 3 || !v.ClassesLoaded || len(v.Classes) != 2 || v.Classes[1].StudentCount != 5 || v.Roster.ExamID != examID {
		t.Fatalf("view = %+v", v)
	}
	if _, err := f.SelectExam(otherExam); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		v := f.Snapshot()
		return v.Loaded && v.ExamID == otherExam && v.Roster.ExamID == otherExam
	})
	f.Refresh()
	time.Sleep(60 * time.Millisecond)
	if f.Snapshot().ExamID != otherExam {
		t.Fatal("the published exam took the selection back from the operator")
	}
	// An exam nobody has taken and that admits nobody has nothing to monitor.
	if _, err := f.SelectExam(unusedExam); err == nil || !strings.Contains(err.Error(), "Publish it first") || f.Snapshot().ExamID != otherExam {
		t.Fatalf("monitoring an exam nobody has taken = %v", err)
	}
	if _, err := f.SelectExam("not-an-id"); err == nil {
		t.Fatal("invalid exam ID accepted")
	}
}

func TestNothingPublishedLeavesNothingSelected(t *testing.T) {
	fake := &adminServer{}
	server := httptestServer(t, fake)
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll = server.URL, secret, 15*time.Millisecond
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err = s.Connect(server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.CatalogLoaded && !v.Stale && !v.Loading })
	v := s.Snapshot()
	if v.ExamID != "" || v.Loaded || v.LastSuccess == nil || v.Catalog.CurrentExamID != "" {
		t.Fatalf("view = %+v", v)
	}
}

func TestSelectedExamThatTheServerDoesNotStoreIsReported(t *testing.T) {
	f := newPublishFixture(t, "44444444-4444-4444-8444-444444444444")
	wait(t, func() bool { return strings.Contains(f.Snapshot().Error, "not stored on this server") })
	if v := f.Snapshot(); !v.Stale || v.Loaded {
		t.Fatalf("view = %+v", v)
	}
}

func TestChosenExamIsCheckedByTheServerAndImportedExactlyOnce(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		switch {
		case r.Method == "POST" && r.URL.Path == examRoute && r.URL.Query().Get("dry_run") == "true":
			checkedExam(w)
		case r.Method == "POST" && r.URL.Path == examRoute:
			w.WriteHeader(http.StatusCreated)
			reply(w, map[string]string{"exam_id": otherExam})
		default:
			return false
		}
		return true
	}
	f.chosen = writeTemp(t, "midterm.md", append([]byte("\xef\xbb\xbf"), examDocument...))

	preview, err := f.ChooseExam(classID)
	if err != nil || !preview.Valid || preview.File != "midterm.md" || preview.Title != "Midterm" || preview.ExerciseCount != 2 || preview.SlotCount != 2 ||
		preview.Points == nil || *preview.Points != 10 || preview.Navigation != "forward_only" || preview.Content.Instructions != "<p>Read carefully.</p>" ||
		preview.DurationSeconds != 600 || preview.GraceSeconds != 30 || preview.MinVolumePercent != 80 || len(preview.Problems) != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	dry := f.server.seen("POST", examRoute+"?dry_run=true")
	if len(dry) != 1 || dry[0].ContentType != "application/json" || dry[0].Authorization != "Bearer "+secret {
		t.Fatalf("dry run = %+v", dry)
	}
	checked := sentExam(t, dry[0].Body)
	if _, hasPIN := checked["pin"]; hasPIN || checked["title"] != "Midterm" || checked["duration_seconds"] != 600.0 || checked["navigation"] != "forward_only" {
		t.Fatalf("checked document = %s", dry[0].Body)
	}
	if len(f.server.seen("POST", examRoute)) != 1 {
		t.Fatal("previewing stored an exam")
	}

	if _, err := f.ImportExam(classID, " ", true); err == nil || len(f.server.seen("POST", examRoute)) != 1 {
		t.Fatalf("imported without a PIN: %v", err)
	}
	// The preview belongs to the class it was made for.
	if _, err := f.ImportExam(otherClass, "announced-pin", false); err == nil || !strings.Contains(err.Error(), "for this class") || len(f.server.seen("POST", "/admin/class/"+otherClass)) != 0 {
		t.Fatalf("imported a preview into another class: %v", err)
	}
	if _, err := f.ImportExam("nope", "announced-pin", false); err == nil {
		t.Fatal("imported into an invalid class")
	}
	result, err := f.ImportExam(classID, "announced-pin", false)
	if err != nil || result.ExamID != otherExam {
		t.Fatalf("import = %+v, %v", result, err)
	}
	stored := f.server.seen("POST", examRoute)
	imported := sentExam(t, stored[1].Body)
	if len(stored) != 2 || imported["pin"] != "announced-pin" || imported["navigation"] != "free" || !reflect.DeepEqual(imported["content"], checked["content"]) {
		t.Fatalf("the imported exam differs from the previewed one: %s", stored[1].Body)
	}
	if _, err := f.ImportExam(classID, "announced-pin", false); err == nil || len(f.server.seen("POST", examRoute)) != 2 {
		t.Fatalf("the same preview was imported twice: %v", err)
	}
}

func TestExamFileProblemsAreShownBeforeAnythingIsSent(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.chosen = writeTemp(t, "broken.md", []byte("---\ntitle: x\npin: 1234\n---\n## One\n[a link](http://example.com) {{x}}\n"))
	preview, err := f.ChooseExam(classID)
	if err != nil || preview.Valid || len(preview.Problems) != 3 || !strings.Contains(preview.Problems[0], "line 3: the PIN does not belong in the exam file") ||
		!strings.Contains(preview.Problems[1], "must set a duration") || !strings.Contains(preview.Problems[2], "line 6: links are not allowed") {
		t.Fatalf("compile problems = %+v, %v", preview, err)
	}
	if len(f.server.seen("POST", "/admin")) != 0 {
		t.Fatal("a file that does not compile was sent to the server")
	}
	if _, err := f.ImportExam(classID, "p", false); err == nil {
		t.Fatal("an invalid file can be imported")
	}

	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		problemsReply(w, 400, "invalid request", "duration_seconds plus grace_seconds is too large", "exercise 2 (2): id is already used by another exercise or item")
		return true
	}
	f.chosen = writeTemp(t, "invalid.md", []byte(examDocument))
	preview, err = f.ChooseExam(classID)
	if err != nil || preview.Valid || len(preview.Problems) != 2 || !strings.Contains(preview.Problems[1], "already used") {
		t.Fatalf("server problems = %+v, %v", preview, err)
	}
	if _, err := f.ImportExam(classID, "p", false); err == nil {
		t.Fatal("an exam the server rejected can be imported")
	}

	for name, content := range map[string][]byte{"latin1.md": []byte("---\ntitle: caf\xe9\n---\n"), "huge.md": bytes.Repeat([]byte(" "), maxExamFile+1)} {
		f.chosen = writeTemp(t, name, content)
		if _, err := f.ChooseExam(classID); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	f.chosen = filepath.Dir(f.chosen)
	if _, err := f.ChooseExam(classID); err == nil {
		t.Error("a directory was accepted as an exam file")
	}
	if _, err := f.ChooseExam("nope"); err == nil {
		t.Error("an exam was chosen for an invalid class")
	}
}

func TestFileDialogCancellationFailureAndConcurrency(t *testing.T) {
	f := newPublishFixture(t, examID)
	if preview, err := f.ChooseExam(classID); err != nil || !preview.Cancelled || preview.Valid {
		t.Fatalf("cancelled dialog = %+v, %v", preview, err)
	}
	if preview, err := f.ChooseClassFile(); err != nil || !preview.Cancelled {
		t.Fatalf("cancelled class dialog = %+v, %v", preview, err)
	}
	f.chosen = "bad dialog"
	if _, err := f.ChooseExam(classID); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("dialog failure = %v", err)
	}

	entered, release := make(chan struct{}), make(chan struct{})
	f.Service.opts.Choose = func(FileDialog) (string, error) { entered <- struct{}{}; <-release; return "", nil }
	done := make(chan error, 1)
	go func() { _, err := f.ChooseExam(classID); done <- err }()
	<-entered
	if _, err := f.ChooseClassFile(); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("second dialog = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.Service.opts.Choose = nil
	if _, err := f.ChooseExam(classID); err == nil {
		t.Fatal("a build without a file dialog offered one")
	}
}

func TestUnclearImportFailureTellsTheOperatorToLookBeforeRetrying(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Query().Get("dry_run") == "true" {
			checkedExam(w)
		} else {
			http.Error(w, "boom "+secret, 500)
		}
		return true
	}
	f.chosen = writeTemp(t, "midterm.md", []byte(examDocument))
	if preview, err := f.ChooseExam(classID); err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	_, err := f.ImportExam(classID, "pin", true)
	if err == nil || !strings.Contains(err.Error(), "Look at the exam list") || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
	if _, err := f.ImportExam(classID, "pin", true); err == nil || len(f.server.seen("POST", examRoute)) != 2 {
		t.Fatal("the failed import was repeated without a new preview")
	}
}

func classAnswer(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" || r.URL.Path != "/admin/classes" {
		return false
	}
	if r.URL.Query().Get("dry_run") == "true" {
		reply(w, map[string]any{"students": 2, "warning_count": 1, "applied": false, "class": nil,
			"warnings": []map[string]any{{"row": 3, "account_number": "", "message": "empty account_number; skipped"}}})
		return true
	}
	w.WriteHeader(http.StatusCreated)
	reply(w, map[string]any{"students": 2, "warning_count": 1, "applied": true, "warnings": []any{},
		"class": map[string]any{"class_id": otherClass, "name": r.URL.Query().Get("name"), "student_count": 2, "exam_count": 0, "created_at": at, "published_exam": nil}})
	return true
}

func TestClassRosterIsCheckedFirstAndStoredOnlyWhenTheClassIsCreated(t *testing.T) {
	f := newPublishFixture(t, examID)
	const document = "canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,a@x.invalid,S1\n2,0002,Bruno,b@x.invalid,S2\n"
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool { return classAnswer(w, r) }
	f.chosen = writeTemp(t, "CIQ42026-Students.csv", append([]byte("\xef\xbb\xbf"), document...))
	preview, err := f.ChooseClassFile()
	if err != nil || !preview.Valid || preview.File != "CIQ42026-Students.csv" || preview.SuggestedName != "CIQ42026-Students" || preview.Students != 2 || preview.WarningCount != 1 || preview.Warnings[0].Row != 3 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	dry := f.server.seen("POST", "/admin/classes?dry_run=true")
	if len(dry) != 1 || string(dry[0].Body) != document || !strings.HasPrefix(dry[0].ContentType, "text/csv") || dry[0].Authorization != "Bearer "+secret {
		t.Fatalf("dry run = %+v", dry)
	}
	if len(f.server.seen("POST", "/admin/classes")) != 1 {
		t.Fatal("previewing stored a class")
	}
	for name, bad := range map[string]string{"empty": "  ", "control": "Math\x00", "long": strings.Repeat("é", 121)} {
		if _, err := f.CreateClass(bad); err == nil {
			t.Errorf("%s name accepted", name)
		}
	}
	if len(f.server.seen("POST", "/admin/classes")) != 1 {
		t.Fatal("an invalid name reached the server")
	}
	created, err := f.CreateClass("  Mathematics 10A ")
	if err != nil || created.ID != otherClass || created.Name != "Mathematics 10A" || created.StudentCount != 2 {
		t.Fatalf("created = %+v, %v", created, err)
	}
	posts := f.server.seen("POST", "/admin/classes")
	if len(posts) != 2 || posts[1].Target != "/admin/classes?name=Mathematics+10A" || string(posts[1].Body) != document {
		t.Fatalf("requests = %+v", posts)
	}
	if _, err := f.CreateClass("Again"); err == nil || len(f.server.seen("POST", "/admin/classes")) != 2 {
		t.Fatal("one preview created two classes")
	}
}

func TestAClassNameTheServerRefusesKeepsTheFileForAnotherTry(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method == "POST" && r.URL.Path == "/admin/classes" && r.URL.Query().Get("dry_run") != "true" && r.URL.Query().Get("name") == "Taken" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			reply(w, map[string]string{"error": "a class with this name already exists"})
			return true
		}
		return classAnswer(w, r)
	}
	f.chosen = writeTemp(t, "class.csv", []byte("canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,a@x.invalid,S1\n"))
	if preview, err := f.ChooseClassFile(); err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	_, err := f.CreateClass("Taken")
	var reject *rejection
	if !errors.As(err, &reject) || reject.Status != 409 || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
	if created, err := f.CreateClass("Another name"); err != nil || created.Name != "Another name" {
		t.Fatalf("second try = %+v, %v (the file should have been kept)", created, err)
	}
}

func TestAnUnclearClassCreationDropsTheFileAndAsksTheOperatorToLook(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Query().Get("dry_run") == "true" {
			return classAnswer(w, r)
		}
		http.Error(w, "boom "+secret, 500)
		return true
	}
	f.chosen = writeTemp(t, "class.csv", []byte("canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,a@x.invalid,S1\n"))
	if _, err := f.ChooseClassFile(); err != nil {
		t.Fatal(err)
	}
	_, err := f.CreateClass("Mathematics")
	if err == nil || !strings.Contains(err.Error(), "Look at the class list") || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
	if _, err := f.CreateClass("Mathematics"); err == nil || len(f.server.seen("POST", "/admin/classes")) != 2 {
		t.Fatal("the failed creation was repeated without a new preview")
	}
}

func TestClassFileProblemsAreListedAndNothingIsStored(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		problemsReply(w, 400, "invalid request", "roster CSV missing required column \"email\"")
		return true
	}
	f.chosen = writeTemp(t, "bad.csv", []byte("account_number\n1\n"))
	preview, err := f.ChooseClassFile()
	if err != nil || preview.Valid || len(preview.Problems) != 1 {
		t.Fatalf("rejected roster = %+v, %v", preview, err)
	}
	if _, err := f.CreateClass("Anything"); err == nil {
		t.Fatal("a rejected roster can be stored")
	}
	f.DiscardClassFile()
	f.chosen = writeTemp(t, "huge.csv", bytes.Repeat([]byte(" "), maxRosterFile+1))
	if _, err := f.ChooseClassFile(); err == nil {
		t.Fatal("an oversized roster file was accepted")
	}
}

func TestClassRosterIsReadFromTheServerAndValidated(t *testing.T) {
	f := newPublishFixture(t, examID)
	roster, err := f.ClassRoster(classID)
	if err != nil || roster.ID != classID || roster.Total != 5 || len(roster.Students) != 5 || roster.Students[0] != (Student{Account: "0001", Name: "Ada Demo"}) {
		t.Fatalf("roster = %+v, %v", roster, err)
	}
	if _, err := f.ClassRoster("not-an-id"); err == nil {
		t.Error("an invalid class id was sent")
	}
	for name, bad := range map[string]ClassRoster{
		"other class":       {ID: otherClass, Total: 1, Students: []Student{{Account: "1"}}},
		"total too small":   {ID: classID, Total: 0, Students: []Student{{Account: "1"}}},
		"blank account":     {ID: classID, Total: 1, Students: []Student{{Account: ""}}},
		"duplicate account": {ID: classID, Total: 2, Students: []Student{{Account: "1"}, {Account: "1"}}},
		"no list":           {ID: classID, Total: 1},
	} {
		bad := bad
		f.server.setClassRoster(&bad)
		if _, err := f.ClassRoster(classID); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	f.server.setClassRoster(nil)
}

func TestPublishNeedsConfirmationSendsWhatTheOperatorSawAndMonitorsTheExam(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/publish") {
			return false
		}
		f.server.setCurrent(unusedExam)
		reply(w, map[string]any{"current_exam_id": unusedExam, "current_class_id": otherClass, "replaced_exam_id": examID, "replaced_class_id": classID, "active_sessions": 1})
		return true
	}
	if _, err := f.Publish(unusedExam, examID, true, false); err == nil || len(f.server.seen("POST", "/admin/exam/")) != 0 {
		t.Fatalf("unconfirmed publish sent: %v", err)
	}
	for name, bad := range map[string][2]string{
		"bad exam":          {"nope", examID},
		"no exam":           {"", examID},
		"bad expected exam": {unusedExam, "nope"},
	} {
		if _, err := f.Publish(bad[0], bad[1], true, true); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// The class is the exam's own: the dashboard sends no class and learns which one was admitted.
	outcome, err := f.Publish(unusedExam, examID, true, true)
	if err != nil || outcome.ExamID != unusedExam || outcome.ClassID != otherClass || outcome.ReplacedExamID != examID || outcome.ReplacedClassID != classID || outcome.ActiveSessions != 1 {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	sent := f.server.seen("POST", "/admin/exam/"+unusedExam+"/publish")
	if len(sent) != 1 || string(sent[0].Body) != `{"expected_current":"`+examID+`","allow_active":true}` {
		t.Fatalf("request = %+v", sent)
	}
	wait(t, func() bool {
		v := f.Snapshot()
		return v.ExamID == unusedExam && v.Loaded && v.Catalog.CurrentExamID == unusedExam && !v.Busy
	})
}

func TestPublishRefusalsExplainThemselvesAndChangeNothing(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		reply(w, map[string]any{"error": "students are still working in the current exam", "active_sessions": 3})
		return true
	}
	_, err := f.Publish(otherExam, examID, false, true)
	var reject *rejection
	if !errors.As(err, &reject) || reject.Status != 409 || reject.Active != 3 || !strings.Contains(err.Error(), "still working") {
		t.Fatalf("error = %v", err)
	}
	if v := f.Snapshot(); v.ExamID != examID {
		t.Fatal("a refused publish changed the monitored exam")
	}
	if _, err := f.Unpublish(examID, false, true); err == nil {
		t.Fatal("refused unpublish reported success")
	}
	if _, err := f.Unpublish("", false, true); err == nil {
		t.Fatal("unpublish without a published exam was sent")
	}
	if _, err := f.Unpublish(examID, false, false); err == nil {
		t.Fatal("unconfirmed unpublish accepted")
	}
}

func TestUnpublishClosesAdmissionAndAnAnswerForTheWrongExamIsRejected(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Path == "/admin/unpublish" {
			f.server.setCurrent("")
			reply(w, map[string]any{"current_exam_id": "", "current_class_id": "", "replaced_exam_id": examID, "replaced_class_id": classID, "active_sessions": 0})
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/publish") {
			reply(w, map[string]any{"current_exam_id": examID, "current_class_id": classID, "replaced_exam_id": "", "active_sessions": 0}) // not the exam asked for
			return true
		}
		return false
	}
	outcome, err := f.Unpublish(examID, false, true)
	if err != nil || outcome.ExamID != "" || outcome.ClassID != "" || outcome.ReplacedExamID != examID || outcome.ReplacedClassID != classID {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	sent := f.server.seen("POST", "/admin/unpublish")
	if len(sent) != 1 || string(sent[0].Body) != `{"expected_current":"`+examID+`","allow_active":false}` {
		t.Fatalf("request = %+v", sent)
	}
	wait(t, func() bool { return f.Snapshot().Catalog.CurrentExamID == "" && !f.Snapshot().Busy })
	if _, err := f.Publish(otherExam, "", false, true); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("a mismatched acknowledgment was accepted: %v", err)
	}
}

func TestADeletedExamIsConfirmedListedAndReportedBackByTheServer(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != "DELETE" || !strings.HasPrefix(r.URL.Path, "/admin/exam/") {
			return false
		}
		reply(w, map[string]string{"exam_id": strings.TrimPrefix(r.URL.Path, "/admin/exam/"), "title": "Never given"})
		return true
	}
	if _, err := f.DeleteExam(unusedExam, false); err == nil || len(f.server.seen("DELETE", "/admin/exam/")) != 0 {
		t.Fatalf("unconfirmed deletion sent: %v", err)
	}
	if _, err := f.DeleteExam("nope", true); err == nil || len(f.server.seen("DELETE", "/admin/exam/")) != 0 {
		t.Fatalf("an invalid exam id was sent: %v", err)
	}
	if _, err := f.DeleteExam("77777777-7777-4777-8777-777777777777", true); err == nil || !strings.Contains(err.Error(), "not on the server's list") || len(f.server.seen("DELETE", "/admin/exam/")) != 0 {
		t.Fatalf("an exam the list does not show was sent: %v", err)
	}
	deleted, err := f.DeleteExam(unusedExam, true)
	if err != nil || deleted != (DeletedExam{ID: unusedExam, Title: "Never given"}) {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	sent := f.server.seen("DELETE", "/admin/exam/"+unusedExam)
	if len(sent) != 1 || sent[0].Authorization != "Bearer "+secret {
		t.Fatalf("request = %+v", sent)
	}
	wait(t, func() bool { return !f.Snapshot().Busy })
	if v := f.Snapshot(); v.ExamID != examID {
		t.Fatalf("deleting another exam changed the monitored one: %q", v.ExamID)
	}
}

func TestDeletingTheMonitoredExamStopsMonitoringIt(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method == "DELETE" {
			reply(w, map[string]string{"exam_id": examID, "title": "Exam one"})
			return true
		}
		return false
	}
	wait(t, func() bool { return f.Snapshot().Loaded })
	if _, err := f.DeleteExam(examID, true); err != nil {
		t.Fatal(err)
	}
	if v := f.Snapshot(); v.ExamID != "" || v.Loaded {
		t.Fatalf("view after deleting the monitored exam = %+v", v)
	}
}

func TestDeleteRefusalsAndUnclearOutcomesAreExplained(t *testing.T) {
	f := newPublishFixture(t, examID)
	status := http.StatusConflict
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != "DELETE" {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		reply(w, map[string]string{"error": "the server's own words"})
		return true
	}
	_, err := f.DeleteExam(otherExam, true)
	if err == nil || !strings.Contains(err.Error(), "published or students have taken it") || strings.Contains(err.Error(), "own words") {
		t.Fatalf("a refused exam = %v", err)
	}
	_, err = f.DeleteClass(classID, true)
	if err == nil || !strings.Contains(err.Error(), "still has exams") || !strings.Contains(err.Error(), "Delete its exams first") || !strings.Contains(err.Error(), "taken cannot be deleted") {
		t.Fatalf("a class with exams = %v", err)
	}
	status = http.StatusInternalServerError
	if _, err := f.DeleteExam(otherExam, true); err == nil || !strings.Contains(err.Error(), "Look at the exam list") {
		t.Fatalf("an unclear failure = %v", err)
	}
	status = http.StatusNotFound
	if _, err := f.DeleteExam(otherExam, true); err == nil || !strings.Contains(err.Error(), "does not have that exam or class") {
		t.Fatalf("an exam that is already gone = %v", err)
	}
}

func TestAnExamForAClassThatIsGoneSaysSo(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		switch {
		case r.URL.Query().Get("dry_run") == "true":
			checkedExam(w)
		case r.Method == "POST" && r.URL.Path == examRoute:
			problemsReply(w, http.StatusNotFound, "not found")
		default:
			return false
		}
		return true
	}
	f.chosen = writeTemp(t, "midterm.md", []byte(examDocument))
	if preview, err := f.ChooseExam(classID); err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	if _, err := f.ImportExam(classID, "pin", false); err == nil || !strings.Contains(err.Error(), "class is no longer on the server") {
		t.Fatalf("error = %v", err)
	}
}

func TestAdministratorActionsNeedAConnection(t *testing.T) {
	opts := DefaultOptions()
	opts.Choose = func(FileDialog) (string, error) {
		t.Fatal("dialog opened while disconnected")
		return "", nil
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err := s.ChooseExam(classID); err == nil {
		t.Error("ChooseExam")
	}
	if _, err := s.ChooseClassFile(); err == nil {
		t.Error("ChooseClassFile")
	}
	if _, err := s.ImportExam(classID, "pin", false); err == nil {
		t.Error("ImportExam")
	}
	if _, err := s.CreateClass("Mathematics"); err == nil {
		t.Error("CreateClass")
	}
	if _, err := s.ClassRoster(classID); err == nil {
		t.Error("ClassRoster")
	}
	if _, err := s.Publish(examID, "", false, true); err == nil {
		t.Error("Publish")
	}
	if _, err := s.Unpublish(examID, false, true); err == nil {
		t.Error("Unpublish")
	}
	if _, err := s.DeleteExam(examID, true); err == nil {
		t.Error("DeleteExam")
	}
	if _, err := s.SelectExam(examID); err == nil {
		t.Error("SelectExam")
	}
}
