//go:build linux

// Command cbe-runner is the one service that holds the container runtime's socket (workspace plan, section 4). It runs as the cbe-runner
// user, as a systemd user unit with lingering, and gives the exam server the operations of Appendix E over a Unix socket, behind a
// shared secret. See docs/workspace/deployment.md.
//
//	cbe-runner [-config /etc/cbe-platform/runner.json]
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"exam-platform/internal/quota"
	"exam-platform/internal/runner"
	"exam-platform/internal/runner/dockerrt"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	configPath := flag.String("config", "", "a JSON file over the defaults (docs/workspace/deployment.md)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("cbe-runner", version)
		return
	}
	if err := run(*configPath, logger); err != nil {
		logger.Error("cbe-runner failed", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := runner.LoadConfig(configPath)
	if err != nil {
		return err
	}
	secret, err := readOrMakeSecret(cfg.SecretFile, cfg.SocketGroup)
	if err != nil {
		return err
	}
	runtime, err := dockerrt.New(cfg.RuntimeSocket)
	if err != nil {
		return err
	}
	defer runtime.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := runtime.Ping(ctx); err != nil {
		return fmt.Errorf("the container runtime at %s does not answer: %w", cfg.RuntimeSocket, err)
	}
	service, err := runner.NewService(cfg, runtime, quota.NewClient(cfg.QuotaSocket), logger)
	if err != nil {
		return err
	}
	defer service.Close()
	adopted, err := service.Reconcile(ctx)
	if err != nil {
		return err
	}
	listener, err := listen(cfg.Socket, cfg.SocketGroup)
	if err != nil {
		return err
	}
	defer os.Remove(cfg.Socket)
	logger.Info("cbe-runner ready", "release", version, "socket", cfg.Socket, "runtime", cfg.RuntimeSocket, "containers_adopted", adopted, "images", len(cfg.Images))
	return runner.Serve(ctx, listener, runner.Handler(service, secret, logger))
}

// listen makes the API socket, readable and writable by the group the exam server is in, and by nobody else.
func listen(path, group string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			listener.Close()
			return nil, err
		}
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(path, -1, gid); err != nil {
			listener.Close()
			return nil, fmt.Errorf("giving the socket to group %s: %w", group, err)
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

// readOrMakeSecret reads the shared secret, or makes one the first time: 32 random bytes in hex, in a file that only the runner and
// the group can read.
func readOrMakeSecret(path, group string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		secret := strings.TrimSpace(string(data))
		if len(secret) < 32 {
			return "", fmt.Errorf("%s holds a secret shorter than 32 characters", path)
		}
		return secret, nil
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o640); err != nil {
		return "", err
	}
	if g, err := user.LookupGroup(group); err == nil {
		gid, _ := strconv.Atoi(g.Gid)
		_ = os.Chown(path, -1, gid)
	}
	return secret, nil
}
