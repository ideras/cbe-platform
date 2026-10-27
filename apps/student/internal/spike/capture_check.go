package spike

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
)

type ScreenshotTestResult struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TestScreenshot performs one requested capture, validates its PNG, and discards the image.
func (s *ShellService) TestScreenshot(ctx context.Context) (ScreenshotTestResult, error) {
	var result ScreenshotTestResult
	if !s.captureEnabled {
		return result, fmt.Errorf("the manual capture test is disabled")
	}
	if !s.captureLock.TryLock() {
		return result, fmt.Errorf("a capture is already running")
	}
	defer s.captureLock.Unlock()
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}

	var output bytes.Buffer
	err := s.runCapture(ctx, &captureLimitWriter{output: &output, remaining: ExamCaptureBytes})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("screenshot test timed out; check the screen-capture permission dialog and retry")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return result, fmt.Errorf("screenshot test cancelled; no image saved")
	}
	if err != nil {
		return result, fmt.Errorf("screenshot test failed; check screen-recording permission or display support and retry")
	}
	config, err := png.DecodeConfig(bytes.NewReader(output.Bytes()))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > ExamCapturePixels {
		return result, fmt.Errorf("screenshot test did not produce a PNG within the supported limits")
	}
	if _, err := png.Decode(bytes.NewReader(output.Bytes())); err != nil {
		return result, fmt.Errorf("screenshot test did not produce a complete valid PNG")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return ScreenshotTestResult{Width: config.Width, Height: config.Height}, nil
}
