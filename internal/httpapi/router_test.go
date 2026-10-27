package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func TestHealthUsesInjectedClock(t *testing.T) {
	router := New(fixedClock{}, stubSessions{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/healthz", nil))
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 200 || response["server_time"] != "2026-01-02T03:04:05Z" || response["status"] != "ok" {
		t.Fatalf("response = %d %v", recorder.Code, response)
	}
	for path, expected := range map[string]int{examPath: 401, "/screenshots/a.png": 404} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != expected {
			t.Fatalf("%s = %d, want %d", path, recorder.Code, expected)
		}
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/healthz", nil))
	if recorder.Code != 405 {
		t.Fatalf("POST health = %d", recorder.Code)
	}
}
