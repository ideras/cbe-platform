//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"exam-platform/internal/agentproto"
)

// Config is how an agent is set up. New fills in what is left empty.
type Config struct {
	// Socket is where the agent listens; Work is the folder of the student's files.
	Socket string
	Work   string
	// Shell is the login shell of a terminal opened without a command.
	Shell string
	// Home, User and Path are the environment of the programs the agent starts; Env is added to it.
	Home string
	User string
	Path string
	Env  []string
	// Heartbeat is how often an agent.hb goes to every connection.
	Heartbeat time.Duration
	// MaxConns is how many connections the agent keeps at once; MaxWatches how many folders it watches.
	MaxConns   int
	MaxWatches int
	// KillGrace is how long a closed terminal's program has after SIGHUP before it is killed.
	KillGrace time.Duration
	// MemoryEvents is the cgroup file whose oom_kill counter says whether the memory limit killed something.
	MemoryEvents string
	// ReapAll makes the agent the reaper of every child: set it when the agent is PID 1.
	ReapAll bool
	// Version is reported in agent.hello.
	Version string
}

func (c *Config) fill() {
	def := func(s *string, value string) {
		if *s == "" {
			*s = value
		}
	}
	def(&c.Socket, "/run/cbe/agent.sock")
	def(&c.Work, "/work")
	def(&c.Shell, "/bin/bash")
	def(&c.Home, "/tmp/home")
	def(&c.User, "student")
	def(&c.Path, "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	def(&c.MemoryEvents, "/sys/fs/cgroup/memory.events")
	def(&c.Version, "dev")
	if c.Heartbeat <= 0 {
		c.Heartbeat = 5 * time.Second
	}
	if c.MaxConns <= 0 {
		c.MaxConns = 8
	}
	if c.MaxWatches <= 0 {
		c.MaxWatches = 4096
	}
	if c.KillGrace <= 0 {
		c.KillGrace = 2 * time.Second
	}
}

// Agent is the program inside a student's container.
type Agent struct {
	cfg     Config
	fs      *FS
	reaper  *Reaper
	terms   *Terminals
	watcher *Watcher
	started time.Time
	beat    atomic.Uint64

	mu       sync.Mutex
	conns    map[*Conn]struct{}
	listener net.Listener
}

// New prepares an agent: it opens /work, makes the home folder, and starts the watcher. Serve then answers on the socket.
func New(cfg Config) (*Agent, error) {
	cfg.fill()
	files, err := OpenFS(cfg.Work)
	if err != nil {
		return nil, err
	}
	// The root file system may be read-only, with a tmpfs at /tmp: the home folder goes there, and it is gone with the container.
	if err := os.MkdirAll(cfg.Home, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		files.Close()
		return nil, fmt.Errorf("home folder: %w", err)
	}
	a := &Agent{cfg: cfg, fs: files, reaper: NewReaper(cfg.ReapAll), started: time.Now(), conns: map[*Conn]struct{}{}}
	a.terms = newTerminals(a)
	a.watcher, err = NewWatcher(cfg.Work, cfg.MaxWatches, a.broadcastEvent)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("watching %s: %w", cfg.Work, err)
	}
	return a, nil
}

// FS is the agent's view of /work, for tests.
func (a *Agent) FS() *FS { return a.fs }

