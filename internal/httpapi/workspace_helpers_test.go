//go:build linux

package httpapi

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/workspace"
	"exam-platform/internal/workspace/workspacetest"
	"github.com/coder/websocket"
)

// tarOf makes a bundle: name to content, and a trailing / for a folder.
func tarOf(t *testing.T, files map[string]string, headers ...tar.Header) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	for _, name := range names {
		header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(files[name]))}
		if strings.HasSuffix(name, "/") {
			header.Typeflag, header.Size, header.Mode = tar.TypeDir, 0, 0o755
		}
		if strings.HasSuffix(name, ".sh") || name == "grade" {
			header.Mode = 0o755
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(files[name]))
	}
	for _, header := range headers {
		header := header
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
	}
	_ = writer.Close()
	return buffer.Bytes()
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var starterFiles = map[string]string{
	"Makefile":     "all:\n\techo built\n",
	"src/":         "",
	"src/main.c":   "int main(void) { return 0; }\n",
	"notes.txt":    "first line\nsecond line\n",
	"scripts/":     "",
	"scripts/x.sh": "#!/bin/sh\necho x\n",
}

// document is a compiled workspace exam for the package of starterFiles.
func document(starter, grading []byte) workspace.Document {
	three := int64(120)
	doc := workspace.Document{
		Kind: "workspace", Title: "Lab 1", PIN: "exam-secret", DurationSeconds: 3600, GraceSeconds: &three,
		Workspace: workspace.Settings{Image: "cbe/cpp20", Terminal: true, Run: "echo running; exit 0", Check: "echo checking; exit 3", Ignore: []string{"build/**"},
			Resources: workspace.Resources{CPU: 1, MemoryMB: 256, DiskMB: 50}},
		Content:       workspace.Content{Instructions: "<p>Build it.</p>", Todos: []workspace.Todo{{ID: "t1", TextHTML: "Do <code>one</code>"}, {ID: "t2", TextHTML: "Do two"}}},
		StarterBundle: hashOf(starter),
	}
	if grading != nil {
		doc.GradingBundle = hashOf(grading)
	}
	return doc
}

var gradingFiles = map[string]string{"grade": "#!/bin/sh\necho '{}'\n", "tests/hidden.txt": "HIDDEN-GRADING-MARKER\n"}

