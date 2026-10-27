package spike

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestScreenshotCapturesWithoutDiskAndLeavesReadinessToClient(t *testing.T) {
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	s := New()
	calls := 0
	s.cacheDir = func() (string, error) { t.Fatal("screenshot test accessed disk cache"); return "", nil }
	s.runCapture = func(ctx context.Context, out io.Writer) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > captureTimeout {
			t.Fatal("screenshot test is not bounded")
		}
		_, err := out.Write(testPNG(t))
		return err
	}
	if calls != 0 {
		t.Fatal("test captured without a click")
	}
	result, err := s.TestScreenshot(t.Context())
	if err != nil || result.Width != 2 || result.Height != 3 || calls != 1 {
		t.Fatalf("test = %+v, %v; calls %d", result, err, calls)
	}
	if runtime.GOOS == "linux" && !CheckCapturePermission(false).TestRequired {
		t.Fatal("Wayland portal access must remain subject to client verification")
	}
}

func TestScreenshotRejectsFailuresAndMalformedImages(t *testing.T) {
	valid := testPNG(t)
	for _, kind := range []string{"backend", "invalid_png", "truncated_png", "dimensions", "pixels", "bytes", "cancelled", "cancelled_success"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s, dir := captureService(t, func(_ context.Context, out io.Writer) error {
				data := bytes.Clone(valid)
				switch kind {
				case "backend":
					return errors.New("sensitive worker path")
				case "invalid_png":
					data = []byte("not a PNG")
				case "truncated_png":
					data = data[:len(data)-12]
				case "dimensions", "pixels":
					width, height := uint32(16385), uint32(1)
					if kind == "pixels" {
						width, height = 10000, 5000
					}
					binary.BigEndian.PutUint32(data[16:20], width)
					binary.BigEndian.PutUint32(data[20:24], height)
					binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
				case "bytes":
					data = make([]byte, ExamCaptureBytes+1)
				case "cancelled", "cancelled_success":
					cancel()
					if kind == "cancelled" {
						return ctx.Err()
					}
				}
				_, err := out.Write(data)
				return err
			})
			result, err := s.TestScreenshot(ctx)
			if err == nil || result != (ScreenshotTestResult{}) || strings.Contains(err.Error(), "sensitive worker path") {
				t.Fatalf("test = %+v, %v", result, err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("test created a cache directory: %v", err)
			}
		})
	}
}

func TestScreenshotDisabledOrExpiredNeverCaptures(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		s := New(!disabled)
		s.runCapture = func(context.Context, io.Writer) error { t.Fatal("capture should not run"); return nil }
		ctx := t.Context()
		if !disabled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			defer cancel()
		}
		if result, err := s.TestScreenshot(ctx); err == nil || result != (ScreenshotTestResult{}) {
			t.Fatalf("test = %+v, %v", result, err)
		}
	}
}

func TestScreenshotDeadlineDuringCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	s := New()
	s.runCapture = func(ctx context.Context, _ io.Writer) error { <-ctx.Done(); return ctx.Err() }
	result, err := s.TestScreenshot(ctx)
	if result != (ScreenshotTestResult{}) || err == nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timed out")) {
		t.Fatalf("timed-out capture = %+v, %v", result, err)
	}
}

func TestScreenshotSharesWorkerLockAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	s := New()
	s.runCapture = func(ctx context.Context, _ io.Writer) error { close(started); <-ctx.Done(); return ctx.Err() }
	done := make(chan error, 1)
	go func() { _, err := s.TestScreenshot(ctx); done <- err }()
	<-started
	_, testErr := s.TestScreenshot(t.Context())
	_, probeErr := s.CaptureProbe(t.Context())
	cancel()
	select {
	case err := <-done:
		if err == nil || testErr == nil || probeErr == nil || !strings.Contains(testErr.Error(), "already running") || !strings.Contains(probeErr.Error(), "already running") {
			t.Fatalf("concurrent/cancelled captures: %v, %v, %v", testErr, probeErr, err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled test did not finish")
	}
}
