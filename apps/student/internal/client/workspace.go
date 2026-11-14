package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Workspace exams (docs/workspace/relay-protocol.md). The student's application owns the connection to the exam server: it holds the
// session token, dials the WebSocket, and hands the page what the server says through the view and two events. The page never opens
// a network connection of its own (decision P0-22), so the token, the reconnection and the lock all have this one path.

// WorkspaceTodo is one item of the exam's TODO list.
type WorkspaceTodo struct {
	ID       string   `json:"id"`
	TextHTML string   `json:"text_html"`
	Points   *float64 `json:"points,omitempty"`
}

// WorkspaceStatus is what the server last said about the student's workspace (ws.status).
type WorkspaceStatus struct {
	State          string     `json:"state"`
	LastSnapshotAt *time.Time `json:"last_snapshot_at,omitempty"`
	DeadlineAt     time.Time  `json:"deadline_at"`
	SecondsLeft    int64      `json:"seconds_left"`
	AgentRestarts  int        `json:"agent_restarts"`
	Terminal       bool       `json:"terminal"`
}

// WorkspaceView is the workspace as the page shows it: the exam's text, and where the connection to the server is.
type WorkspaceView struct {
	Title        string          `json:"title"`
	Instructions string          `json:"instructions"`
	Todos        []WorkspaceTodo `json:"todos"`
	// Done is what the student had ticked when the exam was loaded; the page keeps it from there.
	Done     map[string]bool `json:"done"`
	Terminal bool            `json:"terminal"`
	CanRun   bool            `json:"can_run"`
	CanCheck bool            `json:"can_check"`
	// Connection is idle, connecting, connected, reconnecting, locked, replaced, ended or closed. Note says why, for the last four.
	Connection string `json:"connection"`
	Note       string `json:"note,omitempty"`
	// Generation counts the connections made; the page lists its terminals again when it grows.
	Generation int              `json:"generation"`
	Status     *WorkspaceStatus `json:"status,omitempty"`
}

