package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// RuntimeSocket is the container runtime's API, such as unix:///run/user/<uid>/podman/podman.sock; only the runner opens it. Left out, it is
	// the rootless Podman of the account the runner runs as (podmanSocket), which is the account that owns the containers.
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
	// GradeRoot is where the runner puts a grading bundle for the time of a run (private to the runner, like RunRoot). GradeParallel is
	// how many gradings run at once, GradeMaxSeconds the longest a run an exam asks for may last, and GradeOutputBytes the most of each of
	// a run's two output streams that is kept (Phase 6).
	GradeRoot        string `json:"grade_root"`
	GradeParallel    int    `json:"grade_parallel"`
	GradeMaxSeconds  int    `json:"grade_max_seconds"`
	GradeOutputBytes int    `json:"grade_output_bytes"`
	// BuildMaxSeconds is the longest an image build may last (a request may ask for less), and BuildMemoryMB the memory the steps of a build
	// may use together (Phase 7). One build runs at a time.
	BuildMaxSeconds int `json:"build_max_seconds"`
	BuildMemoryMB   int `json:"build_memory_mb"`
	// HostMemoryMB is the most memory (MiB) that the limits of the student containers (created, running or frozen) may add up to: the runner
	// refuses to make one that would go over it (Phase 8). Every container may use its whole limit at once, so this is what keeps forty of them
	// from taking the machine, which also runs the exam server and whatever else its owner does. 0 means 70% of the machine's memory (a class of forty students at the default 1 GiB fits in a machine of 64 GiB).
	HostMemoryMB int `json:"host_memory_mb"`
	// CustomBase says whether an exam may bring a base image of its own: an image/Containerfile that starts FROM any image, which the runner finishes with the
	// agent and the rest of the contract (internal/runner/finalize.go) and verifies. On by default; off, only the approved images above can be built on.
	CustomBase bool `json:"custom_base"`
	// BaseRegistries are the registries an exam's own base may be pulled from (docker.io, ghcr.io, quay.io, localhost:5000...). None means any.
	BaseRegistries []string `json:"base_registries"`
	// AgentBinary is the static cbe-agent the runner puts in every image it finishes; its hash is part of the image's name, so a new agent makes new
	// images. It is the file the installer put in INSTALL_DIR/bin.
	AgentBinary string `json:"agent_binary"`
}

// DefaultConfig is the configuration of the plan: the limits of its Appendix A, and the maximums of the Phase 0 sizing.
func DefaultConfig() Config {
	return Config{
		Socket: "/data/cbe-platform/ipc/runner.sock", SocketGroup: "cbe-ipc", SecretFile: "/data/cbe-platform/ipc/runner.secret",
		RuntimeSocket: podmanSocket(os.Getuid()), QuotaSocket: "/run/cbe-quota/helper.sock",
		WorkRoot: "/data/cbe-platform/work", RunRoot: "/data/cbe-platform/run", StateRoot: "/data/cbe-platform/runner",
		Images:  map[string]string{"cbe/cpp20": "localhost/cbe/cpp20:latest"},
		Max:     Resources{CPU: 2, MemoryMB: 2048, DiskMB: 500},
		Default: Resources{CPU: 1, MemoryMB: 1024, DiskMB: 100},
		Pids:    256, Inodes: 20000, TmpMB: 64, StopSeconds: 5,
		GradeRoot: "/data/cbe-platform/grading", GradeParallel: 2, GradeMaxSeconds: 1800, GradeOutputBytes: 256 << 10,
		BuildMaxSeconds: 900, BuildMemoryMB: 2048, CustomBase: true,
	}
}

