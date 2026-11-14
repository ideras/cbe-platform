package client

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A student finishes the example package (testdata/workspace/lab-listas) through the real client, a real exam server and a real
// container: they look at the project, run the checks (which fail), implement the two functions, run the checks (which pass), run the
// program, add a file of their own, tick the TODO items and submit. deploy/workspace/test-server.sh runs it (phase D of the end-to-end
// test, internal/workspace/e2e), which then reads the submission back from the server. It needs:
//
//	CBE_E2E_SERVER_URL   the exam server      CBE_E2E_ACCOUNT, CBE_E2E_PIN   a student of the exam it is serving
const solution = `#include "list.hpp"

List::~List() {
    while (head_ != nullptr) {
        Node* next = head_->next;
        delete head_;
        head_ = next;
    }
}

void List::push_front(int value) {
    head_ = new Node{value, head_};
}

void List::insert_sorted(int value) {
    Node** at = &head_;
    while (*at != nullptr && (*at)->value < value) at = &(*at)->next;
    *at = new Node{value, *at};
}

int List::size() const {
    int count = 0;
    for (const Node* node = head_; node != nullptr; node = node->next) {
        ++count;
    }
    return count;
}
`

const ownTest = `#include <cstdio>

#include "list.hpp"

int main() {
    List list;
    list.insert_sorted(5);
    list.insert_sorted(1);
    list.insert_sorted(3);
    if (list.size() != 3 || list.head()->value != 1) { std::puts("mine: FAILED"); return 1; }
    std::puts("mine: ok");
    return 0;
}
`

type page struct {
	mu     sync.Mutex
	output map[uint32]*strings.Builder
	exits  map[uint32]float64
}

func (p *page) options() WorkspaceOptions {
	return WorkspaceOptions{
		OnOutput: func(term uint32, data []byte) {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.output[term] == nil {
				p.output[term] = &strings.Builder{}
			}
			p.output[term].Write(data)
		},
		OnMessage: func(message string) {
			var m struct {
				Type     string  `json:"type"`
				Term     uint32  `json:"term"`
				ExitCode float64 `json:"exit_code"`
			}
			if json.Unmarshal([]byte(message), &m) == nil && m.Type == "pty.exit" {
				p.mu.Lock()
				p.exits[m.Term] = m.ExitCode
				p.mu.Unlock()
			}
		},
		CallTimeout: 60 * time.Second,
	}
}

func TestRealWorkspaceFinishTheExamplePackage(t *testing.T) {
	base, account, pin := os.Getenv("CBE_E2E_SERVER_URL"), os.Getenv("CBE_E2E_ACCOUNT"), os.Getenv("CBE_E2E_PIN")
	if base == "" {
		t.Skip("CBE_E2E_SERVER_URL is not set: this test needs a real exam server (deploy/workspace/test-server.sh)")
	}
	student := &page{output: map[uint32]*strings.Builder{}, exits: map[uint32]float64{}}
	opts := DefaultOptions()
	opts.Screenshots.Required = false
	opts.Server = base
	opts.Workspace = student.options()
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	s.AcknowledgeRules()
	view, err := s.Login(t.Context(), base, account, pin)
	if err != nil {
		t.Fatal(err)
	}
	if view.Phase != "exam" || view.Workspace == nil {
		t.Fatalf("the login did not give a workspace: %+v", view)
	}
	// What the student's page is given before it asks for anything.
	w := view.Workspace
	if w.Title == "" || len(w.Todos) != 3 || !w.Terminal || !w.CanRun || !w.CanCheck || !strings.Contains(w.Instructions, "make") {
		t.Errorf("workspace view = %+v", w)
	}
	waitView(t, s, func(v View) bool {
		return v.Workspace != nil && v.Workspace.Connection == connConnected && v.Workspace.Status != nil && v.Workspace.Status.State == "running"
	})

	call := func(kind string, fields map[string]any) map[string]any {
		t.Helper()
		fields["type"] = kind
		request, _ := json.Marshal(fields)
		reply, err := s.WorkspaceRequest(string(request))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(reply), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	okay := func(reply map[string]any) bool { v, _ := reply["ok"].(bool); return v }
	// A program in a terminal: run, wait for it to end, and what it printed.
	program := func(which string) (int, string) {
		t.Helper()
		reply := call("run.start", map[string]any{"which": which, "cols": 100, "rows": 30})
		if !okay(reply) {
			t.Fatalf("run.start %s = %v", which, reply)
		}
		term := uint32(reply["term"].(float64))
		var code float64
		waitFor(t, which+" to end", func() bool {
			student.mu.Lock()
			defer student.mu.Unlock()
			c, done := student.exits[term]
			code = c
			return done
		})
		time.Sleep(50 * time.Millisecond) // the last of the output, which is batched, is flushed before the exit is told; this is only for the log
		student.mu.Lock()
		defer student.mu.Unlock()
		return int(code), student.output[term].String()
	}

	// The project is there.
	list := call("fs.list", map[string]any{"path": ""})
	var names []string
	for _, entry := range list["entries"].([]any) {
		names = append(names, entry.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "Makefile,include,src,tests" {
		t.Fatalf("the project folder holds %v", names)
	}
	// The checks fail: the two functions are not written.
	if code, out := program("check"); code == 0 || !strings.Contains(out, "FAILED") {
		t.Fatalf("make check before the work: exit %d, %q", code, out)
	}
	// Implement them, writing the file against the version that was read.
	read := call("fs.read", map[string]any{"path": "src/list.cpp"})
	if !okay(read) || !strings.Contains(read["content"].(string), "TODO: put a new node") {
		t.Fatalf("read = %v", read)
	}
	if wrote := call("fs.write", map[string]any{"path": "src/list.cpp", "content": solution, "base_version": read["version"]}); !okay(wrote) {
		t.Fatalf("write = %v", wrote)
	}
	// A write against the old version is refused, not applied over the new one.
	if stale := call("fs.write", map[string]any{"path": "src/list.cpp", "content": "stale", "base_version": read["version"]}); okay(stale) || stale["code"] != "CONFLICT" {
		t.Errorf("a stale write = %v", stale)
	}
	if code, out := program("check"); code != 0 || !strings.Contains(out, "push_front: ok") {
		t.Fatalf("make check after the work: exit %d, %q", code, out)
	}
	if code, out := program("run"); code != 0 || !strings.Contains(out, "1 2 3") || !strings.Contains(out, "g++") {
		t.Fatalf("make run: exit %d, %q", code, out)
	}
	// A file of their own, the TODO ticks and the submission.
	if made := call("fs.create", map[string]any{"path": "tests/mine.cpp", "content": ownTest}); !okay(made) {
		t.Fatalf("create = %v", made)
	}
	for _, item := range []string{"t1", "t2", "t3"} {
		if reply := call("todo.set", map[string]any{"item": item, "done": true}); !okay(reply) {
			t.Fatalf("todo.set %s = %v", item, reply)
		}
	}
	submitted := call("submit", map[string]any{})
	if !okay(submitted) || submitted["snapshot"] == nil {
		t.Fatalf("submit = %v", submitted)
	}
	t.Logf("submitted as snapshot %v", submitted["snapshot"])
}
