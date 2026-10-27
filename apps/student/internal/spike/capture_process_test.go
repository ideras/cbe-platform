package spike

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCaptureEnvironment(t *testing.T) {
	for _, test := range []struct {
		name, goos, session, display string
		portal                       bool
	}{
		{"wayland", "linux", "wayland", "wayland-0", true},
		{"display_only", "linux", "", "wayland-0", true},
		{"x11", "linux", "x11", "", false},
		{"explicit_x11", "linux", "x11", "wayland-0", false},
		{"other_platform", "darwin", "wayland", "wayland-0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := []string{"XDG_SESSION_TYPE=" + test.session, "WAYLAND_DISPLAY=" + test.display, "DISPLAY=:0", "DBUS_SESSION_BUS_ADDRESS=unix:path=/test/bus"}
			before := slices.Clone(env)
			got := captureEnvironment(test.goos, env)
			if !slices.Equal(env, before) {
				t.Fatal("mutated caller environment")
			}
			if waylandSession(test.goos, test.session, test.display) != test.portal {
				t.Fatal("incorrect backend selection")
			}
			if test.portal && !slices.Contains(got, "XDG_SESSION_TYPE=wayland") {
				t.Fatalf("environment = %v", got)
			}
			if !test.portal && !slices.Equal(got, env) {
				t.Fatalf("unexpected environment = %v", got)
			}
			if !slices.Contains(got, "DBUS_SESSION_BUS_ADDRESS=unix:path=/test/bus") {
				t.Fatal("lost session bus address")
			}
		})
	}
}

func TestCaptureProcess(t *testing.T) {
	for _, mode := range []string{"png", "denied", "hang"} {
		t.Run(mode, func(t *testing.T) {
			limit := 5 * time.Second
			if mode == "hang" {
				limit = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), limit)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCaptureProcessHelper$")
			cmd.Env = append(os.Environ(), "EXAM_CAPTURE_TEST="+mode)
			var out bytes.Buffer
			err := runCaptureCommand(ctx, cmd, &out)
			switch mode {
			case "png":
				if err != nil {
					t.Fatal(err)
				}
				if _, err := png.Decode(&out); err != nil {
					t.Fatal(err)
				}
			case "denied":
				if err == nil || !strings.Contains(err.Error(), "permission denied") {
					t.Fatalf("error = %v", err)
				}
			case "hang":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("error = %v", err)
				}
				if cmd.ProcessState == nil || cmd.ProcessState.Success() {
					t.Fatal("worker was not reaped")
				}
			}
		})
	}
}

func TestCaptureProcessHelper(t *testing.T) {
	switch os.Getenv("EXAM_CAPTURE_TEST") {
	case "png":
		if err := png.Encode(os.Stdout, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "denied":
		fmt.Fprintln(os.Stderr, "permission denied")
		os.Exit(1)
	case "hang":
		time.Sleep(time.Hour)
		os.Exit(0)
	}
}

func TestCaptureStderrBound(t *testing.T) {
	var out bytes.Buffer
	writer := &limitedOutput{output: &out, remaining: 4}
	for _, input := range []string{"abc", "def", "ghi"} {
		n, err := writer.Write([]byte(input))
		if err != nil || n != len(input) {
			t.Fatalf("write = %d, %v", n, err)
		}
	}
	if out.String() != "abcd" {
		t.Fatalf("output = %q", out.String())
	}
}
