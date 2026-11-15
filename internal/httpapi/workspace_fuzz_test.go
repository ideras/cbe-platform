//go:build linux

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"exam-platform/internal/agentproto"
	"exam-platform/internal/workspace"
)

// Phase 8 fuzzing of the two places where the exam server reads what a student controls: the WebSocket the student's application opens (everything
// the student types reaches the relay there) and the agent's connection (the agent runs as the student, so whatever it says is hostile input too).
// Each target runs the real relay, over a real WebSocket and the database, with the real agent behind the first and a hostile one behind the second.
// Plain `go test` runs the seeds; the Phase 8 acceptance runs each for five minutes:
//
//	go test ./internal/httpapi -run '^$' -fuzz FuzzStudentMessages -fuzztime 5m
//	go test ./internal/httpapi -run '^$' -fuzz FuzzHostileAgent -fuzztime 5m
//
// The exam of these fixtures has no terminal (Terminal: false), so a program a fuzzer's input starts is only the exam's own run command, `echo
// running`: no input can reach a shell on the machine that runs the fuzzer.

// fuzzLink is a student's WebSocket for a fuzz target: it writes what the fuzzer says and checks everything the server answers.
type fuzzLink struct {
	conn    *websocket.Conn
	closed  chan struct{}
	mu      sync.Mutex
	texts   int
	binary  int
	replies map[uint64]bool
	problem error
}

func dialFuzz(base, token string, check func(kind websocket.MessageType, data []byte) error) (*fuzzLink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/workspace/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(16 << 20)
	l := &fuzzLink{conn: conn, closed: make(chan struct{}), replies: map[uint64]bool{}}
	go func() {
		defer close(l.closed)
		for {
			kind, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			l.mu.Lock()
			if kind == websocket.MessageText {
				l.texts++
				var head struct {
					ID uint64 `json:"id"`
					OK *bool  `json:"ok"`
				}
				if json.Unmarshal(data, &head) == nil && head.ID != 0 && head.OK != nil {
					l.replies[head.ID] = true
				}
			} else {
				l.binary++
			}
			if err := check(kind, data); err != nil && l.problem == nil {
				l.problem = err
			}
			l.mu.Unlock()
		}
	}()
	return l, nil
}

func (l *fuzzLink) write(kind websocket.MessageType, data []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return l.conn.Write(ctx, kind, data) == nil
}

func (l *fuzzLink) problemSoFar() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.problem
}

// waitReply waits for the answer to a request, or for the connection to end (the server may close it on a message it cannot take).
func (l *fuzzLink) waitReply(id uint64, within time.Duration) (answered, ended bool) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		got := l.replies[id]
		l.mu.Unlock()
		if got {
			return true, false
		}
		select {
		case <-l.closed:
			return false, true
		case <-time.After(time.Millisecond):
		}
	}
	return false, false
}

func (l *fuzzLink) close() { _ = l.conn.CloseNow() }

// checkServerMessage is what every message the server sends to the student's application must be: text that is a JSON object with a type, or a
// terminal's bytes with the terminal's number in front.
func checkServerMessage(kind websocket.MessageType, data []byte) error {
	if kind == websocket.MessageBinary {
		if len(data) < 4 {
			return fmt.Errorf("a binary message of %d bytes has no terminal number", len(data))
		}
		return nil
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return fmt.Errorf("the server sent text that is not a JSON object: %.200q", data)
	}
	var kindName string
	if json.Unmarshal(message["type"], &kindName) != nil {
		return fmt.Errorf("the server sent a message with no type: %.200q", data)
	}
	return nil
}

func fuzzFixture(f *testing.F) (*workspaceFixture, string) {
	fixture := newWorkspaceFixtureWith(f, func(d *workspace.Document) { d.Workspace.Terminal = false }, fixtureOptions{controlRate: 1e6})
	login := fixture.loginReady(f, "0012")
	return fixture, login.SessionToken
}

