//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"exam-platform/internal/agentproto"
	"golang.org/x/sys/unix"
)

const (
	ringBytes  = 64 << 10
	readChunk  = 32 << 10
	inputQueue = 64
)

// Terminal is one pseudo-terminal and the program on it.
type Terminal struct {
	id    uint32
	cmd   string
	pid   int
	owner *Terminals

	master *os.File
	input  chan []byte
	// ended is closed when the program has ended and its status is known; pumped is closed when the last output has been read.
	ended  chan struct{}
	pumped chan struct{}

	mu       sync.Mutex
	cols     int
	rows     int
	ring     *ring
	attached map[*Conn]struct{}
	exited   bool
	exit     ExitStatus
	closing  bool
	// exitMessage is the pty.exit event, once it has been made. A connection that attaches after that gets it from Attach; those
	// attached before got it when it was made, so each hears it once.
	exitMessage *agentproto.Message
}

// Terminals are the open terminals: at most agentproto.MaxTerminals.
type Terminals struct {
	agent *Agent
	mu    sync.Mutex
	next  uint32
	byID  map[uint32]*Terminal
}

func newTerminals(a *Agent) *Terminals { return &Terminals{agent: a, byID: map[uint32]*Terminal{}} }

// openPTY makes a pseudo-terminal pair. The master is non-blocking, so closing it ends a read that waits on it.
func openPTY() (master, slave int, err error) {
	master, err = unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, -1, err
	}
	if err := unix.IoctlSetPointerInt(master, unix.TIOCSPTLCK, 0); err != nil {
		_ = unix.Close(master)
		return -1, -1, err
	}
	// TIOCGPTPEER opens the slave from the master, whatever is (or is not) mounted at /dev/pts.
	if fd, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(master), unix.TIOCGPTPEER, uintptr(unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC)); errno == 0 {
		return master, int(fd), nil
	}
	number, err := unix.IoctlGetInt(master, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(master)
		return -1, -1, err
	}
	slave, err = unix.Open("/dev/pts/"+strconv.Itoa(number), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(master)
		return -1, -1, err
	}
	return master, slave, nil
}

func setWinsize(f *os.File, cols, rows int) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err := raw.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)})
	}); err != nil {
		return err
	}
	return ioctlErr
}

// Open starts a terminal. The caller attaches the connection that asked for it once it has answered the request, so the answer comes
// before the first output.
func (ts *Terminals) Open(request *agentproto.PTYOpen) (*Terminal, error) {
	a := ts.agent
	dir := a.cfg.Work
	if request.Cwd != "" {
		if err := a.fs.IsDir(request.Cwd); err != nil {
			return nil, err
		}
		dir = filepath.Join(a.cfg.Work, request.Cwd)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.byID) >= agentproto.MaxTerminals {
		return nil, agentproto.Errorf(agentproto.CodeLimit, "at most %d terminals can be open; close one first", agentproto.MaxTerminals)
	}
	masterFD, slaveFD, err := openPTY()
	if err != nil {
		return nil, agentproto.Errorf(agentproto.CodeUnavailable, "no terminal can be opened here: %v", err)
	}
	defer unix.Close(slaveFD)
	master := os.NewFile(uintptr(masterFD), "ptmx")
	if err := setWinsize(master, request.Cols, request.Rows); err != nil {
		master.Close()
		return nil, agentproto.Errorf(agentproto.CodeInternal, "the terminal size cannot be set: %v", err)
	}
	if settings, err := unix.IoctlGetTermios(slaveFD, unix.TCGETS); err == nil {
		settings.Iflag |= unix.IUTF8
		_ = unix.IoctlSetTermios(slaveFD, unix.TCSETS, settings)
	}
	path, argv := a.cfg.Shell, []string{"-" + filepath.Base(a.cfg.Shell)}
	if request.Cmd != "" {
		path, argv = "/bin/sh", []string{"sh", "-c", request.Cmd}
	}
	attr := &syscall.ProcAttr{
		Dir:   dir,
		Env:   a.childEnv(dir),
		Files: []uintptr{uintptr(slaveFD), uintptr(slaveFD), uintptr(slaveFD)},
		Sys:   &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0},
	}
	before := a.oomKills()
	pid, status, err := a.reaper.Start(path, argv, attr)
	if err != nil {
		master.Close()
		return nil, agentproto.Errorf(agentproto.CodeUnavailable, "the program cannot be started: %v", err)
	}
	ts.next++
	t := &Terminal{id: ts.next, cmd: request.Cmd, pid: pid, owner: ts, master: master, input: make(chan []byte, inputQueue),
		ended: make(chan struct{}), pumped: make(chan struct{}), cols: request.Cols, rows: request.Rows,
		ring: newRing(ringBytes), attached: map[*Conn]struct{}{}}
	ts.byID[t.id] = t
	go t.pump()
	go t.writeInput()
	go t.watch(status, before)
	return t, nil
}

