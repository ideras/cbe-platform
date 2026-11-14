package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeWorkspace is the exam server's side of a workspace exam: GET /workspace and the WebSocket, speaking
// docs/workspace/relay-protocol.md closely enough to test the application's side of it.
type fakeWorkspace struct {
	t      *testing.T
	token  string
	mu     sync.Mutex
	dials  int
	status int // when not 0, the dial is answered with this status
	// closeWith, when set, is how the next connection is ended right after it is made: its close reason.
	closeWith string
	headers   []http.Header
	requests  []map[string]any
	inputs    [][]byte
	conns     []*websocket.Conn
	// silent makes the server not answer requests.
	silent bool
	view   workspaceReply
}

func newFakeWorkspace(t *testing.T) *fakeWorkspace {
	f := &fakeWorkspace{t: t}
	f.view = workspaceReply{Kind: "workspace", ExamID: "exam", SessionID: "session", Title: "Lab 1", Terminal: true, CanRun: true, CanCheck: true,
		Done: map[string]bool{"t1": true}, Status: WorkspaceStatus{State: "running", SecondsLeft: 3600, Terminal: true}}
	f.view.Content.Instructions = "<p>Build it.</p>"
	f.view.Content.Todos = []WorkspaceTodo{{ID: "t1", TextHTML: "One"}, {ID: "t2", TextHTML: "Two"}}
	return f
}

func (f *fakeWorkspace) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/workspace" {
		respond(w, f.view)
		return
	}
	f.mu.Lock()
	f.dials++
	f.headers = append(f.headers, r.Header.Clone())
	status, closeWith := f.status, f.closeWith
	f.closeWith = ""
	f.mu.Unlock()
	if status != 0 {
		http.Error(w, "refused", status)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	f.mu.Lock()
	f.conns = append(f.conns, conn)
	f.mu.Unlock()
	if closeWith != "" {
		_ = conn.Close(websocket.StatusNormalClosure, closeWith)
		return
	}
	conn.SetReadLimit(4 << 20)
	ctx := r.Context()
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind == websocket.MessageBinary {
			f.mu.Lock()
			f.inputs = append(f.inputs, data)
			f.mu.Unlock()
			continue
		}
		var request map[string]any
		if json.Unmarshal(data, &request) != nil {
			continue
		}
		f.mu.Lock()
		f.requests = append(f.requests, request)
		silent := f.silent
		f.mu.Unlock()
		if silent {
			continue
		}
		reply := map[string]any{"id": request["id"], "type": request["type"], "ok": true}
		switch request["type"] {
		case "fs.read":
			if request["path"] == "missing" {
				reply["ok"], reply["code"], reply["message"] = false, "NOT_FOUND", "no such file"
			} else {
				reply["content"], reply["version"] = "hello", "sha256:abc"
			}
		case "pty.open":
			reply["term"] = 1
		}
		payload, _ := json.Marshal(reply)
		_ = conn.Write(ctx, websocket.MessageText, payload)
	}
}

// send writes a message to the connection the server has now (the last one).
func (f *fakeWorkspace) send(kind websocket.MessageType, data []byte) {
	f.mu.Lock()
	conn := f.conns[len(f.conns)-1]
	f.mu.Unlock()
	if err := conn.Write(context.Background(), kind, data); err != nil {
		f.t.Fatalf("send: %v", err)
	}
}

func (f *fakeWorkspace) output(term uint32, text string) {
	f.send(websocket.MessageBinary, append(binary.BigEndian.AppendUint32(nil, term), text...))
}

func (f *fakeWorkspace) drop() {
	f.mu.Lock()
	conn := f.conns[len(f.conns)-1]
	f.mu.Unlock()
	_ = conn.CloseNow()
}

