package workspace

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"

	"exam-platform/internal/agentproto"
)

// The recorder is the server's own connection to a student's agent, apart from the student's: it is attached to every terminal the
// student has, whether or not the student is connected, and writes each one's output to a transcript (an asciinema v2 file). A
// program that goes on printing while the student's connection is down is in the transcript. The agent holds the last 64 KiB of
// every terminal and sends it on an attach, so a terminal that opened and ended between two looks is still recorded.
//
// It also knows how every terminal ended, which is what the history's "run.started" event says (with the exit code).
//
// It starts when a session's container is running and stops when the session is frozen or removed. When the connection to the agent
// ends (the container restarted) the open transcripts end with it, and the terminals of the new agent get transcripts of their own.
type recorder struct {
	m       *Manager
	session string
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}

	mu         sync.Mutex
	conn       net.Conn
	writeMu    sync.Mutex
	next       uint64
	open       map[uint32]*transcript
	finished   map[uint32]bool
	noted      map[uint32]termNote
	firstSeen  map[uint32]time.Time
	lastUptime int64
	// recorded says which terminals this agent has had a transcript for already: one that is taken again (the connection was lost and
	// came back) starts with a line saying what is missing.
	recorded map[uint32]bool
}

type termNote struct {
	kind       string
	cols, rows int
}

type transcript struct {
	id      int64
	kind    string
	cast    *castWriter
	started time.Time
	exit    *int
}

func (m *Manager) startRecorder(session string) {
	if m.opts.TranscriptDir == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recorders[session] != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &recorder{m: m, session: session, ctx: ctx, cancel: cancel, done: make(chan struct{}), open: map[uint32]*transcript{}, finished: map[uint32]bool{}, noted: map[uint32]termNote{}, firstSeen: map[uint32]time.Time{}, recorded: map[uint32]bool{}}
	m.recorders[session] = r
	go r.run()
}

func (m *Manager) stopRecorder(session string) {
	m.mu.Lock()
	r := m.recorders[session]
	delete(m.recorders, session)
	m.mu.Unlock()
	if r != nil {
		r.cancel()
		<-r.done
	}
}

// Close stops every recorder: their transcripts end where they are.
func (m *Manager) Close() {
	m.mu.Lock()
	all := make([]string, 0, len(m.recorders))
	for session := range m.recorders {
		all = append(all, session)
	}
	m.mu.Unlock()
	for _, session := range all {
		m.stopRecorder(session)
	}
}

// noteTerminal is the relay saying a terminal was opened, and for what: the transcript is made at once and the recorder attaches.
func (m *Manager) noteTerminal(session string, term uint32, kind string, cols, rows int) {
	m.mu.Lock()
	r := m.recorders[session]
	m.mu.Unlock()
	if r != nil {
		r.note(term, termNote{kind: kind, cols: cols, rows: rows})
	}
}

func (r *recorder) wait(d time.Duration) {
	select {
	case <-r.ctx.Done():
	case <-time.After(d):
	}
}

func (r *recorder) run() {
	defer close(r.done)
	for r.ctx.Err() == nil {
		conn, err := r.m.opts.Runner.ConnectAgent(r.ctx, r.session)
		if err != nil {
			r.wait(2 * time.Second)
			continue
		}
		r.mu.Lock()
		r.conn = conn
		r.mu.Unlock()
		stop := context.AfterFunc(r.ctx, func() { conn.Close() })
		r.serve(conn)
		stop()
		conn.Close()
		r.mu.Lock()
		r.conn = nil
		r.mu.Unlock()
		r.endAll()
		r.wait(time.Second)
	}
	r.endAll()
}

func (r *recorder) send(message any) {
	r.mu.Lock()
	conn := r.conn
	r.next++
	id := r.next
	r.mu.Unlock()
	if conn == nil {
		return
	}
	var request agentproto.Request
	switch m := message.(type) {
	case *agentproto.PTYList:
		m.ID = id
		request = m
	case *agentproto.PTYAttach:
		m.ID = id
		request = m
	}
	payload, err := agentproto.Encode(request)
	if err != nil {
		return
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = agentproto.WriteFrame(conn, agentproto.Frame{Kind: agentproto.KindJSON, Data: payload})
}

func (r *recorder) list() {
	r.send(&agentproto.PTYList{Header: agentproto.Header{Type: agentproto.TypePTYList}})
}

func (r *recorder) serve(conn net.Conn) {
	poll := make(chan struct{})
	defer close(poll)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-poll:
				return
			case <-ticker.C:
				r.list()
			}
		}
	}()
	r.list()
	for {
		frame, err := agentproto.ReadFrame(conn)
		if err != nil {
			return
		}
		switch frame.Kind {
		case agentproto.KindBinary:
			r.output(frame.Term, frame.Data)
		case agentproto.KindJSON:
			message, err := agentproto.DecodeMessage(frame.Data)
			if err != nil {
				continue
			}
			switch {
			case message.Type == agentproto.TypeHello:
				r.hello(message)
			case message.Type == agentproto.TypePTYExit:
				r.exited(message)
			case message.Type == agentproto.TypePTYList && message.OK != nil && *message.OK:
				r.listed(message.Terminals)
			}
		}
	}
}