// FuzzStudentMessages writes whatever the fuzzer invents to the relay as a text message, or (when the first byte says so) as a binary one, and then a
// request of its own, to see that the relay is still answering. The relay may refuse a message, or end the connection (a terminal message that is
// not a terminal number and bytes does that); it must never answer in a way the application cannot read, never stop answering, and never fail.
func FuzzStudentMessages(f *testing.F) {
	for _, seed := range []string{
		`{"id":1,"type":"fs.list","path":""}`, `{"id":2,"type":"fs.read","path":"src/main.c"}`, `{"id":3,"type":"fs.write","path":"notes.txt","content":"x","base_version":""}`,
		`{"id":4,"type":"fs.rename","path":"notes.txt","to":"n.txt"}`, `{"id":5,"type":"fs.delete","path":"scripts"}`, `{"id":6,"type":"pty.open","cols":80,"rows":24}`,
		`{"id":7,"type":"pty.open","cols":80,"rows":24,"cmd":"sh"}`, `{"id":8,"type":"run.start","which":"run","cols":80,"rows":24}`, `{"id":9,"type":"run.start","which":"check"}`,
		`{"id":10,"type":"todo.set","item":"t1","done":true}`, `{"id":11,"type":"todo.set","item":"nope","done":true}`, `{"id":12,"type":"submit"}`,
		`{"id":13,"type":"paste.blocked","target":"editor","bytes":30}`, `{"id":14,"type":"paste.blocked","target":"x","bytes":-1}`, `{"id":15,"type":"fs.read","path":"../../etc/passwd"}`,
		`{"id":16,"type":"run.start","which":"run","extra":1}`, `{"type":"ws.status"}`, `{"type":"agent.hello"}`, `[]`, `null`, `"x"`, `{}`, `{"id":"1","type":2}`,
		`{"id":18446744073709551615,"type":"submit"}`, "\xef\xbb\xbf{}", `{"type":"fs.read","path":"` + "\xff" + `"}`,
	} {
		f.Add(byte(1), []byte(seed))
	}
	f.Add(byte(0), []byte{0, 0, 0, 1, 'l', 's', '\n'})
	f.Add(byte(0), []byte{0, 0, 0})
	f.Add(byte(0), []byte{0, 0, 0, 0})
	fixture, token := fuzzFixture(f)
	var link *fuzzLink
	var next atomic.Uint64
	next.Store(1 << 40)
	dial := func() *fuzzLink {
		l, err := dialFuzz(fixture.server.URL, token, checkServerMessage)
		if err != nil {
			return nil
		}
		return l
	}
	f.Cleanup(func() {
		if link != nil {
			link.close()
		}
	})
	f.Fuzz(func(t *testing.T, kind byte, payload []byte) {
		if len(payload) > 256<<10 {
			return
		}
		if link == nil {
			if link = dial(); link == nil {
				t.Fatal("the server does not take a connection")
			}
		}
		messageKind := websocket.MessageText
		if kind%4 == 0 {
			messageKind = websocket.MessageBinary
		}
		if !link.write(messageKind, payload) {
			link.close()
			link = nil
			return
		}
		// The relay handles one message after the other, so the answer to this request comes after whatever the message made it do.
		id := next.Add(1)
		sentinel, _ := json.Marshal(map[string]any{"id": id, "type": "fs.list", "path": ""})
		if !link.write(websocket.MessageText, sentinel) {
			link.close()
			link = nil
			return
		}
		answered, ended := link.waitReply(id, 15*time.Second)
		if err := link.problemSoFar(); err != nil {
			t.Fatalf("after %q: %v", payload, err)
		}
		switch {
		case ended:
			// The server ended the connection: a legal answer to a message it cannot take.
			link.close()
			link = nil
		case !answered:
			t.Fatalf("the relay stopped answering after %q", payload)
		}
	})
}

// hostileAgents hands the relay a connection to the fuzzer's agent each time it asks for one.
type hostileAgents struct {
	conns chan net.Conn
}

