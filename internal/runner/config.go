package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Config is how the runner is set up. Everything a deployment may change is here; everything the plan fixes (no network, a read-only
// root, no capabilities, ...) is in BuildSpec and cannot be changed by configuration.
type Config struct {
	// Socket is where the runner's API listens, and SocketGroup the group that may connect to it (the exam server's account is in
	// it). SecretFile holds the shared secret every request carries.
	Socket      string `json:"socket"`
	SocketGroup string `json:"socket_group"`
	SecretFile  string `json:"secret_file"`
	// RuntimeSocket is the container runtime's API, such as unix:///run/user/1001/podman/podman.sock; only the runner opens it.
	RuntimeSocket string `json:"runtime_socket"`
	// QuotaSocket is the quota helper's socket.
	QuotaSocket string `json:"quota_socket"`
	// WorkRoot is where the quota helper makes the /work folders; RunRoot where the runner makes the agent socket folders (it must be
	// private: only the runner may enter it); StateRoot where it keeps what it knows about each container.
	WorkRoot  string `json:"work_root"`
	RunRoot   string `json:"run_root"`
	StateRoot string `json:"state_root"`
	// Images are the approved course images: the name an exam uses, and the reference the runtime has it under.
	Images map[string]string `json:"images"`
	// Max are the largest limits an exam may ask for; Default are what it gets for a limit it leaves at zero.
	Max     Resources `json:"max"`
	Default Resources `json:"default"`
	// Pids, Inodes and TmpMB are fixed for every container: processes, files in /work and the size of /tmp.
	Pids   int `json:"pids"`
	Inodes int `json:"inodes"`
	TmpMB  int `json:"tmp_mb"`
	// StopSeconds is how long a stop waits for the agent to end before it kills it.
	StopSeconds int `json:"stop_seconds"`
}

// DefaultConfig is the configuration of the plan: the limits of its Appendix A, and the maximums of the Phase 0 sizing.
func DefaultConfig() Config {
	return Config{
		Socket: "/data/cbe-platform/ipc/runner.sock", SocketGroup: "cbe-ipc", SecretFile: "/data/cbe-platform/ipc/runner.secret",
		RuntimeSocket: "unix:///run/user/1001/podman/podman.sock", QuotaSocket: "/run/cbe-quota/helper.sock",
		WorkRoot: "/data/cbe-platform/work", RunRoot: "/data/cbe-platform/run", StateRoot: "/data/cbe-platform/runner",
		Images:  map[string]string{"cbe/cpp20": "localhost/cbe/cpp20:latest"},
		Max:     Resources{CPU: 2, MemoryMB: 2048, DiskMB: 500},
		Default: Resources{CPU: 1, MemoryMB: 1024, DiskMB: 100},
		Pids:    256, Inodes: 20000, TmpMB: 64, StopSeconds: 5,
	}
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidID says whether id is a session or exam ID the runner takes: letters, digits, _ and -, at most 64.
func ValidID(id string) bool { return idPattern.MatchString(id) }

var imageNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)

// LoadConfig reads a JSON file over the defaults. A key that is not a setting is an error, so a typo is not silently ignored.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, cfg.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	var overlay Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&overlay); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	merged := cfg
	raw := map[string]json.RawMessage{}
	_ = json.Unmarshal(data, &raw)
	set := func(key string, apply func()) {
		if _, found := raw[key]; found {
			apply()
		}
	}
	set("socket", func() { merged.Socket = overlay.Socket })
	set("socket_group", func() { merged.SocketGroup = overlay.SocketGroup })
	set("secret_file", func() { merged.SecretFile = overlay.SecretFile })
	set("runtime_socket", func() { merged.RuntimeSocket = overlay.RuntimeSocket })
	set("quota_socket", func() { merged.QuotaSocket = overlay.QuotaSocket })
	set("work_root", func() { merged.WorkRoot = overlay.WorkRoot })
	set("run_root", func() { merged.RunRoot = overlay.RunRoot })
	set("state_root", func() { merged.StateRoot = overlay.StateRoot })
	set("images", func() { merged.Images = overlay.Images })
	set("max", func() { merged.Max = overlay.Max })
	set("default", func() { merged.Default = overlay.Default })
	set("pids", func() { merged.Pids = overlay.Pids })
	set("inodes", func() { merged.Inodes = overlay.Inodes })
	set("tmp_mb", func() { merged.TmpMB = overlay.TmpMB })
	set("stop_seconds", func() { merged.StopSeconds = overlay.StopSeconds })
	return merged, merged.Validate()
}

// Validate checks that the configuration can be used.
func (c Config) Validate() error {
	for name, value := range map[string]string{"socket": c.Socket, "secret_file": c.SecretFile, "runtime_socket": c.RuntimeSocket, "quota_socket": c.QuotaSocket,
		"work_root": c.WorkRoot, "run_root": c.RunRoot, "state_root": c.StateRoot} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("runner config: %s is required", name)
		}
	}
	if len(c.Images) == 0 {
		return errors.New("runner config: no approved images")
	}
	for name, ref := range c.Images {
		if !imageNamePattern.MatchString(name) || strings.TrimSpace(ref) == "" {
			return fmt.Errorf("runner config: bad image %q", name)
		}
	}
	for what, r := range map[string]Resources{"max": c.Max, "default": c.Default} {
		if r.CPU <= 0 || r.MemoryMB < 64 || r.DiskMB < 1 {
			return fmt.Errorf("runner config: %s needs a cpu, at least 64 MiB of memory and 1 MiB of disk", what)
		}
	}
	if c.Default.CPU > c.Max.CPU || c.Default.MemoryMB > c.Max.MemoryMB || c.Default.DiskMB > c.Max.DiskMB {
		return errors.New("runner config: the defaults are above the maximums")
	}
	if c.Pids < 16 || c.Inodes < 100 || c.TmpMB < 1 || c.StopSeconds < 1 {
		return errors.New("runner config: pids, inodes, tmp_mb and stop_seconds are too small")
	}
	return nil
}

// StopTimeout is how long a stop waits.
func (c Config) StopTimeout() time.Duration { return time.Duration(c.StopSeconds) * time.Second }
