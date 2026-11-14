#!/usr/bin/env bash
# Run the real end-to-end test of workspace exams (internal/workspace/e2e) on the container host: the exam server's binary as its own
# account (cbe-server), against the installed runner and quota helper, rootless Podman, the course image and the example package.
#
#   deploy/workspace/test-server.sh
#   deploy/workspace/test-server.sh --manual [address]    a throwaway exam server for a person to try the applications against (below)
#
# Needs deploy/workspace/install.sh to have been run and the image (images/build.sh cpp20). It uses sudo. Phase A does everything, the
# runner is restarted, phase B checks the containers were found again by a new server and then lets an exam run out, and the runtime's
# user manager is restarted (what a boot does to containers) for phase C to check the workspaces come back with their files, and
# phase D has a student finish the example package through the student application's own client. Takes about six minutes. Whatever happens, the test's containers and folders are removed at the end.
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
    CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE="$1" CBE_E2E_PACKAGE="$work/pkg" CBE_E2E_STUDENT_TEST="$work/student.test" \
    CBE_E2E_RUNTIME_SOCKET="/run/user/$RUID/podman/podman.sock" CBE_E2E_BIND="${CBE_E2E_BIND:-}" "$work/e2e.test" -test.run '^TestRealServer$' -test.v -test.timeout "${TEST_TIMEOUT:-15m}"
}
(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-portal-server" ./cmd/server)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
(cd "$repo/apps/student" && go test -c -o "$work/student.test" ./internal/client)
mkdir -p "$work/pkg"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-listas" > "$work/pkg/document.json"
chmod -R a+rX "$work"; chmod a+rx "$work"
sudo -n -u cbe-server install -d -m 0700 "$state"
trap cleanup EXIT
if [ "${1:-}" = "--manual" ]; then
  # Not a test: the server on 127.0.0.1:8090 (or the address given, for a virtual machine to reach), the example exam published,
  # until Ctrl+C. Everything it made is removed then. See phaseManual in internal/workspace/e2e.
  export CBE_E2E_BIND="${2:-127.0.0.1:8090}" TEST_TIMEOUT=6h
  run M
  exit 0
fi
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
echo "== phase D: a student finishes the example package through the student application's client"
run D