// importBody makes the multipart form of an import.
func importBody(t *testing.T, doc workspace.Document, starter, grading []byte, extra map[string][]byte) (string, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	put := func(name string, data []byte) {
		part, err := writer.CreateFormField(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	put("document", encoded)
	put("starter", starter)
	if grading != nil {
		put("grading", grading)
	}
	for name, data := range extra {
		put(name, data)
	}
	_ = writer.Close()
	return body.String(), writer.FormDataContentType()
}

type workspaceFixture struct {
	*apiFixture
	runner  *workspacetest.Runner
	manager *workspace.Manager
	service *workspace.Service
	blobs   workspace.Blobs
	repo    workspace.Repository
	server  *httptest.Server
	examID  string
	options Options
	starter []byte
	grading []byte
	dir     string
}

// newWorkspaceFixture has a class, a published workspace exam made by importing a package through the admin route, and a server on a
// real socket.
func newWorkspaceFixture(t *testing.T, change func(*workspace.Document)) *workspaceFixture {
	t.Helper()
	f := &workspaceFixture{apiFixture: newUnpublishedAPIFixture(t)}
	f.dir = t.TempDir()
	f.runner = workspacetest.New(f.dir)
	f.runner.Now = f.clock.Now
	t.Cleanup(f.runner.Close)
	var err error
	if f.blobs, err = workspace.NewBlobs(f.dir + "/blobs"); err != nil {
		t.Fatal(err)
	}
	f.repo = f.store.Workspaces()
	f.manager = workspace.NewManager(workspace.Options{Repo: f.repo, Runner: f.runner, Blobs: f.blobs, Logger: f.logger, Now: f.clock.Now, SnapshotEvery: 60 * time.Second,
		HeartbeatTimeout: 5 * time.Second, ReadyTimeout: 10 * time.Second, Retention: time.Hour})
	f.service = &workspace.Service{Manager: f.manager, Repo: f.repo, Importer: &workspace.Importer{Repo: f.repo, Runner: f.runner, Blobs: f.blobs, Now: f.clock.Now,
		Max: workspace.Resources{CPU: 2, MemoryMB: 2048, DiskMB: 500}, DefaultGrace: f.sessions.DefaultGraceSeconds}}
	f.options = Options{AdminToken: testAdminToken, Workspace: f.service}
	f.router = New(f.clock, f.sessions, f.logger, f.options)
	f.server = httptest.NewServer(f.router)
	t.Cleanup(f.server.Close)
	f.starter, f.grading = tarOf(t, starterFiles), tarOf(t, gradingFiles)
	doc := document(f.starter, f.grading)
	if change != nil {
		change(&doc)
	}
	body, contentType := importBody(t, doc, f.starter, f.grading, nil)
	w := requestWithType(f.router, "POST", "/admin/class/"+apiClassID+"/workspace-exam", body, contentType, "Bearer "+testAdminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("import = %d %s", w.Code, w.Body.String())
	}
	f.examID = decoded[struct {
		ID string `json:"exam_id"`
	}](t, w.Body.Bytes()).ID
	f.publish(t, f.examID)
	return f
}

func requestWithType(handler http.Handler, method, path, body, contentType, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// loginReady logs a student in and waits for their workspace to be ready.
func (f *workspaceFixture) loginReady(t *testing.T, account string) loginResponse {
	t.Helper()
	login := f.login(t, account)
	eventually(t, "the workspace of "+account, func() bool {
		info, err := f.repo.Session(context.Background(), login.SessionID)
		return err == nil && info.State == workspace.StateReady
	})
	return login
}

// wsClient is a student's application, as far as the WebSocket goes.
type wsClient struct {
	t      *testing.T
	conn   *websocket.Conn
	mu     sync.Mutex
	texts  []map[string]any
	output map[uint32]*bytes.Buffer
	closed chan struct{}
	err    error
	next   uint64
}

func (f *workspaceFixture) dial(t *testing.T, token string) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+"/workspace/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c := &wsClient{t: t, conn: conn, output: map[uint32]*bytes.Buffer{}, closed: make(chan struct{})}
	t.Cleanup(func() { conn.CloseNow() })
	go c.read()
	return c
}

func (c *wsClient) read() {
	defer close(c.closed)
	for {
		kind, data, err := c.conn.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		if kind == websocket.MessageBinary && len(data) >= 4 {
			term := binary.BigEndian.Uint32(data[:4])
			if c.output[term] == nil {
				c.output[term] = &bytes.Buffer{}
			}
			c.output[term].Write(data[4:])
		} else if kind == websocket.MessageText {
			var message map[string]any
			if json.Unmarshal(data, &message) == nil {
				c.texts = append(c.texts, message)
			}
		}
		c.mu.Unlock()
	}
}

func (c *wsClient) send(message map[string]any) {
	c.t.Helper()
	payload, _ := json.Marshal(message)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		c.t.Fatalf("send %s: %v", payload, err)
	}
}

func (c *wsClient) sendRaw(payload string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

func (c *wsClient) keys(term uint32, data string) {
	c.t.Helper()
	payload := binary.BigEndian.AppendUint32(nil, term)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageBinary, append(payload, data...)); err != nil {
		c.t.Fatalf("keys: %v", err)
	}
}

// wait returns the first text message (from the ones not taken yet) that matches.
func (c *wsClient) wait(what string, match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for i, message := range c.texts {
			if match(message) {
				c.texts = append(c.texts[:i], c.texts[i+1:]...)
				c.mu.Unlock()
				return message
			}
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for %s", what)
	return nil
}

// call sends a request and returns its reply.
func (c *wsClient) call(message map[string]any) map[string]any {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id := c.next
	c.mu.Unlock()
	message["id"] = id
	c.send(message)
	return c.wait("the reply to "+message["type"].(string), func(m map[string]any) bool { return uint64OrZero(m["id"]) == id && m["ok"] != nil })
}

func uint64OrZero(v any) uint64 {
	if f, ok := v.(float64); ok {
		return uint64(f)
	}
	return 0
}

func (c *wsClient) ofType(kind string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == kind }
}

func (c *wsClient) outputOf(term uint32) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if b := c.output[term]; b != nil {
		return b.String()
	}
	return ""
}

func (c *wsClient) waitOutput(term uint32, want string) string {
	c.t.Helper()
	eventually(c.t, "output "+want, func() bool { return strings.Contains(c.outputOf(term), want) })
	return c.outputOf(term)
}

func ok(m map[string]any) bool { v, _ := m["ok"].(bool); return v }

func code(m map[string]any) string { s, _ := m["code"].(string); return s }

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
