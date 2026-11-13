//go:build linux

// Command cbe-quota-helper makes and removes the size-limited folders that become the students' /work. It runs as root (only root can
// set XFS project quotas) and serves only the cbe-runner user, over a Unix socket. It replaces the shell prototype of Phase 0.
//
//	cbe-quota-helper -volume /data/cbe-platform/work -state /data/cbe-platform -socket /run/cbe-quota/helper.sock -runner cbe-runner
//
// Decision P3-3 in docs/workspace/DECISIONS.md and docs/workspace/deployment.md say how it is installed.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"exam-platform/internal/quota"
)

func main() {
	volume := flag.String("volume", "/data/cbe-platform/work", "the XFS volume's mount point (mounted with prjquota)")
	state := flag.String("state", "/data/cbe-platform", "where the helper keeps its project counter")
	socket := flag.String("socket", "/run/cbe-quota/helper.sock", "the Unix socket to serve")
	runner := flag.String("runner", "cbe-runner", "the only user the helper serves")
	flag.Parse()
	if os.Geteuid() != 0 {
		log.Fatal("cbe-quota-helper: it must run as root, to set project quotas")
	}
	account, err := user.Lookup(*runner)
	if err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	uid, _ := strconv.Atoi(account.Uid)
	gid, _ := strconv.Atoi(account.Gid)
	// The student is uid and gid 1000 in the image (decision P0-30); on the host that is a subordinate ID of the runner.
	mappedUID, err := quota.MappedID("/etc/subuid", *runner, 1000)
	if err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	mappedGID, err := quota.MappedID("/etc/subgid", *runner, 1000)
	if err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	helper, err := quota.New(quota.Config{Volume: *volume, State: *state, OwnerUID: mappedUID, OwnerGID: mappedGID, RequireMount: true})
	if err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*socket), 0o750); err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	// The folder is root's and the runner's group; the socket is the runner's only.
	if err := os.Chown(filepath.Dir(*socket), 0, gid); err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	_ = os.Remove(*socket)
	listener, err := net.Listen("unix", *socket)
	if err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	if err := os.Chown(*socket, uid, gid); err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	if err := os.Chmod(*socket, 0o600); err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Printf("cbe-quota-helper: serving %s for %s; folders on %s are owned by %d:%d", *socket, *runner, *volume, mappedUID, mappedGID)
	if err := quota.Serve(ctx, listener, helper, uid); err != nil {
		log.Fatalf("cbe-quota-helper: %v", err)
	}
}
