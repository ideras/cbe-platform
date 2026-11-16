//go:build linux

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/agentproto"
)

func fsEvent(kind, path string) func(agentproto.Message) bool {
	return func(m agentproto.Message) bool {
		return m.Type == agentproto.TypeFSEvent && m.Kind == kind && m.Path == path
	}
}

func TestFileChangesProduceEvents(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	path := filepath.Join(r.work, "a.txt")

	write(t, path, "one")
	event(t, c, "create a.txt", fsEvent("create", "a.txt"))
	event(t, c, "modify a.txt", fsEvent("modify", "a.txt"))
	if err := os.Rename(path, filepath.Join(r.work, "b.txt")); err != nil {
		t.Fatal(err)
	}
	m := event(t, c, "rename", func(m agentproto.Message) bool { return m.Kind == "rename" })
	if m.Path != "b.txt" || m.From != "a.txt" || m.IsDir {
		t.Errorf("rename = %+v", m)
	}
	if err := os.Remove(filepath.Join(r.work, "b.txt")); err != nil {
		t.Fatal(err)
	}
	event(t, c, "delete b.txt", fsEvent("delete", "b.txt"))
}

func TestNewFoldersAreWatchedAndTheirContentsAnnounced(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	// A folder with files in it appears at once, as when a student unpacks an archive: the files are announced even though they
	// were made before the folder had a watch.
	write(t, filepath.Join(r.work, "stage", "inner", "deep.txt"), "x")
	event(t, c, "create stage", func(m agentproto.Message) bool { return m.Kind == "create" && m.Path == "stage" && m.IsDir })
	event(t, c, "create stage/inner/deep.txt", fsEvent("create", "stage/inner/deep.txt"))
	// And what happens in it afterwards is seen.
	write(t, filepath.Join(r.work, "stage", "inner", "later.txt"), "y")
	event(t, c, "create later.txt", fsEvent("create", "stage/inner/later.txt"))
	// A folder that is renamed keeps being watched under its new name.
	if err := os.Rename(filepath.Join(r.work, "stage"), filepath.Join(r.work, "moved")); err != nil {
		t.Fatal(err)
	}
	event(t, c, "rename stage", func(m agentproto.Message) bool {
		return m.Kind == "rename" && m.Path == "moved" && m.From == "stage" && m.IsDir
	})
	write(t, filepath.Join(r.work, "moved", "inner", "after.txt"), "z")
	event(t, c, "create after.txt", fsEvent("create", "moved/inner/after.txt"))
}

// The plan's acceptance: files created from the terminal produce fs.event messages.
func TestFilesCreatedInATerminalProduceEvents(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	open(t, c, "touch from-terminal.txt; mkdir -p built/obj; echo data > built/obj/out.o; mv from-terminal.txt renamed.txt; rm renamed.txt")
	event(t, c, "create from-terminal.txt", fsEvent("create", "from-terminal.txt"))
	event(t, c, "create built", fsEvent("create", "built"))
	event(t, c, "create out.o", fsEvent("create", "built/obj/out.o"))
	event(t, c, "rename", func(m agentproto.Message) bool {
		return m.Kind == "rename" && m.From == "from-terminal.txt" && m.Path == "renamed.txt"
	})
	event(t, c, "delete renamed.txt", fsEvent("delete", "renamed.txt"))
}

// A write through the agent is a rename of its temporary file over the file: the student's tab hears "modify", and never of the
// temporary file.
func TestAnAgentWriteIsAModifyAndNeverMentionsItsTemporaryFile(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	write(t, filepath.Join(r.work, "doc.txt"), "v1")
	event(t, c, "create", fsEvent("create", "doc.txt"))
	reply := do(t, c, &agentproto.FSRead{Header: agentproto.Header{Type: agentproto.TypeFSRead}, Path: "doc.txt"})
	do(t, c, &agentproto.FSWrite{Header: agentproto.Header{Type: agentproto.TypeFSWrite}, Path: "doc.txt", Content: "v2", BaseVersion: reply.Version})
	deadline := time.After(2 * time.Second)
	sawModify := false
	for !sawModify {
		select {
		case m := <-c.Events:
			if m.Type == agentproto.TypeFSEvent {
				if filepath.Base(m.Path) != "doc.txt" && m.Kind != "overflow" || m.From != "" {
					t.Fatalf("an event names something else: %+v", m)
				}
				sawModify = m.Kind == "modify" && m.Path == "doc.txt"
			}
		case <-deadline:
			t.Fatal("no modify event for the write")
		}
	}
}

// Repeated writes to one file are one event; a flood becomes an overflow, never a flood of events.
func TestRepeatedWritesAreMergedAndFloodsOverflow(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	path := filepath.Join(r.work, "log.txt")
	write(t, path, "")
	event(t, c, "create", fsEvent("create", "log.txt"))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 500 {
		_, _ = file.WriteString("line\n")
	}
	file.Close()
	time.Sleep(400 * time.Millisecond)
	modifies := 0
	for done := false; !done; {
		select {
		case m := <-c.Events:
			if m.Kind == "modify" && m.Path == "log.txt" {
				modifies++
			}
		default:
			done = true
		}
	}
	if modifies == 0 || modifies > 5 {
		t.Errorf("500 writes made %d modify events", modifies)
	}

}

// More events than can wait for a flush are replaced by one overflow event, so a flood costs the client one message, never thousands.
func TestAFloodOfEventsBecomesAnOverflow(t *testing.T) {
	var mu sync.Mutex
	var got []agentproto.Message
	w, err := NewWatcher(t.TempDir(), 10, func(m agentproto.Message) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.mu.Lock()
	for i := range maxPending + 500 {
		w.queue(agentproto.Message{Type: agentproto.TypeFSEvent, Kind: "create", Path: fmt.Sprintf("f%d", i)})
	}
	w.mu.Unlock()
	waitFor(t, "the flush", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) > 0
	})
	time.Sleep(2 * flushEvery)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != maxPending+1 || got[len(got)-1].Kind != "overflow" || got[0].Path != "f0" {
		t.Fatalf("%d events, last %+v", len(got), got[len(got)-1])
	}
}

func TestTooManyFoldersToWatchIsAnOverflow(t *testing.T) {
	shortDir := t.TempDir()
	for i := range 6 {
		if err := os.MkdirAll(filepath.Join(shortDir, "work", "d", string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := startAgent(t, func(cfg *Config) {
		cfg.Work = filepath.Join(shortDir, "work")
		cfg.MaxWatches = 3
	})
	c := r.dial(t)
	if got := r.agent.watcher.Watches(); got != 3 {
		t.Fatalf("watches = %d, want 3", got)
	}
	event(t, c, "overflow", func(m agentproto.Message) bool { return m.Type == agentproto.TypeFSEvent && m.Kind == "overflow" })
}