func (f *fakeWorkspace) count() (dials int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

type recorder struct {
	mu       sync.Mutex
	events   []string // "out:<term>:<text>" and "msg:<json>", in the order they came
	messages []string
}

func (r *recorder) options() WorkspaceOptions {
	return WorkspaceOptions{
		OnOutput: func(term uint32, data []byte) {
			r.mu.Lock()
			r.events = append(r.events, "out:"+string(rune('0'+term))+":"+string(data))
			r.mu.Unlock()
		},
		OnMessage: func(message string) {
			r.mu.Lock()
			r.events = append(r.events, "msg:"+message)
			r.messages = append(r.messages, message)
			r.mu.Unlock()
		},
		CallTimeout: 400 * time.Millisecond,
	}
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newLink makes a link to a fake server, allowed and configured.
func newLink(t *testing.T, f *fakeWorkspace, rec *recorder) (*workspaceLink, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	opts := WorkspaceOptions{CallTimeout: 400 * time.Millisecond}
	if rec != nil {
		opts = rec.options()
	}
	link := newWorkspaceLink(ctx, server.Client(), opts, 10*time.Millisecond, 40*time.Millisecond)
	t.Cleanup(link.close)
	link.setAllowed(true)
	link.configure(server.URL, "private-token", &f.view)
	return link, server
}

func waitConnection(t *testing.T, link *workspaceLink, want string) *WorkspaceView {
	t.Helper()
	var view *WorkspaceView
	waitFor(t, "the connection to be "+want, func() bool {
		view = link.view()
		return view != nil && view.Connection == want
	})
	return view
}

// waitGeneration waits for the n-th connection to be there.
func waitGeneration(t *testing.T, link *workspaceLink, n int) {
	t.Helper()
	waitFor(t, "connection number "+string(rune('0'+n)), func() bool {
		v := link.view()
		return v != nil && v.Connection == connConnected && v.Generation == n
	})
}

func TestTheLinkConnectsWithTheTokenAndAnswersRequests(t *testing.T) {
	f := newFakeWorkspace(t)
	link, _ := newLink(t, f, nil)
	view := waitConnection(t, link, connConnected)
	if view.Title != "Lab 1" || len(view.Todos) != 2 || !view.Done["t1"] || !view.Terminal || !view.CanRun || !view.CanCheck || view.Generation != 1 {
		t.Errorf("view = %+v", view)
	}
	if got := f.headers[0].Get("Authorization"); got != "Bearer private-token" {
		t.Errorf("Authorization = %q", got)
	}
	// The id is the link's own, whatever the page sent.
	reply, err := link.call(`{"id": 999, "type": "fs.read", "path": "a.c"}`)
	if err != nil || !strings.Contains(reply, `"content":"hello"`) {
		t.Fatalf("reply = %q, %v", reply, err)
	}
	if id := f.requests[0]["id"]; id != float64(1) {
		t.Errorf("the server saw id %v", id)
	}
	// An answer that says it failed is an answer.
	reply, err = link.call(`{"type":"fs.read","path":"missing"}`)
	if err != nil || !strings.Contains(reply, `"code":"NOT_FOUND"`) {
		t.Errorf("reply = %q, %v", reply, err)
	}
	// The page cannot send what the workspace does not take, and nothing of that kind leaves the application.
	for _, bad := range []string{`not json`, `[]`, `null`, `{}`, `{"type":7}`, `{"type":"agent.hello"}`, `{"type":"ws.status"}`, `{"type":"fs.chmod","path":"a"}`, `{"type":"pty.open","cmd":"x"}`[:0] + `{"type":"exec"}`} {
		if _, err := link.call(bad); err == nil {
			t.Errorf("%s was sent", bad)
		}
	}
	f.mu.Lock()
	sent := len(f.requests)
	f.mu.Unlock()
	if sent != 2 {
		t.Errorf("the server got %d requests", sent)
	}
}

func TestTerminalOutputComesBatchedInOrderBeforeWhatFollowsIt(t *testing.T) {
	f := newFakeWorkspace(t)
	rec := &recorder{}
	link, _ := newLink(t, f, rec)
	waitConnection(t, link, connConnected)
	for i := range 50 {
		f.output(1, string(rune('a'+i%26)))
	}
	f.output(2, "other")
	f.send(websocket.MessageText, []byte(`{"type":"pty.exit","term":1,"exit_code":0}`))
	f.output(1, "later")
	waitFor(t, "the output after the exit", func() bool { return strings.Contains(strings.Join(rec.snapshot(), "|"), "later") })
	events := rec.snapshot()
	var typed strings.Builder
	exitAt := -1
	for i, event := range events {
		if strings.HasPrefix(event, "out:1:") {
			if exitAt < 0 {
				typed.WriteString(strings.TrimPrefix(event, "out:1:"))
			}
		}
		if strings.HasPrefix(event, "msg:") && strings.Contains(event, "pty.exit") {
			exitAt = i
		}
	}
	want := ""
	for i := range 50 {
		want += string(rune('a' + i%26))
	}
	if typed.String() != want || exitAt < 0 {
		t.Errorf("the output before the exit = %q (exit at %d), want %q; events %q", typed.String(), exitAt, want, events)
	}
	if len(events) > 12 {
		t.Errorf("%d events for 52 frames: the output was not batched", len(events))
	}
	if !strings.HasSuffix(events[len(events)-1], "later") {
		t.Errorf("the last event is %q", events[len(events)-1])
	}
}

func TestInputGoesAsFramesOfATerminalAndAPasteIsSplit(t *testing.T) {
	f := newFakeWorkspace(t)
	link, _ := newLink(t, f, nil)
	waitConnection(t, link, connConnected)
	if err := link.input(3, "héllo\n"); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", workspaceInputChunk*2+100)
	if err := link.input(3, big); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the frames", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.inputs) == 4
	})
	if binary.BigEndian.Uint32(f.inputs[0][:4]) != 3 || string(f.inputs[0][4:]) != "héllo\n" {
		t.Errorf("first frame = %q", f.inputs[0])
	}
	var joined []byte
	for _, frame := range f.inputs[1:] {
		if len(frame)-4 > workspaceInputChunk {
			t.Errorf("a frame of %d bytes", len(frame)-4)
		}
		joined = append(joined, frame[4:]...)
	}
	if !bytes.Equal(joined, []byte(big)) {
		t.Errorf("the pieces do not make the paste: %d bytes", len(joined))
	}
}

