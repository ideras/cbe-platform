package client

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTheAlertRepeatsUntilTheStudentReturns(t *testing.T) {
	f := newFixture(t)
	var mu sync.Mutex
	var beeps []int
	s := monitorClient(t, f, func(o *Options) {
		o.Monitoring.AlertInterval = 20 * time.Millisecond
		o.Monitoring.OnAlert = func(i Incident) { mu.Lock(); beeps = append(beeps, i.Beep); mu.Unlock() }
	})
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(MonitoringView) bool { mu.Lock(); defer mu.Unlock(); return len(beeps) >= 3 })
	ReportNativeFocus(s, true)
	mu.Lock()
	heard := len(beeps)
	if !slices.IsSorted(beeps) || beeps[0] != 1 || slices.Compact(slices.Clone(beeps))[0] != 1 || len(slices.Compact(slices.Clone(beeps))) != len(beeps) {
		t.Fatalf("beeps = %v", beeps)
	}
	mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(beeps) > heard+1 {
		t.Fatalf("the alert kept beeping after the student returned: %v", beeps)
	}
}

func TestCapturesRepeatWhileAwayUpToTheIncidentLimitAsJPEG(t *testing.T) {
	f := newFixture(t)
	s, captures := captureClient(t, f, func(o *Options) {
		o.Monitoring.CaptureInterval = 40 * time.Millisecond
		o.Monitoring.IncidentCaptures = 3
	})
	ReportNativeFocus(s, false)
	waitView(t, s, func(View) bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.screenshots) == 3 })
	time.Sleep(120 * time.Millisecond)
	f.mu.Lock()
	shots := slices.Clone(f.screenshots)
	f.mu.Unlock()
	if captures.Load() != 3 || len(shots) != 3 {
		t.Fatalf("captures = %d, uploads = %d: the incident limit was not kept", captures.Load(), len(shots))
	}
	for i, shot := range shots {
		if shot.index != i || shot.event != shots[0].event || shot.contentType != "image/jpeg" {
			t.Fatalf("upload %d = index %d, event %d, %s", i, shot.index, shot.event, shot.contentType)
		}
		if _, err := jpeg.DecodeConfig(bytes.NewReader(shot.data)); err != nil {
			t.Fatalf("upload %d is not a JPEG: %v", i, err)
		}
	}
	// A new focus loss starts again from its first capture.
	ReportNativeFocus(s, true)
	ReportNativeFocus(s, false)
	waitView(t, s, func(View) bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.screenshots) >= 4 })
	f.mu.Lock()
	next := f.screenshots[3]
	f.mu.Unlock()
	if next.index != 0 || next.event == shots[0].event {
		t.Fatalf("the next focus loss = %+v", next)
	}
}

func TestALaterCaptureTheServerRefusesIsNotAFailure(t *testing.T) {
	f := newFixture(t)
	var refused atomic.Int64
	f.onScreenshot = func(w http.ResponseWriter, _ *http.Request, record screenshotRecord, _ int) bool {
		if record.index > 0 {
			refused.Add(1)
			http.Error(w, "the session reached its capture limit", 403)
			return true
		}
		return false
	}
	s, _ := captureClient(t, f, func(o *Options) {
		o.Monitoring.CaptureInterval = 30 * time.Millisecond
		o.Monitoring.IncidentCaptures = 2
	})
	ReportNativeFocus(s, false)
	waitView(t, s, func(View) bool { return refused.Load() == 1 })
	time.Sleep(30 * time.Millisecond)
	if view := s.Snapshot(); view.Capture.Failures != 0 || !view.Capture.Ready || view.Capture.Blocked {
		t.Fatalf("a refused later capture locked the student: %+v", view.Capture)
	}
}

func TestLargeDesktopsAreScaledDownAndTextKeepsItsWeight(t *testing.T) {
	for _, size := range []image.Point{{1920, 1080}, {3840, 1080}, {3840, 2160}, {7680, 2160}, {1080, 7680}} {
		fitted := fitUpload(image.NewGray(image.Rect(0, 0, size.X, size.Y))).Bounds()
		w, h := fitted.Dx(), fitted.Dy()
		if int64(w)*int64(h) > maxUploadPixels || max(w, h) > maxUploadSide {
			t.Fatalf("%v fitted to %dx%d", size, w, h)
		}
		if int64(size.X)*int64(size.Y) <= maxUploadPixels && max(size.X, size.Y) <= maxUploadSide && (w != size.X || h != size.Y) {
			t.Fatalf("%v was scaled although it fits", size)
		}
		if ratio, original := float64(w)/float64(h), float64(size.X)/float64(size.Y); ratio < original*0.98 || ratio > original*1.02 {
			t.Fatalf("%v lost its shape: %dx%d", size, w, h)
		}
	}
	// A one-pixel black line halved stays a grey line instead of vanishing.
	lines := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			lines.Set(x, y, color.White)
		}
		lines.Set(x, 1, color.Black)
	}
	small := shrink(lines, 2, 2)
	if gray := small.RGBAAt(0, 0).R; gray < 100 || gray > 160 {
		t.Fatalf("averaged pixel = %d", gray)
	}
}