// pump reads the terminal's output and hands it to every connection that is attached. A connection that cannot keep up holds this
// loop, so the program stops when its output is not being read: that is the only throttle the agent needs.
func (t *Terminal) pump() {
	defer close(t.pumped)
	buffer := make([]byte, readChunk)
	for {
		n, err := t.master.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			t.mu.Lock()
			t.ring.write(chunk)
			targets := make([]*Conn, 0, len(t.attached))
			for c := range t.attached {
				targets = append(targets, c)
			}
			t.mu.Unlock()
			for _, c := range targets {
				c.send(agentproto.Frame{Kind: agentproto.KindBinary, Term: t.id, Data: chunk})
			}
		}
		if err != nil {
			return
		}
	}
}

// writeInput writes keystrokes to the terminal in order, apart from the connection's reader, which a program that is not reading its
// input must not hold.
func (t *Terminal) writeInput() {
	for {
		select {
		case data := <-t.input:
			for len(data) > 0 {
				n, err := t.master.Write(data)
				if err != nil {
					return
				}
				data = data[n:]
			}
		case <-t.ended:
			return
		}
	}
}

// watch waits for the program to end, lets the last of its output through, and tells the connections that are attached.
func (t *Terminal) watch(status <-chan ExitStatus, oomBefore uint64) {
	end := <-status
	t.mu.Lock()
	t.exited, t.exit = true, end
	t.mu.Unlock()
	close(t.ended)
	select {
	case <-t.pumped:
	case <-time.After(300 * time.Millisecond):
	}
	message := agentproto.Message{Type: agentproto.TypePTYExit, Term: t.id, ExitCode: &end.Code, OOMKilled: t.owner.agent.oomKills() > oomBefore}
	if end.Signal != 0 {
		message.Signal = signalName(end.Signal)
	}
	t.mu.Lock()
	t.exitMessage = &message
	targets := make([]*Conn, 0, len(t.attached))
	for c := range t.attached {
		targets = append(targets, c)
	}
	t.mu.Unlock()
	for _, c := range targets {
		c.sendMessage(message)
	}
}

func signalName(s syscall.Signal) string {
	names := map[syscall.Signal]string{syscall.SIGHUP: "SIGHUP", syscall.SIGINT: "SIGINT", syscall.SIGQUIT: "SIGQUIT", syscall.SIGILL: "SIGILL", syscall.SIGABRT: "SIGABRT",
		syscall.SIGFPE: "SIGFPE", syscall.SIGKILL: "SIGKILL", syscall.SIGSEGV: "SIGSEGV", syscall.SIGPIPE: "SIGPIPE", syscall.SIGALRM: "SIGALRM", syscall.SIGTERM: "SIGTERM",
		syscall.SIGBUS: "SIGBUS", syscall.SIGXCPU: "SIGXCPU", syscall.SIGXFSZ: "SIGXFSZ", syscall.SIGSYS: "SIGSYS", syscall.SIGTRAP: "SIGTRAP"}
	if name, found := names[s]; found {
		return name
	}
	return "SIG" + strconv.Itoa(int(s))
}

func (ts *Terminals) get(id uint32) (*Terminal, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t := ts.byID[id]
	if t == nil {
		return nil, agentproto.Errorf(agentproto.CodeNotFound, "there is no terminal %d", id)
	}
	return t, nil
}

// Attach attaches c to a terminal and queues what the terminal still holds of its output, ahead of anything new: the replay is queued
// while the terminal is locked, and the pump takes its list of connections under the same lock.
func (ts *Terminals) Attach(c *Conn, id uint32) (*Terminal, error) {
	t, err := ts.get(id)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ring.size > 0 {
		if !c.enqueue(agentproto.Frame{Kind: agentproto.KindBinary, Term: t.id, Data: t.ring.snapshot()}) {
			return nil, agentproto.Errorf(agentproto.CodeUnavailable, "the connection is too busy to take the terminal's output")
		}
	}
	if _, already := t.attached[c]; !already {
		// A program that ends at once can end before the connection that started it is attached: it is told now.
		if t.exitMessage != nil {
			c.trySendMessage(*t.exitMessage)
		}
		t.attached[c] = struct{}{}
	}
	return t, nil
}

// Detach removes c from every terminal; the terminals go on.
func (ts *Terminals) Detach(c *Conn) {
	ts.mu.Lock()
	all := make([]*Terminal, 0, len(ts.byID))
	for _, t := range ts.byID {
		all = append(all, t)
	}
	ts.mu.Unlock()
	for _, t := range all {
		t.mu.Lock()
		delete(t.attached, c)
		t.mu.Unlock()
	}
}

