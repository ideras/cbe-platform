package spike

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"
)

type fakeCapturer struct {
	displays []image.Rectangle
	rect     image.Rectangle
	calls    int
	err      error
	empty    bool
}

func (f *fakeCapturer) NumActiveDisplays() int                 { return len(f.displays) }
func (f *fakeCapturer) GetDisplayBounds(i int) image.Rectangle { return f.displays[i] }
func (f *fakeCapturer) CaptureRect(rect image.Rectangle) (*image.RGBA, error) {
	f.rect = rect
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.empty {
		return nil, nil
	}
	return image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy())), nil
}

func TestCaptureDesktopUnionAndPortalOrigin(t *testing.T) {
	for _, wayland := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "wayland"}[wayland], func(t *testing.T) {
			backend := &fakeCapturer{displays: []image.Rectangle{image.Rect(0, 0, 20, 10), image.Rect(-10, -2, 0, 8)}}
			var out bytes.Buffer
			if err := captureDesktopPNG(&out, backend, wayland); err != nil {
				t.Fatal(err)
			}
			want := image.Rect(-10, -2, 20, 10)
			if wayland {
				want = image.Rect(0, 0, 30, 12)
			}
			if backend.calls != 1 || backend.rect != want {
				t.Fatalf("capture = %v, calls %d", backend.rect, backend.calls)
			}
			config, err := png.DecodeConfig(&out)
			if err != nil || config.Width != 30 || config.Height != 12 {
				t.Fatalf("PNG config = %v, %v", config, err)
			}
		})
	}
}

func TestCaptureDesktopFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		backend fakeCapturer
	}{
		{"no_displays", fakeCapturer{}},
		{"missing_bounds", fakeCapturer{displays: []image.Rectangle{image.Rect(0, 0, 2, 2), {}}}},
		{"backend", fakeCapturer{displays: []image.Rectangle{image.Rect(0, 0, 2, 2)}, err: errors.New("denied")}},
		{"empty_image", fakeCapturer{displays: []image.Rectangle{image.Rect(0, 0, 2, 2)}, empty: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := captureDesktopPNG(io.Discard, &test.backend, false); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	backend := &fakeCapturer{displays: []image.Rectangle{image.Rect(0, 0, 2, 2)}}
	if err := captureDesktopPNG(failingWriter{}, backend, false); err == nil {
		t.Fatal("expected PNG write error")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
