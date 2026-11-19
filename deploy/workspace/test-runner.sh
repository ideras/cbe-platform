#!/usr/bin/env bash
# Run the runner's real-runtime tests (internal/runner/integration_test.go) on the container host, as the exam server's account, against the
# INSTALLED runner.
#
#   CBE_ALLOW_RESTART=1 deploy/workspace/test-runner.sh
#
# Needs a server set up by the installer and cbe-portalctl configure for workspace exams (host.sh reads it), with the course image cbe/cpp20
# (sudo cbe-portalctl image install cbe/cpp20 images/cpp20 --restart). It uses sudo. The tests make two containers, fork-bomb one, fill its disk,
# kill its agent, and check the other is not touched; then the installed runner is restarted, and phase B checks that it finds its containers
# again. A restart cuts every student's connection for a moment, so this is for a test host: it runs only with CBE_ALLOW_RESTART=1 and while no
# container of the runner is running. test-security.sh runs the same tests, but the restart, against a private runner on any server.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=host.sh
. "$repo/deploy/workspace/host.sh"
allow_restarts "$0" "the installed runner"
course_image cbe/cpp20 >/dev/null
work=$(mktemp -d)
state=/tmp/cbe-it-state-$$
trap 'rm -rf "$work"; sudo -n rm -f "$state"' EXIT
(cd "$repo" && go test -c -o "$work/runner.test" ./internal/runner)
mkdir -p "$work/internal/runner"
cp -r "$repo/testdata" "$work/testdata"
chmod -R a+rX "$work"; chmod a+rx "$work"

run() { # phase, keep
  sudo -n -u "$SERVER" env CBE_RUNNER_SOCKET="$SOCKET" CBE_RUNNER_SECRET_FILE="$SECRET" \
    CBE_IT_PHASE="$1" CBE_IT_KEEP="$2" CBE_IT_STATE="$state" bash -c "cd '$work/internal/runner' && ../../runner.test -test.run '^TestRealRunner\$' -test.v -test.timeout 10m"
}
echo "== phase A: the whole of it, leaving two sessions behind"
run A 1
echo "== restarting the runner (its containers go on)"
sudo -n -u "$RUNNER" env HOME="$RUNNER_HOME" XDG_RUNTIME_DIR="/run/user/$RUID" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$RUID/bus" systemctl --user restart cbe-runner.service
sleep 2
echo "== phase B: after the restart"
run B ""
