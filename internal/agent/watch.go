//go:build linux

package agent

import (
	"encoding/binary"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"exam-platform/internal/agentproto"
	"golang.org/x/sys/unix"
)

const (
	watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO |
		unix.IN_DONT_FOLLOW | unix.IN_ONLYDIR | unix.IN_EXCL_UNLINK
	// flushEvery is how long events are held to merge repeated writes to one file into one event.
	flushEvery = 100 * time.Millisecond
	// maxPending is how many events may wait for a flush; beyond it they are replaced by one overflow event, and the client
	// lists the folders again.
	maxPending = 2000
)

// Watcher turns inotify into fs.event messages. It watches every folder under /work (not following links), adds a watch when a
// folder appears, merges repeated writes to the same file, and says "overflow" when it cannot be sure of the picture: more events
// than it can hold, a kernel queue that overflowed, or no room for another watch.
type Watcher struct {
	root  string
	emit  func(agentproto.Message)
	limit int

	// fd is kept apart from file because os.File.Fd would put the descriptor back into blocking mode.
	fd   int
	file *os.File

	mu      sync.Mutex
	dirs    map[int32]string // watch descriptor -> folder, from /work
	wds     map[string]int32 // folder -> watch descriptor
	pending []agentproto.Message
	// lastModify is where in pending the latest modify event of a path is, while nothing else has happened to that path.
	lastModify map[string]int
	moves      map[uint32]move // moved-from events waiting for their moved-to, by cookie
	overflow   bool
	closed     bool
}

type move struct {
	path  string
	isDir bool
	at    time.Time
}

// NewWatcher starts watching root. emit is called, from the watcher's own goroutine, with each event; it must not block for long.
func NewWatcher(root string, limit int, emit func(agentproto.Message)) (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	w := &Watcher{root: root, emit: emit, limit: limit, fd: fd, file: os.NewFile(uintptr(fd), "inotify"), dirs: map[int32]string{}, wds: map[string]int32{},
		lastModify: map[string]int{}, moves: map[uint32]move{}}
	w.mu.Lock()
	w.addTree("", false)
	w.mu.Unlock()
	go w.read()
	go w.flushLoop()
	return w, nil
}

// Watches is how many folders are being watched.
func (w *Watcher) Watches() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.dirs)
}

// Close stops the watcher.
func (w *Watcher) Close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.file.Close()
}

// addTree watches the folder rel and everything under it, and, when announce is set, reports what it finds as created (it was made
// before the watch was in place, so nothing else would say so). The caller holds w.mu.
func (w *Watcher) addTree(rel string, announce bool) {
	abs := path.Join(w.root, rel)
	if _, watched := w.wds[rel]; !watched {
		if len(w.dirs) >= w.limit {
			w.overflow = true
			return
		}
		wd, err := unix.InotifyAddWatch(w.fd, abs, watchMask)
		if err != nil {
			if err == unix.ENOSPC {
				w.overflow = true
			}
			return
		}
		if old, again := w.dirs[int32(wd)]; again && old != rel {
			delete(w.wds, old)
		}
		w.dirs[int32(wd)] = rel
		w.wds[rel] = int32(wd)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, agentproto.ReservedPrefix) {
			continue
		}
		child := path.Join(rel, name)
		if announce {
			w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "create", Path: child, IsDir: entry.IsDir()})
		}
		if entry.IsDir() {
			w.addTree(child, announce)
		}
	}
}

func (w *Watcher) read() {
	buffer := make([]byte, 64<<10)
	for {
		n, err := w.file.Read(buffer)
		if err != nil {
			return
		}
		w.handle(buffer[:n])
	}
}

func (w *Watcher) handle(buffer []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(buffer) >= unix.SizeofInotifyEvent {
		wd := int32(binary.NativeEndian.Uint32(buffer[0:4]))
		mask := binary.NativeEndian.Uint32(buffer[4:8])
		cookie := binary.NativeEndian.Uint32(buffer[8:12])
		length := int(binary.NativeEndian.Uint32(buffer[12:16]))
		if unix.SizeofInotifyEvent+length > len(buffer) {
			return
		}
		name := string(trimNUL(buffer[unix.SizeofInotifyEvent : unix.SizeofInotifyEvent+length]))
		buffer = buffer[unix.SizeofInotifyEvent+length:]
		w.event(wd, mask, cookie, name)
	}
}

