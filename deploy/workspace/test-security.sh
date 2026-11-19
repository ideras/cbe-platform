#!/usr/bin/env bash
# Phase 8's security checks that need the container host (docs/workspace/security-checklist.md): the runner's real-runtime tests
# (internal/runner/integration_test.go), including the links, mounts, privileges and file-count checks of a real container, against a PRIVATE runner
# (deploy/workspace/private-runner.sh), as the exam server's account. It restarts nothing and removes what it made.
#
#   deploy/workspace/test-security.sh
#
# Needs a server set up by the installer and cbe-portalctl configure for workspace exams (host.sh reads it), with the course image cbe/cpp20
# (sudo cbe-portalctl image install cbe/cpp20 images/cpp20 --restart). It runs only while the server is quiet, and uses sudo.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=host.sh
. "$repo/deploy/workspace/host.sh"
# shellcheck source=private-runner.sh
. "$repo/deploy/workspace/private-runner.sh"
require_quiet "$0"
course_image cbe/cpp20 >/dev/null
work=$(mktemp -d)
cleanup() { private_runner_stop; rm -rf "$work"; }
trap cleanup EXIT
(cd "$repo" && go test -c -o "$work/runner.test" ./internal/runner)
mkdir -p "$work/internal/runner"
cp -r "$repo/testdata" "$work/testdata"
private_runner_start "$work"
echo "== the runner's real-runtime tests, against the private runner (pid $PR_PID)"
status=0
sudo -n -u "$SERVER" env CBE_RUNNER_SOCKET="$PR_SOCKET" CBE_RUNNER_SECRET_FILE="$PR_SECRET" CBE_IT_PHASE=A CBE_IT_KEEP="" CBE_IT_STATE="" \
  bash -c "cd '$work/internal/runner' && ../../runner.test -test.run '^TestRealRunner\$' -test.v -test.timeout 15m" || status=$?
[ "$status" -eq 0 ] && echo "== the security checks passed" || echo "== the security checks FAILED (exit $status)"
exit "$status"
