package client

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func requiredCaptureClient(t *testing.T, f *fixture, failing *atomic.Bool, configure ...func(*Options)) *Service {
	t.Helper()
	data := syntheticPNG(t)
	return monitorClient(t, f, func(o *Options) {
		o.Screenshots.Required = true
		o.Screenshots.Test = func(context.Context) (CaptureTestResult, error) {
			if failing.Load() {
				return CaptureTestResult{}, errors.New("private native error")
			}
			return CaptureTestResult{Width: 4, Height: 3}, nil
		}
		o.Screenshots.Capture = func(context.Context) ([]byte, error) {
			if failing.Load() {
				return nil, errors.New("private native error")
			}
			return bytes.Clone(data), nil
		}
		o.Screenshots.Permission = func(bool) CapturePermission {
			return CapturePermission{Supported: true, TestRequired: true, Message: "portal requires verification"}
		}
		for _, change := range configure {
			change(o)
		}
	})
}

func verifyRequired(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.TestScreenshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Capture.Ready && !v.Capture.Blocked })
}

func TestCaptureAdmissionRequiresGoVerificationAndReverificationAfterRestart(t *testing.T) {
	f := newFixture(t)
	var failing atomic.Bool
	s := requiredCaptureClient(t, f, &failing)
	if _, err := s.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil || s.Snapshot().Phase != "login" {
		t.Fatal("session started without capture verification")
	}
	verifyRequired(t, s)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked })
	if !slices.Contains(telemetryTypes(f), "capture_ready") {
		t.Fatal("admission verification was not server logged")
	}
	fresh := requiredCaptureClient(t, f, &failing)
	if _, err := fresh.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil {
		t.Fatal("new process resumed without a fresh screenshot test")
	}
	verifyRequired(t, fresh)
	loginClient(t, fresh, f)
}

func TestCaptureFailureLocksPreservesAutosaveRetriesLogAndRequiresRetest(t *testing.T) {
	f := newFixture(t)
	var failing, logOffline atomic.Bool
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, event telemetryRecord) bool {
		if event.kind == "capture_failed" && logOffline.Load() {
			http.Error(w, "offline", 503)
			return true
		}
		return false
	}
	s := requiredCaptureClient(t, f, &failing)
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked && v.Monitoring.Queued == 0 })
	update(t, s, "visible work before failure", 1)
	failing.Store(true)
	logOffline.Store(true)
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return v.Capture.Blocked && !v.Capture.Ready && v.Capture.ReportError != "" })
	// A transfer already queued by the frontend must still reach durable autosave.
	update(t, s, "latest visible text retained across lock", 2)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 2 })
	time.Sleep(160 * time.Millisecond)
	f.mu.Lock()
	var failures []telemetryRecord
	for _, record := range f.telemetry {
		if record.kind == "capture_failed" {
			failures = append(failures, record)
		}
	}
	work := f.answers["q1"]
	f.mu.Unlock()
	if len(failures) < 4 || work != "latest visible text retained across lock" {
		t.Fatal("failure log exhausted retries or work was lost")
	}
	for _, record := range failures {
		if record.detail["client_report_id"] != failures[0].detail["client_report_id"] {
			t.Fatal("log retry changed report identity")
		}
	}
	failing.Store(false)
	if _, err := s.TestScreenshot(t.Context()); err == nil {
		t.Fatal("retest ignored unacknowledged failure log")
	}
	logOffline.Store(false)
	waitView(t, s, func(v View) bool { return v.Capture.PendingReports == 0 })
	if !s.Snapshot().Capture.Blocked {
		t.Fatal("log acknowledgment alone reopened editing without a retest")
	}
	verifyRequired(t, s)
	if !slices.Contains(telemetryTypes(f), "capture_restored") || s.Snapshot().Phase != "exam" {
		t.Fatal("capture recovery missing or changed lifecycle")
	}
	f.mu.Lock()
	f.status.State = "grace"
	f.status.SecondsRemaining = 0
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Status != nil && v.Status.State == "grace" && !v.Capture.Blocked })
	ReportNativeFocus(s, true)
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return v.Capture.State == "uploaded" })
}