func trimNUL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

func (w *Watcher) event(wd int32, mask, cookie uint32, name string) {
	if mask&unix.IN_Q_OVERFLOW != 0 {
		w.overflow = true
		return
	}
	dir, known := w.dirs[wd]
	if mask&unix.IN_IGNORED != 0 {
		if known {
			delete(w.dirs, wd)
			if w.wds[dir] == wd {
				delete(w.wds, dir)
			}
		}
		return
	}
	if !known || name == "" {
		return
	}
	isDir := mask&unix.IN_ISDIR != 0
	full := path.Join(dir, name)
	reserved := strings.HasPrefix(name, agentproto.ReservedPrefix)
	switch {
	case mask&unix.IN_MOVED_FROM != 0:
		w.moves[cookie] = move{path: full, isDir: isDir, at: time.Now()}
		if reserved {
			w.moves[cookie] = move{path: "", isDir: isDir, at: time.Now()}
		}
	case mask&unix.IN_MOVED_TO != 0:
		from, paired := w.moves[cookie]
		delete(w.moves, cookie)
		switch {
		case paired && from.path == "":
			// The agent's own temporary file was renamed over a file: the file was written.
			w.queueModify(full)
		case paired:
			w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "rename", Path: full, From: from.path, IsDir: isDir})
			if isDir {
				w.renameDirs(from.path, full)
			}
		case !reserved:
			w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "create", Path: full, IsDir: isDir})
			if isDir {
				w.addTree(full, true)
			}
		}
	case reserved:
	case mask&unix.IN_CREATE != 0:
		w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "create", Path: full, IsDir: isDir})
		if isDir {
			w.addTree(full, true)
		}
	case mask&unix.IN_DELETE != 0:
		w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "delete", Path: full, IsDir: isDir})
	case mask&(unix.IN_MODIFY|unix.IN_CLOSE_WRITE) != 0 && !isDir:
		w.queueModify(full)
	}
}

// renameDirs makes the watches under a folder that was renamed follow it.
func (w *Watcher) renameDirs(from, to string) {
	for wd, dir := range w.dirs {
		if dir == from || strings.HasPrefix(dir, from+"/") {
			moved := to + strings.TrimPrefix(dir, from)
			delete(w.wds, dir)
			w.dirs[wd] = moved
			w.wds[moved] = wd
		}
	}
}

func (w *Watcher) queueModify(p string) {
	if i, found := w.lastModify[p]; found && i < len(w.pending) && w.pending[i].Kind == "modify" && w.pending[i].Path == p {
		return
	}
	w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "modify", Path: p})
	w.lastModify[p] = len(w.pending) - 1
}

func (w *Watcher) queue(m agentproto.Message) {
	if len(w.pending) >= maxPending {
		w.overflow = true
		return
	}
	// Anything else that happens to a path ends the run of modifications that could be merged.
	delete(w.lastModify, m.Path)
	if m.From != "" {
		delete(w.lastModify, m.From)
	}
	w.pending = append(w.pending, m)
}

func (w *Watcher) flushLoop() {
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	for range ticker.C {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		// A file moved out of /work never gets a moved-to: it is gone.
		for cookie, m := range w.moves {
			if time.Since(m.at) < flushEvery {
				continue
			}
			delete(w.moves, cookie)
			if m.path != "" {
				w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "delete", Path: m.path, IsDir: m.isDir})
				if m.isDir {
					w.dropTree(m.path)
				}
			}
		}
		events := w.pending
		overflow := w.overflow
		w.pending, w.overflow = nil, false
		w.lastModify = map[string]int{}
		w.mu.Unlock()
		if overflow {
			events = append(events[:min(len(events), maxPending)], agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "overflow"})
		}
		for _, event := range events {
			w.emit(event)
		}
	}
}

// dropTree stops watching a folder that moved out of /work, and everything under it.
func (w *Watcher) dropTree(rel string) {
	for wd, dir := range w.dirs {
		if dir == rel || strings.HasPrefix(dir, rel+"/") {
			_, _ = unix.InotifyRmWatch(w.fd, uint32(wd))
			delete(w.dirs, wd)
			delete(w.wds, dir)
		}
	}
}