func TestADroppedConnectionIsMadeAgainAndFailsWhatWasWaitingOnIt(t *testing.T) {
	f := newFakeWorkspace(t)
	f.view.Status.State = "running"
	link, _ := newLink(t, f, nil)
	waitConnection(t, link, connConnected)
	f.mu.Lock()
	f.silent = true
	f.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	var callErr error
	go func() {
		defer wg.Done()
		_, callErr = link.call(`{"type":"fs.list","path":""}`)
	}()
	waitFor(t, "the request to arrive", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.requests) == 1
	})
	f.drop()
	wg.Wait()
	if callErr == nil || !strings.Contains(callErr.Error(), "lost") {
		t.Errorf("the waiting request got %v", callErr)
	}
	f.mu.Lock()
	f.silent = false
	f.mu.Unlock()
	waitGeneration(t, link, 2)
	if reply, err := link.call(`{"type":"fs.list","path":""}`); err != nil || !strings.Contains(reply, `"ok":true`) {
		t.Errorf("after the reconnect: %q, %v", reply, err)
	}
	// While there is no connection, requests are refused at once rather than queued: the page keeps what it has and writes it later.
	f.mu.Lock()
	f.status = http.StatusServiceUnavailable
	f.mu.Unlock()
	f.drop()
	waitConnection(t, link, connReconnecting)
	if _, err := link.call(`{"type":"fs.list","path":""}`); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("while reconnecting: %v", err)
	}
	f.mu.Lock()
	f.status = 0
	f.mu.Unlock()
	waitGeneration(t, link, 3)
}

func TestARequestThatIsNeverAnsweredTimesOut(t *testing.T) {
	f := newFakeWorkspace(t)
	f.silent = true
	link, _ := newLink(t, f, nil)
	waitConnection(t, link, connConnected)
	started := time.Now()
	if _, err := link.call(`{"type":"fs.list","path":""}`); err == nil || !strings.Contains(err.Error(), "did not answer") || time.Since(started) > 2*time.Second {
		t.Errorf("err = %v after %v", err, time.Since(started))
	}
	link.mu.Lock()
	left := len(link.pending)
	link.mu.Unlock()
	if left != 0 {
		t.Errorf("%d requests are still pending", left)
	}
}

func TestStatusMessagesUpdateTheViewAndAreToldToThePage(t *testing.T) {
	f := newFakeWorkspace(t)
	rec := &recorder{}
	link, _ := newLink(t, f, rec)
	waitConnection(t, link, connConnected)
	f.send(websocket.MessageText, []byte(`{"type":"ws.status","state":"restarting","seconds_left":50,"agent_restarts":2,"terminal":true,"deadline_at":"2035-01-02T03:04:05Z"}`))
	waitFor(t, "the status", func() bool { v := link.view(); return v.Status != nil && v.Status.State == "restarting" })
	if v := link.view(); v.Status.SecondsLeft != 50 || v.Status.AgentRestarts != 2 {
		t.Errorf("status = %+v", v.Status)
	}
	waitFor(t, "the page to be told", func() bool { return len(rec.snapshot()) > 0 && strings.Contains(rec.snapshot()[0], "ws.status") })
}

