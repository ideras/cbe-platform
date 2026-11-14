#!/usr/bin/env bash
# Run the runner's real-runtime tests (internal/runner/integration_test.go) on the container host, as the exam server's account.
#
#   deploy/workspace/test-runner.sh
#
# Needs deploy/workspace/install.sh to have been run, and the image (images/build.sh cpp20). It uses sudo. The tests make two containers,
# fork-bomb one, fill its disk, kill its agent, and check the other is not touched; then the runner is restarted, and phase B checks that
# it finds its containers again.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
work=$(mktemp -d)
state=/tmp/cbe-it-state-$$
trap 'rm -rf "$work"; sudo -n rm -f "$state"' EXIT
(cd "$repo" && go test -c -o "$work/runner.test" ./internal/runner)
mkdir -p "$work/internal/runner"
cp -r "$repo/testdata" "$work/testdata"
chmod -R a+rX "$work"; chmod a+rx "$work"

run() { # phase, keep
  sudo -n -u cbe-server env CBE_RUNNER_SOCKET=/data/cbe-platform/ipc/runner.sock CBE_RUNNER_SECRET_FILE=/data/cbe-platform/ipc/runner.secret \
    CBE_IT_PHASE="$1" CBE_IT_KEEP="$2" CBE_IT_STATE="$state" bash -c "cd '$work/internal/runner' && ../../runner.test -test.run '^TestRealRunner\$' -test.v -test.timeout 10m"
}
echo "== phase A: the whole of it, leaving two sessions behind"
run A 1
echo "== restarting the runner (its containers go on)"
sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$RUID/bus" systemctl --user restart cbe-runner.service
sleep 2
echo "== phase B: after the restart"
run B ""
