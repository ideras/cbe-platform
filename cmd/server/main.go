package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"exam-platform/internal/config"
	"exam-platform/internal/server"
)

// version is the release, set by the build: go build -ldflags "-X main.version=v0.15.0".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.Parse(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	if cfg.ShowVersion {
		fmt.Println("cbe-portal-server", version)
		return
	}
	cfg.Version = version
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := server.Run(ctx, cfg, logger); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
