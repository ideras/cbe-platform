#!/usr/bin/env bash
# Run the real end-to-end test of workspace exams (internal/workspace/e2e) on the container host: the exam server's binary as its own
# account (cbe-server), against the installed runner and quota helper, rootless Podman, the course image and the example package.
#
#   deploy/workspace/test-server.sh
#
# Needs deploy/workspace/install.sh to have been run and the image (images/build.sh cpp20). It uses sudo. Phase A does everything, the
# runner is restarted, phase B checks the containers were found again by a new server and then lets an exam run out, and the runtime's
# user manager is restarted (what a boot does to containers) for phase C to check the workspaces come back with their files. Takes
# about five minutes. Whatever happens, the test's containers and folders are removed at the end.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
work=$(mktemp -d)
state=/data/cbe-platform/server/e2e-$$
cleanup() {
  run cleanup || true
  sudo -n -u cbe-server rm -rf "$state" || true
  rm -rf "$work"
}
run() { # phase
  sudo -n -u cbe-server env CBE_RUNNER_SOCKET=/data/cbe-platform/ipc/runner.sock CBE_RUNNER_SECRET_FILE=/data/cbe-platform/ipc/runner.secret \
    CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE="$1" CBE_E2E_PACKAGE="$work/pkg" \
    CBE_E2E_RUNTIME_SOCKET="/run/user/$RUID/podman/podman.sock" "$work/e2e.test" -test.run '^TestRealServer$' -test.v -test.timeout 15m
}
(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-portal-server" ./cmd/server)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
mkdir -p "$work/pkg"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-listas" > "$work/pkg/document.json"
chmod -R a+rX "$work"; chmod a+rx "$work"
sudo -n -u cbe-server install -d -m 0700 "$state"
trap cleanup EXIT
echo "== phase A"
run A
echo "== restarting the runner (its containers go on)"
sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$RUID/bus" systemctl --user restart cbe-runner.service
sleep 3
echo "== phase B"
run B
echo "== the runtime's user manager restarts, as at a boot: every container goes down with it, and the runner comes back by itself"
sudo -n systemctl restart "user@$RUID.service"
sleep 5
echo "== phase C"
run C
