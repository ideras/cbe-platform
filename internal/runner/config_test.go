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
	// The settings that deploy/workspace/install.sh copies leave the runtime out, so the runner finds its own; and every key in them is a setting.
	data, err := os.ReadFile("../../deploy/workspace/runner.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(data, "deploy/workspace/runner.json")
	if err != nil || cfg.RuntimeSocket != own {
		t.Errorf("deploy/workspace/runner.json: the runtime is %q (%v), want %s", cfg.RuntimeSocket, err, own)
	}
	// Settings that name one (cbe-portalctl configure writes the runner account's) keep it.
	cfg, err = ParseConfig([]byte(`{"runtime_socket": "unix:///run/user/1234/podman/podman.sock"}`), "runner.json")
	if err != nil || cfg.RuntimeSocket != "unix:///run/user/1234/podman/podman.sock" {
		t.Errorf("a runtime the settings name: %q, %v", cfg.RuntimeSocket, err)
	}
}
