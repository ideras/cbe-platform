package client

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Phase 8 fuzzing of the student application's side of the workspace connection: what the exam server sends (the server is trusted, but what
// passes through it is the student's own files and an agent's words, and one bad message must never stop the connection or the page) and what
// the page asks for. Plain `go test` runs the seeds; the Phase 8 acceptance runs each for five minutes:
//
//	(cd apps/student && go test ./internal/client -run '^$' -fuzz FuzzWorkspaceServerMessage -fuzztime 5m)
//	(cd apps/student && go test ./internal/client -run '^$' -fuzz FuzzWorkspacePageRequest -fuzztime 5m)
func FuzzWorkspaceServerMessage(f *testing.F) {
	for _, seed := range []string{
		`{"type":"ws.status","state":"running","seconds_left":60,"terminal":true}`, `{"id":1,"type":"fs.list","ok":true,"entries":[]}`, `{"id":1,"type":"fs.read","ok":false,"code":"NOT_FOUND"}`,
		`{"type":"fs.event","kind":"modify","path":"a.c"}`, `{"type":"pty.exit","term":1,"exit_code":0}`, `{"type":"ws.agent_restarted"}`, `{"type":"agent.error","code":"RATE"}`,
		`{"id":18446744073709551615,"ok":true}`, `{"id":0,"ok":true}`, `{"id":1}`, `null`, `[]`, `"x"`, `1`, `{}`, `{"type":1,"id":"1","ok":"yes"}`, "\xef\xbb\xbf{}", `{"type":"ws.status","state":`,
	} {
		f.Add([]byte(seed))
	}
	var mu sync.Mutex
	var told []string
	link := newWorkspaceLink(context.Background(), http.DefaultClient, WorkspaceOptions{OnMessage: func(message string) {
		mu.Lock()
		told = append(told, message)
		mu.Unlock()
	}}, time.Millisecond, time.Millisecond)
	f.Fuzz(func(t *testing.T, data []byte) {
		mu.Lock()
		told = told[:0]
		mu.Unlock()
		// A request somebody is waiting for, as the page's is.
		waiting := make(chan []byte, 1)
		link.mu.Lock()
		link.pending[1] = waiting
		link.mu.Unlock()
		out := newOutputBatcher(nil)
		link.text(data, out)
		link.mu.Lock()
		delete(link.pending, 1)
		link.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		// What the page is told is always a JSON text it can read, and is the message itself.
		for _, message := range told {
			if !json.Valid([]byte(message)) || message != string(data) {
				t.Fatalf("the page was told %q for %q", message, data)
			}
		}
		// An answer reaches the one waiting for it only if it is an answer.
		select {
		case reply := <-waiting:
			var head struct {
				ID uint64 `json:"id"`
				OK *bool  `json:"ok"`
			}
			if json.Unmarshal(reply, &head) != nil || head.ID != 1 || head.OK == nil {
				t.Fatalf("%q was taken for the answer to request 1", reply)
			}
			if len(told) != 0 {
				t.Fatalf("an answer was also told to the page: %q", data)
			}
		default:
		}
	})
}

// FuzzWorkspacePageRequest is whatever the page's code (or a script that got into it) hands WorkspaceRequest: it is answered or refused, never
// sent as it is, and what is sent is a request of the protocol.
func FuzzWorkspacePageRequest(f *testing.F) {
	for _, seed := range []string{
		`{"type":"fs.list","path":""}`, `{"type":"fs.write","path":"a","content":"x"}`, `{"type":"run.start","which":"run"}`, `{"type":"todo.set","item":"t1","done":true}`,
		`{"type":"submit"}`, `{"type":"paste.blocked","target":"editor","bytes":3}`, `{"type":"agent.hello"}`, `{"type":"ws.status"}`, `{"id":5,"type":"fs.list"}`, `{}`, `[]`, `null`, `"x"`, `{"type":1}`, `{"type":"fs.list","id":"x"}`,
	} {
		f.Add(seed)
	}
	link := newWorkspaceLink(context.Background(), http.DefaultClient, WorkspaceOptions{CallTimeout: 5 * time.Millisecond}, time.Millisecond, time.Millisecond)
	f.Fuzz(func(t *testing.T, message string) {
		reply, err := link.call(message)
		if err == nil {
			// Without a connection nothing can be answered: whatever the page asked, it has an error and no answer.
			t.Fatalf("%q was answered with %q on a link that has no connection", message, reply)
		}
		link.mu.Lock()
		left := len(link.pending)
		link.mu.Unlock()
		if left != 0 {
			t.Fatalf("%q left %d requests waiting", message, left)
		}
	})
}
