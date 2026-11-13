package workspace

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"exam-platform/internal/agentproto"
	"github.com/coder/websocket"
)

// relay is one student's WebSocket and everything behind it. It is the only thing the student's application talks to: it checks every
// message the student sends against the protocol, the exam's settings and the student's rate limits before the agent sees it, and it
// takes everything the agent says for hostile input, because the agent runs as the student.
type relay struct {
	m       *Manager
	session string
	conn    *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc

	control, controlBytes, input, output *bucket

	mu       sync.Mutex
	agent    net.Conn
	writeMu  sync.Mutex
	notified chan struct{}
	exam     ExamRecord
	closeMsg string
}

func (m *Manager) register(r *relay) {
	m.mu.Lock()
	old := m.relays[r.session]
	m.relays[r.session] = r
	m.mu.Unlock()
	if old != nil {
		old.close("replaced by a newer connection of the same student")
	}
}

func (m *Manager) unregister(r *relay) {
	m.mu.Lock()
	if m.relays[r.session] == r {
		delete(m.relays, r.session)
	}
	m.mu.Unlock()
}

func (m *Manager) closeRelay(session, reason string) {
	m.mu.Lock()
	r := m.relays[session]
	m.mu.Unlock()
	if r != nil {
		r.close(reason)
	}
}

func (m *Manager) notifyRestart(session string) {
	m.mu.Lock()
	r := m.relays[session]
	m.mu.Unlock()
	if r != nil {
		select {
		case r.notified <- struct{}{}:
		default:
		}
	}
}

func (r *relay) close(reason string) {
	r.mu.Lock()
	r.closeMsg = reason
	agent := r.agent
	r.mu.Unlock()
	if agent != nil {
		agent.Close()
	}
	r.cancel()
}

// Serve is the student's WebSocket: GET /workspace/ws, authenticated by the caller with the session token. It does not return until the
// connection ends.
func (m *Manager) Serve(w http.ResponseWriter, req *http.Request, session string) {
	info, err := m.opts.Repo.Session(req.Context(), session)
	if err != nil {
		http.Error(w, "this session has no workspace", http.StatusNotFound)
		return
	}
	exam, err := m.opts.Repo.Exam(req.Context(), info.ExamID)
	if err != nil {
		http.Error(w, "the exam cannot be read", http.StatusInternalServerError)
		return
	}
	// Browsers are not the client: it is the student's own application, which authenticates with a bearer token, not a cookie, so there
	// is no origin to check.
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	conn.SetReadLimit(MaxClientMessage)
	ctx, cancel := context.WithCancel(context.Background())
	r := &relay{m: m, session: session, conn: conn, ctx: ctx, cancel: cancel, exam: exam, notified: make(chan struct{}, 1),
		control: newBucket(controlRate, controlBurst), controlBytes: newBucket(controlBytesRate, controlBytesBurst), input: newBucket(inputRate, inputBurst), output: newBucket(outputRate, outputBurst)}
	m.register(r)
	defer m.unregister(r)
	r.run()
}

func (r *relay) run() {
	defer r.cancel()
	go r.statusLoop()
	go r.keepalive()
	// The client's side is read for as long as the connection lives; the agent's side comes and goes (a container that restarts).
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		r.fromClient()
	}()
	for r.ctx.Err() == nil {
		if !r.attach() {
			break
		}
	}
	reason := "closed"
	r.mu.Lock()
	if r.closeMsg != "" {
		reason = r.closeMsg
	}
	r.mu.Unlock()
	_ = r.conn.Close(websocket.StatusNormalClosure, truncate(reason, 120))
	<-clientDone
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// attach makes sure the container is up, connects to its agent and relays until that connection ends. It says whether to go on.
func (r *relay) attach() bool {
	if _, err := r.m.Ensure(r.ctx, r.session); err != nil {
		if errors.Is(err, ErrOver) {
			r.sendStatus()
			r.close("the exam is over")
			return false
		}
		r.sendStatus()
		select {
		case <-r.ctx.Done():
		case <-time.After(2 * time.Second):
		}
		return true
	}
	agent, err := r.m.opts.Runner.ConnectAgent(r.ctx, r.session)
	if err != nil {
		select {
		case <-r.ctx.Done():
		case <-time.After(time.Second):
		}
		return true
	}
	r.mu.Lock()
	r.agent = agent
	r.mu.Unlock()
	r.sendStatus()
	r.fromAgent(agent)
	agent.Close()
	r.mu.Lock()
	r.agent = nil
	r.mu.Unlock()
	return true
}

// sendJSON sends a message to the student as a text message.
func (r *relay) sendJSON(value any) {
	payload, err := agentproto.Encode(value)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
	defer cancel()
	if err := r.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		r.cancel()
	}
}

// statusMessage is ws.status: the container's state, when the last snapshot was, and the time left.
type statusMessage struct {
	Type string `json:"type"`
	ClientStatus
}

func (r *relay) sendStatus() {
	status, err := r.m.Status(r.ctx, r.session)
	if err != nil {
		return
	}
	r.sendJSON(statusMessage{Type: "ws.status", ClientStatus: status})
}

