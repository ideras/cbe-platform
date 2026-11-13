package dockerrt

import (
	"testing"

	"exam-platform/internal/runner"
	"github.com/moby/moby/api/types/container"
)

// The spec reaches the runtime as it is: the translation to the SDK adds nothing and drops nothing.
func TestTheDockerTranslationKeepsTheHardening(t *testing.T) {
	cfg := runner.DefaultConfig()
	req := runner.CreateRequest{Session: "s1", Exam: "exam1", Image: "cbe/cpp20", Resources: runner.Resources{CPU: 1, MemoryMB: 512, DiskMB: 50}}
	resources, ref, err := cfg.Check(req)
	if err != nil {
		t.Fatal(err)
	}
	spec := runner.BuildSpec(cfg, req, resources, ref)
	config, host := hostConfig(spec)
	if host.NetworkMode != container.NetworkMode("none") || !host.ReadonlyRootfs || len(host.CapDrop) != 1 || host.CapDrop[0] != "ALL" || host.Privileged ||
		len(host.CapAdd) != 0 || len(host.SecurityOpt) != 1 || *host.PidsLimit != 256 || host.Memory != 512<<20 || host.MemorySwap != host.Memory || host.NanoCPUs != 1e9 ||
		len(host.Binds) != 2 || len(host.Tmpfs) != 1 || host.PidMode != "" || host.IpcMode != "" || host.UsernsMode != "" || len(host.Devices) != 0 || len(host.ExtraHosts) != 0 {
		t.Errorf("host config = %+v", host)
	}
	if config.User != "1000:1000" || config.Image != ref || config.WorkingDir != "/work" || len(config.Entrypoint) != 0 || len(config.Env) != 0 || len(config.Cmd) != 0 {
		t.Errorf("config = %+v", config)
	}
}
