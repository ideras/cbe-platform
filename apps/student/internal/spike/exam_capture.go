package spike

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"io"
	"os"
	"runtime"
)

const ExamCaptureBytes = 16 << 20
const ExamCapturePixels int64 = 40000000

type CapturePermission struct {
	Supported    bool
	Granted      bool
	TestRequired bool
	Message      string
}

func CheckCapturePermission(request bool) CapturePermission {
	if waylandSession(runtime.GOOS, os.Getenv("XDG_SESSION_TYPE"), os.Getenv("WAYLAND_DISPLAY")) {
		return CapturePermission{Supported: true, TestRequired: true, Message: "Wayland portal capture requires a successful screenshot test. Permissions may change; monitor coverage still requires instructor verification."}
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" {
		return CapturePermission{Message: "X11 display access is unavailable. Use the instructor-tested graphical session and notify the instructor; current answers are preserved, but a successful retest is required."}
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return CapturePermission{Message: "Screen capture is unsupported on this platform. Notify the instructor."}
	}
	return platformCapturePermission(request)
}
func CaptureExamPNG(ctx context.Context) ([]byte, error) {
	permission := CheckCapturePermission(false)
	if permission.TestRequired {
		// The portal authorizes each actual request; the client enforces prior verification.
		permission.Granted = true
	}
	return captureExamPNG(ctx, permission, runCaptureProcess)
}
func captureExamPNG(ctx context.Context, permission CapturePermission, run func(context.Context, io.Writer) error) ([]byte, error) {
	if !permission.Supported || !permission.Granted {
		return nil, errors.New(permission.Message)
	}
	var output bytes.Buffer
	writer := &captureLimitWriter{output: &output, remaining: ExamCaptureBytes}
	if err := run(ctx, writer); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("Screen capture cancelled or timed out. Notify the instructor; current answers are preserved, but a successful retest is required.")
		}
		return nil, errors.New("Screen capture failed or exceeded limits. Check screen-recording permission and connected monitors; notify the instructor.")
	}
	config, err := png.DecodeConfig(bytes.NewReader(output.Bytes()))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > ExamCapturePixels {
		return nil, errors.New("Screen capture did not produce a supported full-screen PNG. Notify the instructor.")
	}
	return output.Bytes(), nil
}

type captureLimitWriter struct {
	output    io.Writer
	remaining int
}

func (w *captureLimitWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errors.New("capture image too large")
	}
	n, err := w.output.Write(p)
	w.remaining -= n
	return n, err
}