// Serve listens on the socket until ctx ends, then shuts everything down. The socket file is made with permissions 0666: what
// guards it is the folder it sits in, which only the runner and the container can reach.
func (a *Agent) Serve(ctx context.Context) error {
	_ = os.Remove(a.cfg.Socket)
	listener, err := net.Listen("unix", a.cfg.Socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(a.cfg.Socket, 0o666); err != nil {
		listener.Close()
		return err
	}
	a.mu.Lock()
	a.listener = listener
	a.mu.Unlock()
	go a.heartbeats(ctx)
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		nc, err := listener.Accept()
		if err != nil {
			a.Shutdown()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		a.accept(nc)
	}
}

func (a *Agent) accept(nc net.Conn) {
	a.mu.Lock()
	if len(a.conns) >= a.cfg.MaxConns {
		a.mu.Unlock()
		nc.Close()
		return
	}
	c := newConn(a, nc)
	a.conns[c] = struct{}{}
	a.mu.Unlock()
	c.start()
}

// Shutdown ends every connection and every terminal and removes the socket.
func (a *Agent) Shutdown() {
	a.mu.Lock()
	listener := a.listener
	conns := make([]*Conn, 0, len(a.conns))
	for c := range a.conns {
		conns = append(conns, c)
	}
	a.mu.Unlock()
	if listener != nil {
		listener.Close()
	}
	for _, c := range conns {
		c.close()
	}
	a.terms.CloseAll()
	a.watcher.Close()
	a.reaper.Stop()
	_ = os.Remove(a.cfg.Socket)
}

func (a *Agent) removeConn(c *Conn) {
	a.mu.Lock()
	delete(a.conns, c)
	a.mu.Unlock()
	a.terms.Detach(c)
}

func (a *Agent) connections() []*Conn {
	a.mu.Lock()
	defer a.mu.Unlock()
	conns := make([]*Conn, 0, len(a.conns))
	for c := range a.conns {
		conns = append(conns, c)
	}
	return conns
}

// broadcastEvent sends an event to every connection, dropping it for one whose queue is full.
func (a *Agent) broadcastEvent(m agentproto.Message) {
	for _, c := range a.connections() {
		c.trySendMessage(m)
	}
}

func (a *Agent) heartbeats(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			message := a.status(agentproto.TypeHeartbeat)
			message.Seq = a.beat.Add(1)
			for _, c := range a.connections() {
				c.trySendMessage(message)
			}
		}
	}
}

// status is what agent.hello and agent.hb carry.
func (a *Agent) status(kind string) agentproto.Message {
	return agentproto.Message{Type: kind, Proto: agentproto.ProtocolVersion, AgentVersion: a.cfg.Version, PID1: os.Getpid() == 1,
		UptimeMS: time.Since(a.started).Milliseconds(), OpenTerminals: a.terms.Count(), OOMKills: a.oomKills(), Watches: a.watcher.Watches(),
		Work: a.cfg.Work, SlowPaths: a.fs.SlowPaths(), Connections: len(a.connections())}
}

// Conn is one connection to the agent. Its frames go out through one queue and one writer, so replies, events and terminal output
// never interleave, and a peer that reads slowly makes its own senders wait, not the agent.
type Conn struct {
	agent *Agent
	nc    net.Conn
	out   chan agentproto.Frame
	jobs  chan func()
	done  chan struct{}
	once  sync.Once
}

const (
	outQueue = 256
	jobQueue = 64
)

func newConn(a *Agent, nc net.Conn) *Conn {
	return &Conn{agent: a, nc: nc, out: make(chan agentproto.Frame, outQueue), jobs: make(chan func(), jobQueue), done: make(chan struct{})}
}

func (c *Conn) start() {
	go c.write()
	go c.work()
	hello := c.agent.status(agentproto.TypeHello)
	c.sendMessage(hello)
	go c.read()
}

func (c *Conn) close() {
	c.once.Do(func() {
		close(c.done)
		c.nc.Close()
		c.agent.removeConn(c)
	})
}

// enqueue queues a frame without waiting; it says whether there was room.
func (c *Conn) enqueue(f agentproto.Frame) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- f:
		return true
	default:
		return false
	}
}

// send queues a frame, waiting for room as long as the connection lives.
func (c *Conn) send(f agentproto.Frame) bool {
	select {
	case c.out <- f:
		return true
	case <-c.done:
		return false
	}
}

func (c *Conn) sendMessage(m agentproto.Message) bool {
	payload, err := agentproto.Encode(m)
	if err != nil {
		return false
	}
	return c.send(agentproto.Frame{Kind: agentproto.KindJSON, Data: payload})
}

func (c *Conn) trySendMessage(m agentproto.Message) bool {
	payload, err := agentproto.Encode(m)
	if err != nil {
		return false
	}
	return c.enqueue(agentproto.Frame{Kind: agentproto.KindJSON, Data: payload})
}