// workspaceReply is GET /workspace.
type workspaceReply struct {
	Kind      string `json:"kind"`
	ExamID    string `json:"exam_id"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Terminal  bool   `json:"terminal"`
	CanRun    bool   `json:"can_run"`
	CanCheck  bool   `json:"can_check"`
	Content   struct {
		Instructions string          `json:"instructions"`
		Todos        []WorkspaceTodo `json:"todos"`
	} `json:"content"`
	Done   map[string]bool `json:"done"`
	Status WorkspaceStatus `json:"status"`
}

// WorkspaceOptions are the hooks the application wires to its page.
type WorkspaceOptions struct {
	// OnOutput gets the bytes a terminal printed, in order and batched. OnMessage gets every text message of the server that is not the
	// answer to a request (events, status), as JSON, after the output that came before it.
	OnOutput  func(term uint32, data []byte)
	OnMessage func(message string)
	// CallTimeout is how long a request waits for its answer. Zero is 30 seconds.
	CallTimeout time.Duration
}

// The kinds of request the page may make. The server checks every one of them again; this is the first gate, not the only one.
var workspaceRequestTypes = []string{"fs.list", "fs.read", "fs.write", "fs.create", "fs.mkdir", "fs.rename", "fs.delete",
	"pty.open", "pty.attach", "pty.list", "pty.resize", "pty.close", "run.start", "todo.set", "submit"}

const (
	connIdle         = "idle"
	connConnecting   = "connecting"
	connConnected    = "connected"
	connReconnecting = "reconnecting"
	connLocked       = "locked"
	connReplaced     = "replaced"
	connEnded        = "ended"
	connClosed       = "closed"

	// workspaceReadLimit is the largest message the server may send: a file of 1 MiB with every control character escaped.
	workspaceReadLimit = 8 << 20
	// workspaceInputChunk is how much of a paste goes in one frame.
	workspaceInputChunk = 32 << 10
)

// workspaceLink is the application's connection to one workspace.
type workspaceLink struct {
	http               *http.Client
	opts               WorkspaceOptions
	retryMin, retryMax time.Duration
	parent             context.Context

	mu      sync.Mutex
	base    string
	token   string
	content WorkspaceView
	// allowed says whether the exam may be worked on now: the exam is shown and it is not locked. The connection exists only then.
	allowed bool
	// manual means the connection ended in a way that is not retried by itself (another window took the workspace over).
	manual  bool
	started bool
	cancel  context.CancelFunc
	wake    chan struct{}
	conn    *websocket.Conn
	state   string
	note    string
	gen     int
	status  *WorkspaceStatus
	pending map[uint64]chan []byte
	next    uint64

	writeMu sync.Mutex
}

func newWorkspaceLink(parent context.Context, client *http.Client, opts WorkspaceOptions, retryMin, retryMax time.Duration) *workspaceLink {
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = 30 * time.Second
	}
	return &workspaceLink{http: client, opts: opts, retryMin: retryMin, retryMax: retryMax, parent: parent, wake: make(chan struct{}, 1),
		state: connIdle, pending: map[uint64]chan []byte{}}
}

func (l *workspaceLink) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// configure is called when the exam is loaded (and again when it is loaded again): the text, the session and where to connect.
func (l *workspaceLink) configure(base, token string, reply *workspaceReply) {
	l.mu.Lock()
	l.base, l.token = base, token
	l.content = WorkspaceView{Title: reply.Title, Instructions: reply.Content.Instructions, Todos: slices.Clone(reply.Content.Todos), Done: reply.Done,
		Terminal: reply.Terminal, CanRun: reply.CanRun, CanCheck: reply.CanCheck}
	if l.content.Done == nil {
		l.content.Done = map[string]bool{}
	}
	status := reply.Status
	l.status = &status
	if !l.started {
		l.started = true
		ctx, cancel := context.WithCancel(l.parent)
		l.cancel = cancel
		go l.run(ctx)
	}
	l.mu.Unlock()
	l.signal()
}

// active says whether this is a workspace exam at all.
func (l *workspaceLink) active() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.started
}

// setAllowed opens or closes the connection to the state of the exam.
func (l *workspaceLink) setAllowed(allowed bool) {
	l.mu.Lock()
	changed := l.allowed != allowed
	l.allowed = allowed
	l.mu.Unlock()
	if changed {
		l.signal()
	}
}

func (l *workspaceLink) view() *WorkspaceView {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.started {
		return nil
	}
	v := l.content
	v.Connection, v.Note, v.Generation = l.state, l.note, l.gen
	if l.status != nil {
		status := *l.status
		v.Status = &status
	}
	return &v
}

func (l *workspaceLink) setState(state, note string) {
	l.mu.Lock()
	l.state, l.note = state, note
	l.mu.Unlock()
}

// close ends the connection for good (the application is closing).
func (l *workspaceLink) close() {
	l.mu.Lock()
	cancel, conn := l.cancel, l.conn
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.CloseNow()
	}
}

// reconnect is the student's answer to a connection that does not retry by itself.
func (l *workspaceLink) reconnect() error {
	l.mu.Lock()
	if !l.started {
		l.mu.Unlock()
		return errors.New("This exam has no workspace.")
	}
	l.manual = false
	l.mu.Unlock()
	l.signal()
	return nil
}

func websocketURL(base string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/workspace/ws"
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/workspace/ws"
	}
	return base + "/workspace/ws"
}

// run keeps the connection while the exam is open to work on, and waits while it is not.
func (l *workspaceLink) run(ctx context.Context) {
	backoff := l.retryMin
	everConnected := false
	for ctx.Err() == nil {
		l.mu.Lock()
		allowed, manual, base, token := l.allowed, l.manual, l.base, l.token
		l.mu.Unlock()
		if !allowed || manual {
			if !allowed && !manual {
				l.setState(connIdle, "")
			}
			l.wait(ctx, 0)
			continue
		}
		if everConnected {
			l.setState(connReconnecting, "")
		} else {
			l.setState(connConnecting, "")
		}
		dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
		conn, response, err := websocket.Dial(dialCtx, websocketURL(base), &websocket.DialOptions{HTTPClient: l.http, HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		cancelDial()
		if err != nil {
			status := 0
			if response != nil {
				status = response.StatusCode
			}
			switch status {
			case http.StatusUnauthorized:
				l.setState(connClosed, "This computer's session is no longer valid: the account was opened on another computer. Call your instructor.")
				l.holdUntilAsked()
			case http.StatusLocked:
				l.setState(connLocked, "Your exam is locked. Call your instructor.")
				l.wait(ctx, l.retryMax)
			case http.StatusGone, http.StatusNotFound, http.StatusForbidden:
				l.setState(connEnded, "The exam is over.")
				l.holdUntilAsked()
			default:
				l.wait(ctx, backoff)
				backoff = min(backoff*2, l.retryMax)
			}
			continue
		}
		conn.SetReadLimit(workspaceReadLimit)
		l.mu.Lock()
		l.conn, l.gen = conn, l.gen+1
		l.state, l.note = connConnected, ""
		l.mu.Unlock()
		everConnected = true
		began := time.Now()
		reason := l.serve(ctx, conn)
		l.mu.Lock()
		l.conn = nil
		l.failPendingLocked()
		l.mu.Unlock()
		_ = conn.CloseNow()
		l.mu.Lock()
		stillAllowed := l.allowed
		l.mu.Unlock()
		switch {
		case !stillAllowed:
			// The application itself closed it, for a lock, the end of the exam or its own shutdown: nothing to retry until it is open again.
			l.setState(connIdle, "")
		case strings.HasPrefix(reason, "replaced by a newer connection"):
			l.mu.Lock()
			l.manual = true
			l.mu.Unlock()
			l.setState(connReplaced, "This exam is open in another window, which took the workspace over. Close the other window, or take it back here.")
		case reason == "the exam is over":
			l.setState(connEnded, "The exam is over.")
			l.holdUntilAsked()
		case reason == "the exam is locked":
			l.setState(connLocked, "Your exam is locked. Call your instructor.")
			l.wait(ctx, l.retryMin)
		default:
			// A connection that lasted a while is not a reason to wait longer than the first time.
			if time.Since(began) > 5*time.Second {
				backoff = l.retryMin
			}
			l.setState(connReconnecting, "")
			l.wait(ctx, backoff)
			backoff = min(backoff*2, l.retryMax)
		}
	}
}

// holdUntilAsked waits for the page to ask again (Reconnect) or for the exam to change; used when retrying would be of no use.
func (l *workspaceLink) holdUntilAsked() {
	l.mu.Lock()
	l.manual = true
	l.mu.Unlock()
}

// wait sleeps for d (or until woken, when d is 0 it only waits to be woken, with a ceiling so state changes are never missed).
func (l *workspaceLink) wait(ctx context.Context, d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-l.wake:
	case <-timer.C:
	}
}

// serve reads the connection until it ends, and returns the reason the server closed it with, if it did.
func (l *workspaceLink) serve(ctx context.Context, conn *websocket.Conn) string {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The exam being locked, or ending, closes the connection from this side too, without waiting for the server to notice.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-readCtx.Done():
				return
			case <-ticker.C:
				l.mu.Lock()
				allowed := l.allowed
				l.mu.Unlock()
				if !allowed {
					_ = conn.CloseNow()
					cancel()
					return
				}
			}
		}
	}()
	out := newOutputBatcher(l.opts.OnOutput)
	defer out.flush()
	for {
		kind, data, err := conn.Read(readCtx)
		if err != nil {
			var closed websocket.CloseError
			if errors.As(err, &closed) {
				return closed.Reason
			}
			return ""
		}
		switch kind {
		case websocket.MessageBinary:
			if len(data) > 4 {
				out.add(binary.BigEndian.Uint32(data[:4]), data[4:])
			}
		case websocket.MessageText:
			l.text(data, out)
		}
	}
}

// text handles one message of the server: the answer to a request goes to whoever asked; the rest is told to the page.
func (l *workspaceLink) text(data []byte, out *outputBatcher) {
	var head struct {
		ID   uint64 `json:"id"`
		Type string `json:"type"`
		OK   *bool  `json:"ok"`
	}
	if json.Unmarshal(data, &head) != nil {
		return
	}
	if head.ID != 0 && head.OK != nil {
		l.mu.Lock()
		reply := l.pending[head.ID]
		delete(l.pending, head.ID)
		l.mu.Unlock()
		if reply != nil {
			reply <- bytes.Clone(data)
		}
		return
	}
	if head.Type == "ws.status" {
		var status WorkspaceStatus
		if json.Unmarshal(data, &status) == nil {
			l.mu.Lock()
			l.status = &status
			l.mu.Unlock()
		}
	}
	// What the page is told comes after the output that came before it: a program's last words, then its exit.
	out.flush()
	if l.opts.OnMessage != nil {
		l.opts.OnMessage(string(data))
	}
}

func (l *workspaceLink) failPendingLocked() {
	for id, reply := range l.pending {
		delete(l.pending, id)
		reply <- nil
	}
}

// call sends one request of the page and waits for its answer, which it returns as the server wrote it (an answer that says ok:false
// is still an answer). The id is the link's own.
func (l *workspaceLink) call(message string) (string, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal([]byte(message), &request); err != nil || request == nil {
		return "", errors.New("The workspace request is not a JSON object.")
	}
	var kind string
	if json.Unmarshal(request["type"], &kind) != nil || !slices.Contains(workspaceRequestTypes, kind) {
		return "", errors.New("The workspace does not take that kind of request.")
	}
	reply := make(chan []byte, 1)
	l.mu.Lock()
	conn := l.conn
	if conn == nil || l.state != connConnected {
		l.mu.Unlock()
		return "", errors.New("The workspace is not connected.")
	}
	l.next++
	id := l.next
	l.pending[id] = reply
	l.mu.Unlock()
	forget := func() {
		l.mu.Lock()
		delete(l.pending, id)
		l.mu.Unlock()
	}
	request["id"], _ = json.Marshal(id)
	payload, err := json.Marshal(request)
	if err != nil {
		forget()
		return "", errors.New("The workspace request cannot be sent.")
	}
	writeCtx, cancel := context.WithTimeout(l.parent, 10*time.Second)
	l.writeMu.Lock()
	err = conn.Write(writeCtx, websocket.MessageText, payload)
	l.writeMu.Unlock()
	cancel()
	if err != nil {
		forget()
		return "", errors.New("The workspace connection was lost; the request may not have been done.")
	}
	timer := time.NewTimer(l.opts.CallTimeout)
	defer timer.Stop()
	select {
	case answer := <-reply:
		if answer == nil {
			return "", errors.New("The workspace connection was lost; the request may or may not have been done.")
		}
		return string(answer), nil
	case <-timer.C:
		forget()
		return "", errors.New("The workspace did not answer.")
	case <-l.parent.Done():
		forget()
		return "", errors.New("The application is closing.")
	}
}

// input sends what the student typed or pasted to a terminal.
func (l *workspaceLink) input(term uint32, data string) error {
	l.mu.Lock()
	conn := l.conn
	connected := l.state == connConnected
	l.mu.Unlock()
	if conn == nil || !connected {
		return errors.New("The workspace is not connected.")
	}
	bytes := []byte(data)
	for len(bytes) > 0 {
		n := min(len(bytes), workspaceInputChunk)
		frame := binary.BigEndian.AppendUint32(make([]byte, 0, 4+n), term)
		frame = append(frame, bytes[:n]...)
		writeCtx, cancel := context.WithTimeout(l.parent, 5*time.Second)
		l.writeMu.Lock()
		err := conn.Write(writeCtx, websocket.MessageBinary, frame)
		l.writeMu.Unlock()
		cancel()
		if err != nil {
			return errors.New("The workspace connection was lost; the input may not have arrived.")
		}
		bytes = bytes[n:]
	}
	return nil
}

// outputBatcher joins a terminal's output into few, larger pieces: a program that prints fast would otherwise cost the page one event
// for every few bytes. Pieces go out in the order they came, and flush sends what is held at once.
type outputBatcher struct {
	emit   func(uint32, []byte)
	emitMu sync.Mutex
	mu     sync.Mutex
	held   map[uint32][]byte
	order  []uint32
	timer  *time.Timer
}

const (
	batchDelay = 12 * time.Millisecond
	batchLimit = 32 << 10
)

func newOutputBatcher(emit func(uint32, []byte)) *outputBatcher {
	return &outputBatcher{emit: emit, held: map[uint32][]byte{}}
}

func (b *outputBatcher) add(term uint32, data []byte) {
	if b.emit == nil {
		return
	}
	b.mu.Lock()
	if _, found := b.held[term]; !found {
		b.order = append(b.order, term)
	}
	b.held[term] = append(b.held[term], data...)
	big := len(b.held[term]) >= batchLimit
	if b.timer == nil && !big {
		b.timer = time.AfterFunc(batchDelay, b.flush)
	}
	b.mu.Unlock()
	if big {
		b.flush()
	}
}

func (b *outputBatcher) flush() {
	if b.emit == nil {
		return
	}
	b.emitMu.Lock()
	defer b.emitMu.Unlock()
	b.mu.Lock()
	held, order := b.held, b.order
	b.held, b.order = map[uint32][]byte{}, nil
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.mu.Unlock()
	for _, term := range order {
		b.emit(term, held[term])
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// What the Service offers the page.

// fetchWorkspace asks the server whether the session is a workspace session. It answers nil when the server says it is not (a question
// exam, or a server that has no workspace exams), and an error when it cannot tell.
func (s *Service) fetchWorkspace(ctx context.Context, base, token string) (*workspaceReply, error) {
	var reply workspaceReply
	err := requestJSON(ctx, s.http, base, token, http.MethodGet, "/workspace", nil, &reply)
	if err != nil {
		if failureCode(err) == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if reply.Kind != "workspace" || reply.Title == "" {
		return nil, errors.New("Workspace response is invalid. Retry loading the session.")
	}
	return &reply, nil
}

// syncWorkspace opens or closes the connection to what the exam is doing now. The caller holds the service's lock.
func (s *Service) syncWorkspace() {
	s.workspace.setAllowed(s.phase == "exam" && !s.examLocked())
}

// WorkspaceRequest sends one request of the workspace (a file operation, a terminal, Run or Check, a TODO tick, Submit) and returns the
// server's answer as JSON. An answer that says ok:false is a value, not an error; an error means the request did not get an answer.
func (s *Service) WorkspaceRequest(message string) (string, error) {
	return s.workspace.call(message)
}

// WorkspaceInput sends what the student typed or pasted to a terminal.
func (s *Service) WorkspaceInput(term int, data string) error {
	if term < 1 || term > 1<<31 {
		return errors.New("The terminal number is invalid.")
	}
	return s.workspace.input(uint32(term), data)
}

// WorkspaceReconnect connects again after a connection that does not retry by itself, such as one another window took over.
func (s *Service) WorkspaceReconnect() error {
	return s.workspace.reconnect()
}
