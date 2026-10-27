package spike

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"runtime"
	"testing"
)

func TestExamCapturePermissionWorkerOutputLimitsAndCancellation(t *testing.T) {
	ctx := t.Context()
	called := false
	run := func(context.Context, io.Writer) error { called = true; return nil }
	if _, err := captureExamPNG(ctx, CapturePermission{Supported: true, Message: "permission denied"}, run); err == nil || called {
		t.Fatal("permission failure invoked worker")
	}
	allowed := CapturePermission{Supported: true, Granted: true}
	var valid bytes.Buffer
	png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	data, err := captureExamPNG(ctx, allowed, func(_ context.Context, w io.Writer) error { _, err := w.Write(valid.Bytes()); return err })
	if err != nil || !bytes.Equal(data, valid.Bytes()) {
		t.Fatal("valid worker output lost")
	}
	for _, output := range [][]byte{[]byte("invalid PNG"), make([]byte, ExamCaptureBytes+1)} {
		if _, err := captureExamPNG(ctx, allowed, func(_ context.Context, w io.Writer) error { _, err := w.Write(output); return err }); err == nil {
			t.Fatal("invalid/oversized worker output accepted")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := captureExamPNG(cancelled, allowed, func(ctx context.Context, _ io.Writer) error { return ctx.Err() }); err == nil || errors.Is(err, io.EOF) {
		t.Fatal("worker cancellation unreported")
	}
	backend := &fakeCapturer{displays: []image.Rectangle{image.Rect(0, 0, 20000, 100)}}
	if err := captureDesktopPNG(io.Discard, backend, false); err == nil || backend.calls != 0 {
		t.Fatal("large display union allocated before validation")
	}
}
func TestWaylandCaptureRequiresActualVerificationRatherThanBlanketRejection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux display-server policy")
	}
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	permission := CheckCapturePermission(false)
	if !permission.Supported || permission.Granted || !permission.TestRequired || permission.Message == "" {
		t.Fatal("Wayland must support portal testing without claiming an untested permission grant")
	}
}
