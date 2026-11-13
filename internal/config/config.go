package config

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Database        string
	Screenshots     string
	Bind            string
	AdminTokenFile  string
	AdminToken      string
	WorkerInterval  time.Duration
	MaxJSONBytes    int64
	MaxAnswerBytes  int64
	MaxAnswersBytes int64
	// RunnerSocket is the socket of cbe-runner. Empty, the server has no workspace exams. RunnerSecretFile holds the shared secret.
	RunnerSocket     string
	RunnerSecretFile string
	// WorkspaceDir is where the starter and grading bundles and the snapshots' blobs are kept.
	WorkspaceDir string
	// WorkspaceRetention is how long a frozen workspace's container is kept after its exam ended; its snapshots stay.
	WorkspaceRetention time.Duration
	// WorkspaceSnapshotEvery is the periodic snapshot interval of an exam that sets none (decision D8).
	WorkspaceSnapshotEvery time.Duration
	// WorkspaceMax are the largest limits an exam may ask for; the runner enforces its own again.
	WorkspaceMaxCPU      float64
	WorkspaceMaxMemoryMB int
	WorkspaceMaxDiskMB   int
	// ShowVersion asks for the version to be printed instead of starting the server.
	ShowVersion bool
	// Version is the server's release, stamped into the binary by the build; it is not a flag.
	Version string
}

func Parse(args []string, output io.Writer) (Config, error) {
	var cfg Config
	flags := flag.NewFlagSet("cbe-portal-server", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.Database, "database", "data/exams.db", "SQLite database file")
	flags.StringVar(&cfg.Screenshots, "screenshots", "data/screenshots", "private image directory")
	flags.StringVar(&cfg.Bind, "bind", ":8080", "HTTP listen address")
	flags.StringVar(&cfg.AdminTokenFile, "admin-token-file", "data/admin-token", "private file containing a proctor-only bearer token")
	flags.DurationVar(&cfg.WorkerInterval, "worker-interval", time.Second, "lifecycle worker cadence")
	flags.Int64Var(&cfg.MaxJSONBytes, "max-json-bytes", 1<<20, "maximum exam/answer JSON request bytes")
	flags.Int64Var(&cfg.MaxAnswerBytes, "max-answer-bytes", 64<<10, "maximum UTF-8 bytes per answer")
	flags.Int64Var(&cfg.MaxAnswersBytes, "max-answers-bytes", 512<<10, "maximum total UTF-8 answer bytes per snapshot")
	flags.StringVar(&cfg.RunnerSocket, "runner-socket", "", "cbe-runner's socket; with it the server serves workspace exams (docs/workspace/deployment.md)")
	flags.StringVar(&cfg.RunnerSecretFile, "runner-secret-file", "", "the file with the secret shared with cbe-runner")
	flags.StringVar(&cfg.WorkspaceDir, "workspace-dir", "data/workspace", "where workspace bundles and snapshots are kept")
	flags.DurationVar(&cfg.WorkspaceRetention, "workspace-retention", 48*time.Hour, "how long a finished workspace's container is kept before it is removed (its snapshots stay)")
	flags.DurationVar(&cfg.WorkspaceSnapshotEvery, "workspace-snapshot-every", 60*time.Second, "the interval of periodic workspace snapshots, for exams that set none")
	flags.Float64Var(&cfg.WorkspaceMaxCPU, "workspace-max-cpu", 2, "the most cores an exam may ask for each student")
	flags.IntVar(&cfg.WorkspaceMaxMemoryMB, "workspace-max-memory-mb", 2048, "the most memory (MiB) an exam may ask for each student")
	flags.IntVar(&cfg.WorkspaceMaxDiskMB, "workspace-max-disk-mb", 500, "the most disk (MiB) an exam may ask for each student")
	flags.BoolVar(&cfg.ShowVersion, "version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	if flags.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected positional arguments")
	}
	if cfg.ShowVersion {
		return cfg, nil // nothing is started, so the other settings do not matter
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	for name, value := range map[string]time.Duration{
		"worker-interval": c.WorkerInterval,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be positive", name)
		}
	}
	if c.MaxJSONBytes <= 0 || c.MaxAnswerBytes <= 0 || c.MaxAnswersBytes < c.MaxAnswerBytes || c.MaxAnswersBytes > c.MaxJSONBytes {
		return fmt.Errorf("request/answer limits must be positive, with per-answer <= total-answer <= request bytes")
	}
	_, port, err := net.SplitHostPort(c.Bind)
	if err != nil {
		return fmt.Errorf("invalid bind address: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("bind port must be between 1 and 65535")
	}
	if (c.RunnerSocket == "") != (c.RunnerSecretFile == "") {
		return fmt.Errorf("runner-socket and runner-secret-file go together")
	}
	if c.RunnerSocket != "" && (c.WorkspaceDir == "" || c.WorkspaceRetention <= 0 || c.WorkspaceSnapshotEvery < 10*time.Second || c.WorkspaceMaxCPU <= 0 || c.WorkspaceMaxMemoryMB < 64 || c.WorkspaceMaxDiskMB < 1) {
		return fmt.Errorf("the workspace settings are not usable: a folder, a retention, a snapshot interval of at least 10s and limits are needed")
	}
	for name, path := range map[string]string{"database": c.Database, "screenshots": c.Screenshots, "admin-token-file": c.AdminTokenFile} {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("%s path is required", name)
		}
	}
	return nil
}

// Prepare checks access without truncating existing files; created data paths are private.
func (c *Config) Prepare() error {
	if err := c.Validate(); err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(c.Database), c.Screenshots} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
		probe, err := os.CreateTemp(dir, ".exam-write-check-*")
		if err != nil {
			return fmt.Errorf("directory %s is not writable: %w", dir, err)
		}
		name := probe.Name()
		closeErr := probe.Close()
		removeErr := os.Remove(name)
		if closeErr != nil {
			return closeErr
		}
		if removeErr != nil {
			return removeErr
		}
	}
	if err := checkRegular(c.AdminTokenFile); err != nil {
		return fmt.Errorf("admin token: %w", err)
	}
	file, err := os.Open(c.AdminTokenFile)
	if err != nil {
		return fmt.Errorf("open admin token: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("admin token must be a regular file")
	}
	if err := checkPrivateToken(file, info); err != nil {
		return fmt.Errorf("admin token: %w", err)
	}
	token, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read admin token: %w", err)
	}
	c.AdminToken = strings.TrimSpace(string(token))
	if len(c.AdminToken) < 32 || strings.ContainsAny(c.AdminToken, " \t\r\n") {
		return fmt.Errorf("admin token must contain at least 32 non-whitespace characters")
	}
	if err := checkRegular(c.Database); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("database: %w", err)
	}
	db, err := os.OpenFile(c.Database, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("database is not writable: %w", err)
	}
	return db.Close()
}

func checkRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file, not a directory or symlink", path)
	}
	return nil
}