// Input hands keystrokes to a terminal. It never blocks: when the program is not reading, the keystrokes are dropped and the
// sender is told (RATE).
func (ts *Terminals) Input(c *Conn, id uint32, data []byte) {
	t, err := ts.get(id)
	if err != nil {
		c.sendMessage(agentproto.Message{Type: agentproto.TypeSessionErr, Term: id, Code: agentproto.CodeNotFound, Message: "there is no such terminal"})
		return
	}
	t.mu.Lock()
	exited := t.exited
	t.mu.Unlock()
	if exited {
		return
	}
	select {
	case t.input <- append([]byte(nil), data...):
	default:
		c.sendMessage(agentproto.Message{Type: agentproto.TypeSessionErr, Term: id, Code: agentproto.CodeRate, Message: "the program is not reading its input; the keystrokes were dropped"})
	}
}

// Resize changes the size of a terminal.
func (ts *Terminals) Resize(id uint32, cols, rows int) error {
	t, err := ts.get(id)
	if err != nil {
		return err
	}
	if err := setWinsize(t.master, cols, rows); err != nil {
		return agentproto.Errorf(agentproto.CodeInternal, "the terminal size cannot be set: %v", err)
	}
	t.mu.Lock()
	t.cols, t.rows = cols, rows
	t.mu.Unlock()
	return nil
}

// Close ends a terminal: its program's process group is hung up and, if it is still there after the grace time, killed.
func (ts *Terminals) Close(id uint32) error {
	ts.mu.Lock()
	t := ts.byID[id]
	delete(ts.byID, id)
	ts.mu.Unlock()
	if t == nil {
		return agentproto.Errorf(agentproto.CodeNotFound, "there is no terminal %d", id)
	}
	t.stop()
	return nil
}

func (t *Terminal) stop() {
	t.mu.Lock()
	t.closing = true
	exited := t.exited
	t.mu.Unlock()
	if !exited {
		_ = unix.Kill(-t.pid, unix.SIGHUP)
		grace := t.owner.agent.cfg.KillGrace
		go func() {
			select {
			case <-t.ended:
			case <-time.After(grace):
				_ = unix.Kill(-t.pid, unix.SIGKILL)
			}
			t.master.Close()
		}()
		return
	}
	t.master.Close()
}

// List describes the open terminals, oldest first.
func (ts *Terminals) List() []agentproto.TermInfo {
	ts.mu.Lock()
	all := make([]*Terminal, 0, len(ts.byID))
	for _, t := range ts.byID {
		all = append(all, t)
	}
	ts.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	infos := make([]agentproto.TermInfo, 0, len(all))
	for _, t := range all {
		t.mu.Lock()
		info := agentproto.TermInfo{Term: t.id, Cols: t.cols, Rows: t.rows, Cmd: t.cmd, Exited: t.exited}
		if t.exited {
			code := t.exit.Code
			info.ExitCode = &code
		}
		t.mu.Unlock()
		infos = append(infos, info)
	}
	return infos
}

// Info describes one terminal.
func (t *Terminal) Info() agentproto.TermInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	info := agentproto.TermInfo{Term: t.id, Cols: t.cols, Rows: t.rows, Cmd: t.cmd, Exited: t.exited}
	if t.exited {
		code := t.exit.Code
		info.ExitCode = &code
	}
	return info
}

// CloseAll ends every terminal, for the agent's own shutdown.
func (ts *Terminals) CloseAll() {
	ts.mu.Lock()
	all := make([]*Terminal, 0, len(ts.byID))
	for id, t := range ts.byID {
		all = append(all, t)
		delete(ts.byID, id)
	}
	ts.mu.Unlock()
	for _, t := range all {
		t.stop()
	}
}

// Count is how many terminals are open.
func (ts *Terminals) Count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.byID)
}

// childEnv is the environment of a program the agent starts: a short list of its own, not the agent's, so nothing the runner put in
// the agent's environment reaches the student's programs.
func (a *Agent) childEnv(dir string) []string {
	env := []string{
		"PATH=" + a.cfg.Path,
		"HOME=" + a.cfg.Home,
		"USER=" + a.cfg.User,
		"LOGNAME=" + a.cfg.User,
		"SHELL=" + a.cfg.Shell,
		"TERM=xterm-256color",
		"LANG=C.UTF-8",
		"TMPDIR=/tmp",
		"PWD=" + dir,
	}
	return append(env, a.cfg.Env...)
}

// oomKills reads how many times the memory limit has killed a process in this container, from the cgroup's own memory.events (the
// API's OOMKilled flag is never set by Podman, P0-8). It is 0 when the file cannot be read.
func (a *Agent) oomKills() uint64 {
	data, err := os.ReadFile(a.cfg.MemoryEvents)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, found := strings.CutPrefix(line, "oom_kill "); found {
			n, _ := strconv.ParseUint(strings.TrimSpace(rest), 10, 64)
			return n
		}
	}
	return 0
}
