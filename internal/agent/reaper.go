//go:build linux

package agent

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ExitStatus is how a program ended.
type ExitStatus struct {
	// Code is the exit status, or 128 plus the signal number when a signal ended the program.
	Code int
	// Signal is the signal that ended it, or 0.
	Signal syscall.Signal
}

// Reaper starts programs and waits for them. As the container's PID 1 the agent is also the parent of every program whose own
// parent has exited, and each of those stays a zombie until someone waits for it. With all set (the agent is PID 1), the Reaper
// waits for every child there is, and tells the ones it started how they ended. Without it (tests, or an agent run as an ordinary
// program) it waits only for the programs it started, so it takes nothing from other code in the same process.
type Reaper struct {
	all bool
	mu  sync.Mutex
	// started holds the programs that have not ended yet, each with the channel their status goes to.
	started map[int]chan ExitStatus
	stop    chan struct{}
}

// NewReaper makes a Reaper. With all set it starts reaping at once.
func NewReaper(all bool) *Reaper {
	r := &Reaper{all: all, started: map[int]chan ExitStatus{}, stop: make(chan struct{})}
	if all {
		go r.loop()
	}
	return r
}

// Stop ends the loop of a Reaper that waits for everything.
func (r *Reaper) Stop() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}

// Start starts a program and returns its process ID and a channel that receives its status when it ends.
func (r *Reaper) Start(path string, argv []string, attr *syscall.ProcAttr) (int, <-chan ExitStatus, error) {
	// The lock is held from before the program exists until it is on the list, so the loop cannot wait for it first.
	r.mu.Lock()
	defer r.mu.Unlock()
	pid, err := syscall.ForkExec(path, argv, attr)
	if err != nil {
		return 0, nil, err
	}
	status := make(chan ExitStatus, 1)
	r.started[pid] = status
	if !r.all {
		go r.wait(pid)
	}
	return pid, status, nil
}

func (r *Reaper) wait(pid int) {
	var ws syscall.WaitStatus
	for {
		_, err := syscall.Wait4(pid, &ws, 0, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			// Someone else took the status; the program is gone all the same.
			ws = 0
		}
		break
	}
	r.deliver(pid, ws)
}

func (r *Reaper) deliver(pid int, ws syscall.WaitStatus) {
	r.mu.Lock()
	status := r.started[pid]
	delete(r.started, pid)
	r.mu.Unlock()
	if status == nil {
		return
	}
	end := ExitStatus{Code: ws.ExitStatus()}
	if ws.Signaled() {
		end.Signal = ws.Signal()
		end.Code = 128 + int(ws.Signal())
	}
	status <- end
}

// loop waits for every child whenever one has changed state. SIGCHLD is the prompt, and a timer is the backstop for a signal that
// coalesces with another.
func (r *Reaper) loop() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGCHLD)
	defer signal.Stop(signals)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-signals:
		case <-ticker.C:
		}
		r.reapAll()
	}
}

func (r *Reaper) reapAll() {
	// Holding the lock keeps a program that is just being started from being waited for before it is on the list.
	r.mu.Lock()
	type ended struct {
		pid int
		ws  syscall.WaitStatus
	}
	var done []ended
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		if err == syscall.EINTR {
			continue
		}
		if pid <= 0 || err != nil {
			break
		}
		if ws.Exited() || ws.Signaled() {
			done = append(done, ended{pid, ws})
		}
	}
	r.mu.Unlock()
	for _, e := range done {
		r.deliver(e.pid, e.ws)
	}
}