func TestCaptureFailureLogsContinueAfterTerminalAndNeverReopenCompletedExam(t *testing.T) {
	f := newFixture(t)
	var failing, offline atomic.Bool
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, event telemetryRecord) bool {
		if event.kind == "capture_failed" && offline.Load() {
			http.Error(w, "offline", 503)
			return true
		}
		return false
	}
	s := requiredCaptureClient(t, f, &failing)
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked })
	failing.Store(true)
	offline.Store(true)
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return v.Capture.PendingReports > 0 && !v.Capture.Ready })
	f.mu.Lock()
	f.status.State = "force_submitted"
	f.status.SecondsRemaining = 0
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	offline.Store(false)
	waitView(t, s, func(v View) bool { return v.Capture.PendingReports == 0 })
	if _, err := s.TestScreenshot(t.Context()); err == nil || s.Snapshot().Phase != "complete" {
		t.Fatal("retesting reopened completed exam")
	}
}

func TestCaptureFailureDuringRetestCannotRestoreReadinessBeforeFailureLogAck(t *testing.T) {
	f := newFixture(t)
	var failing, revoked, blocking, logOffline atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, event telemetryRecord) bool {
		if event.kind == "capture_failed" && logOffline.Load() {
			http.Error(w, "offline", 503)
			return true
		}
		return false
	}
	s := requiredCaptureClient(t, f, &failing, func(o *Options) {
		o.Screenshots.Permission = func(bool) CapturePermission {
			return CapturePermission{Supported: true, Granted: !revoked.Load()}
		}
		o.Screenshots.Test = func(ctx context.Context) (CaptureTestResult, error) {
			if blocking.Load() {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return CaptureTestResult{}, ctx.Err()
				}
			}
			return CaptureTestResult{Width: 4, Height: 3}, nil
		}
	})
	verifyRequired(t, s)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked })
	blocking.Store(true)
	done := make(chan error, 1)
	go func() { _, err := s.TestScreenshot(t.Context()); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("retest did not start")
	}
	logOffline.Store(true)
	revoked.Store(true)
	s.capture.permission(false)
	waitView(t, s, func(v View) bool { return v.Capture.ReportError != "" })
	revoked.Store(false)
	close(release)
	if err := <-done; err == nil || s.Snapshot().Capture.Ready {
		t.Fatal("a concurrent unacknowledged failure was hidden by retest success")
	}
	logOffline.Store(false)
	blocking.Store(false)
	waitView(t, s, func(v View) bool { return v.Capture.PendingReports == 0 })
	verifyRequired(t, s)
}

func TestCaptureVerificationCancelledDuringPermissionRefreshNeverArms(t *testing.T) {
	f := newFixture(t)
	var failing, cancelOnRefresh atomic.Bool
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := requiredCaptureClient(t, f, &failing, func(o *Options) {
		o.Screenshots.Permission = func(bool) CapturePermission {
			if cancelOnRefresh.Load() {
				cancel()
			}
			return CapturePermission{Supported: true, Granted: true}
		}
	})
	cancelOnRefresh.Store(true)
	if _, err := s.TestScreenshot(ctx); err == nil || s.Snapshot().Capture.Ready {
		t.Fatal("cancelled verification granted capture readiness")
	}
}

func TestRequiredCaptureDefaultsFailClosedWithoutNativeAdapters(t *testing.T) {
	o := DefaultOptions()
	if !o.Screenshots.Required {
		t.Fatal("native verification is not required by default")
	}
	if _, err := New(o); err == nil {
		t.Fatal("required native adapters were optional")
	}
}

func TestObservedPermissionRevocationLocksUntilSuccessfulRetest(t *testing.T) {
	f := newFixture(t)
	var failing, revoked atomic.Bool
	s := requiredCaptureClient(t, f, &failing, func(o *Options) {
		o.Screenshots.Permission = func(bool) CapturePermission {
			return CapturePermission{Supported: true, Granted: !revoked.Load()}
		}
	})
	verifyRequired(t, s)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked })
	update(t, s, "preserved across permission revocation", 1)
	revoked.Store(true)
	waitView(t, s, func(v View) bool { return !v.Capture.Ready && v.Capture.PendingReports == 0 })
	if !slices.Contains(telemetryTypes(f), "capture_failed") {
		t.Fatal("observed permission revocation was not logged")
	}
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	if _, err := s.TestScreenshot(t.Context()); err == nil {
		t.Fatal("retest ignored revoked native permission")
	}
	revoked.Store(false)
	s.pollOnce()
	if !s.Snapshot().Capture.Blocked {
		t.Fatal("permission restoration bypassed required real retest")
	}
	verifyRequired(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.answers["q1"] != "preserved across permission revocation" {
		t.Fatal("permission recovery replaced current answers")
	}
}