// hello tells a new agent from the one that was there: terminal numbers start again with a new agent, so what was finished is forgotten.
func (r *recorder) hello(message agentproto.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if message.UptimeMS < r.lastUptime {
		r.finished = map[uint32]bool{}
		r.noted = map[uint32]termNote{}
		r.firstSeen = map[uint32]time.Time{}
		r.recorded = map[uint32]bool{}
	}
	r.lastUptime = message.UptimeMS
}

// note is the relay's word on a terminal it opened.
func (r *recorder) note(term uint32, note termNote) {
	r.mu.Lock()
	r.noted[term] = note
	delete(r.finished, term)
	known := r.open[term] != nil
	connected := r.conn != nil
	r.mu.Unlock()
	if !known && connected {
		r.begin(term, note)
	}
}

// listed is the agent's list of terminals: the ones that are new get a transcript, the ones that are gone end theirs. A terminal that
// the relay has not told of (opened before this server started, say) waits half a second for the relay's word before it is taken.
func (r *recorder) listed(infos []agentproto.TermInfo) {
	present := map[uint32]bool{}
	for _, info := range infos {
		present[info.Term] = true
		r.mu.Lock()
		skip := r.open[info.Term] != nil || r.finished[info.Term]
		note, noted := r.noted[info.Term]
		first, seen := r.firstSeen[info.Term]
		if !skip && !noted && !seen {
			r.firstSeen[info.Term] = time.Now()
		}
		r.mu.Unlock()
		if skip || (!noted && (!seen || time.Since(first) < 500*time.Millisecond)) {
			continue
		}
		if !noted {
			note = termNote{kind: TerminalShell, cols: info.Cols, rows: info.Rows}
			if info.Cmd != "" {
				note.kind = TerminalProgram
			}
		}
		if note.cols == 0 {
			note.cols, note.rows = info.Cols, info.Rows
		}
		r.begin(info.Term, note)
	}
	r.mu.Lock()
	var gone []*transcript
	for term, t := range r.open {
		if !present[term] {
			gone = append(gone, t)
			delete(r.open, term)
		}
	}
	r.mu.Unlock()
	for _, t := range gone {
		r.finish(t, nil)
	}
}

// begin makes the transcript of a terminal and attaches to it, which sends what it printed so far.
func (r *recorder) begin(term uint32, note termNote) {
	now := r.m.opts.Now().UTC()
	name := fmt.Sprintf("%s/%d-%d.cast", r.session, now.UnixNano(), term)
	title := map[string]string{TerminalShell: "Terminal", TerminalRun: "Run", TerminalCheck: "Check", TerminalProgram: "Program"}[note.kind]
	cast, err := newCastWriter(filepath.Join(r.m.opts.TranscriptDir, filepath.FromSlash(name)), now, note.cols, note.rows, title, maxTranscriptBytes)
	if err != nil {
		r.m.opts.Logger.Error("workspace: a transcript could not be started", "session", r.session, "error", err)
		return
	}
	id, err := r.m.opts.Repo.RecordTerminal(r.ctx, TerminalRecord{SessionID: r.session, Term: term, Kind: note.kind, StartedAt: now, CastFile: name})
	if err != nil {
		cast.Close(now)
		r.m.opts.Logger.Error("workspace: a transcript could not be recorded", "session", r.session, "error", err)
		return
	}
	r.mu.Lock()
	r.open[term] = &transcript{id: id, kind: note.kind, cast: cast, started: now}
	again := r.recorded[term]
	r.recorded[term] = true
	r.mu.Unlock()
	if again {
		cast.Write(now, []byte("\r\n[The recorder lost its connection and found this terminal again: what it printed in between is missing, and the next lines may repeat what came before.]\r\n"))
	}
	r.send(&agentproto.PTYAttach{Header: agentproto.Header{Type: agentproto.TypePTYAttach}, Term: term})
}

func (r *recorder) output(term uint32, data []byte) {
	r.mu.Lock()
	t := r.open[term]
	r.mu.Unlock()
	if t != nil {
		t.cast.Write(r.m.opts.Now(), data)
	}
}

// exited is how a terminal's program ended: its transcript ends, and a Run or Check is told to the history with its exit code.
func (r *recorder) exited(message agentproto.Message) {
	r.mu.Lock()
	t := r.open[message.Term]
	delete(r.open, message.Term)
	r.finished[message.Term] = true
	r.mu.Unlock()
	if t == nil {
		return
	}
	r.finish(t, message.ExitCode)
	if t.kind == TerminalRun || t.kind == TerminalCheck {
		r.m.event(r.session, EventRunStarted, map[string]any{"which": t.kind, "exit_code": message.ExitCode})
	}
}

func (r *recorder) finish(t *transcript, exit *int) {
	now := r.m.opts.Now().UTC()
	bytes, truncated := t.cast.Close(now)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.m.opts.Repo.EndTerminal(ctx, t.id, now, exit, bytes, truncated); err != nil {
		r.m.opts.Logger.Error("workspace: a transcript could not be closed", "session", r.session, "error", err)
	}
}

// endAll ends every open transcript, as when the connection to the agent is lost.
func (r *recorder) endAll() {
	r.mu.Lock()
	open := r.open
	r.open = map[uint32]*transcript{}
	r.mu.Unlock()
	for _, t := range open {
		r.finish(t, nil)
	}
}
