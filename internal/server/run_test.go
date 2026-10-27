package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/config"
	"exam-platform/internal/storage"
)

func serverConfig(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Parse([]string{
		"-database=" + filepath.Join(dir, "data", "exam.db"), "-screenshots=" + filepath.Join(dir, "images"),
		"-admin-token-file=" + filepath.Join(dir, "token"),
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.AdminTokenFile, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestListenerPreflightDoesNotImport(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := serverConfig(t)
	cfg.Bind = listener.Addr().String()
	err = Run(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "listener preflight") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(cfg.Database); !os.IsNotExist(err) {
		t.Fatalf("listener failure touched database: %v", err)
	}
}

func TestStartupAndGracefulShutdown(t *testing.T) {
	cfg := serverConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bind = listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + cfg.Bind + "/healthz")
		if err == nil {
			var body map[string]string
			err = json.NewDecoder(response.Body).Decode(&body)
			response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK || body["status"] != "ok" {
				t.Fatalf("health response = %v, %v", body, err)
			}
			ready = true
			break
		}
		select {
		case err := <-stopped:
			t.Fatalf("server exited before startup: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("server did not start")
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	s, err := storage.Open(t.Context(), cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	classes, err := s.ListClasses(t.Context())
	if err != nil || len(classes) != 0 {
		t.Fatalf("a server starts with no classes: %+v, %v", classes, err)
	}
}

// A server started with no class and no exam is the normal exam-day start: the proctor prepares everything through the
// admin API, and the classes, their exams and what is published are remembered across a restart.
func TestStartsEmptyAndKeepsWhatTheProctorPublishedAcrossARestart(t *testing.T) {
	cfg := serverConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bind = listener.Addr().String()
	listener.Close()
	base := "http://" + cfg.Bind
	client := &http.Client{Timeout: time.Second}
	send := func(method, path, contentType, body string) (int, string) {
		t.Helper()
		request, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(data)
	}
	run := func() (stop func()) {
		ctx, cancel := context.WithCancel(t.Context())
		stopped := make(chan error, 1)
		go func() { stopped <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if response, err := client.Get(base + "/healthz"); err == nil {
				response.Body.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("server did not start without a roster")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return func() {
			cancel()
			if err := <-stopped; err != nil {
				t.Fatal(err)
			}
		}
	}

	stop := run()
	if status, body := send("GET", "/admin/classes", "application/json", ""); status != 200 || !strings.Contains(body, `"classes":[]`) {
		t.Fatalf("classes = %d %s", status, body)
	}
	if status, body := send("POST", "/login", "application/json", `{"account_number":"0012","exam_pin":"pin"}`); status != 503 {
		t.Fatalf("login with nothing published = %d %s", status, body)
	}
	status, body := send("POST", "/admin/classes?name=Mathematics%2010A", "text/csv", "canvas_id,account_number,name,email,sis_user_id\n1,0012,Ada,a@x.invalid,S1\n")
	var class struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	if err := json.Unmarshal([]byte(body), &class); err != nil || status != 201 || class.Class.ID == "" {
		t.Fatalf("class upload = %d %s", status, body)
	}
	status, body = send("POST", "/admin/class/"+class.Class.ID+"/exam", "application/json", `{"title":"T","pin":"pin","duration_seconds":60,"content":{"exercises":[{"id":"1","title":"T","body":"<span data-slot=\"q1\"></span>","slots":[{"id":"q1","kind":"blank"}]}]}}`)
	var created struct {
		ID string `json:"exam_id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || status != 201 {
		t.Fatalf("create = %d %s", status, body)
	}
	if status, body := send("POST", "/admin/exam/"+created.ID+"/publish", "application/json", `{}`); status != 200 {
		t.Fatalf("publish = %d %s", status, body)
	}
	if status, _ := send("POST", "/login", "application/json", `{"account_number":"0012","exam_pin":"pin"}`); status != 200 {
		t.Fatalf("login after publishing = %d", status)
	}
	stop()

	stop = run()
	defer stop()
	if status, body := send("GET", "/admin/classes", "application/json", ""); status != 200 || !strings.Contains(body, `"name":"Mathematics 10A"`) || !strings.Contains(body, `"student_count":1`) || !strings.Contains(body, `"exam_count":1`) || !strings.Contains(body, `"published_exam":{"exam_id":"`+created.ID+`"`) {
		t.Fatalf("classes after restart = %d %s", status, body)
	}
	if status, _ := send("POST", "/login", "application/json", `{"account_number":"0012","exam_pin":"pin"}`); status != 200 {
		t.Fatalf("login after restart = %d: the published exam was forgotten", status)
	}
}