func (c *Conn) write() {
	for {
		select {
		case f := <-c.out:
			if err := agentproto.WriteFrame(c.nc, f); err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

// work runs the file requests of one connection one after the other, in the order they came.
func (c *Conn) work() {
	for {
		select {
		case job := <-c.jobs:
			job()
		case <-c.done:
			return
		}
	}
}

func (c *Conn) read() {
	defer c.close()
	for {
		frame, err := agentproto.ReadFrame(c.nc)
		if err != nil {
			return
		}
		if frame.Kind == agentproto.KindBinary {
			c.agent.terms.Input(c, frame.Term, frame.Data)
			continue
		}
		c.handle(frame.Data)
	}
}

// reply sends the answer to a request.
func (c *Conn) reply(h *agentproto.Header, message agentproto.Message, err error) {
	message.ID, message.Type = h.ID, h.Type
	ok := err == nil
	message.OK = &ok
	if err != nil {
		message.Code, message.Message = agentproto.CodeOf(err), err.Error()
		var protocol *agentproto.Error
		if errors.As(err, &protocol) {
			message.Code, message.Message = protocol.Code, protocol.Message
		}
	}
	c.sendMessage(message)
}

func (c *Conn) handle(payload []byte) {
	request, err := agentproto.DecodeRequest(payload)
	if err != nil {
		// The reply names the request when its ID can be read at all.
		var header agentproto.Header
		_ = jsonUnmarshalLenient(payload, &header)
		if header.Type == "" {
			header.Type = agentproto.TypeSessionErr
		}
		c.reply(&header, agentproto.Message{}, err)
		return
	}
	head := request.Head()
	switch r := request.(type) {
	case *agentproto.Ping:
		c.reply(head, agentproto.Message{UptimeMS: time.Since(c.agent.started).Milliseconds()}, nil)
	case *agentproto.PTYOpen:
		t, err := c.agent.terms.Open(r)
		if err != nil {
			c.reply(head, agentproto.Message{}, err)
			return
		}
		info := t.Info()
		c.reply(head, agentproto.Message{Term: info.Term, Cols: info.Cols, Rows: info.Rows}, nil)
		// Attached after the reply is queued, with whatever the program printed meanwhile replayed first.
		_, _ = c.agent.terms.Attach(c, info.Term)
	case *agentproto.PTYAttach:
		t, err := c.agent.terms.Attach(c, r.Term)
		if err != nil {
			c.reply(head, agentproto.Message{}, err)
			return
		}
		info := t.Info()
		c.reply(head, agentproto.Message{Term: info.Term, Cols: info.Cols, Rows: info.Rows, Exited: info.Exited, ExitCode: info.ExitCode}, nil)
	case *agentproto.PTYList:
		c.reply(head, agentproto.Message{Terminals: c.agent.terms.List()}, nil)
	case *agentproto.PTYResize:
		c.reply(head, agentproto.Message{}, c.agent.terms.Resize(r.Term, r.Cols, r.Rows))
	case *agentproto.PTYClose:
		c.reply(head, agentproto.Message{}, c.agent.terms.Close(r.Term))
	default:
		// File requests may take a while and must keep their order, so they wait their turn on this connection's worker.
		select {
		case c.jobs <- func() { c.file(request) }:
		default:
			c.reply(head, agentproto.Message{}, agentproto.Errorf(agentproto.CodeRate, "too many requests are waiting; slow down"))
		}
	}
}

func (c *Conn) file(request agentproto.Request) {
	files := c.agent.fs
	head := request.Head()
	switch r := request.(type) {
	case *agentproto.FSList:
		entries, err := files.List(r.Path)
		c.reply(head, agentproto.Message{Entries: entries}, err)
	case *agentproto.FSRead:
		content, version, err := files.Read(r.Path)
		c.reply(head, agentproto.Message{Content: &content, Version: version, Size: int64(len(content))}, err)
	case *agentproto.FSWrite:
		version, current, err := files.Write(r.Path, r.Content, r.BaseVersion)
		if err != nil {
			c.reply(head, agentproto.Message{Version: current}, err)
			return
		}
		c.reply(head, agentproto.Message{Version: version, Size: int64(len(r.Content))}, nil)
	case *agentproto.FSCreate:
		version, err := files.Create(r.Path, r.Content)
		c.reply(head, agentproto.Message{Version: version}, err)
	case *agentproto.FSMkdir:
		c.reply(head, agentproto.Message{}, files.Mkdir(r.Path))
	case *agentproto.FSRename:
		c.reply(head, agentproto.Message{}, files.Rename(r.Path, r.To))
	case *agentproto.FSDelete:
		c.reply(head, agentproto.Message{}, files.Delete(r.Path))
	}
}

// Socket is where the agent listens.
func (a *Agent) Socket() string { return filepath.Clean(a.cfg.Socket) }
