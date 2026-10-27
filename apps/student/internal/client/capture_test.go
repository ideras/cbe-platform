package client

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func syntheticPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func captureClient(t *testing.T, f *fixture, modify func(*Options)) (*Service, *atomic.Int64) {
	t.Helper()
	data := syntheticPNG(t)
	var count atomic.Int64
	s := monitorClient(t, f, func(o *Options) {
		o.Screenshots.Timeout = 100 * time.Millisecond
		o.Screenshots.Lifetime = 600 * time.Millisecond
		o.Screenshots.Capture = func(context.Context) ([]byte, error) { count.Add(1); return bytes.Clone(data), nil }
		o.Screenshots.Permission = func(bool) CapturePermission {
			return CapturePermission{Supported: true, Granted: true, Message: "synthetic permission"}
		}
		if modify != nil {
			modify(o)
		}
	})
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	return s, &count
}
func TestCaptureShortBlurOneImagePerSustainedIncidentAndRetrySameBytes(t *testing.T) {
	f := newFixture(t)
	f.onScreenshot = func(w http.ResponseWriter, _ *http.Request, _ screenshotRecord, n int) bool {
		if n < 3 {
			http.Error(w, "ambiguous upload", 503)
			return true
		}
		return false
	}
	var alerts atomic.Int64
	s, captures := captureClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, false)
	time.Sleep(8 * time.Millisecond)
	ReportNativeFocus(s, true)
	time.Sleep(80 * time.Millisecond)
	if captures.Load() != 0 {
		t.Fatal("short blur captured")
	}
	ReportNativeFocus(s, false)
	v := waitView(t, s, func(v View) bool { return v.Capture.State == "uploaded" })
	if captures.Load() != 1 || alerts.Load() != 1 || v.Capture.EventID <= 0 || v.Capture.ScreenshotID <= 0 {
		t.Fatal("incident capture/alert mismatch")
	}
	f.mu.Lock()
	shots := append([]screenshotRecord(nil), f.screenshots...)
	f.mu.Unlock()
	if len(shots) != 3 {
		t.Fatal("upload retries not bounded")
	}
	for _, r := range shots {
		if r.event != shots[0].event || r.at != shots[0].at || !bytes.Equal(r.data, shots[0].data) {
			t.Fatal("retry recaptured or changed captured bytes/reference")
		}
	}
	ReportNativeFocus(s, false)
	time.Sleep(80 * time.Millisecond)
	if captures.Load() != 1 {
		t.Fatal("duplicate native blur recaptured")
	}
	ReportNativeFocus(s, true)
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return v.Capture.Pending == 0 && captures.Load() == 2 })
	if alerts.Load() != 2 {
		t.Fatal("new incident missing alert")
	}
	current := s.monitor.view()
	s.capture.start(*current.Incident, 0, current)
	time.Sleep(5 * time.Millisecond)
	if captures.Load() != 2 {
		t.Fatal("completed incident recaptured")
	}
}
func TestCaptureStartsWithAlertBeforeDelayedLossAckAndUploadsAfterRegain(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	var once sync.Once
	f.onTelemetry = func(w http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if record.kind != "focus_lost" {
			return false
		}
		select {
		case <-release:
			respond(w, eventAck{ID: 444, ServerTime: time.Now().UTC()})
		case <-r.Context().Done():
		}
		return true
	}
	s, captures := captureClient(t, f, nil)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return captures.Load() == 1 && v.Capture.State == "waiting_for_event" })
	ReportNativeFocus(s, true)
	once.Do(func() { close(release) })
	v := waitView(t, s, func(v View) bool { return v.Capture.State == "uploaded" })
	if v.Capture.EventID != 444 || captures.Load() != 1 {
		t.Fatal("late ack lost/wrong incident identity")
	}
}
func TestCaptureAndUploadRemainIndependentOfAnswersStatusAndTerminalStop(t *testing.T) {
	f := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.onScreenshot = func(_ http.ResponseWriter, r *http.Request, _ screenshotRecord, _ int) bool {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return false
		case <-r.Context().Done():
			return true
		}
	}
	s, _ := captureClient(t, f, func(o *Options) { o.RequestTimeout = time.Second; o.Screenshots.Lifetime = 2 * time.Second })
	ReportNativeFocus(s, false)
	<-entered
	var releaseOnce sync.Once
	releaseUpload := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseUpload()
	update(t, s, "saved while capture upload is blocked", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	if _, err := s.Submit(map[string]string{"q1": "final while screenshot pending", "q2": ""}, 2); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Phase == "complete" && !v.Monitoring.Active && v.Capture.Pending == 1 })
	// The terminal state stops new incidents, not a bounded image already captured.
	releaseUpload()
	waitView(t, s, func(v View) bool { return v.Capture.State == "uploaded" && v.Capture.Pending == 0 })
}
func TestCaptureFailureTimeoutQueueBoundsAndShutdown(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	var once sync.Once
	s, _ := captureClient(t, f, func(o *Options) {
		o.Screenshots.Timeout = 80 * time.Millisecond
		o.Screenshots.Capture = func(ctx context.Context) ([]byte, error) {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return nil, errors.New("secret native path must not leak")
		}
	})
	ReportNativeFocus(s, false)
	<-started
	update(t, s, "autosave survives native timeout", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	v := waitView(t, s, func(v View) bool { return v.Capture.Failures == 1 && v.Capture.Pending == 0 })
	if strings.Contains(v.Capture.Error, "secret") {
		t.Fatal("native details leaked")
	}
	f2 := newFixture(t)
	f2.onTelemetry = func(_ http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if record.kind == "focus_lost" {
			<-r.Context().Done()
			return true
		}
		return false
	}
	s2, count := captureClient(t, f2, func(o *Options) {
		o.Screenshots.MaxPending = 1
		o.Screenshots.Lifetime = 200 * time.Millisecond
		o.RequestTimeout = time.Second
	})
	ReportNativeFocus(s2, false)
	waitView(t, s2, func(v View) bool { return v.Capture.State == "waiting_for_event" })
	ReportNativeFocus(s2, true)
	ReportNativeFocus(s2, false)
	waitView(t, s2, func(v View) bool { return v.Capture.Failures >= 1 })
	if count.Load() != 1 {
		t.Fatal("bounded pending image queue exceeded")
	}
	waitView(t, s2, func(v View) bool { return v.Capture.Pending == 0 })
	if err := s2.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
}
func TestCaptureAuthorizationFailureDoesNotFinalizeAnswersAndReleasesBuffer(t *testing.T) {
	f := newFixture(t)
	f.onScreenshot = func(w http.ResponseWriter, _ *http.Request, _ screenshotRecord, _ int) bool {
		http.Error(w, "denied", 403)
		return true
	}
	s, count := captureClient(t, f, nil)
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool { return v.Capture.Failures == 1 && v.Capture.Pending == 0 })
	if count.Load() != 1 || s.Snapshot().Phase != "exam" {
		t.Fatal("image failure changed session authority")
	}
	update(t, s, "answers still work", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
}
