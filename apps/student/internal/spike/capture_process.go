package spike

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func runCaptureProcess(ctx context.Context, output io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, executable, "--capture-spike", "--capture-spike-worker")
	cmd.Env = captureEnvironment(runtime.GOOS, os.Environ())
	return runCaptureCommand(ctx, cmd, output)
}

func runCaptureCommand(ctx context.Context, cmd *exec.Cmd, output io.Writer) error {
	cmd.Stdout = output
	var stderr bytes.Buffer
	cmd.Stderr = &limitedOutput{output: &stderr, remaining: 4096}
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("capture worker: %w: %s", err, message)
		}
		return fmt.Errorf("capture worker: %w", err)
	}
	return nil
}

func captureEnvironment(goos string, env []string) []string {
	var sessionType, display string
	for _, value := range env {
		if v, ok := strings.CutPrefix(value, "XDG_SESSION_TYPE="); ok {
			sessionType = v
		}
		if v, ok := strings.CutPrefix(value, "WAYLAND_DISPLAY="); ok {
			display = v
		}
	}
	result := append([]string(nil), env...)
	if !waylandSession(goos, sessionType, display) {
		return result
	}
	// The fork selects its portal backend only from XDG_SESSION_TYPE.
	result = result[:0]
	for _, value := range env {
		if !strings.HasPrefix(value, "XDG_SESSION_TYPE=") {
			result = append(result, value)
		}
	}
	return append(result, "XDG_SESSION_TYPE=wayland")
}

type limitedOutput struct {
	output    io.Writer
	remaining int
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > w.remaining {
		p = p[:w.remaining]
	}
	written, err := w.output.Write(p)
	w.remaining -= written
	if err != nil {
		return written, err
	}
	return n, nil
}
