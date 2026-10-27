package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealServerRequiredCaptureFailureRecoveryPreservesAnswersAndDeadline(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 60, 10)
	stop := server.start(t, exam, fastPolicy())
	defer stop()
	var failing atomic.Bool
	data := syntheticPNG(t)
	o := DefaultOptions()
	o.Server = server.base
	o.Debounce, o.Periodic, o.Poll = 10*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond
	o.RetryMin, o.RetryMax = 20*time.Millisecond, 80*time.Millisecond
	o.Monitoring.Tick = 5 * time.Millisecond
	o.Screenshots.Test = func(context.Context) (CaptureTestResult, error) {
		return CaptureTestResult{Width: 4, Height: 3}, nil
	}
	o.Screenshots.Permission = func(bool) CapturePermission {
		return CapturePermission{Supported: true, TestRequired: true}
	}
	o.Screenshots.Capture = func(context.Context) ([]byte, error) {
		if failing.Load() {
			return nil, errors.New("synthetic native failure")
		}
		return bytes.Clone(data), nil
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.ServiceShutdown()
	s.AcknowledgeRules()
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	if _, err := s.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	initial := waitView(t, s, func(v View) bool {
		return !v.Capture.Blocked && v.Monitoring.HeartbeatAt != nil && v.Monitoring.Queued == 0
	})
	update(t, s, "work before capture failure", 1)
	failing.Store(true)
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return v.Capture.Blocked && v.Capture.PendingReports == 0 })
	update(t, s, "latest preserved work while capture is locked", 2)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 2 })
	failing.Store(false)
	verifyRequired(t, s)
	if !s.Snapshot().Status.DeadlineAt.Equal(initial.Status.DeadlineAt) {
		t.Fatal("recovery reset the server deadline")
	}
	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	if len(list.Sessions) != 1 {
		t.Fatal("unexpected session identity")
	}
	var history json.RawMessage
	server.admin(t, "GET", "/admin/session/"+list.Sessions[0].ID+"/timeline", nil, &history, 200)
	for _, value := range []string{"capture_ready", "capture_failed", "capture_restored", "latest preserved work while capture is locked"} {
		if !strings.Contains(string(history), value) {
			t.Fatalf("protected server history omitted %q", value)
		}
	}
}
