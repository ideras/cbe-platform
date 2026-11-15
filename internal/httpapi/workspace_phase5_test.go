//go:build linux

package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
	"exam-platform/internal/workspace"
	"exam-platform/internal/workspace/workspacetest"
)

// ---------------------------------------------------------------------------------------------------------------------------------
// Phase 5: submissions, the history, transcripts and what the proctor reads.

type event struct {
	Type   string
	Detail map[string]any
}

// history reads a session's events the way the proctor's window does, through the timeline route, and keeps the workspace ones.
func (f *workspaceFixture) history(t *testing.T, session string) []event {
	t.Helper()
	var out []event
	cursor := ""
	for {
		path := "/admin/session/" + session + "/timeline?limit=200"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := request(f.router, "GET", path, "", "Bearer "+testAdminToken)
		if w.Code != 200 {
			t.Fatalf("timeline = %d %s", w.Code, w.Body.String())
		}
		page := decoded[service.TimelinePage](t, w.Body.Bytes())
		for _, item := range page.Items {
			if item.Event == nil {
				continue
			}
			kind := item.Event.Type
			if strings.HasPrefix(kind, "workspace.") || strings.HasPrefix(kind, "file.") || strings.HasPrefix(kind, "pty.") || strings.HasPrefix(kind, "run.") || strings.HasPrefix(kind, "grading.") || kind == "todo.set" || kind == "paste.blocked" {
				e := event{Type: kind, Detail: map[string]any{}}
				_ = json.Unmarshal(item.Event.Detail, &e.Detail)
				out = append(out, e)
			}
		}
		if page.NextCursor == nil {
			return out
		}
		cursor = *page.NextCursor
	}
}

func types(events []event) string {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.Type
	}
	return strings.Join(names, " ")
}