func TestALockedExamIsWaitedOutAndTheConnectionComesBackWithTheUnlock(t *testing.T) {
	f := newFakeWorkspace(t)
	f.status = http.StatusLocked
	link, _ := newLink(t, f, nil)
	view := waitConnection(t, link, connLocked)
	if !strings.Contains(view.Note, "locked") {
		t.Errorf("note = %q", view.Note)
	}
	f.mu.Lock()
	f.status = 0
	f.mu.Unlock()
	waitConnection(t, link, connConnected)
}

func TestAnotherComputersSessionAndTheEndOfTheExamAreNotRetried(t *testing.T) {
	f := newFakeWorkspace(t)
	f.status = http.StatusUnauthorized
	link, _ := newLink(t, f, nil)
	view := waitConnection(t, link, connClosed)
	if !strings.Contains(view.Note, "another computer") {
		t.Errorf("note = %q", view.Note)
	}
	time.Sleep(150 * time.Millisecond)
	if n := f.count(); n != 1 {
		t.Errorf("%d attempts with a token the server refuses", n)
	}
	// The exam ends from the server's side.
	g := newFakeWorkspace(t)
	g.closeWith = "the exam is over"
	other, _ := newLink(t, g, nil)
	view = waitConnection(t, other, connEnded)
	if !strings.Contains(view.Note, "over") {
		t.Errorf("note = %q", view.Note)
	}
	time.Sleep(150 * time.Millisecond)
	if n := g.count(); n != 1 {
		t.Errorf("%d attempts after the exam ended", n)
	}
}

func TestAWorkspaceAnotherWindowTookIsTakenBackOnlyWhenAsked(t *testing.T) {
	f := newFakeWorkspace(t)
	f.closeWith = "replaced by a newer connection of the same student"
	link, _ := newLink(t, f, nil)
	view := waitConnection(t, link, connReplaced)
	if !strings.Contains(view.Note, "another window") {
		t.Errorf("note = %q", view.Note)
	}
	time.Sleep(150 * time.Millisecond)
	if n := f.count(); n != 1 {
		t.Errorf("%d attempts: two windows would fight over the workspace", n)
	}
	if err := link.reconnect(); err != nil {
		t.Fatal(err)
	}
	waitConnection(t, link, connConnected)
}

func TestTheConnectionExistsOnlyWhileTheExamIsOpenToWorkOn(t *testing.T) {
	f := newFakeWorkspace(t)
	link, _ := newLink(t, f, nil)
	waitConnection(t, link, connConnected)
	link.setAllowed(false)
	waitConnection(t, link, connIdle)
	if _, err := link.call(`{"type":"fs.list","path":""}`); err == nil {
		t.Error("a request was sent while the exam was closed to work")
	}
	before := f.count()
	time.Sleep(100 * time.Millisecond)
	if f.count() != before {
		t.Error("the link dialled while it was not allowed")
	}
	link.setAllowed(true)
	waitGeneration(t, link, 2)
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Through the service: how a workspace exam is told from a question exam.

func workspaceClient(t *testing.T, f *fixture, rec *recorder) *Service {
	return newClient(t, f, func(o *Options) { o.Workspace = rec.options() })
}

func TestAnExamWithoutExercisesIsAWorkspaceWhenTheServerSaysSo(t *testing.T) {
	f := newFixture(t)
	f.emptyExam = true
	fake := newFakeWorkspace(t)
	f.workspace = fake
	s := workspaceClient(t, f, &recorder{})
	view := loginClient(t, s, f)
	if view.Phase != "exam" || view.Workspace == nil || view.Workspace.Title != "Lab 1" {
		t.Fatalf("view = %+v", view)
	}
	waitView(t, s, func(v View) bool { return v.Workspace != nil && v.Workspace.Connection == connConnected })
	if view.Exam == nil || len(view.Exam.Content.Exercises) != 0 {
		t.Errorf("exam = %+v", view.Exam)
	}
	if got := fake.headers[0].Get("Authorization"); got != "Bearer "+f.token {
		t.Errorf("Authorization = %q", got)
	}
	if got := fake.headers[0].Get("User-Agent"); !strings.HasPrefix(got, "cbe-portal-student/") {
		t.Errorf("User-Agent = %q", got)
	}
	// Through the service's own methods, as the page calls them.
	reply, err := s.WorkspaceRequest(`{"type":"fs.read","path":"a.c"}`)
	if err != nil || !strings.Contains(reply, `"version":"sha256:abc"`) {
		t.Errorf("reply = %q, %v", reply, err)
	}
	if err := s.WorkspaceInput(1, "ls\n"); err != nil {
		t.Error(err)
	}
	for _, term := range []int{0, -1, 1 << 40} {
		if err := s.WorkspaceInput(term, "x"); err == nil {
			t.Errorf("terminal %d was taken", term)
		}
	}
}

func TestAQuestionExamNeverAsksAboutAWorkspace(t *testing.T) {
	f := newFixture(t)
	var asked atomic.Int32
	f.workspace = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked.Add(1); http.Error(w, "no", 404) })
	s := workspaceClient(t, f, &recorder{})
	view := loginClient(t, s, f)
	if view.Phase != "exam" || view.Workspace != nil || asked.Load() != 0 {
		t.Errorf("phase %s, workspace %v, asked %d", view.Phase, view.Workspace, asked.Load())
	}
	if _, err := s.WorkspaceRequest(`{"type":"fs.list","path":""}`); err == nil {
		t.Error("a workspace request was taken in a question exam")
	}
}

