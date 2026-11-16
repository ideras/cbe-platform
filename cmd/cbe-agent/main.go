//go:build linux

// Command cbe-agent is the first process of a student's container. It does the file operations, watches the files, runs the
// terminals and answers on a Unix socket in the language of internal/agentproto (docs/workspace/agent-protocol.md).
//
//	cbe-agent [-socket /run/cbe/agent.sock] [-work /work] [-shell /bin/bash] [-heartbeat 5s]
//
// It runs as the student, never as root, and needs no network. As PID 1 it also reaps every orphaned process.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"exam-platform/internal/agent"
)

var version = "dev"

func main() {
	cfg := agent.Config{Version: version}
	flag.StringVar(&cfg.Socket, "socket", envOr("CBE_AGENT_SOCKET", "/run/cbe/agent.sock"), "the Unix socket to listen on")
	flag.StringVar(&cfg.Work, "work", envOr("CBE_AGENT_WORK", "/work"), "the folder of the student's files")
	flag.StringVar(&cfg.Shell, "shell", "/bin/bash", "the shell of a terminal")
	flag.StringVar(&cfg.Home, "home", "/tmp/home", "the home folder of the programs the agent starts (made if it is missing)")
	flag.DurationVar(&cfg.Heartbeat, "heartbeat", 5*time.Second, "how often to send a heartbeat to every connection")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	cfg.ReapAll = os.Getpid() == 1
	if os.Geteuid() == 0 {
		log.Print("cbe-agent: warning: running as root; the image should run it as the student user")
	}
	a, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("cbe-agent: %v", err)
	}
	// As PID 1 the agent gets no default action for a signal, so TERM and INT are handled here, or `podman stop` would wait for the kill.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Printf("cbe-agent %s: listening on %s, files in %s, pid %d", version, a.Socket(), cfg.Work, os.Getpid())
	if err := a.Serve(ctx); err != nil {
		log.Fatalf("cbe-agent: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
