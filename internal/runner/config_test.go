package runner

import (
	"fmt"
	"os"
	"testing"
)

// The runtime is the rootless Podman of the account the runner runs as, whatever uid that account got from useradd, unless the settings
// name another one.
func TestTheRuntimeIsTheRunnersOwnPodmanUnlessTheSettingsNameAnother(t *testing.T) {
	own := fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", os.Getuid())
	if got := DefaultConfig().RuntimeSocket; got != own {
		t.Errorf("the default runtime is %s, want this account's %s", got, own)
	}
	// Settings that leave the runtime out get this account's own.
	cfg, err := ParseConfig([]byte(`{"socket": "/data/cbe-portal/ipc/runner.sock", "work_root": "/data/cbe-portal/work", "images": {"cbe/cpp20": "localhost/cbe-course/cbe/cpp20:latest"}}`), "runner.json")
	if err != nil || cfg.RuntimeSocket != own {
		t.Errorf("settings without a runtime: the runtime is %q (%v), want %s", cfg.RuntimeSocket, err, own)
	}
	// Settings that name one (cbe-portalctl configure writes the runner account's) keep it.
	cfg, err = ParseConfig([]byte(`{"runtime_socket": "unix:///run/user/1234/podman/podman.sock"}`), "runner.json")
	if err != nil || cfg.RuntimeSocket != "unix:///run/user/1234/podman/podman.sock" {
		t.Errorf("a runtime the settings name: %q, %v", cfg.RuntimeSocket, err)
	}
}