func TestAnExamWithNoExercisesAndNoWorkspaceIsStillRefused(t *testing.T) {
	f := newFixture(t)
	f.emptyExam = true // and no /workspace route: an older server, or a mistake in the exam
	s := workspaceClient(t, f, &recorder{})
	if _, err := s.Login(t.Context(), f.server.URL, " 00123 ", f.pin); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("err = %v", err)
	}
}

func TestAWorkspaceThatCannotBeAskedAboutIsRetriedNotTakenForAQuestionExam(t *testing.T) {
	f := newFixture(t)
	f.emptyExam = true
	var code atomic.Int32
	code.Store(500)
	fake := newFakeWorkspace(t)
	f.workspace = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := int(code.Load()); c != 0 && r.URL.Path == "/workspace" {
			http.Error(w, "down", c)
			return
		}
		fake.ServeHTTP(w, r)
	})
	s := workspaceClient(t, f, &recorder{})
	if _, err := s.Login(t.Context(), f.server.URL, " 00123 ", f.pin); err == nil {
		t.Fatal("the exam loaded without the workspace")
	}
	code.Store(0)
	view, err := s.RetryLoad(t.Context())
	if err != nil || view.Workspace == nil || view.Phase != "exam" {
		t.Errorf("view %+v, err %v", view, err)
	}
}

func TestALockedOrEndedExamClosesTheWorkspaceConnection(t *testing.T) {
	f := newFixture(t)
	f.emptyExam = true
	f.workspace = newFakeWorkspace(t)
	s := workspaceClient(t, f, &recorder{})
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return v.Workspace != nil && v.Workspace.Connection == connConnected })
	f.mu.Lock()
	f.status.Locked = &Lock{Since: time.Now(), Reason: "away"}
	f.mu.Unlock()
	view := waitView(t, s, func(v View) bool { return v.Phase == "locked" })
	view = waitView(t, s, func(v View) bool { return v.Workspace != nil && v.Workspace.Connection != connConnected })
	if _, err := s.WorkspaceRequest(`{"type":"fs.list","path":""}`); err == nil {
		t.Error("a locked exam took a request")
	}
	f.mu.Lock()
	f.status.Locked = nil
	f.mu.Unlock()
	view = waitView(t, s, func(v View) bool {
		return v.Phase == "exam" && v.Workspace != nil && v.Workspace.Connection == connConnected
	})
	_ = view
	f.mu.Lock()
	f.status.State, f.status.SecondsRemaining = "submitted", 0
	f.mu.Unlock()
	waitView(t, s, func(v View) bool {
		return v.Phase == "complete" && v.Workspace != nil && v.Workspace.Connection == connIdle
	})
}

var _ = context.Background
