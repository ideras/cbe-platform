package spike

import (
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func captureService(t *testing.T, runner captureRunner) (*ShellService, string) {
	t.Helper()
	root := t.TempDir()
	s := New(true)
	s.cacheDir = func() (string, error) { return root, nil }
	s.runCapture = runner
	return s, filepath.Join(root, "exam-platform-spike")
}

func TestCaptureAvailableByDefaultButNeverRunsUntilRequested(t *testing.T) {
	s := New()
	calls := 0
	s.cacheDir = func() (string, error) { return t.TempDir(), nil }
	s.runCapture = func(_ context.Context, out io.Writer) error {
		calls++
		return png.Encode(out, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	}
	if !s.captureEnabled || calls != 0 {
		t.Fatal("default capture unavailable or ran automatically")
	}
	if _, err := s.CaptureProbe(t.Context()); err != nil || calls != 1 {
		t.Fatal("manual capture requires a launch flag", err)
	}
}

func TestCaptureCanBeDisabled(t *testing.T) {
	s := New(false)
	s.runCapture = func(context.Context, io.Writer) error { t.Fatal("capture ran without opt-in"); return nil }
	path, err := s.CaptureProbe(t.Context())
	if err == nil || path != "" || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("capture = %q, %v", path, err)
	}
}

func TestCaptureAllowsWaylandAndSavesPrivatePNG(t *testing.T) {
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	s, _ := captureService(t, func(ctx context.Context, out io.Writer) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > captureTimeout {
			t.Fatal("capture is not bounded")
		}
		return png.Encode(out, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	})
	path, err := s.CaptureProbe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %v", info.Mode())
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil || img.Bounds() != image.Rect(0, 0, 2, 3) {
		t.Fatalf("PNG = %v, %v", img, err)
	}
}

func TestCaptureFailuresRemovePartialFiles(t *testing.T) {
	for _, kind := range []string{"backend", "invalid_png", "cancelled", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer stop()
			}
			s, dir := captureService(t, func(ctx context.Context, out io.Writer) error {
				io.WriteString(out, "partial")
				switch kind {
				case "backend":
					return errors.New("permission denied")
				case "cancelled":
					cancel()
					return ctx.Err()
				case "timeout":
					return ctx.Err()
				default:
					return nil
				}
			})
			path, err := s.CaptureProbe(ctx)
			if path != "" || err == nil {
				t.Fatalf("capture = %q, %v", path, err)
			}
			files, readErr := os.ReadDir(dir)
			if readErr != nil || len(files) != 0 {
				t.Fatalf("partial captures = %v, %v", files, readErr)
			}
		})
	}
}

func TestCaptureSerializesWorkers(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s, _ := captureService(t, func(ctx context.Context, out io.Writer) error {
		close(started)
		<-release
		return png.Encode(out, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	})
	done := make(chan error, 1)
	go func() { _, err := s.CaptureProbe(t.Context()); done <- err }()
	<-started
	path, err := s.CaptureProbe(t.Context())
	close(release)
	firstErr := <-done
	if path != "" || err == nil || !strings.Contains(err.Error(), "already running") || firstErr != nil {
		t.Fatalf("captures: %q %v; first %v", path, err, firstErr)
	}
}