// podmanSocket is where the rootless Podman of the account uid listens (that account's podman.socket). No uid is assumed: the runner's account
// has whatever id useradd gave it.
func podmanSocket(uid int) string { return fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", uid) }

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// registryPattern is a registry as an image reference writes it: lower case, with an optional port.
var registryPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]{1,5})?$`)

// ValidID says whether id is a session or exam ID the runner takes: letters, digits, _ and -, at most 64.
func ValidID(id string) bool { return idPattern.MatchString(id) }

var imageNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)

// ValidImageName says whether name is the name of a course image: lower case words joined by /, ., _ or -, with no tag and no digest (`cbe/cpp20`).
func ValidImageName(name string) bool { return len(name) <= 128 && imageNamePattern.MatchString(name) }

// LoadConfig reads a JSON file over the defaults. A key that is not a setting is an error, so a typo is not silently ignored.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		cfg := DefaultConfig()
		return cfg, cfg.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), err
	}
	return ParseConfig(data, path)
}

// ParseConfig is LoadConfig for a file that has been read: source names it in the errors.
func ParseConfig(data []byte, path string) (Config, error) {
	cfg := DefaultConfig()
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
	set("grade_root", func() { merged.GradeRoot = overlay.GradeRoot })
	set("grade_parallel", func() { merged.GradeParallel = overlay.GradeParallel })
	set("grade_max_seconds", func() { merged.GradeMaxSeconds = overlay.GradeMaxSeconds })
	set("grade_output_bytes", func() { merged.GradeOutputBytes = overlay.GradeOutputBytes })
	set("build_max_seconds", func() { merged.BuildMaxSeconds = overlay.BuildMaxSeconds })
	set("build_memory_mb", func() { merged.BuildMemoryMB = overlay.BuildMemoryMB })
	set("host_memory_mb", func() { merged.HostMemoryMB = overlay.HostMemoryMB })
	set("custom_base", func() { merged.CustomBase = overlay.CustomBase })
	set("base_registries", func() { merged.BaseRegistries = overlay.BaseRegistries })
	set("agent_binary", func() { merged.AgentBinary = overlay.AgentBinary })
	return merged, merged.Validate()
}

// Validate checks that the configuration can be used.
func (c Config) Validate() error {
	for name, value := range map[string]string{"socket": c.Socket, "secret_file": c.SecretFile, "runtime_socket": c.RuntimeSocket, "quota_socket": c.QuotaSocket,
		"work_root": c.WorkRoot, "run_root": c.RunRoot, "state_root": c.StateRoot, "grade_root": c.GradeRoot} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("runner config: %s is required", name)
		}
	}
	if len(c.Images) == 0 && !c.CustomBase {
		return errors.New("runner config: no approved images, and custom_base is off: no exam could have an image")
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
	if c.GradeParallel < 1 || c.GradeParallel > 32 || c.GradeMaxSeconds < 10 || c.GradeOutputBytes < 1<<10 || c.GradeOutputBytes > 4<<20 {
		return errors.New("runner config: grade_parallel is 1 to 32, grade_max_seconds at least 10 and grade_output_bytes 1 KiB to 4 MiB")
	}
	if c.HostMemoryMB < 0 || (c.HostMemoryMB > 0 && c.HostMemoryMB < c.Max.MemoryMB) {
		return errors.New("runner config: host_memory_mb is 0 (70% of the machine's memory) or at least the most one container may have")
	}
	if c.BuildMaxSeconds < 10 || c.BuildMemoryMB < 256 {
		return errors.New("runner config: build_max_seconds is at least 10 and build_memory_mb at least 256")
	}
	for _, registry := range c.BaseRegistries {
		if !registryPattern.MatchString(registry) {
			return fmt.Errorf("runner config: base_registries has %q, which is not a registry (docker.io, ghcr.io, localhost:5000)", registry)
		}
	}
	// A full path: rooted at "/" (the runner's host is Linux), or absolute where the code is only tested (a drive letter on Windows).
	if c.AgentBinary != "" && !strings.HasPrefix(c.AgentBinary, "/") && !filepath.IsAbs(c.AgentBinary) {
		return errors.New("runner config: agent_binary is a full path")
	}
	return nil
}

// StopTimeout is how long a stop waits.
func (c Config) StopTimeout() time.Duration { return time.Duration(c.StopSeconds) * time.Second }
