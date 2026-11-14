//go:build linux

package httpapi

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
	"exam-platform/internal/workspace"
	"exam-platform/internal/workspace/workspacetest"
	"github.com/coder/websocket"
)

// ---------------------------------------------------------------------------------------------------------------------------------
// Importing a package.

func TestImportingAWorkspacePackage(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	// The exam is stored as an exam, with its kind, and the grading bundle is kept where only the server can read it.
	record, err := f.repo.Exam(context.Background(), f.examID)
	if err != nil || record.Document.Kind != "workspace" || record.PIN != "exam-secret" || record.Document.PIN != "" || record.GraceSeconds != 120 {
		t.Fatalf("record = %+v, %v", record, err)
	}
	if !f.blobs.Has(record.StarterHash) || !f.blobs.Has(record.GradingHash) {
		t.Error("a bundle is not kept")
	}
	if kind, err := f.store.Workspaces().Kind(context.Background(), f.examID); err != nil || kind != "workspace" {
		t.Errorf("kind = %q, %v", kind, err)
	}
	if kind, err := f.store.Workspaces().Kind(context.Background(), apiExamID); err != nil || kind != "question" {
		t.Errorf("the question exam's kind = %q, %v", kind, err)
	}
}

func TestAnImportRefusesWhatIsWrongWithAPackage(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	starter, grading := f.starter, f.grading
	good := document(starter, grading)
	evil := tarOf(t, map[string]string{"a.txt": "x"}, tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"})
	traversal := tarOf(t, nil, tar.Header{Name: "../x", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	noGrade := tarOf(t, map[string]string{"tests/a": "x"})
	for name, test := range map[string]struct {
		change  func(*workspace.Document)
		starter []byte
		grading []byte
		want    string
	}{
		"wrong kind":       {func(d *workspace.Document) { d.Kind = "question" }, starter, grading, `kind must be`},
		"no pin":           {func(d *workspace.Document) { d.PIN = "" }, starter, grading, "pin is required"},
		"no title":         {func(d *workspace.Document) { d.Title = " " }, starter, grading, "title is required"},
		"no duration":      {func(d *workspace.Document) { d.DurationSeconds = 0 }, starter, grading, "duration_seconds"},
		"unapproved image": {func(d *workspace.Document) { d.Workspace.Image = "evil/image" }, starter, grading, "does not have the course image"},
		"bad image name":   {func(d *workspace.Document) { d.Workspace.Image = "Evil:latest" }, starter, grading, "workspace.image must be the name"},
		"too much cpu":     {func(d *workspace.Document) { d.Workspace.Resources.CPU = 8 }, starter, grading, "cpu must be"},
		"too much memory":  {func(d *workspace.Document) { d.Workspace.Resources.MemoryMB = 99999 }, starter, grading, "memory_mb must be"},
		"too much disk":    {func(d *workspace.Document) { d.Workspace.Resources.DiskMB = 5000 }, starter, grading, "disk_mb must be"},
		"a command":        {func(d *workspace.Document) { d.Workspace.Run = "a\nb" }, starter, grading, "one line of plain text"},
		"a bad glob":       {func(d *workspace.Document) { d.Workspace.Ignore = []string{"/etc/**"} }, starter, grading, "not a path from /work down"},
		"a glob up":        {func(d *workspace.Document) { d.Workspace.Submit = []string{"../x"} }, starter, grading, "not a path from /work down"},
		"snapshot rate":    {func(d *workspace.Document) { s := int64(1); d.Workspace.SnapshotSeconds = &s }, starter, grading, "snapshot_seconds"},
		"no todos":         {func(d *workspace.Document) { d.Content.Todos = nil }, starter, grading, "content.todos"},
		"todo twice":       {func(d *workspace.Document) { d.Content.Todos = append(d.Content.Todos, d.Content.Todos[0]) }, starter, grading, "used twice"},
		"todo id":          {func(d *workspace.Document) { d.Content.Todos[0].ID = "a b" }, starter, grading, "the id must be"},
		"todo points":      {func(d *workspace.Document) { p := -1.0; d.Content.Todos[0].Points = &p }, starter, grading, "points must be"},
		"a picture":        {func(d *workspace.Document) { d.Content.Instructions = `<img src="http://evil/x.png">` }, starter, grading, "content.instructions"},
		"hash mismatch":    {func(d *workspace.Document) { d.StarterBundle = hashOf([]byte("other")) }, starter, grading, "does not match the hash"},
		"bad hash":         {func(d *workspace.Document) { d.StarterBundle = "md5:abc" }, starter, grading, "starter_bundle must be sha256"},
		"a starter link":   {func(d *workspace.Document) { d.StarterBundle = hashOf(evil) }, evil, grading, "leaves /work"},
		"a starter path":   {func(d *workspace.Document) { d.StarterBundle = hashOf(traversal) }, traversal, grading, ".. part"},
		"no grade":         {func(d *workspace.Document) { d.GradingBundle = hashOf(noGrade) }, starter, noGrade, "no file called grade"},
		"a ghost grading":  {func(d *workspace.Document) {}, starter, nil, "names a grading bundle but none came"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := good
			doc.Content.Todos = append([]workspace.Todo(nil), good.Content.Todos...)
			test.change(&doc)
			body, contentType := importBody(t, doc, test.starter, test.grading, nil)
			w := requestWithType(f.router, "POST", "/admin/class/"+apiClassID+"/workspace-exam", body, contentType, "Bearer "+testAdminToken)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), test.want) {
				t.Fatalf("status %d, body %s; want 400 with %q", w.Code, w.Body.String(), test.want)
			}
		})
	}
	// Other ways a request can be wrong.
	body, contentType := importBody(t, good, starter, grading, map[string][]byte{"extra": []byte("x")})
	body2, contentType2 := importBody(t, good, starter, grading, nil)
	for name, test := range map[string]struct {
		path, body, contentType, authorization string
		want                                   int
	}{
		"an unknown part":           {"/admin/class/" + apiClassID + "/workspace-exam", body, contentType, "Bearer " + testAdminToken, 400},
		"not multipart":             {"/admin/class/" + apiClassID + "/workspace-exam", "{}", "application/json", "Bearer " + testAdminToken, 415},
		"a class that is not there": {"/admin/class/nope/workspace-exam", body2, contentType2, "Bearer " + testAdminToken, 404},
		"no token":                  {"/admin/class/" + apiClassID + "/workspace-exam", body, contentType, "", 401},
		"a student token":           {"/admin/class/" + apiClassID + "/workspace-exam", body, contentType, "Bearer " + f.login(t, "0034").SessionToken, 401},
	} {
		w := requestWithType(f.router, "POST", test.path, test.body, test.contentType, test.authorization)
		if w.Code != test.want {
			t.Errorf("%s: status %d, want %d (%s)", name, w.Code, test.want, w.Body.String())
		}
	}
	// A refused import left no exam behind: the exam of the fixture and the question exam are all there are.
	if catalog, err := service.NewPublisher(f.store, f.clock).Catalog(context.Background()); err != nil || len(catalog.Exams) != 2 {
		t.Errorf("exams = %+v, %v", catalog, err)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Login makes the container, and the student's view has nothing in it that is not theirs.

func TestLoginMakesTheContainerWithTheStarter(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	if f.runner.State(login.SessionID) != "running" || f.runner.Starts(login.SessionID) != 1 {
		t.Fatalf("container = %s, starts %d", f.runner.State(login.SessionID), f.runner.Starts(login.SessionID))
	}
	for name, want := range starterFiles {
		if strings.HasSuffix(name, "/") {
			continue
		}
		if got := readFile(t, filepath.Join(f.runner.Work(login.SessionID), name)); got != want {
			t.Errorf("%s = %q", name, got)
		}
	}
	if r := f.runner.Resources(login.SessionID); r.MemoryMB != 256 || r.DiskMB != 50 || r.CPU != 1 {
		t.Errorf("resources = %+v", r)
	}
	// What the student may read.
	w := request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken)
	if w.Code != 200 {
		t.Fatalf("view = %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, secret := range []string{"HIDDEN-GRADING-MARKER", "cbe/cpp20", "echo running", "echo checking", "memory_mb", "grading", "exam-secret", "starter_bundle"} {
		if strings.Contains(body, secret) {
			t.Errorf("the student's view has %q: %s", secret, body)
		}
	}
	view := decoded[workspace.StudentView](t, w.Body.Bytes())
	if view.Kind != "workspace" || !view.Terminal || !view.CanRun || !view.CanCheck || len(view.Content.Todos) != 2 || view.Status.State != "running" || view.Title != "Lab 1" {
		t.Errorf("view = %+v", view)
	}
	// Asking again, from anywhere, makes nothing more.
	for range 5 {
		if _, err := f.manager.Ensure(context.Background(), login.SessionID); err != nil {
			t.Fatal(err)
		}
	}
	if f.runner.Starts(login.SessionID) != 1 {
		t.Errorf("starts = %d", f.runner.Starts(login.SessionID))
	}
	// Two students have two containers and see only their own files.
	other := f.loginReady(t, "0034")
	if f.runner.Work(other.SessionID) == f.runner.Work(login.SessionID) {
		t.Fatal("one folder for two students")
	}
	if err := os.WriteFile(filepath.Join(f.runner.Work(login.SessionID), "mine.txt"), []byte("ada"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.runner.Work(other.SessionID), "mine.txt")); err == nil {
		t.Error("a file of one student is in the other's folder")
	}
}

func TestAnExamWithoutAWorkspaceIsUntouched(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	// The question exam of the fixture has no workspace: its students can log in, and there is no workspace for them.
	f.publish(t, apiExamID)
	login := f.login(t, "0012")
	time.Sleep(100 * time.Millisecond)
	if w := request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken); w.Code != 404 {
		t.Errorf("a question session has a workspace: %d", w.Code)
	}
	if len(f.runner.Calls) != 0 {
		t.Errorf("a question exam asked the runner: %v", f.runner.Calls)
	}
	if w := request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken); w.Code != 200 {
		t.Errorf("GET /exam = %d", w.Code)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The relay.

func TestTheRelayWorksTheFilesAndRunsTheButtons(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	status := c.Wait("ws.status", c.OfType("ws.status"))
	if status["state"] != "running" || status["terminal"] != true {
		t.Errorf("status = %v", status)
	}
	// Files.
	list := c.Call(map[string]any{"type": "fs.list", "path": ""})
	if !ok(list) || len(list["entries"].([]any)) != 4 {
		t.Fatalf("list = %v", list)
	}
	read := c.Call(map[string]any{"type": "fs.read", "path": "notes.txt"})
	if !ok(read) || read["content"] != starterFiles["notes.txt"] {
		t.Fatalf("read = %v", read)
	}
	wrote := c.Call(map[string]any{"type": "fs.write", "path": "notes.txt", "content": "changed\n", "base_version": read["version"]})
	if !ok(wrote) {
		t.Fatalf("write = %v", wrote)
	}
	if got := readFile(t, filepath.Join(f.runner.Work(login.SessionID), "notes.txt")); got != "changed\n" {
		t.Errorf("the file = %q", got)
	}
	if stale := c.Call(map[string]any{"type": "fs.write", "path": "notes.txt", "content": "stale", "base_version": read["version"]}); code(stale) != "CONFLICT" {
		t.Errorf("a stale write = %v", stale)
	}
	for _, message := range []map[string]any{{"type": "fs.mkdir", "path": "dir"}, {"type": "fs.create", "path": "dir/new.c", "content": "int x;"}, {"type": "fs.rename", "path": "dir/new.c", "to": "dir/old.c"}} {
		if reply := c.Call(message); !ok(reply) {
			t.Fatalf("%v = %v", message, reply)
		}
	}
	if event := c.Wait("an fs.event", func(m map[string]any) bool { return m["type"] == "fs.event" && m["path"] == "dir/old.c" }); event["kind"] != "rename" && event["kind"] != "create" {
		t.Errorf("event = %v", event)
	}
	if reply := c.Call(map[string]any{"type": "fs.delete", "path": "dir"}); !ok(reply) {
		t.Fatal(reply)
	}
	// The Run and Check buttons run the exam's commands; the student has no say in what they are.
	run := c.Call(map[string]any{"type": "run.start", "which": "run", "cols": 80, "rows": 24})
	if !ok(run) {
		t.Fatalf("run.start = %v", run)
	}
	term := uint32(run["term"].(float64))
	c.WaitOutput(term, "running")
	if exit := c.Wait("pty.exit", func(m map[string]any) bool {
		return m["type"] == "pty.exit" && uint32(workspacetest.Num(m["term"])) == term
	}); exit["exit_code"] != float64(0) {
		t.Errorf("run exit = %v", exit)
	}
	check := c.Call(map[string]any{"type": "run.start", "which": "check"})
	checkTerm := uint32(check["term"].(float64))
	c.WaitOutput(checkTerm, "checking")
	if exit := c.Wait("pty.exit", func(m map[string]any) bool {
		return m["type"] == "pty.exit" && uint32(workspacetest.Num(m["term"])) == checkTerm
	}); exit["exit_code"] != float64(3) {
		t.Errorf("check exit = %v", exit)
	}
	// A terminal of their own: keystrokes go in and the output comes back.
	open := c.Call(map[string]any{"type": "pty.open", "cols": 80, "rows": 24})
	if !ok(open) {
		t.Fatalf("pty.open = %v", open)
	}
	shell := uint32(open["term"].(float64))
	c.Keys(shell, "echo typed-$((6*7))\n")
	c.WaitOutput(shell, "typed-42")
	// Something the student cannot do is refused, and the connection goes on.
	for name, test := range map[string]struct {
		message map[string]any
		want    string
	}{
		"a command of their own":   {map[string]any{"type": "pty.open", "cols": 80, "rows": 24, "cmd": "id"}, "DENIED"},
		"an escape":                {map[string]any{"type": "fs.read", "path": "../../etc/passwd"}, "BAD_PATH"},
		"an absolute path":         {map[string]any{"type": "fs.read", "path": "/etc/passwd"}, "BAD_PATH"},
		"an unknown type":          {map[string]any{"type": "fs.chmod", "path": "a"}, "BAD_REQUEST"},
		"an unknown field":         {map[string]any{"type": "fs.read", "path": "a", "mode": "x"}, "BAD_REQUEST"},
		"the agent's own message":  {map[string]any{"type": "agent.hello"}, "BAD_REQUEST"},
		"a run of something else":  {map[string]any{"type": "run.start", "which": "rm"}, "BAD_REQUEST"},
		"a todo that is not there": {map[string]any{"type": "todo.set", "item": "t9", "done": true}, "NOT_FOUND"},
	} {
		if reply := c.Call(test.message); ok(reply) || code(reply) != test.want {
			t.Errorf("%s: %v, want %s", name, reply, test.want)
		}
	}
	c.SendRaw(`not json`)
	if reply := c.Wait("an error", c.OfType("ws.error")); code(reply) != "BAD_REQUEST" {
		t.Errorf("garbage: %v", reply)
	}
	// Progress is self-reported and kept.
	if reply := c.Call(map[string]any{"type": "todo.set", "item": "t1", "done": true}); !ok(reply) {
		t.Fatalf("todo.set = %v", reply)
	}
	if done, err := f.repo.Todo(context.Background(), login.SessionID); err != nil || !done["t1"] || done["t2"] {
		t.Errorf("todo = %v, %v", done, err)
	}
	// Submit takes a snapshot and answers with its number.
	submitted := c.Call(map[string]any{"type": "submit"})
	if !ok(submitted) || submitted["snapshot"] == nil {
		t.Fatalf("submit = %v", submitted)
	}
	snaps, _ := f.repo.Snapshots(context.Background(), login.SessionID)
	if len(snaps) != 1 || snaps[0].Reason != "submit" {
		t.Errorf("snapshots = %+v", snaps)
	}
	if reply := c.Call(map[string]any{"type": "agent.ping"}); !ok(reply) {
		t.Errorf("the connection did not survive the refusals: %v", reply)
	}
}

func TestAnExamWithoutATerminalHasOnlyTheButtons(t *testing.T) {
	f := newWorkspaceFixture(t, func(d *workspace.Document) { d.Workspace.Terminal = false })
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	if reply := c.Call(map[string]any{"type": "pty.open", "cols": 80, "rows": 24}); code(reply) != "DENIED" {
		t.Errorf("a terminal: %v", reply)
	}
	run := c.Call(map[string]any{"type": "run.start", "which": "run"})
	if !ok(run) {
		t.Fatalf("run.start = %v", run)
	}
	c.WaitOutput(uint32(run["term"].(float64)), "running")
	if status := c.Wait("ws.status", c.OfType("ws.status")); status["terminal"] != false {
		t.Errorf("status = %v", status)
	}
}

func TestAnExamWithoutACheckCommand(t *testing.T) {
	f := newWorkspaceFixture(t, func(d *workspace.Document) { d.Workspace.Check = "" })
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	if reply := c.Call(map[string]any{"type": "run.start", "which": "check"}); code(reply) != "NOT_FOUND" {
		t.Errorf("check: %v", reply)
	}
	if view := decoded[workspace.StudentView](t, request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken).Body.Bytes()); view.CanCheck || !view.CanRun {
		t.Errorf("view = %+v", view)
	}
}

func TestAReconnectingStudentFindsTheirTerminals(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	first := f.dial(t, login.SessionToken)
	open := first.Call(map[string]any{"type": "pty.open", "cols": 80, "rows": 24})
	if !ok(open) {
		t.Fatalf("pty.open = %v", open)
	}
	shell := uint32(open["term"].(float64))
	first.Keys(shell, "echo before-the-drop\n")
	first.WaitOutput(shell, "before-the-drop")
	// A second connection of the same student takes over; the first is closed.
	second := f.dial(t, login.SessionToken)
	select {
	case <-first.Closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the first connection was not closed")
	}
	list := second.Call(map[string]any{"type": "pty.list"})
	terms, _ := list["terminals"].([]any)
	if !ok(list) || len(terms) != 1 {
		t.Fatalf("list = %v", list)
	}
	if reply := second.Call(map[string]any{"type": "pty.attach", "term": shell}); !ok(reply) {
		t.Fatal(reply)
	}
	second.WaitOutput(shell, "before-the-drop")
	second.Keys(shell, "echo after-the-drop\n")
	second.WaitOutput(shell, "after-the-drop")
	if f.runner.Starts(login.SessionID) != 1 {
		t.Errorf("the container was started %d times", f.runner.Starts(login.SessionID))
	}
}

func TestOnlyTheStudentOfASessionReachesItsWorkspace(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	f.loginReady(t, "0012")
	for name, test := range map[string]struct {
		path, token string
		want        int
	}{
		"no token":           {"/workspace/ws", "", 401},
		"a wrong token":      {"/workspace/ws", "nope", 401},
		"the admin's":        {"/workspace/ws", testAdminToken, 401},
		"the view, no token": {"/workspace", "", 401},
	} {
		header := ""
		if test.token != "" {
			header = "Bearer " + test.token
		}
		if w := request(f.router, "GET", test.path, "", header); w.Code != test.want {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	login := f.login(t, "0034")
	for _, path := range []string{"/admin/workspace/session/" + login.SessionID, "/admin/workspace/snapshot/1/tar"} {
		if w := request(f.router, "GET", path, "", "Bearer "+login.SessionToken); w.Code != 401 {
			t.Errorf("a student on %s: %d", path, w.Code)
		}
	}
}

func TestFloodsAreHeldBack(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	for range 600 {
		c.Send(map[string]any{"type": "agent.ping"})
	}
	eventually(t, "a RATE reply", func() bool { return c.Has("RATE") })
	// And the student can go on after a moment.
	time.Sleep(1200 * time.Millisecond)
	if reply := c.Call(map[string]any{"type": "agent.ping"}); !ok(reply) {
		t.Errorf("after the flood: %v", reply)
	}
	// Terminal output is throttled, not unbounded: a program that prints a lot is read at a bounded rate.
	run := c.Call(map[string]any{"type": "pty.open", "cols": 80, "rows": 24})
	shell := uint32(run["term"].(float64))
	c.Keys(shell, "yes | head -c 6000000\n")
	started := time.Now()
	eventually(t, "output", func() bool { return len(c.OutputOf(shell)) > 100000 })
	time.Sleep(2 * time.Second)
	if got := len(c.OutputOf(shell)); got > int(outputBudget(time.Since(started))) {
		t.Errorf("%d bytes of output in %v is faster than the limit", got, time.Since(started))
	}
}

// outputBudget is the most output the limit lets through in a time: its burst and its rate (with a margin).
func outputBudget(d time.Duration) float64 { return 1<<20 + 2.5*float64(1<<20)*d.Seconds() }

// ---------------------------------------------------------------------------------------------------------------------------------
// Snapshots.

func (f *workspaceFixture) work(session string) string { return f.runner.Work(session) }

func writeWork(t *testing.T, f *workspaceFixture, session, name, content string) {
	t.Helper()
	path := filepath.Join(f.work(session), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *workspaceFixture) superviseAt(at time.Time) {
	f.clock.Set(at)
	f.manager.SuperviseOnce(context.Background())
}

func TestSnapshotsAreTakenAtTheMomentsOfD8(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	start := f.clock.Now()
	ctx := context.Background()
	count := func() int {
		snaps, _ := f.repo.Snapshots(ctx, login.SessionID)
		return len(snaps)
	}
	// The first round has no snapshot yet and takes one; the next, with nothing changed and the interval not up, none.
	f.superviseAt(start)
	if count() != 1 {
		t.Fatalf("snapshots = %d", count())
	}
	f.superviseAt(start.Add(30 * time.Second))
	if count() != 1 {
		t.Errorf("a snapshot before the interval: %d", count())
	}
	// When the interval is up and nothing changed, the last snapshot stands; when something changed, there is a new one.
	f.superviseAt(start.Add(61 * time.Second))
	if count() != 1 {
		t.Errorf("an unchanged workspace made a snapshot: %d", count())
	}
	writeWork(t, f, login.SessionID, "src/new.c", "int added;\n")
	f.superviseAt(start.Add(125 * time.Second))
	if count() != 2 {
		t.Fatalf("a changed workspace made no snapshot: %d", count())
	}
	// The other moments: a submission, a lost agent and the deadline each leave a row with their reason, changed or not.
	c := f.dial(t, login.SessionToken)
	c.Call(map[string]any{"type": "submit"})
	f.runner.Kill(login.SessionID)
	f.superviseAt(start.Add(130 * time.Second))
	f.superviseAt(start.Add(time.Hour + 3*time.Minute))
	snaps, _ := f.repo.Snapshots(ctx, login.SessionID)
	var reasons []string
	for _, snap := range snaps {
		reasons = append(reasons, snap.Reason)
	}
	if got := strings.Join(reasons, " "); got != "periodic periodic submit agent_loss deadline" {
		t.Fatalf("reasons = %s", got)
	}
	// The interval an exam sets is its own.
	g := newWorkspaceFixture(t, func(d *workspace.Document) { s := int64(15); d.Workspace.SnapshotSeconds = &s })
	other := g.loginReady(t, "0012")
	g.superviseAt(g.clock.Now())
	writeWork(t, g, other.SessionID, "a.txt", "a")
	g.superviseAt(g.clock.Now().Add(16 * time.Second))
	if snaps, _ := g.repo.Snapshots(ctx, other.SessionID); len(snaps) != 2 {
		t.Errorf("with an interval of 15 s: %d snapshots", len(snaps))
	}
}

func TestASnapshotRestoresByteForByte(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	big := strings.Repeat("0123456789abcdef", 1<<16) // 1 MiB
	writeWork(t, f, login.SessionID, "src/big.bin", big)
	writeWork(t, f, login.SessionID, "src/unicode ñ ü 世界.txt", "contenido\r\nsin salto")
	writeWork(t, f, login.SessionID, "empty", "")
	if err := os.Mkdir(filepath.Join(f.work(login.SessionID), "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src/main.c", filepath.Join(f.work(login.SessionID), "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(f.work(login.SessionID), "scripts/x.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	record, created, err := f.manager.Snapshots.Take(context.Background(), login.SessionID, workspace.ReasonSubmit)
	if err != nil || !created || record.Files < 7 || record.Skipped != 0 {
		t.Fatalf("snapshot = %+v, %v, %v", record, created, err)
	}
	// Change the workspace afterwards: the snapshot does not.
	writeWork(t, f, login.SessionID, "src/main.c", "changed after the snapshot")
	var buffer bytes.Buffer
	if err := f.service.SnapshotTar(context.Background(), record.ID, &buffer); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	modes := map[string]int64{}
	links := map[string]string{}
	dirs := map[string]bool{}
	reader := tar.NewReader(&buffer)
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		name := strings.TrimSuffix(header.Name, "/")
		switch header.Typeflag {
		case tar.TypeDir:
			dirs[name] = true
		case tar.TypeSymlink:
			links[name] = header.Linkname
		default:
			data, _ := io.ReadAll(reader)
			got[name], modes[name] = string(data), header.Mode
		}
	}
	for name, want := range starterFiles {
		if strings.HasSuffix(name, "/") || name == "src/main.c" {
			continue
		}
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
	if got["src/main.c"] != starterFiles["src/main.c"] {
		t.Errorf("main.c is %q: the snapshot changed with the files", got["src/main.c"])
	}
	if got["src/big.bin"] != big || got["src/unicode ñ ü 世界.txt"] != "contenido\r\nsin salto" || got["empty"] != "" {
		t.Error("a file is not what it was")
	}
	if _, found := got["empty"]; !found || !dirs["emptydir"] || !dirs["src"] || links["link"] != "src/main.c" || modes["scripts/x.sh"]&0o111 == 0 {
		t.Errorf("dirs %v links %v modes %v", dirs, links, modes)
	}
	// The same snapshot is the same bytes, and an unchanged tree shares its blobs: another snapshot of it adds only a manifest.
	var again bytes.Buffer
	_ = f.service.SnapshotTar(context.Background(), record.ID, &again)
	var first bytes.Buffer
	_ = f.service.SnapshotTar(context.Background(), record.ID, &first)
	if !bytes.Equal(again.Bytes(), first.Bytes()) {
		t.Error("a snapshot is not the same bytes twice")
	}
	// Over the admin route.
	w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/snapshot/%d/tar", record.ID), "", "Bearer "+testAdminToken)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), first.Bytes()) {
		t.Errorf("the download: %d, %d bytes", w.Code, w.Body.Len())
	}
	if w := request(f.router, "GET", "/admin/workspace/snapshot/99999/tar", "", "Bearer "+testAdminToken); w.Code != 404 {
		t.Errorf("a snapshot that is not there: %d", w.Code)
	}
	if view := decoded[workspace.AdminSession](t, request(f.router, "GET", "/admin/workspace/session/"+login.SessionID, "", "Bearer "+testAdminToken).Body.Bytes()); len(view.Snapshots) != 1 || view.State != "ready" {
		t.Errorf("admin view = %+v", view)
	}
}

// A student's files are theirs to name and shape: what a snapshot cannot carry is left out and counted, and never followed.
func TestASnapshotOfHostileFiles(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	hostile := tarOf(t, map[string]string{"ok.txt": "fine", "dir/": ""},
		tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
		tar.Header{Name: "/abs", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
		tar.Header{Name: "a/../../b", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
		tar.Header{Name: "bad\x01name", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
		tar.Header{Name: "back\\slash", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
		tar.Header{Name: "link-out", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		tar.Header{Name: "link-up", Typeflag: tar.TypeSymlink, Linkname: "../../../x"},
		tar.Header{Name: "fifo", Typeflag: tar.TypeFifo, Mode: 0o644},
		tar.Header{Name: "chr", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3},
		tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "ok.txt"},
		tar.Header{Name: "hard-missing", Typeflag: tar.TypeLink, Linkname: "nothing"},
		tar.Header{Name: "empty-link", Typeflag: tar.TypeSymlink, Linkname: ""})
	f.runner.ArchiveHook = func(string) io.ReadCloser { return io.NopCloser(bytes.NewReader(hostile)) }
	record, _, err := f.manager.Snapshots.Take(context.Background(), login.SessionID, workspace.ReasonSubmit)
	if err != nil {
		t.Fatal(err)
	}
	_, manifest, err := f.manager.Snapshots.Manifest(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]workspace.Entry{}
	for _, entry := range manifest.Entries {
		have[entry.Path] = entry
	}
	for _, name := range []string{"ok.txt", "dir", "hard", "link-out", "link-up"} {
		if _, found := have[name]; !found {
			t.Errorf("%s is not in the snapshot: %v", name, have)
		}
	}
	for name := range have {
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\x01\\") || name == "fifo" || name == "chr" || name == "hard-missing" || name == "empty-link" {
			t.Errorf("%q is in the snapshot", name)
		}
	}
	if have["hard"].Hash != have["ok.txt"].Hash || have["link-out"].Link != "/etc/passwd" || have["link-out"].Hash != "" {
		t.Errorf("entries = %+v", have)
	}
	if record.Skipped != 9 {
		t.Errorf("skipped = %d, want 9 (%v)", record.Skipped, have)
	}
	// Restoring it writes the links as links.
	var buffer bytes.Buffer
	if err := f.service.SnapshotTar(context.Background(), record.ID, &buffer); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(&buffer)
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		if header.Name == "link-out" && (header.Typeflag != tar.TypeSymlink || header.Linkname != "/etc/passwd") {
			t.Errorf("link-out = %+v", header)
		}
	}
}

func TestSnapshotLimits(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	small := workspace.NewSnapshots(f.repo, f.blobs, f.runner, workspace.SnapshotLimits{MaxFiles: 3, MaxBytes: 1 << 20, MaxFileBytes: 100}, f.clock.Now)
	if _, _, err := small.Take(context.Background(), login.SessionID, workspace.ReasonSubmit); err == nil {
		t.Error("a snapshot of more files than the limit")
	}
	writeWork(t, f, login.SessionID, "huge.txt", strings.Repeat("x", 500))
	few := workspace.NewSnapshots(f.repo, f.blobs, f.runner, workspace.SnapshotLimits{MaxFiles: 1000, MaxBytes: 1 << 20, MaxFileBytes: 100}, f.clock.Now)
	if _, _, err := few.Take(context.Background(), login.SessionID, workspace.ReasonSubmit); err == nil {
		t.Error("a snapshot of a file over the limit")
	}
	f.runner.FailArchive = fmt.Errorf("the runner is down")
	if _, _, err := f.manager.Snapshots.Take(context.Background(), login.SessionID, workspace.ReasonSubmit); err == nil {
		t.Error("a snapshot without an archive")
	}
	if snaps, _ := f.repo.Snapshots(context.Background(), login.SessionID); len(snaps) != 0 {
		t.Errorf("a failed snapshot left a row: %v", snaps)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The watch the server keeps.

func TestALostAgentIsSnapshottedAndTheContainerRestartedOnTheSameVolume(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	writeWork(t, f, login.SessionID, "work-in-progress.c", "int not_lost;\n")
	f.runner.Kill(login.SessionID)
	if f.runner.State(login.SessionID) != "exited" {
		t.Fatalf("state = %s", f.runner.State(login.SessionID))
	}
	f.manager.SuperviseOnce(context.Background())
	if f.runner.State(login.SessionID) != "running" || f.runner.Starts(login.SessionID) != 2 {
		t.Fatalf("the container did not come back: %s, starts %d", f.runner.State(login.SessionID), f.runner.Starts(login.SessionID))
	}
	snaps, _ := f.repo.Snapshots(context.Background(), login.SessionID)
	if len(snaps) == 0 || snaps[0].Reason != "agent_loss" {
		t.Fatalf("snapshots = %+v\nlog: %s", snaps, f.logs.String())
	}
	if info, _ := f.repo.Session(context.Background(), login.SessionID); info.AgentRestarts != 1 || info.State != workspace.StateReady {
		t.Errorf("session = %+v", info)
	}
	// The files are still there, and the student's connection is told and carries on.
	c.Wait("ws.agent_restarted", c.OfType("ws.agent_restarted"))
	eventually(t, "the relay to reach the new agent", func() bool {
		reply := c.Call(map[string]any{"type": "fs.read", "path": "work-in-progress.c"})
		return ok(reply) && reply["content"] == "int not_lost;\n"
	})
	// An agent that is there but silent is treated the same, after a few rounds.
	f.runner.SilentAgents[login.SessionID] = true
	for range 3 {
		f.manager.SuperviseOnce(context.Background())
	}
	if f.runner.Starts(login.SessionID) != 3 {
		t.Errorf("a silent agent: starts = %d", f.runner.Starts(login.SessionID))
	}
}

func TestTheDeadlineFreezesTheContainerAndRetentionRemovesIt(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	start := f.clock.Now()
	writeWork(t, f, login.SessionID, "last-minute.txt", "in time")
	// Inside the grace period nothing is frozen.
	f.superviseAt(start.Add(3600*time.Second + 60*time.Second))
	if f.runner.State(login.SessionID) != "running" {
		t.Fatalf("frozen inside the grace period: %s", f.runner.State(login.SessionID))
	}
	// After it, the last snapshot is taken and the container paused.
	f.superviseAt(start.Add(3600*time.Second + 121*time.Second))
	if f.runner.State(login.SessionID) != "paused" {
		t.Fatalf("state = %s", f.runner.State(login.SessionID))
	}
	snap, err := f.repo.LatestSnapshot(context.Background(), login.SessionID)
	if err != nil || snap.Reason != "deadline" {
		t.Fatalf("latest snapshot = %+v, %v", snap, err)
	}
	var buffer bytes.Buffer
	if err := f.service.SnapshotTar(context.Background(), snap.ID, &buffer); err != nil || !bytes.Contains(buffer.Bytes(), []byte("in time")) {
		t.Errorf("the final snapshot lacks the last file: %v", err)
	}
	select {
	case <-c.Closed:
	case <-time.After(10 * time.Second):
		t.Error("the student's connection was not closed at the deadline")
	}
	// A frozen workspace takes no more connections.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := f.manager.Ensure(ctx, login.SessionID); err == nil {
		t.Error("a frozen workspace was ensured")
	}
	if status, _ := f.manager.Status(ctx, login.SessionID); status.State != "frozen" {
		t.Errorf("status = %+v", status)
	}
	// And a new snapshot is not taken of what is frozen: a periodic one inside the grace period, the final one, and no more.
	f.superviseAt(start.Add(3600*time.Second + 400*time.Second))
	if snaps, _ := f.repo.Snapshots(context.Background(), login.SessionID); len(snaps) != 2 || snaps[1].Reason != "deadline" {
		t.Errorf("snapshots = %+v", snaps)
	}
	// After the retention period the container and its folder go; the snapshots stay.
	f.superviseAt(start.Add(3600*time.Second + 121*time.Second + time.Hour + time.Minute))
	if f.runner.State(login.SessionID) != "missing" {
		t.Errorf("state = %s", f.runner.State(login.SessionID))
	}
	if info, _ := f.repo.Session(context.Background(), login.SessionID); info.State != workspace.StateRemoved {
		t.Errorf("state = %s", info.State)
	}
	var again bytes.Buffer
	if err := f.service.SnapshotTar(context.Background(), snap.ID, &again); err != nil || !bytes.Equal(again.Bytes(), buffer.Bytes()) {
		t.Errorf("the snapshot did not survive the container: %v", err)
	}
}

// A restarted exam server finds its sessions as they were: the containers are the runner's, the state is the database's.
func TestARestartedServerPicksUpWhereItWas(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	writeWork(t, f, login.SessionID, "survives.txt", "yes")
	// A new manager over the same database and runner, as after a restart.
	restarted := workspace.NewManager(workspace.Options{Repo: f.repo, Runner: f.runner, Blobs: f.blobs, Logger: f.logger, Now: f.clock.Now, HeartbeatTimeout: 5 * time.Second, ReadyTimeout: 10 * time.Second})
	info, err := restarted.Ensure(context.Background(), login.SessionID)
	if err != nil || info.State != workspace.StateReady {
		t.Fatalf("Ensure = %+v, %v", info, err)
	}
	if f.runner.Starts(login.SessionID) != 1 {
		t.Errorf("a restarted server restarted the container: %d starts", f.runner.Starts(login.SessionID))
	}
	restarted.SuperviseOnce(context.Background())
	// A restart while a provisioning was half done: the state says provisioning, the container is there, and the next ensure completes it.
	if err := f.repo.SetState(context.Background(), login.SessionID, workspace.StateProvisioning, "", f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if info, err := restarted.Ensure(context.Background(), login.SessionID); err != nil || info.State != workspace.StateReady {
		t.Errorf("after a half-done provisioning: %+v, %v", info, err)
	}
	if got := readFile(t, filepath.Join(f.work(login.SessionID), "survives.txt")); got != "yes" {
		t.Errorf("file = %q", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// More of what the server keeps an eye on.

func TestAProvisioningNobodyIsFinishingIsTakenUp(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	if err := f.repo.SetState(context.Background(), login.SessionID, workspace.StateProvisioning, "", f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.manager.SuperviseOnce(context.Background())
	if info, _ := f.repo.Session(context.Background(), login.SessionID); info.State != workspace.StateReady {
		t.Errorf("state = %s", info.State)
	}
	if f.runner.Starts(login.SessionID) != 1 {
		t.Errorf("the container was started %d times", f.runner.Starts(login.SessionID))
	}
}

func TestTheSupervisorLoopRunsByItselfAndStops(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	manager := workspace.NewManager(workspace.Options{Repo: f.repo, Runner: f.runner, Blobs: f.blobs, Logger: f.logger, Now: f.clock.Now, Tick: 20 * time.Millisecond,
		SnapshotEvery: 10 * time.Second, HeartbeatTimeout: 5 * time.Second, ReadyTimeout: 10 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { manager.Supervise(ctx); close(done) }()
	eventually(t, "the loop's first snapshot", func() bool {
		snaps, _ := f.repo.Snapshots(context.Background(), login.SessionID)
		return len(snaps) == 1
	})
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor did not stop")
	}
}

func TestTooMuchTypingIsDroppedAndTheStudentIsToldOnce(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	shell := uint32(workspacetest.Num(c.Call(map[string]any{"type": "pty.open", "cols": 80, "rows": 24})["term"]))
	chunk := strings.Repeat("a", 4096)
	for range 400 { // 1.6 MiB at once: past the burst the limit allows
		c.Keys(shell, chunk)
	}
	eventually(t, "a RATE message", func() bool { return c.Has("RATE") })
	c.Keys(shell, "\x15") // kill the line the shell was left with
	time.Sleep(3 * time.Second)
	if reply := c.Call(map[string]any{"type": "agent.ping"}); !ok(reply) {
		t.Errorf("after the flood: %v", reply)
	}
}

func TestASubmitThatCannotBeTakenSaysSoWithoutTheDetails(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	f.runner.FailArchive = fmt.Errorf("dial unix /data/cbe-platform/ipc/runner.sock: the runner is down")
	reply := c.Call(map[string]any{"type": "submit"})
	if ok(reply) || code(reply) != "UNAVAILABLE" || strings.Contains(fmt.Sprint(reply), "runner") || strings.Contains(fmt.Sprint(reply), "/data") {
		t.Errorf("reply = %v", reply)
	}
	if !strings.Contains(f.logs.String(), "the runner is down") {
		t.Error("the failure is not in the server's log")
	}
	f.runner.FailArchive = nil
	if reply := c.Call(map[string]any{"type": "submit"}); !ok(reply) {
		t.Errorf("the next submit: %v", reply)
	}
}

// The lock is the monitoring's answer to a student who left the exam window; it takes the exam's content away, and a workspace is
// content too.
func TestALockedSessionReachesNothingOfItsWorkspace(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	token := "Bearer " + login.SessionToken
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	if w := request(f.router, "POST", "/event", `{"event_type":"away_lock"}`, token); w.Code != http.StatusOK {
		t.Fatalf("away_lock = %d %s", w.Code, w.Body.String())
	}
	select {
	case <-c.Closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection of a locked session was not closed")
	}
	if w := request(f.router, "GET", "/workspace", "", token); w.Code != http.StatusLocked {
		t.Errorf("the view of a locked session: %d %s", w.Code, w.Body.String())
	}
	if _, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(f.server.URL, "http")+"/workspace/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + login.SessionToken}}}); err == nil || resp == nil || resp.StatusCode != http.StatusLocked {
		t.Errorf("a locked session took a connection: %v %v", resp, err)
	}
	// The container went on running meanwhile, and the proctor's unlock gives the workspace back.
	if f.runner.State(login.SessionID) != "running" {
		t.Errorf("the container is %s", f.runner.State(login.SessionID))
	}
	if w := request(f.router, "POST", "/admin/session/"+login.SessionID+"/unlock", `{"reason":"back","give_back_time":false}`, "Bearer "+testAdminToken); w.Code != http.StatusOK {
		t.Fatalf("unlock = %d %s", w.Code, w.Body.String())
	}
	if w := request(f.router, "GET", "/workspace", "", token); w.Code != http.StatusOK {
		t.Errorf("the view after the unlock: %d", w.Code)
	}
	again := f.dial(t, login.SessionToken)
	if reply := again.Call(map[string]any{"type": "fs.list", "path": ""}); !ok(reply) {
		t.Errorf("after the unlock: %v", reply)
	}
}

// Why a workspace failed can name the runner's socket or a path; the student is told it failed, and the proctor why.
func TestAFailedWorkspaceTellsTheStudentNothingOfWhy(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	const reason = "the runner: dial unix /data/cbe-platform/ipc/runner.sock: connection refused"
	if err := f.repo.SetState(context.Background(), login.SessionID, workspace.StateFailed, reason, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	w := request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken)
	if w.Code != 200 || strings.Contains(w.Body.String(), "runner") || strings.Contains(w.Body.String(), "/data") || !strings.Contains(w.Body.String(), `"state":"failed"`) {
		t.Errorf("view = %d %s", w.Code, w.Body.String())
	}
	status, err := f.manager.Status(context.Background(), login.SessionID)
	if encoded, _ := json.Marshal(status); err != nil || status.State != "failed" || strings.Contains(string(encoded), "failure") {
		t.Errorf("ws.status = %s, %v", encoded, err)
	}
	if view := decoded[workspace.AdminSession](t, request(f.router, "GET", "/admin/workspace/session/"+login.SessionID, "", "Bearer "+testAdminToken).Body.Bytes()); view.Failure != reason {
		t.Errorf("the proctor's view = %+v", view)
	}
}
