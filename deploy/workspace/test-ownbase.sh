#!/usr/bin/env bash
# The real test of exams with a base image of their own (internal/runner/ownbase_real_test.go): real image builds through the container runtime of
# this host (the author's Containerfile, the runner's finishing layer, the agent's self test in a container made as a student's is), and real
# containers made from what was built and driven through their agents. It starts a PRIVATE cbe-runner (its own socket, secret and folders; the same
# container runtime and quota helper), so the installed runner and whatever containers anyone has made with it are not touched: nothing here
# restarts a service or the runtime's user manager. Whatever containers and images the run made or pulled are removed at the end (and only those:
# by ID, against what was there before).
#
#   deploy/workspace/test-ownbase.sh                   the bases that are on the host (docker.io/library/alpine:3.20 and ubuntu:24.04): no network
#   CBE_OWNBASE_NET=1 deploy/workspace/test-ownbase.sh also pulls python:3.12-slim and redis:7-alpine (needs the network; a few minutes)
#
# Needs deploy/workspace/install.sh to have been run (the accounts, the quota helper, the folders). It uses sudo.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
BASE=/data/cbe-platform
work=$(mktemp -d)
images_before=$(sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" podman images -a --format '{{.ID}}' | sort)
# shellcheck source=private-runner.sh
. "$repo/deploy/workspace/private-runner.sh"
cleanup() {
  private_runner_stop || true
  # Every image the run made or pulled (the exams' images, the pending ones, the bases it pulled) goes, and only those: by ID, against what was there before.
  for id in $(runtime images -a --format '{{.ID}}' | sort | comm -13 <(echo "$images_before") -); do runtime rmi -f "$id" >/dev/null 2>&1 || true; done
  rm -rf "$work"
}
trap cleanup EXIT

(cd "$repo" && go test -c -o "$work/runner.test" ./internal/runner)
mkdir -p "$work/internal/runner"
cp -r "$repo/testdata" "$work/testdata"
chmod -R a+rX "$work"; chmod a+rx "$work"
private_runner_start "$work"
echo "the private runner is up ($PR_TAG)"
sudo -n -u cbe-server env CBE_RUNNER_SOCKET="$PR_SOCKET" CBE_RUNNER_SECRET_FILE="$PR_SECRET" CBE_OWNBASE_NET="${CBE_OWNBASE_NET:-}" \
  bash -c "cd '$work/internal/runner' && ../../runner.test -test.run '^TestRealOwnBase\$' -test.v -test.timeout 30m"
echo "== what the run left"
left=$(runtime ps -a --format '{{.Names}}' | sort | comm -13 <(echo "$PR_BEFORE") -)
[ -z "$left" ] && echo "no containers left" || { echo "containers left: $left"; exit 1; }
