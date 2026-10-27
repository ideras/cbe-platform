package client

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readRealImage(t *testing.T, server *realServer, id int64, thumbnail bool, expected int) []byte {
	t.Helper()
	path := fmt.Sprintf("/admin/screenshot/%d", id)
	if thumbnail {
		path += "/thumbnail"
	}
	request, _ := http.NewRequest("GET", server.base+path, nil)
	request.Header.Set("Authorization", "Bearer "+server.credential)
	h := newHTTPClient(time.Second)
	defer h.CloseIdleConnections()
	response, err := h.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		t.Fatalf("image status %d want %d", response.StatusCode, expected)
	}
	data, _ := io.ReadAll(response.Body)
	return data
}
func TestRealServerPendingCaptureAfterSubmitRestartProtectedReadAndPurge(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 60, 10)
	stop := server.start(t, exam, fastPolicy())
	data := syntheticPNG(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	releaseCapture := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCapture()
	var captures atomic.Int64
	options := DefaultOptions()
	options.Screenshots.Required = false
	options.Server = server.base
	options.Debounce = 10 * time.Millisecond
	options.Periodic = 50 * time.Millisecond
	options.Poll = 20 * time.Millisecond
	options.Monitoring.Tick = 5 * time.Millisecond
	options.RetryMin = 20 * time.Millisecond
	options.RetryMax = 80 * time.Millisecond
	options.Screenshots.Timeout = time.Second
	options.Screenshots.Lifetime = 3 * time.Second
	options.Screenshots.Capture = func(ctx context.Context) ([]byte, error) {
		captures.Add(1)
		once.Do(func() { close(started) })
		select {
		case <-release:
			return bytes.Clone(data), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	options.Screenshots.Permission = func(bool) CapturePermission {
		return CapturePermission{Supported: true, Granted: true, Message: "synthetic test-only adapter"}
	}
	s, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.ServiceShutdown()
	s.AcknowledgeRules()
	ReportNativeFocus(s, true)
	if _, err := s.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportNativeFocus(s, false)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("real policy did not trigger capture")
	}
	update(t, s, "autosaved while capture pending", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	if _, err := s.Submit(map[string]string{"q1": "immutable final", "q2": ""}, 2); err != nil {
		t.Fatal(err)
	}
	terminal := waitView(t, s, func(v View) bool { return v.Phase == "complete" && !v.Monitoring.Active })
	releaseCapture()
	uploaded := waitView(t, s, func(v View) bool { return v.Capture.State == "uploaded" })
	if captures.Load() != 1 || uploaded.Capture.EventID <= 0 || uploaded.Capture.ScreenshotID <= 0 || uploaded.Final.VersionID != terminal.Final.VersionID {
		t.Fatal("pending upload changed final or recaptured")
	}
	for _, thumbnail := range []bool{false, true} {
		// The client uploads JPEG and the server stores and serves canonical JPEG.
		if _, err := jpeg.DecodeConfig(bytes.NewReader(readRealImage(t, server, uploaded.Capture.ScreenshotID, thumbnail, 200))); err != nil {
			t.Fatal(err)
		}
	}
	request, _ := http.NewRequest("GET", fmt.Sprintf("%s/admin/screenshot/%d", server.base, uploaded.Capture.ScreenshotID), nil)
	request.Header.Set("Authorization", "Bearer "+s.token)
	response, err := s.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("student downloaded private image")
	}
	_ = s.ServiceShutdown()
	stop()
	stop = server.start(t, exam, fastPolicy())
	readRealImage(t, server, uploaded.Capture.ScreenshotID, false, 200)
	var sessions struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &sessions, 200)
	if len(sessions.Sessions) != 1 {
		t.Fatal("lost session")
	}
	var timeline map[string]any
	server.admin(t, "GET", "/admin/session/"+sessions.Sessions[0].ID+"/timeline", nil, &timeline, 200)
	var purge struct {
		Complete bool `json:"complete"`
		Deleted  int  `json:"deleted"`
	}
	server.admin(t, "POST", "/admin/purge-screenshots/"+exam, map[string]bool{"confirm": true}, &purge, 200)
	if !purge.Complete || purge.Deleted != 1 {
		t.Fatal("real purge failed")
	}
	readRealImage(t, server, uploaded.Capture.ScreenshotID, false, 404)
	stop()
	stop = server.start(t, exam, fastPolicy())
	readRealImage(t, server, uploaded.Capture.ScreenshotID, false, 404)
	server.admin(t, "GET", "/admin/session/"+sessions.Sessions[0].ID+"/timeline", nil, &timeline, 200)
	text := fmt.Sprint(timeline)
	if !strings.Contains(text, "immutable final") || !strings.Contains(text, "focus_lost") || strings.Contains(text, "file_path") {
		t.Fatal("purge lost event/answer history or exposed paths")
	}
	stop()
}
