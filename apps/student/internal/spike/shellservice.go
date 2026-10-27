package spike

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const captureTimeout = 2 * time.Minute

type captureRunner func(context.Context, io.Writer) error

type ShellService struct {
	captureEnabled bool
	runCapture     captureRunner
	cacheDir       func() (string, error)
	captureLock    sync.Mutex
}

func New(enabled ...bool) *ShellService {
	captureEnabled := len(enabled) == 0 || enabled[0]
	return &ShellService{captureEnabled: captureEnabled, runCapture: runCaptureProcess, cacheDir: os.UserCacheDir}
}

func (*ShellService) Identity() string {
	return "Student application: offline assets and isolated Go bindings are ready."
}

// CaptureProbe is an opt-in local feasibility test, never an upload or automatic capture.
func (s *ShellService) CaptureProbe(ctx context.Context) (string, error) {
	if !s.captureEnabled {
		return "", fmt.Errorf("the manual capture test is disabled")
	}
	if !s.captureLock.TryLock() {
		return "", fmt.Errorf("a capture is already running")
	}
	defer s.captureLock.Unlock()
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	cache, err := s.cacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "exam-platform-spike")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "capture-*.png")
	if err != nil {
		return "", err
	}
	path := file.Name()
	keep := false
	defer func() {
		file.Close()
		if !keep {
			os.Remove(path)
		}
	}()
	if err := s.runCapture(ctx, file); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("capture timed out; check the screen-capture permission dialog and retry")
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", fmt.Errorf("capture cancelled; no image saved")
		}
		return "", fmt.Errorf("capture desktop: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	config, err := png.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return "", fmt.Errorf("capture worker did not produce a valid PNG")
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	keep = true
	return path, nil
}
