//go:build linux

package agent

import (
	"encoding/binary"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/agentproto"
)

func TestHelloHeartbeatAndPing(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	hello := event(t, c, "agent.hello", ofType(agentproto.TypeHello))
	if hello.Proto != agentproto.ProtocolVersion || hello.Work != r.work || hello.PID1 || hello.AgentVersion != "dev" {
		t.Errorf("hello = %+v", hello)
	}
	first := event(t, c, "a heartbeat", ofType(agentproto.TypeHeartbeat))
	second := event(t, c, "another heartbeat", ofType(agentproto.TypeHeartbeat))
	if second.Seq <= first.Seq || second.UptimeMS < first.UptimeMS || second.Connections < 1 || second.Watches < 1 {
		t.Errorf("heartbeats = %+v, %+v", first, second)
	}
	reply := do(t, c, &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
	if reply.Type != agentproto.TypePing || reply.OK == nil || !*reply.OK {
		t.Errorf("ping = %+v", reply)
	}
}

func TestFileRequestsThroughTheSocket(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	h := func(kind string) agentproto.Header { return agentproto.Header{Type: kind} }
	do(t, c, &agentproto.FSMkdir{Header: h(agentproto.TypeFSMkdir), Path: "src"})
	created := do(t, c, &agentproto.FSCreate{Header: h(agentproto.TypeFSCreate), Path: "src/a.c", Content: "int a;\n"})
	read := do(t, c, &agentproto.FSRead{Header: h(agentproto.TypeFSRead), Path: "src/a.c"})
	if read.Content == nil || *read.Content != "int a;\n" || read.Version != created.Version || read.Size != 7 {
		t.Fatalf("read = %+v", read)
	}
	wrote := do(t, c, &agentproto.FSWrite{Header: h(agentproto.TypeFSWrite), Path: "src/a.c", Content: "int b;\n", BaseVersion: read.Version})
	conflict := doFails(t, c, &agentproto.FSWrite{Header: h(agentproto.TypeFSWrite), Path: "src/a.c", Content: "stale", BaseVersion: read.Version}, agentproto.CodeConflict)
	if conflict.Version != wrote.Version {
		t.Errorf("the conflict says the version is %q, want %q", conflict.Version, wrote.Version)
	}
	list := do(t, c, &agentproto.FSList{Header: h(agentproto.TypeFSList), Path: "src"})
	if len(list.Entries) != 1 || list.Entries[0].Name != "a.c" {
		t.Errorf("list = %+v", list.Entries)
	}
	do(t, c, &agentproto.FSRename{Header: h(agentproto.TypeFSRename), Path: "src/a.c", To: "src/b.c"})
	do(t, c, &agentproto.FSDelete{Header: h(agentproto.TypeFSDelete), Path: "src"})
	doFails(t, c, &agentproto.FSRead{Header: h(agentproto.TypeFSRead), Path: "src/b.c"}, agentproto.CodeNotFound)
	doFails(t, c, &agentproto.FSRead{Header: h(agentproto.TypeFSRead), Path: "../../etc/passwd"}, agentproto.CodeBadPath)
}

// Requests sent without waiting for each answer are carried out in the order they were sent.
func TestPipelinedRequestsKeepTheirOrder(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	payloads := []string{
		`{"id":1,"type":"fs.mkdir","path":"d"}`,
		`{"id":2,"type":"fs.create","path":"d/f","content":"one"}`,
		`{"id":3,"type":"fs.read","path":"d/f"}`,
		`{"id":4,"type":"fs.rename","path":"d/f","to":"d/g"}`,
		`{"id":5,"type":"fs.read","path":"d/f"}`,
		`{"id":6,"type":"fs.read","path":"d/g"}`,
		`{"id":7,"type":"fs.delete","path":"d"}`,
		`{"id":8,"type":"fs.list","path":""}`,
	}
	replies := map[uint64]chan agentproto.Message{}
	_ = replies
	for _, payload := range payloads {
		if err := c.SendRaw([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	// The client matches replies to requests it made itself, so these arrive as events; collect them.
	got := map[uint64]agentproto.Message{}
	for len(got) < len(payloads) {
		m := event(t, c, "the replies", func(m agentproto.Message) bool { return m.ID != 0 })
		got[m.ID] = m
	}
	want := map[uint64]bool{1: true, 2: true, 3: true, 4: true, 5: false, 6: true, 7: true, 8: true}
	for id, ok := range want {
		if (got[id].OK != nil && *got[id].OK) != ok {
			t.Errorf("request %d: %+v", id, got[id])
		}
	}
	if got[5].Code != agentproto.CodeNotFound || got[3].Content == nil || *got[3].Content != "one" || len(got[8].Entries) != 0 {
		t.Errorf("replies = %+v", got)
	}
}

func TestBadRequestsGetAnErrorAndTheConnectionLives(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	for _, payload := range []string{`not json`, `{"type":"fs.chmod","id":5}`, `{"type":"fs.read","id":6,"path":"/etc/passwd"}`, `{"type":"fs.read","id":7,"path":"a","extra":1}`, `[]`, `{"id":8}`} {
		if err := c.SendRaw([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		m := event(t, c, "an error for "+payload, func(m agentproto.Message) bool { return m.OK != nil })
		if *m.OK || (m.Code != agentproto.CodeBadRequest && m.Code != agentproto.CodeBadPath) || m.Message == "" {
			t.Errorf("%s: reply = %+v", payload, m)
		}
	}
	// The ID of a request that was refused is in its reply, when it could be read.
	if err := c.SendRaw([]byte(`{"id":99,"type":"fs.read","path":"../x"}`)); err != nil {
		t.Fatal(err)
	}
	if m := event(t, c, "reply 99", func(m agentproto.Message) bool { return m.ID == 99 }); m.Type != agentproto.TypeFSRead || m.Code != agentproto.CodeBadPath {
		t.Errorf("reply = %+v", m)
	}
	do(t, c, &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
}

func TestAMalformedFrameClosesTheConnection(t *testing.T) {
	r := startAgent(t, nil)
	for name, bytes := range map[string][]byte{
		"empty frame": {0, 0, 0, 0}, "huge": {0xff, 0xff, 0xff, 0xff}, "unknown kind": {0, 0, 0, 2, 9, 'x'},
		"terminal frame without id": binary.BigEndian.AppendUint32(nil, 3)[:4], // cut short on purpose below
	} {
		conn, err := net.Dial("unix", r.socket)
		if err != nil {
			t.Fatal(err)
		}
		if name == "terminal frame without id" {
			bytes = append(bytes, agentproto.KindBinary, 0, 1)
		}
		_, _ = conn.Write(bytes)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			// Whatever the agent had to say first (its hello), the connection ends.
			if _, err := agentproto.ReadFrame(conn); err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					t.Errorf("%s: the connection was not closed", name)
				}
				break
			}
		}
		conn.Close()
	}
	// The agent is fine.
	do(t, r.dial(t), &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
}

func TestOnlyAsManyConnectionsAsConfigured(t *testing.T) {
	r := startAgent(t, func(c *Config) { c.MaxConns = 2 })
	a, b := r.dial(t), r.dial(t)
	do(t, a, &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
	do(t, b, &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
	third, err := net.Dial("unix", r.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	_ = third.SetReadDeadline(time.Now().Add(5 * time.Second))
	if frame, err := agentproto.ReadFrame(third); err == nil {
		t.Fatalf("a third connection got %q", frame.Data)
	}
	// When one goes, there is room again.
	a.Close()
	waitFor(t, "room", func() bool { return len(r.agent.connections()) == 1 })
	do(t, r.dial(t), &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
}

func TestEventsGoToEveryConnection(t *testing.T) {
	r := startAgent(t, nil)
	a, b := r.dial(t), r.dial(t)
	write(t, r.work+"/seen.txt", "x")
	event(t, a, "create on a", fsEvent("create", "seen.txt"))
	event(t, b, "create on b", fsEvent("create", "seen.txt"))
}

func TestTheSocketAndShutdown(t *testing.T) {
	r := startAgent(t, nil)
	info, err := os.Stat(r.socket)
	if err != nil || info.Mode().Perm() != 0o666 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket = %v, %v", info, err)
	}
	c := r.dial(t)
	term := open(t, c, "sleep 30")
	r.agent.Shutdown()
	if _, err := os.Stat(r.socket); !os.IsNotExist(err) {
		t.Errorf("the socket is still there: %v", err)
	}
	waitFor(t, "the connection to close", func() bool {
		_, err := c.Do(&agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}}, time.Second)
		return err != nil
	})
	_ = term
}

// An agent that starts where a dead one left its socket takes the name over.
func TestAStaleSocketIsReplaced(t *testing.T) {
	r := startAgent(t, nil)
	r.agent.Shutdown()
	waitFor(t, "shutdown", func() bool { _, err := os.Stat(r.socket); return os.IsNotExist(err) })
	if err := os.WriteFile(r.socket, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := startAgent(t, func(c *Config) { c.Socket = r.socket; c.Work = r.work; c.Home = r.work + "-home" })
	do(t, second.dial(t), &agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}})
}

func TestHomeFolderIsMadeAndWorkMustExist(t *testing.T) {
	r := startAgent(t, nil)
	if info, err := os.Stat(r.agent.cfg.Home); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("home = %v, %v", info, err)
	}
	if _, err := New(Config{Work: "/does/not/exist", Socket: r.socket + "2"}); err == nil || !strings.Contains(err.Error(), "does/not/exist") {
		t.Errorf("a missing work folder: %v", err)
	}
}
