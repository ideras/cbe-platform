#!/usr/bin/env bash
# Phase 6's real test of grading (internal/workspace/e2e, phase E): the exam server's binary as its own account, real containers from the
# course image, the example package's own grade script, and grade programs that misbehave. It starts a PRIVATE cbe-runner (its own socket,
# secret and folders; the same container runtime and quota helper), so the installed runner, and whatever containers anyone has made
# with it, are not touched: nothing here restarts a service or the runtime's user manager. What it made is removed at the end, and what
# it finds left is reported.
#
#   deploy/workspace/test-grading.sh
#
# Needs a server set up by the installer and cbe-portalctl configure for workspace exams (host.sh reads it), with the course image cbe/cpp20
# (sudo cbe-portalctl image install cbe/cpp20 images/cpp20 --restart). It runs only while the server is quiet, and uses sudo. Takes about five
# minutes.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=host.sh
. "$repo/deploy/workspace/host.sh"
# shellcheck source=private-runner.sh
. "$repo/deploy/workspace/private-runner.sh"
require_quiet "$0"
course_image cbe/cpp20 >/dev/null
work=$(mktemp -d)
state=$(server_state grading)
cleanup() {
  private_runner_stop || true
  sudo -n rm -rf "$state" || true
  rm -rf "$work"
}
trap cleanup EXIT

(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-portal-server" ./cmd/server)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
mkdir -p "$work/pkg"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-listas" > "$work/pkg/document.json"
cp -r "$repo/testdata/workspace/lab-listas-submissions" "$work/submissions"
PR_EXTRA='{"grade_parallel": 2}' private_runner_start "$work"

status=0
sudo -n -u "$SERVER" env CBE_RUNNER_SOCKET="$PR_SOCKET" CBE_RUNNER_SECRET_FILE="$PR_SECRET" \
  CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE=E CBE_E2E_PACKAGE="$work/pkg" CBE_E2E_SUBMISSIONS="$work/submissions" \
  "$work/e2e.test" -test.run '^TestRealServer$' -test.v -test.timeout "${TEST_TIMEOUT:-20m}" || status=$?

echo "== what the runs left"
left=0
containers=$(runtime ps -a --filter label=cbe.role=grading --format '{{.Names}}')
[ -z "$containers" ] || { echo "grading containers left:"; echo "$containers"; left=1; }
folders=$(sudo -n ls "$DATA_DIR/grading-$PR_TAG" 2>/dev/null || true)
[ -z "$folders" ] || { echo "grading bundles left: $folders"; left=1; }
works=$(sudo -n ls "$WORK_ROOT" 2>/dev/null | grep '^grade-' || true)
[ -z "$works" ] || { echo "/work folders of grading runs left: $works"; left=1; }
notes=$(sudo -n ls "$DATA_DIR/runner-$PR_TAG/grading" 2>/dev/null || true)
[ -z "$notes" ] || { echo "run notes left: $notes"; left=1; }
[ "$left" -eq 0 ] && echo "nothing left behind"
[ "$status" -eq 0 ] || echo "the test failed (exit $status)"
exit $((left || status))
