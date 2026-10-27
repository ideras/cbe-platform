package spike

import (
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"runtime"

	"webtyp.com/screenshot"
)

type desktopCapturer interface {
	NumActiveDisplays() int
	GetDisplayBounds(int) image.Rectangle
	CaptureRect(image.Rectangle) (*image.RGBA, error)
}

type nativeCapturer struct{}

func (nativeCapturer) NumActiveDisplays() int { return screenshot.NumActiveDisplays() }
func (nativeCapturer) GetDisplayBounds(index int) image.Rectangle {
	return screenshot.GetDisplayBounds(index)
}
func (nativeCapturer) CaptureRect(rect image.Rectangle) (*image.RGBA, error) {
	return screenshot.CaptureRect(rect)
}

// CaptureDesktopPNG runs in the isolated worker used by diagnostics and disclosed exam capture.
func CaptureDesktopPNG(output io.Writer) error {
	return captureDesktopPNG(output, nativeCapturer{}, waylandSession(runtime.GOOS, os.Getenv("XDG_SESSION_TYPE"), os.Getenv("WAYLAND_DISPLAY")))
}

func captureDesktopPNG(output io.Writer, backend desktopCapturer, wayland bool) error {
	n := backend.NumActiveDisplays()
	if n < 1 {
		return fmt.Errorf("no capturable displays; check display access (XWayland metadata on Wayland) and screen-capture permissions")
	}
	var bounds image.Rectangle
	for i := 0; i < n; i++ {
		display := backend.GetDisplayBounds(i)
		if display.Empty() {
			return fmt.Errorf("display %d has empty bounds; retry after checking connected displays", i)
		}
		bounds = bounds.Union(display)
	}
	if bounds.Empty() {
		return fmt.Errorf("display bounds are empty")
	}
	if bounds.Dx() > 16384 || bounds.Dy() > 16384 || int64(bounds.Dx())*int64(bounds.Dy()) > ExamCapturePixels {
		return fmt.Errorf("display union exceeds capture dimension/pixel limits")
	}
	if wayland {
		// Portal PNG coordinates start at the composite image's top-left, not the primary monitor.
		bounds = bounds.Sub(bounds.Min)
	}
	img, err := backend.CaptureRect(bounds)
	if err != nil {
		return fmt.Errorf("capture desktop: %w", err)
	}
	if img == nil || img.Bounds().Empty() {
		return fmt.Errorf("capture returned an empty image")
	}
	if img.Bounds().Dx() != bounds.Dx() || img.Bounds().Dy() != bounds.Dy() {
		return fmt.Errorf("capture dimensions do not match the all-monitor union")
	}
	if err := png.Encode(output, img); err != nil {
		return fmt.Errorf("encode capture PNG: %w", err)
	}
	return nil
}

func waylandSession(goos, sessionType, display string) bool {
	if goos != "linux" || sessionType == "x11" {
		return false
	}
	return sessionType == "wayland" || display != ""
}