func (f *workspaceFixture) waitHistory(t *testing.T, session, want string) []event {
	t.Helper()
	var events []event
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if events = f.history(t, session); types(events) == want {
			return events
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the history is\n  %s\nwanted\n  %s", types(events), want)
	return nil
}

func TestAFullSessionLeavesTheExpectedHistory(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	call := func(message map[string]any) map[string]any {
		t.Helper()
		reply := c.Call(message)
		if !ok(reply) {
			t.Fatalf("%v = %v", message, reply)
		}
		return reply
	}
	read := call(map[string]any{"type": "fs.read", "path": "notes.txt"})
	call(map[string]any{"type": "fs.write", "path": "notes.txt", "content": "first line\nsecond line\nand a third one\n", "base_version": read["version"]})
	call(map[string]any{"type": "fs.create", "path": "new.c", "content": "int x;\n"})
	call(map[string]any{"type": "fs.mkdir", "path": "dir"})
	call(map[string]any{"type": "fs.rename", "path": "new.c", "to": "dir/new.c"})
	call(map[string]any{"type": "fs.delete", "path": "dir/new.c"})
	shell := call(map[string]any{"type": "pty.open", "cols": 80, "rows": 24})
	call(map[string]any{"type": "pty.close", "term": shell["term"]})
	run := call(map[string]any{"type": "run.start", "which": "run", "cols": 80, "rows": 24})
	c.WaitExit(uint32(workspacetest.Num(run["term"])))
	// A Run is told to the history with its exit code once it has ended, by the recorder.
	eventually(t, "the run's end in the history", func() bool { return strings.Contains(types(f.history(t, login.SessionID)), "run.started") })
	call(map[string]any{"type": "todo.set", "item": "t1", "done": true})
	call(map[string]any{"type": "paste.blocked", "target": "editor", "bytes": 120})
	call(map[string]any{"type": "submit"})
	c.Close()
	f.waitHistory(t, login.SessionID, "workspace.opened file.saved file.created file.created file.renamed file.deleted pty.opened pty.closed pty.opened run.started todo.set paste.blocked workspace.submitted workspace.disconnected")
	events := f.history(t, login.SessionID)
	detail := func(kind string, n int) map[string]any {
		seen := 0
		for _, e := range events {
			if e.Type == kind {
				if seen == n {
					return e.Detail
				}
				seen++
			}
		}
		t.Fatalf("no %s number %d", kind, n)
		return nil
	}
	if d := detail("file.saved", 0); d["path"] != "notes.txt" || d["bytes"] != float64(39) || d["delta_bytes"] != float64(39-len(starterFiles["notes.txt"])) {
		t.Errorf("file.saved = %v", d)
	}
	if d := detail("file.created", 0); d["path"] != "new.c" || d["dir"] != false {
		t.Errorf("file.created = %v", d)
	}
	if d := detail("file.created", 1); d["path"] != "dir" || d["dir"] != true {
		t.Errorf("file.created (folder) = %v", d)
	}
	if d := detail("file.renamed", 0); d["path"] != "new.c" || d["to"] != "dir/new.c" {
		t.Errorf("file.renamed = %v", d)
	}
	if d := detail("pty.opened", 0); d["kind"] != "shell" || d["id"] == nil {
		t.Errorf("pty.opened = %v", d)
	}
	if d := detail("pty.opened", 1); d["kind"] != "run" {
		t.Errorf("pty.opened (run) = %v", d)
	}
	if d := detail("run.started", 0); d["which"] != "run" || d["exit_code"] != float64(0) {
		t.Errorf("run.started = %v", d)
	}
	if d := detail("todo.set", 0); d["id"] != "t1" || d["done"] != true {
		t.Errorf("todo.set = %v", d)
	}
	if d := detail("paste.blocked", 0); d["target"] != "editor" || d["bytes"] != float64(120) {
		t.Errorf("paste.blocked = %v", d)
	}
	if d := detail("workspace.submitted", 0); d["final"] != false || d["files"] == nil || d["snapshot"] == nil {
		t.Errorf("workspace.submitted = %v", d)
	}
	// The end of the exam: the final submission is told too, and the agent's restart has its event.
	f.runner.Kill(login.SessionID)
	f.manager.SuperviseOnce(context.Background())
	f.superviseAt(f.clock.Now().Add(time.Hour + 3*time.Minute))
	finish := f.history(t, login.SessionID)
	if got := types(finish); !strings.HasSuffix(got, "workspace.disconnected workspace.agent_restarted workspace.submitted") {
		t.Errorf("history at the end = %s", got)
	}
	if d := finish[len(finish)-1].Detail; d["final"] != true {
		t.Errorf("the last submission = %v", d)
	}
}

func submissionsOf(t *testing.T, f *workspaceFixture, session string) []workspace.AdminSubmission {
	t.Helper()
	return decoded[workspace.AdminSession](t, request(f.router, "GET", "/admin/workspace/session/"+session, "", "Bearer "+testAdminToken).Body.Bytes()).Submissions
}

func TestAStudentWhoNeverPressesSubmitHasASubmissionAtTheDeadline(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	writeWork(t, f, login.SessionID, "src/answer.c", "int answer(void) { return 42; }\n")
	if got := submissionsOf(t, f, login.SessionID); len(got) != 0 {
		t.Fatalf("submissions before the deadline: %v", got)
	}
	start := f.clock.Now()
	// Inside the grace period nothing is final yet.
	f.superviseAt(start.Add(3600*time.Second + 60*time.Second))
	if got := submissionsOf(t, f, login.SessionID); len(got) != 0 {
		t.Fatalf("a submission inside the grace period: %v", got)
	}
	f.superviseAt(start.Add(3600*time.Second + 121*time.Second))
	got := submissionsOf(t, f, login.SessionID)
	if len(got) != 1 || !got[0].Final || got[0].Files < 5 {
		t.Fatalf("submissions at the deadline: %+v", got)
	}
	file := decoded[workspace.FileContent](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/file?path=src/answer.c", got[0].ID), "", "Bearer "+testAdminToken).Body.Bytes())
	if file.Content != "int answer(void) { return 42; }\n" {
		t.Errorf("the final submission's file = %+v", file)
	}
	// And once more: a frozen workspace is not submitted again.
	f.superviseAt(start.Add(3600*time.Second + 400*time.Second))
	if again := submissionsOf(t, f, login.SessionID); len(again) != 1 {
		t.Errorf("a frozen workspace made %d submissions", len(again))
	}
}

func TestTheLatestSubmissionCountsAndTheFinalOneIsTheWorkAtTheEnd(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	writeWork(t, f, login.SessionID, "version.txt", "one\n")
	first := c.Call(map[string]any{"type": "submit"})
	writeWork(t, f, login.SessionID, "version.txt", "two\n")
	c.Call(map[string]any{"type": "submit"})
	writeWork(t, f, login.SessionID, "version.txt", "three, after the last submit\n")
	if !ok(first) {
		t.Fatal(first)
	}
	f.superviseAt(f.clock.Now().Add(3600*time.Second + 121*time.Second))
	got := submissionsOf(t, f, login.SessionID)
	if len(got) != 3 || got[0].Final || got[1].Final || !got[2].Final {
		t.Fatalf("submissions = %+v", got)
	}
	texts := []string{"one\n", "two\n", "three, after the last submit\n"}
	for i, want := range texts {
		file := decoded[workspace.FileContent](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/file?path=version.txt", got[i].ID), "", "Bearer "+testAdminToken).Body.Bytes())
		if file.Content != want {
			t.Errorf("submission %d has %q, want %q", i, file.Content, want)
		}
	}
	// The report is of the last one.
	report := request(f.router, "GET", "/admin/workspace/session/"+login.SessionID+"/report", "", "Bearer "+testAdminToken).Body.String()
	if !strings.Contains(report, "three, after the last submit") || strings.Contains(report, "\none\n") {
		t.Errorf("the report is not of the latest submission:\n%s", report)
	}
}

func TestWhatTheExamIgnoresIsNotInTheSubmission(t *testing.T) {
	f := newWorkspaceFixture(t, func(d *workspace.Document) { d.Workspace.Ignore = []string{"build/**", "**/*.o"} })
	login := f.loginReady(t, "0012")
	writeWork(t, f, login.SessionID, "build/app", "ELF")
	writeWork(t, f, login.SessionID, "build/deeper/thing", "x")
	writeWork(t, f, login.SessionID, "src/main.o", "object")
	writeWork(t, f, login.SessionID, "src/keep.c", "int k;\n")
	c := f.dial(t, login.SessionToken)
	if reply := c.Call(map[string]any{"type": "submit"}); !ok(reply) {
		t.Fatal(reply)
	}
	got := submissionsOf(t, f, login.SessionID)
	if len(got) != 1 {
		t.Fatalf("submissions = %+v", got)
	}
	tree := decoded[struct {
		Entries []workspace.TreeEntry `json:"entries"`
	}](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/tree", got[0].ID), "", "Bearer "+testAdminToken).Body.Bytes()).Entries
	have := map[string]bool{}
	for _, e := range tree {
		have[e.Path] = true
	}
	for _, name := range []string{"build", "build/app", "build/deeper", "build/deeper/thing", "src/main.o"} {
		if have[name] {
			t.Errorf("%s is in the submission", name)
		}
	}
	for _, name := range []string{"src", "src/keep.c", "Makefile", "notes.txt"} {
		if !have[name] {
			t.Errorf("%s is not in the submission (%v)", name, have)
		}
	}
	// The snapshot of the same moment has all of it: it is for recovery.
	snap := decoded[struct {
		Entries []workspace.TreeEntry `json:"entries"`
	}](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/snapshot/%d/tree", got[0].SnapshotID), "", "Bearer "+testAdminToken).Body.Bytes()).Entries
	all := map[string]bool{}
	for _, e := range snap {
		all[e.Path] = true
	}
	if !all["build/app"] || !all["src/main.o"] {
		t.Errorf("the snapshot lost the ignored files: %v", all)
	}
	// With a submit list, only what it names, and ignore still wins.
	g := newWorkspaceFixture(t, func(d *workspace.Document) {
		d.Workspace.Submit = []string{"src/**", "Makefile"}
		d.Workspace.Ignore = []string{"**/*.o"}
	})
	other := g.loginReady(t, "0012")
	writeWork(t, g, other.SessionID, "src/main.o", "object")
	writeWork(t, g, other.SessionID, "src/keep.c", "int k;\n")
	d := g.dial(t, other.SessionToken)
	d.Call(map[string]any{"type": "submit"})
	list := submissionsOf(t, g, other.SessionID)
	narrow := decoded[struct {
		Entries []workspace.TreeEntry `json:"entries"`
	}](t, request(g.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/tree", list[0].ID), "", "Bearer "+testAdminToken).Body.Bytes()).Entries
	var names []string
	for _, e := range narrow {
		names = append(names, e.Path)
	}
	if got := strings.Join(names, " "); got != "Makefile src src/keep.c src/main.c" {
		t.Errorf("with a submit list: %s", got)
	}
}

func TestFinishingEarlyEndsTheWorkspaceAndMakesTheFinalSubmission(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	writeWork(t, f, login.SessionID, "done.txt", "finished early\n")
	// The student finishes the exam: the application's own final submit, with no answers (a workspace has none).
	w := request(f.router, "POST", "/submit", `{"answers":{}}`, "Bearer "+login.SessionToken)
	if w.Code != 200 {
		t.Fatalf("final submit = %d %s", w.Code, w.Body.String())
	}
	// The next round of the supervisor makes the last snapshot and the final submission, and freezes the container.
	f.manager.SuperviseOnce(context.Background())
	got := submissionsOf(t, f, login.SessionID)
	if len(got) != 1 || !got[0].Final {
		t.Fatalf("submissions = %+v", got)
	}
	file := decoded[workspace.FileContent](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/file?path=done.txt", got[0].ID), "", "Bearer "+testAdminToken).Body.Bytes())
	if file.Content != "finished early\n" {
		t.Errorf("the file = %+v", file)
	}
	admin := decoded[workspace.AdminSession](t, request(f.router, "GET", "/admin/workspace/session/"+login.SessionID, "", "Bearer "+testAdminToken).Body.Bytes())
	last := admin.Snapshots[len(admin.Snapshots)-1]
	if last.Reason != "final" || admin.State != "frozen" {
		t.Errorf("last snapshot %+v, state %s", last, admin.State)
	}
	if f.runner.State(login.SessionID) != "paused" {
		t.Errorf("the container is %s", f.runner.State(login.SessionID))
	}
	select {
	case <-c.Closed:
	case <-time.After(10 * time.Second):
		t.Error("the student's connection was not closed")
	}
	// A student whose session ended cannot get back in.
	if w := request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken); w.Code == 200 {
		t.Errorf("a finished session read its workspace: %d", w.Code)
	}
}

func TestAPasteThatWasBlockedIsKeptAndTheRateIsHeldDown(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	for name, message := range map[string]map[string]any{
		"an unknown target": {"type": "paste.blocked", "target": "kitchen", "bytes": 1},
		"negative bytes":    {"type": "paste.blocked", "target": "editor", "bytes": -1},
		"an extra field":    {"type": "paste.blocked", "target": "editor", "bytes": 1, "text": "what was pasted"},
		"no target":         {"type": "paste.blocked", "bytes": 1},
	} {
		if reply := c.Call(message); ok(reply) || code(reply) != "BAD_REQUEST" {
			t.Errorf("%s: %v", name, reply)
		}
	}
	for range 40 {
		if reply := c.Call(map[string]any{"type": "paste.blocked", "target": "terminal", "bytes": 10}); !ok(reply) {
			t.Fatal(reply)
		}
	}
	events := f.history(t, login.SessionID)
	pastes := 0
	for _, e := range events {
		if e.Type == "paste.blocked" {
			pastes++
			if e.Detail["target"] != "terminal" || e.Detail["text"] != nil {
				t.Errorf("event = %v", e.Detail)
			}
		}
	}
	if pastes < 1 || pastes > 8 {
		t.Errorf("%d events for 40 blocked pastes in a burst", pastes)
	}
}

func TestTheProctorSeesTodoProgressAsSelfReported(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	other := f.loginReady(t, "0034")
	roster := func() []workspace.RosterEntry {
		w := request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/roster", "", "Bearer "+testAdminToken)
		if w.Code != 200 {
			t.Fatalf("roster = %d %s", w.Code, w.Body.String())
		}
		return decoded[struct {
			Students []workspace.RosterEntry `json:"students"`
		}](t, w.Body.Bytes()).Students
	}
	entries := roster()
	if len(entries) != 2 || entries[0].Account != "0012" || entries[0].TodoTotal != 2 || entries[0].TodoDone != 0 || entries[0].State != "ready" || entries[0].Connected {
		t.Fatalf("roster = %+v", entries)
	}
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	c.Call(map[string]any{"type": "todo.set", "item": "t1", "done": true})
	entries = roster()
	if entries[0].TodoDone != 1 || !entries[0].Connected || entries[0].LastActivity == nil || entries[1].TodoDone != 0 || entries[1].Connected {
		t.Errorf("after a tick: %+v", entries)
	}
	c.Call(map[string]any{"type": "todo.set", "item": "t2", "done": true})
	c.Call(map[string]any{"type": "todo.set", "item": "t1", "done": false})
	if entries = roster(); entries[0].TodoDone != 1 {
		t.Errorf("after two more: %+v", entries[0])
	}
	// A student who has not logged in has no row, and a question exam has no workspace roster.
	if w := request(f.router, "GET", "/admin/workspace/exam/"+apiExamID+"/roster", "", "Bearer "+testAdminToken); w.Code != http.StatusNotFound {
		t.Errorf("the roster of a question exam = %d", w.Code)
	}
	if w := request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/roster", "", "Bearer "+other.SessionToken); w.Code != http.StatusUnauthorized {
		t.Errorf("a student read the roster: %d", w.Code)
	}
	// CPU: what the manager last saw of the container (the fake runner says one process and 1 MiB).
	f.manager.RefreshStats(context.Background())
	if e := roster()[0]; e.MemoryMB == nil || *e.MemoryMB != 1 {
		t.Errorf("stats = %+v", e)
	}
}

func TestTheProctorReadsFilesAndDownloadsThatMatchTheSnapshot(t *testing.T) {
	f := newWorkspaceFixture(t, nil)
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	writeWork(t, f, login.SessionID, "src/unicode ñ.txt", "contenido\r\nsin salto")
	writeWork(t, f, login.SessionID, "bin/blob.bin", "x\x00y")
	writeWork(t, f, login.SessionID, "big.txt", strings.Repeat("a", workspace.MaxViewedFile+1))
	writeWork(t, f, login.SessionID, "tricky.md", "```\nnot a fence of the report\n```\n")
	if err := os.Symlink("src/main.c", f.work(login.SessionID)+"/link"); err != nil {
		t.Fatal(err)
	}
	c.Call(map[string]any{"type": "todo.set", "item": "t1", "done": true})
	c.Call(map[string]any{"type": "submit"})
	sub := submissionsOf(t, f, login.SessionID)[0]
	auth := "Bearer " + testAdminToken
	// The viewer: a text file, one that is not text, one that is too big, and one that is not there.
	view := func(path string) workspace.FileContent {
		return decoded[workspace.FileContent](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/file?path=%s", sub.ID, path), "", auth).Body.Bytes())
	}
	if file := view("src/main.c"); file.Content != starterFiles["src/main.c"] || file.Binary || file.TooLarge {
		t.Errorf("main.c = %+v", file)
	}
	if file := view("bin/blob.bin"); !file.Binary || file.Content != "" {
		t.Errorf("blob = %+v", file)
	}
	if file := view("big.txt"); !file.TooLarge || file.Content != "" || file.Size != workspace.MaxViewedFile+1 {
		t.Errorf("big = %+v", file.Size)
	}
	if w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/file?path=nothing", sub.ID), "", auth); w.Code != 404 {
		t.Errorf("a file that is not there = %d", w.Code)
	}
	if w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/file", sub.ID), "", auth); w.Code != 404 {
		t.Errorf("no path = %d", w.Code)
	}
	if w := request(f.router, "GET", "/admin/workspace/submission/99999/tree", "", auth); w.Code != 404 {
		t.Errorf("a submission that is not there = %d", w.Code)
	}
	// The zip matches the submission, file for file and byte for byte; links are left out.
	w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/zip", sub.ID), "", auth)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("zip = %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	inZip := map[string]string{}
	for _, entry := range archive.File {
		file, _ := entry.Open()
		data, _ := io.ReadAll(file)
		file.Close()
		inZip[entry.Name] = string(data)
		if strings.Contains(entry.Name, "..") || strings.HasPrefix(entry.Name, "/") || entry.Mode()&0o111 != 0 && !strings.HasSuffix(entry.Name, ".sh") {
			t.Errorf("zip entry %s (%v)", entry.Name, entry.Mode())
		}
	}
	tree := decoded[struct {
		Entries []workspace.TreeEntry `json:"entries"`
	}](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/tree", sub.ID), "", auth).Body.Bytes()).Entries
	for _, e := range tree {
		switch e.Type {
		case "file":
			var tarred bytes.Buffer
			if err := f.service.SnapshotTar(context.Background(), sub.SnapshotID, &tarred); err != nil {
				t.Fatal(err)
			}
			if _, found := inZip[e.Path]; !found {
				t.Errorf("%s is in the submission and not in the zip", e.Path)
			}
		case "link":
			if _, found := inZip[e.Path]; found {
				t.Errorf("a link is in the zip: %s", e.Path)
			}
		}
	}
	if inZip["src/unicode ñ.txt"] != "contenido\r\nsin salto" || inZip["bin/blob.bin"] != "x\x00y" || inZip["src/main.c"] != starterFiles["src/main.c"] || len(inZip["big.txt"]) != workspace.MaxViewedFile+1 {
		t.Error("a file in the zip is not what the student had")
	}
	// The same bytes as the snapshot's own tar.
	var tarred bytes.Buffer
	if err := f.service.SnapshotTar(context.Background(), sub.SnapshotID, &tarred); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tarred.Bytes(), []byte("contenido\r\nsin salto")) {
		t.Error("the snapshot does not have what the zip has")
	}
	// The report: the instructions, the ticks said to be the student's own, the files in fences that the files cannot close.
	report := request(f.router, "GET", "/admin/workspace/session/"+login.SessionID+"/report", "", auth)
	if report.Code != 200 || !strings.HasPrefix(report.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("report = %d %s", report.Code, report.Header().Get("Content-Type"))
	}
	text := report.Body.String()
	for _, want := range []string{"# Lab 1 — student 0012", "## Instructions", "Build it.", "self-reported", "- [x] Do `one`", "- [ ] Do two", "### `src/main.c`", "```c\nint main(void) { return 0; }\n```",
		"### `tricky.md`", "````markdown\n```\nnot a fence of the report\n```\n````", "`bin/blob.bin` is not text", "`big.txt` is larger than 1 MiB", "`link` is a symbolic link to `src/main.c`", "contenido\r\nsin salto", "(pressed Save)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "HIDDEN-GRADING-MARKER") {
		t.Error("the report has the grading bundle")
	}
	if w := request(f.router, "GET", "/admin/workspace/session/"+f.login(t, "0034").SessionID+"/report", "", auth); w.Code != 404 {
		t.Errorf("the report of a student who submitted nothing = %d", w.Code)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Transcripts.

func (f *workspaceFixture) cast(t *testing.T, id int64) (workspace.CastHeader, []workspace.CastEvent, string) {
	t.Helper()
	w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/terminal/%d/cast", id), "", "Bearer "+testAdminToken)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-asciicast" {
		t.Fatalf("cast = %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	header, events, err := workspace.ReplayCast(w.Body.Bytes())
	if err != nil {
		t.Fatalf("the transcript does not replay: %v\n%s", err, w.Body.String())
	}
	var text strings.Builder
	for _, e := range events {
		text.WriteString(e.Text)
	}
	return header, events, text.String()
}

func (f *workspaceFixture) terminalsOf(t *testing.T, session string) []workspace.AdminTerminal {
	t.Helper()
	return decoded[workspace.AdminSession](t, request(f.router, "GET", "/admin/workspace/session/"+session, "", "Bearer "+testAdminToken).Body.Bytes()).Terminals
}

func TestATerminalTranscriptReplaysWhatTheStudentSaw(t *testing.T) {
	f := newWorkspaceFixture(t, func(d *workspace.Document) {
		d.Workspace.Run = "echo compiling; printf 'caf\\303\\251 \\344\\270\\226\\347\\225\\214\\n'; seq 1 3000; exit 3"
	})
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	c.Wait("ws.status", c.OfType("ws.status"))
	// A Run that prints a lot and ends with a code.
	run := c.Call(map[string]any{"type": "run.start", "which": "run", "cols": 90, "rows": 30})
	runTerm := uint32(workspacetest.Num(run["term"]))
	if code := c.WaitExit(runTerm); code != 3 {
		t.Fatalf("exit = %v", code)
	}
	c.WaitOutput(runTerm, "3000")
	// A shell, with a program that goes on printing after the student's connection is gone.
	shell := c.Call(map[string]any{"type": "pty.open", "cols": 100, "rows": 40})
	shellTerm := uint32(workspacetest.Num(shell["term"]))
	c.WaitOutput(shellTerm, "$")
	c.Keys(shellTerm, "echo before-the-drop\n")
	c.WaitOutput(shellTerm, "before-the-drop")
	c.Keys(shellTerm, "sleep 1; echo after-the-drop-1; sleep 1; echo after-the-drop-2\n")
	c.WaitOutput(shellTerm, "sleep 1")
	seenRun, seenShell := c.OutputOf(runTerm), c.OutputOf(shellTerm)
	c.Close()
	eventually(t, "the output printed with nobody connected", func() bool {
		for _, term := range f.terminalsOf(t, login.SessionID) {
			if term.Kind == "shell" {
				_, _, text := f.cast(t, term.ID)
				return strings.Contains(text, "after-the-drop-2")
			}
		}
		return false
	})
	terminals := f.terminalsOf(t, login.SessionID)
	if len(terminals) != 2 || terminals[0].Kind != "run" || terminals[1].Kind != "shell" {
		t.Fatalf("terminals = %+v", terminals)
	}
	// The Run: its transcript is the output the student saw, the header says how big its terminal was, and how it ended is on record.
	header, events, text := f.cast(t, terminals[0].ID)
	if text != seenRun || header.Width != 90 || header.Height != 30 || header.Title != "Run" || header.Version != 2 || len(events) == 0 {
		t.Errorf("the run's transcript:\n%q\nthe student saw\n%q\nheader %+v", text[:min(len(text), 120)], seenRun[:min(len(seenRun), 120)], header)
	}
	if !strings.Contains(text, "café 世界") || !strings.Contains(text, "compiling") {
		t.Errorf("the transcript's text: %q", text[:min(len(text), 80)])
	}
	if terminals[0].ExitCode == nil || *terminals[0].ExitCode != 3 || terminals[0].EndedAt == nil || terminals[0].Bytes == 0 {
		t.Errorf("the run's record = %+v", terminals[0])
	}
	// The shell: what the student saw is in it, in order, and so is what came after they were gone.
	_, _, shellText := f.cast(t, terminals[1].ID)
	if !strings.HasPrefix(shellText, seenShell[:min(len(seenShell), 60)]) || !strings.Contains(shellText, "before-the-drop") ||
		strings.Index(shellText, "before-the-drop") > strings.Index(shellText, "after-the-drop-1") || strings.Index(shellText, "after-the-drop-1") > strings.Index(shellText, "after-the-drop-2") {
		t.Errorf("the shell's transcript:\n%q", shellText)
	}
	if !strings.HasPrefix(shellText, seenShell[:min(len(seenShell), len(shellText))]) && !strings.Contains(shellText, seenShell) {
		t.Errorf("what the student saw is not at the start of the transcript:\nsaw %q\ngot %q", seenShell, shellText)
	}
	// The history has the Run, with its exit code.
	for _, e := range f.history(t, login.SessionID) {
		if e.Type == "run.started" && (e.Detail["which"] != "run" || e.Detail["exit_code"] != float64(3)) {
			t.Errorf("run.started = %v", e.Detail)
		}
	}
	// A transcript is read by the proctor only.
	if w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/terminal/%d/cast", terminals[0].ID), "", "Bearer "+login.SessionToken); w.Code != 401 {
		t.Errorf("a student read a transcript: %d", w.Code)
	}
	if w := request(f.router, "GET", "/admin/workspace/terminal/9999/cast", "", "Bearer "+testAdminToken); w.Code != 404 {
		t.Errorf("a transcript that is not there: %d", w.Code)
	}
	// After the exam the transcripts are closed with the last of the output.
	f.superviseAt(f.clock.Now().Add(3600*time.Second + 121*time.Second))
	for _, term := range f.terminalsOf(t, login.SessionID) {
		if term.EndedAt == nil {
			t.Errorf("a transcript is still open after the exam: %+v", term)
		}
	}
}

func TestATerminalThatEndsBeforeTheRecorderLooksIsStillRecorded(t *testing.T) {
	f := newWorkspaceFixture(t, func(d *workspace.Document) { d.Workspace.Run = "echo instantly; exit 0" })
	login := f.loginReady(t, "0012")
	c := f.dial(t, login.SessionToken)
	for range 4 {
		run := c.Call(map[string]any{"type": "run.start", "which": "run"})
		c.WaitExit(uint32(workspacetest.Num(run["term"])))
		c.Call(map[string]any{"type": "pty.close", "term": run["term"]})
	}
	eventually(t, "four transcripts", func() bool { return len(f.terminalsOf(t, login.SessionID)) == 4 })
	for _, term := range f.terminalsOf(t, login.SessionID) {
		eventually(t, "the transcript to end", func() bool {
			for _, now := range f.terminalsOf(t, login.SessionID) {
				if now.ID == term.ID {
					return now.EndedAt != nil
				}
			}
			return false
		})
		if _, _, text := f.cast(t, term.ID); !strings.Contains(text, "instantly") {
			t.Errorf("transcript %d = %q", term.ID, text)
		}
	}
}
