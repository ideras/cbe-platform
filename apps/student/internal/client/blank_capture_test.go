package client

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
)

// picturePNG is a capture with something on it: one lit pixel in a black screen.
func picturePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.SetRGBA(2, 1, color.RGBA{R: 10, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// capturing makes a client whose screen is whatever the test puts in screen.
func capturing(t *testing.T, f *fixture, screen *[]byte) *Service {
	t.Helper()
	var failing atomic.Bool
	return requiredCaptureClient(t, f, &failing, func(o *Options) {
		o.Screenshots.Capture = func(context.Context) ([]byte, error) { return bytes.Clone(*screen), nil }
	})
}

func shotCount(f *fixture) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.screenshots)
}

// X11 gives a black screen for a console it is not showing. That is no picture of what the student did: it is a capture that
// failed, with the reason the session was off screen, and nothing is stored, as for a user switch on Windows.
func TestABlackScreenWhileTheSessionIsOffScreenIsAFailedCapture(t *testing.T) {
	f := newFixture(t)
	screen := syntheticPNG(t)
	s := capturing(t, f, &screen)
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked && v.Monitoring.Queued == 0 })
	ReportSessionActive(s, false, "other_console")
	waitUntil(t, func() bool { return len(failedCaptureReasons(f)) == 1 })
	if got := failedCaptureReasons(f); got[0] != "other_console" {
		t.Fatalf("reasons of the failures = %q, want [other_console]", got)
	}
	if n := shotCount(f); n != 0 {
		t.Fatalf("%d black screenshots were stored", n)
	}
	// Back on screen, the retest is what the student has to do, as after any failed capture.
	ReportSessionActive(s, true, "")
	waitView(t, s, func(v View) bool { return v.Monitoring.Focus == "focused" })
	if v := s.Snapshot(); !v.Capture.Blocked {
		t.Fatal("the failed capture asks for no retest")
	}
}

// A session that is off screen but shows something, as when a remote person has taken it over, is captured as ever: that is
// what the remote person sees, and it is what a proctor wants to read.
func TestAPictureWhileTheSessionIsOffScreenIsKept(t *testing.T) {
	f := newFixture(t)
	screen := picturePNG(t)
	s := capturing(t, f, &screen)
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked && v.Monitoring.Queued == 0 })
	ReportSessionActive(s, false, "other_session")
	waitUntil(t, func() bool { return shotCount(f) >= 1 })
	if got := failedCaptureReasons(f); len(got) != 0 {
		t.Fatalf("a capture with a picture failed: %q", got)
	}
}

// With the session on screen a black screen is a real picture of it, and is kept.
func TestABlackScreenWithTheSessionOnScreenIsKept(t *testing.T) {
	f := newFixture(t)
	screen := syntheticPNG(t)
	s := capturing(t, f, &screen)
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked && v.Monitoring.Queued == 0 })
	ReportNativeFocus(s, false)
	waitUntil(t, func() bool { return shotCount(f) >= 1 })
	if got := failedCaptureReasons(f); len(got) != 0 {
		t.Fatalf("a capture of a black screen failed: %q", got)
	}
}

func TestIsBlack(t *testing.T) {
	lit := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	lit.SetNRGBA(1, 2, color.NRGBA{B: 1, A: 255})
	gray := image.NewGray(image.Rect(0, 0, 3, 3))
	cases := []struct {
		name  string
		image image.Image
		want  bool
	}{
		{"opaque black", func() image.Image {
			img := image.NewRGBA(image.Rect(0, 0, 3, 3))
			for i := 3; i < len(img.Pix); i += 4 {
				img.Pix[i] = 255
			}
			return img
		}(), true},
		{"transparent black", image.NewRGBA(image.Rect(0, 0, 3, 3)), true},
		{"one lit pixel", lit, false},
		{"gray, not an RGBA", gray, true},
		{"a cropped view", image.NewRGBA(image.Rect(0, 0, 9, 9)).SubImage(image.Rect(2, 2, 5, 5)), true},
	}
	for _, c := range cases {
		if got := isBlack(c.image); got != c.want {
			t.Errorf("%s: isBlack = %v, want %v", c.name, got, c.want)
		}
	}
	// The lit pixel of a sub-image is found at its own row, not the parent's.
	parent := image.NewRGBA(image.Rect(0, 0, 9, 9))
	parent.SetRGBA(3, 3, color.RGBA{G: 9, A: 255})
	if isBlack(parent.SubImage(image.Rect(2, 2, 5, 5))) {
		t.Error("a lit pixel in a cropped view was missed")
	}
}
