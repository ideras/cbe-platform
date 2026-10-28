package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type realServer struct {
	binary, base, bind, dir, credential string
	logs                                bytes.Buffer
}

func setupRealServer(t *testing.T) *realServer {
	t.Helper()
	binary := os.Getenv("EXAM_SERVER_BINARY")
	if binary == "" {
		t.Skip("make student-client-test enables the built-server demonstration")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bind := listener.Addr().String()
	_ = listener.Close()
	server := &realServer{binary: binary, base: "http://" + bind, bind: bind, dir: t.TempDir(), credential: strings.Repeat("synthetic-private-", 4)}
	for name, value := range map[string]string{"credential": server.credential} {
		if err := os.WriteFile(filepath.Join(server.dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := restrictTokenACL(filepath.Join(server.dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return server
}

// start runs the built server with the given monitoring settings and, when exam is not empty, makes sure the proctor has
// published it (as the dashboard does). The settings are stored before the publish, so the exam freezes them.
func (s *realServer) start(t *testing.T, exam string, policy testPolicy) func() {
	t.Helper()
	args := []string{"-bind", s.bind, "-database", filepath.Join(s.dir, "exam.db"), "-screenshots", filepath.Join(s.dir, "images"), "-admin-token-file", filepath.Join(s.dir, "credential"), "-worker-interval", "20ms"}
	command := exec.Command(s.binary, args...)
	command.Stdout = &s.logs
	command.Stderr = &s.logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if err := command.Process.Signal(os.Interrupt); err != nil {
				_ = command.Process.Kill()
			}
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				_ = command.Process.Kill()
				<-finished
			}
		})
	}
	t.Cleanup(stop)
	h := newHTTPClient(200 * time.Millisecond)
	defer h.CloseIdleConnections()
	ready := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		response, err := h.Get(s.base + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		stop()
		t.Fatal("built server did not start")
	}
	storeTestPolicy(t, s.dir, policy)
	if exam != "" {
		s.publish(t, exam)
	}
	return stop
}

const syntheticRoster = "canvas_id,account_number,name,email,sis_user_id\n1,00123,One,one@example.invalid,1\n2,00456,Two,two@example.invalid,2\n"

// class makes sure the synthetic class exists, as the proctor adds a class from the dashboard, and returns its ID.
func (s *realServer) class(t *testing.T) string {
	t.Helper()
	var classes struct {
		Classes []struct {
			ID string `json:"class_id"`
		} `json:"classes"`
	}
	s.admin(t, "GET", "/admin/classes", nil, &classes, 200)
	if len(classes.Classes) > 0 {
		return classes.Classes[0].ID
	}
	request, err := http.NewRequest("POST", s.base+"/admin/classes?name=Synthetic+class", strings.NewReader(syntheticRoster))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+s.credential)
	request.Header.Set("Content-Type", "text/csv")
	h := newHTTPClient(time.Second)
	defer h.CloseIdleConnections()
	response, err := h.Do(request)
	if err != nil || response.StatusCode != 201 {
		t.Fatal("fixture class creation failed")
	}
	defer response.Body.Close()
	var created struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created.Class.ID
}

// publish admits exam, which belongs to the synthetic class, as the proctor does from the dashboard.
func (s *realServer) publish(t *testing.T, exam string) {
	t.Helper()
	var catalog struct {
		CurrentExamID string `json:"current_exam_id"`
	}
	s.admin(t, "GET", "/admin/exams", nil, &catalog, 200)
	if catalog.CurrentExamID != exam {
		s.admin(t, "POST", "/admin/exam/"+exam+"/publish", map[string]any{"expected_current": catalog.CurrentExamID, "allow_active": true}, nil, 200)
	}
}
func (s *realServer) admin(t *testing.T, method, path string, input, output any, expected int) {
	t.Helper()
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, s.base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+s.credential)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	h := newHTTPClient(time.Second)
	defer h.CloseIdleConnections()
	response, err := h.Do(request)
	if err != nil {
		t.Fatal("proctor fixture request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		t.Fatalf("fixture HTTP status: %d", response.StatusCode)
	}
	if output != nil {
		if err := json.NewDecoder(response.Body).Decode(output); err != nil {
			t.Fatal(err)
		}
	}
}
func (s *realServer) create(t *testing.T, duration, grace int) string {
	t.Helper()
	stop := s.start(t, "", fastPolicy())
	var reply struct {
		ID string `json:"exam_id"`
	}
	s.admin(t, "POST", "/admin/class/"+s.class(t)+"/exam", map[string]any{"title": "Synthetic exam", "pin": "synthetic PIN", "duration_seconds": duration, "grace_seconds": grace, "content": Content{Exercises: []Exercise{{ID: "1", Title: "Synthetic", Body: `<p>pure text</p><div data-slot="q1"></div><div data-slot="q2"></div>`, Slots: []Slot{{ID: "q1", Kind: "text"}, {ID: "q2", Kind: "code"}}}}}}, &reply, 201)
	stop()
	return reply.ID
}
func realClient(t *testing.T, base string) *Service { t.Helper(); return realClientWith(t, base, nil) }
func realClientWith(t *testing.T, base string, modify func(*Options)) *Service {
	t.Helper()
	options := DefaultOptions()
	options.Screenshots.Required = false
	options.Server = base
	options.Debounce = 25 * time.Millisecond
	options.Periodic = 100 * time.Millisecond
	options.Poll = 20 * time.Millisecond
	options.RetryMin = 20 * time.Millisecond
	options.RetryMax = 80 * time.Millisecond
	if modify != nil {
		modify(&options)
	}
	s, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	s.AcknowledgeRules()
	return s
}
func TestRealServerClientResumeSubmitAndForce(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 60, 10)
	stop := server.start(t, exam, fastPolicy())
	first := realClient(t, server.base)
	initial, err := first.Login(t.Context(), server.base, "00123", "synthetic PIN")
	if err != nil {
		t.Fatal(err)
	}
	update(t, first, "acknowledged π\nline two", 1)
	saved := waitView(t, first, func(v View) bool { return v.AcknowledgedRevision == 1 })
	_ = first.ServiceShutdown()
	stop()
	stop = server.start(t, exam, fastPolicy())
	second := realClient(t, server.base)
	resumed, err := second.Login(t.Context(), server.base, "00123", "synthetic PIN")
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Status.DeadlineAt.Equal(initial.Status.DeadlineAt) || resumed.Answers["q1"] != "acknowledged π\nline two" || resumed.Saved.Sequence != saved.Saved.Sequence {
		t.Fatal("restart did not restore acknowledged work and fixed deadline")
	}
	_, err = second.Submit(map[string]string{"q1": "latest visible text", "q2": "debounce never fired"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	final := waitView(t, second, func(v View) bool { return v.Final != nil })
	if final.Final.Sequence != 2 {
		t.Fatal("submit did not append current editor snapshot")
	}
	third := realClient(t, server.base)
	if _, err = third.Login(t.Context(), server.base, "00123", "synthetic PIN"); failureCode(err) != 409 {
		t.Fatal("terminal login not distinct")
	}
	fourth := realClient(t, server.base)
	if _, err = fourth.Login(t.Context(), server.base, "00456", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	update(t, fourth, "proctor selects latest saved", 1)
	waitView(t, fourth, func(v View) bool { return v.AcknowledgedRevision == 1 })
	var list struct {
		Sessions []struct {
			Account string `json:"account_number"`
			ID      string `json:"session_id"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	var firstID, otherID string
	for _, session := range list.Sessions {
		if session.Account == "00123" {
			firstID = session.ID
		}
		if session.Account == "00456" {
			otherID = session.ID
		}
	}
	if firstID == "" || otherID == "" {
		t.Fatal("fixture sessions missing")
	}
	var selected Final
	server.admin(t, "POST", "/admin/force-submit/"+otherID, map[string]string{"reason": "synthetic force check"}, &selected, 200)
	waitView(t, fourth, func(v View) bool { return v.Phase == "complete" && v.Status.State == "force_submitted" })
	if _, err = fourth.UpdateAnswers(map[string]string{"q1": "blocked"}, 2); err == nil {
		t.Fatal("forced client remained editable")
	}
	var timeline struct {
		Items []struct {
			Version *struct {
				Answers map[string]string `json:"answers"`
				IsFinal bool              `json:"is_final"`
			} `json:"answer_version"`
		} `json:"items"`
	}
	server.admin(t, "GET", "/admin/session/"+firstID+"/timeline", nil, &timeline, 200)
	found := false
	for _, item := range timeline.Items {
		if item.Version != nil && item.Version.IsFinal {
			found = item.Version.Answers["q1"] == "latest visible text" && item.Version.Answers["q2"] == "debounce never fired"
		}
	}
	if !found {
		t.Fatal("final history differs from visible submitted text")
	}
	_ = second.ServiceShutdown()
	_ = fourth.ServiceShutdown()
	stop()
	if strings.Contains(server.logs.String(), "synthetic PIN") || strings.Contains(server.logs.String(), second.token) || strings.Contains(server.logs.String(), server.credential) {
		t.Fatal("built server logs leaked credentials")
	}
}
func TestRealServerClientGraceAndExpiry(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 1, 1)
	stop := server.start(t, exam, fastPolicy())
	s := realClient(t, server.base)
	if _, err := s.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Status != nil && v.Status.State == "grace" })
	update(t, s, "committed during grace", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	if _, err := s.UpdateAnswers(map[string]string{"q1": "late"}, 2); err == nil {
		t.Fatal("expiry did not lock client")
	}
	_ = s.ServiceShutdown()
	stop()
}