func (r *relay) statusLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.notified:
			r.sendJSON(struct {
				Type string `json:"type"`
			}{"ws.agent_restarted"})
			r.sendStatus()
		case <-ticker.C:
			r.sendStatus()
		}
	}
}

// keepalive pings the student's application, so a connection that was cut without a word does not live for ever.
func (r *relay) keepalive() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
			err := r.conn.Ping(ctx)
			cancel()
			if err != nil {
				r.close("the connection stopped answering")
				return
			}
		}
	}
}

// serverReply is a reply the server itself makes, in the shape of the agent's.
type serverReply struct {
	ID       uint64 `json:"id,omitempty"`
	Type     string `json:"type"`
	OK       bool   `json:"ok"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	Snapshot int64  `json:"snapshot,omitempty"`
}

func (r *relay) refuse(id uint64, kind, code, message string) {
	r.sendJSON(serverReply{ID: id, Type: kind, Code: code, Message: message})
}

// agentWait is how long a message from the student waits for the agent's connection to be there: the student's application opens the
// WebSocket and may speak at once, while the container is still starting or restarting.
const agentWait = 8 * time.Second

// currentAgent returns the connection to the agent, waiting for it a while if there is none yet.
func (r *relay) currentAgent() net.Conn {
	deadline := time.Now().Add(agentWait)
	for {
		r.mu.Lock()
		agent := r.agent
		r.mu.Unlock()
		if agent != nil {
			return agent
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-r.ctx.Done():
			return nil
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// toAgent writes one frame to the agent, once there is one.
func (r *relay) toAgent(frame agentproto.Frame) bool {
	agent := r.currentAgent()
	if agent == nil {
		return false
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_ = agent.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return agentproto.WriteFrame(agent, frame) == nil
}

// fromClient reads what the student sends until the connection ends.
func (r *relay) fromClient() {
	defer r.cancel()
	for {
		kind, data, err := r.conn.Read(r.ctx)
		if err != nil {
			return
		}
		switch kind {
		case websocket.MessageBinary:
			r.keystrokes(data)
		case websocket.MessageText:
			r.message(data)
		}
	}
}

// keystrokes handles a binary message: a 4-byte terminal ID and the bytes the student typed.
func (r *relay) keystrokes(data []byte) {
	if len(data) < 5 || len(data)-4 > agentproto.MaxTerminalData {
		r.close("a terminal message was not a terminal ID and bytes")
		return
	}
	if !r.input.allow(float64(len(data))) {
		r.refuse(0, "agent.error", agentproto.CodeRate, "too much input too fast; it was dropped")
		return
	}
	if !r.toAgent(agentproto.Frame{Kind: agentproto.KindBinary, Term: binary.BigEndian.Uint32(data[:4]), Data: data[4:]}) {
		r.refuse(0, "agent.error", agentproto.CodeUnavailable, "the workspace is not ready")
	}
}

type header struct {
	ID   uint64 `json:"id"`
	Type string `json:"type"`
}

// message handles a text message from the student: the server's own messages here, and everything else only after it has passed the
// protocol's own decoder, which refuses an unknown type, an unknown field or a path that leaves /work.
func (r *relay) message(data []byte) {
	var h header
	if err := json.Unmarshal(data, &h); err != nil {
		r.refuse(0, "ws.error", agentproto.CodeBadRequest, "the message is not a JSON object with a type")
		return
	}
	if !r.control.allow(1) || !r.controlBytes.allow(float64(len(data))) {
		r.refuse(h.ID, h.Type, agentproto.CodeRate, "too many requests; slow down")
		return
	}
	switch h.Type {
	case "run.start":
		r.runStart(h, data)
	case "todo.set":
		r.todoSet(h, data)
	case "submit":
		r.submit(h, data)
	default:
		r.forward(h, data)
	}
}

func strict(data []byte, into any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("data after the object")
	}
	return nil
}

func (r *relay) forward(h header, data []byte) {
	request, err := agentproto.DecodeRequest(data)
	if err != nil {
		var e *agentproto.Error
		code, message := agentproto.CodeBadRequest, err.Error()
		if errors.As(err, &e) {
			code, message = e.Code, e.Message
		}
		r.refuse(h.ID, h.Type, code, message)
		return
	}
	if open, isOpen := request.(*agentproto.PTYOpen); isOpen {
		if open.Cmd != "" {
			r.refuse(h.ID, h.Type, agentproto.CodeDenied, "programs are started with run.start")
			return
		}
		if !r.exam.Document.Workspace.Terminal {
			r.refuse(h.ID, h.Type, agentproto.CodeDenied, "this exam has no terminal; use the Run and Check buttons")
			return
		}
	}
	payload, err := agentproto.Encode(request)
	if err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeInternal, "the request cannot be sent")
		return
	}
	if !r.toAgent(agentproto.Frame{Kind: agentproto.KindJSON, Data: payload}) {
		r.refuse(h.ID, h.Type, agentproto.CodeUnavailable, "the workspace is not ready; try again in a moment")
	}
}

// runStart is the Run and Check buttons: a terminal that runs the command the exam names. The command comes from the exam, never from
// the student.
func (r *relay) runStart(h header, data []byte) {
	var request struct {
		ID    uint64 `json:"id"`
		Type  string `json:"type"`
		Which string `json:"which"`
		Cols  int    `json:"cols"`
		Rows  int    `json:"rows"`
	}
	if err := strict(data, &request); err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeBadRequest, "run.start takes which (run or check), cols and rows")
		return
	}
	var command string
	switch request.Which {
	case "run":
		command = r.exam.Document.Workspace.Run
	case "check":
		command = r.exam.Document.Workspace.Check
	default:
		r.refuse(h.ID, h.Type, agentproto.CodeBadRequest, "which must be run or check")
		return
	}
	if command == "" {
		r.refuse(h.ID, h.Type, agentproto.CodeNotFound, "this exam has no "+request.Which+" command")
		return
	}
	if request.Cols == 0 {
		request.Cols = 80
	}
	if request.Rows == 0 {
		request.Rows = 24
	}
	open := &agentproto.PTYOpen{Header: agentproto.Header{ID: h.ID, Type: agentproto.TypePTYOpen}, Cols: request.Cols, Rows: request.Rows, Cmd: command}
	if err := open.Validate(); err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeBadRequest, err.Error())
		return
	}
	payload, _ := agentproto.Encode(open)
	if !r.toAgent(agentproto.Frame{Kind: agentproto.KindJSON, Data: payload}) {
		r.refuse(h.ID, h.Type, agentproto.CodeUnavailable, "the workspace is not ready; try again in a moment")
	}
}

// todoSet records what the student ticked. It is self-reported progress, and only that.
func (r *relay) todoSet(h header, data []byte) {
	var request struct {
		ID   uint64 `json:"id"`
		Type string `json:"type"`
		Item string `json:"item"`
		Done bool   `json:"done"`
	}
	if err := strict(data, &request); err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeBadRequest, "todo.set takes item (a TODO ID) and done")
		return
	}
	found := false
	for _, todo := range r.exam.Document.Content.Todos {
		found = found || todo.ID == request.Item
	}
	if !found {
		r.refuse(h.ID, h.Type, agentproto.CodeNotFound, "the exam has no TODO item "+truncate(request.Item, 40))
		return
	}
	todo, err := r.m.opts.Repo.Todo(r.ctx, r.session)
	if err == nil {
		todo[request.Item] = request.Done
		err = r.m.opts.Repo.SetTodo(r.ctx, r.session, todo)
	}
	if err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeInternal, "the TODO state could not be saved")
		return
	}
	r.sendJSON(serverReply{ID: h.ID, Type: h.Type, OK: true})
}

// submit takes a snapshot and records it as the submission. (Phase 5 gives it its full meaning: filtering by submit and ignore, and the
// final submission at the deadline.)
func (r *relay) submit(h header, data []byte) {
	var request struct {
		ID   uint64 `json:"id"`
		Type string `json:"type"`
	}
	if err := strict(data, &request); err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeBadRequest, "submit takes no fields")
		return
	}
	record, _, err := r.m.Snapshots.Take(r.ctx, r.session, ReasonSubmit)
	if err != nil {
		r.refuse(h.ID, h.Type, agentproto.CodeUnavailable, "the snapshot could not be taken: "+err.Error())
		return
	}
	r.sendJSON(serverReply{ID: h.ID, Type: h.Type, OK: true, Snapshot: record.ID})
}

// fromAgent relays what the agent says to the student until the agent's connection ends. Everything is checked on the way: a frame
// that is not one ends the connection; a message is decoded and sent again from what was decoded, never as the bytes the agent
// sent; only replies and the events the student's application knows are passed on; terminal output is held to the output limit.
func (r *relay) fromAgent(agent net.Conn) {
	for {
		frame, err := agentproto.ReadFrame(agent)
		if err != nil {
			return
		}
		switch frame.Kind {
		case agentproto.KindBinary:
			if err := r.output.wait(r.ctx, float64(len(frame.Data))); err != nil {
				return
			}
			out := make([]byte, 4, 4+len(frame.Data))
			binary.BigEndian.PutUint32(out, frame.Term)
			ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
			err := r.conn.Write(ctx, websocket.MessageBinary, append(out, frame.Data...))
			cancel()
			if err != nil {
				r.cancel()
				return
			}
		case agentproto.KindJSON:
			message, err := agentproto.DecodeMessage(frame.Data)
			if err != nil {
				continue
			}
			passes := message.OK != nil
			switch message.Type {
			case agentproto.TypeFSEvent, agentproto.TypePTYExit, agentproto.TypeSessionErr:
				passes = true
			}
			if !passes {
				continue
			}
			// A message from the agent counts against the output limit too, so an agent that floods events is slowed down.
			if err := r.output.wait(r.ctx, float64(len(frame.Data))); err != nil {
				return
			}
			r.sendJSON(message)
		}
	}
}