func (h *hostileAgents) connect(ctx context.Context, _ string) (net.Conn, error) {
	select {
	case conn := <-h.conns:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// FuzzHostileAgent is an agent that says whatever the fuzzer invents, as a stream of bytes (frames, half a frame, a frame that is too large). What
// the student's application gets from it must be what the relay made of it: text that is a JSON message of the protocol (a reply, or one of the
// few events passed on) and terminal bytes, never the agent's own bytes; the relay must not fail, and must go on serving the student.
func FuzzHostileAgent(f *testing.F) {
	frame := func(kind byte, term uint32, data string) []byte {
		var b bytes.Buffer
		_ = agentproto.WriteFrame(&b, agentproto.Frame{Kind: kind, Term: term, Data: []byte(data)})
		return b.Bytes()
	}
	for _, seed := range [][]byte{
		frame(agentproto.KindJSON, 0, `{"id":1,"type":"fs.list","ok":true,"entries":[]}`),
		frame(agentproto.KindJSON, 0, `{"type":"fs.event","path":"a","op":"create"}`),
		frame(agentproto.KindJSON, 0, `{"type":"pty.exit","term":1,"exit_code":0}`),
		frame(agentproto.KindJSON, 0, `{"type":"agent.error","code":"INTERNAL","message":"x"}`),
		frame(agentproto.KindJSON, 0, `{"type":"agent.hello","proto":1}`),
		frame(agentproto.KindJSON, 0, `{"id":2,"type":"fs.write","ok":true}`),
		frame(agentproto.KindJSON, 0, `{"id":3,"type":"pty.open","ok":true,"term":9}`),
		frame(agentproto.KindJSON, 0, `{"id":3,"type":"x","ok":false,"code":"NOPE","message":"<script>"}`),
		frame(agentproto.KindBinary, 1, "output\r\n"),
		frame(agentproto.KindBinary, 4294967295, ""),
		frame(agentproto.KindJSON, 0, `not json`),
		{0, 0, 0, 0}, {0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 3, 2, 0, 1},
		append(frame(agentproto.KindBinary, 2, "a"), frame(agentproto.KindJSON, 0, `{"type":"fs.event"}`)...),
	} {
		f.Add(seed)
	}
	agents := &hostileAgents{conns: make(chan net.Conn)}
	fixture, token := fuzzFixture(f)
	// From here on the relay is given the fuzzer's agent (the container is up, so nothing else is waiting for the real one).
	fixture.runner.SetConnectHook(agents.connect)
	f.Cleanup(func() { fixture.runner.SetConnectHook(nil) })
	var protocolTypes = map[string]bool{agentproto.TypeFSEvent: true, agentproto.TypePTYExit: true, agentproto.TypeSessionErr: true, "ws.status": true, "ws.agent_restarted": true}
	check := func(kind websocket.MessageType, data []byte) error {
		if err := checkServerMessage(kind, data); err != nil {
			return err
		}
		if kind == websocket.MessageBinary {
			return nil
		}
		// A message the agent made must have been decoded and encoded again by the relay: only fields of the protocol, and (unless it is a
		// reply) only the types the application knows.
		var message agentproto.Message
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&message); err != nil {
			var status map[string]any
			if json.Unmarshal(data, &status) == nil && (status["type"] == "ws.status" || status["type"] == "ws.agent_restarted") {
				return nil
			}
			return fmt.Errorf("a message that is not of the protocol reached the student: %.200q (%v)", data, err)
		}
		if message.OK == nil && !protocolTypes[message.Type] {
			return fmt.Errorf("an event the application does not know reached the student: %.200q", data)
		}
		return nil
	}
	link, err := dialFuzz(fixture.server.URL, token, check)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(link.close)
	f.Fuzz(func(t *testing.T, stream []byte) {
		if len(stream) > 1<<20 {
			return
		}
		if err := link.problemSoFar(); err != nil {
			t.Fatal(err)
		}
		server, agent := net.Pipe()
		select {
		case agents.conns <- server:
		case <-time.After(20 * time.Second):
			server.Close()
			agent.Close()
			t.Fatal("the relay does not ask for the agent's connection")
		}
		// The relay reads until the stream ends; a stream that ends in the middle of a frame is what the relay sees when an agent dies.
		_ = agent.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, _ = agent.Write(stream)
		_ = agent.Close()
		// The relay must have noticed the end (it asks for the next connection) and must still serve the student.
		select {
		case <-link.closed:
			t.Fatalf("the student's connection ended after the agent said %q", stream)
		case <-time.After(5 * time.Millisecond):
		}
		if err := link.problemSoFar(); err != nil {
			t.Fatalf("after the agent said %q: %v", stream, err)
		}
	})
}
