package portalctl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exam-platform/internal/portalctl/sys"
)

// imageCommand runs `cbe-portalctl image check` with real files, and the settings and agent named, so that nothing of the machine the test runs on is read.
func imageCommand(t *testing.T, containerfile, runnerJSON string, extra ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "image"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"image/Containerfile": containerfile, "agent": "AGENT", "runner.json": runnerJSON} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	args := append([]string{"image", "check", dir, "--agent", filepath.Join(dir, "agent"), "--runner-config", filepath.Join(dir, "runner.json")}, extra...)
	code := Main(args, Env{Sys: sys.NewFake(), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errs, Exe: "/opt/cbe-portal/bin/cbe-portalctl", Version: "test"})
	return code, out.String(), errs.String()
}

func TestImageCheckSaysWhatTheContainerfileBreaksWithoutNeedingPodman(t *testing.T) {
	code, out, _ := imageCommand(t, "FROM python:3.12\nVOLUME /data\n", "{}", "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(out, "NOT ACCEPTED: the Containerfile is not accepted: line 2: VOLUME is not allowed") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestImageCheckAppliesTheServersSettings(t *testing.T) {
	code, out, _ := imageCommand(t, "FROM ghcr.io/acme/lab:1\n", `{"base_registries": ["docker.io"]}`, "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(out, "does not take images from ghcr.io; it allows docker.io") {
		t.Errorf("registries: exit %d\n%s", code, out)
	}
	code, out, _ = imageCommand(t, "FROM python:3.12\n", `{"custom_base": false}`, "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(out, "custom_base is off") {
		t.Errorf("custom_base: exit %d\n%s", code, out)
	}
	code, _, errs := imageCommand(t, "FROM python:3.12\n", `{"custom_basee": true}`)
	if code != 1 || !strings.Contains(errs, "the runner's settings") {
		t.Errorf("a settings file that is wrong: exit %d\n%s", code, errs)
	}
}

func TestImageCheckWithNoContainerRuntimeIsAnErrorThatSaysSo(t *testing.T) {
	code, _, errs := imageCommand(t, "FROM python:3.12\n", "{}", "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(errs, "the container runtime does not answer (is podman installed") {
		t.Errorf("exit %d\n%s", code, errs)
	}
}

func TestImageCheckUsage(t *testing.T) {
	for _, args := range [][]string{{"image"}, {"image", "list"}, {"image", "check"}, {"image", "check", "a", "b"}} {
		var out, errs bytes.Buffer
		if code := Main(args, Env{Sys: sys.NewFake(), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errs, Exe: "/opt/cbe-portal/bin/cbe-portalctl"}); code != 2 || !strings.Contains(errs.String(), "cbe-portalctl image check") {
			t.Errorf("%v: exit %d\n%s", args, code, errs.String())
		}
	}
	var out bytes.Buffer
	Main([]string{"help"}, Env{Sys: sys.NewFake(), Stdout: &out, Stderr: &out})
	if !strings.Contains(out.String(), "cbe-portalctl image check") {
		t.Error("the usage does not list the image command")
	}
}
