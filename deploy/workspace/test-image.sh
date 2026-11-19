#!/usr/bin/env bash
# Phase 7's real test of an exam's own image (internal/workspace/e2e, phase I): the exam server's binary as its own account, real image builds
# through the container runtime from the course image, real containers made from what was built, and Containerfiles that break the rules. It
# starts a PRIVATE cbe-runner (its own socket, secret and folders; the same container runtime and quota helper), so the installed runner and
# whatever containers anyone has made with it are not touched: nothing here restarts a service or the runtime's user manager. Whatever
# containers and images the run made are removed at the end (the images named localhost/cbe-exam/* and localhost/cbe-pending/*, and only the ones
# that were not there before), and what the run left that it should not have is reported.
#
#   deploy/workspace/test-image.sh             hermetic: the library is built from source in the Containerfile, no network is needed
#   CBE_E2E_APT=1 deploy/workspace/test-image.sh   also builds a library from the Ubuntu archive snapshot over HTTPS (needs the network; a few minutes)
#
# Needs a server set up by the installer and cbe-portalctl configure for workspace exams (host.sh reads it), with the course image cbe/cpp20
# (sudo cbe-portalctl image install cbe/cpp20 images/cpp20 --restart). It runs only while the server is quiet, and uses sudo. Takes about three
# minutes (about six with CBE_E2E_APT=1).
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=host.sh
. "$repo/deploy/workspace/host.sh"
# shellcheck source=private-runner.sh
. "$repo/deploy/workspace/private-runner.sh"
require_quiet "$0"
course=$(course_image cbe/cpp20)
work=$(mktemp -d)
state=$(server_state image)
containers_before=$(runtime ps -a --external --format '{{.ID}}' | sort)
images_before=$(runtime images -a --format '{{.ID}}' | sort)
cleanup() {
  private_runner_stop || true
  # A build's working containers the run left: they were not there before it began.
  for id in $(runtime ps -a --external --format '{{.ID}}' | sort | comm -13 <(echo "$containers_before") -); do runtime rm -f "$id" >/dev/null 2>&1 || true; done
  # The images this run built: by name, and only those that were not there before it.
  for ref in $(runtime images --format '{{.Repository}}:{{.Tag}}' | grep -E '^localhost/cbe-(exam|pending)/' || true); do
    id=$(runtime image inspect "$ref" --format '{{.Id}}' 2>/dev/null | cut -c1-12 || true)
    echo "$images_before" | grep -q "^$id" || runtime rmi -f "$ref" >/dev/null 2>&1 || true
  done
  # The layers its builds left (untagged, new since it began), from the last one down: rmi without --force removes none that an image is made from.
  for _ in $(seq 1 30); do
    removed=0
    for id in $(runtime images -a --filter dangling=true --format '{{.ID}}' | sort | comm -13 <(echo "$images_before") -); do
      runtime rmi "$id" >/dev/null 2>&1 && removed=1
    done
    [ "$removed" -eq 1 ] || break
  done
  sudo -n rm -rf "$state" || true
  rm -rf "$work"
}
trap cleanup EXIT

(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-portal-server" ./cmd/server)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
mkdir -p "$work/pkg" "$work/plain"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-image" > "$work/pkg/document.json"
"$work/examc" -json -bundles "$work/plain" "$repo/testdata/workspace/lab-listas" > "$work/plain/document.json"
cp -r "$repo/testdata/workspace/lab-image-submissions" "$work/submissions"
base_digest=$(runtime image inspect "$course" --format '{{index .RepoDigests 0}}' | cut -d@ -f2)
[ -n "$base_digest" ] || { echo "the course image cbe/cpp20 ($course) has no digest on this host" >&2; exit 1; }
echo "the course image: $course, digest $base_digest"

# A memory limit for builds that a test can reach.
PR_EXTRA='{"build_memory_mb": 256}' private_runner_start "$work"

status=0
sudo -n -u "$SERVER" env CBE_RUNNER_SOCKET="$PR_SOCKET" CBE_RUNNER_SECRET_FILE="$PR_SECRET" \
  CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE=I CBE_E2E_PACKAGE="$work/pkg" CBE_E2E_PLAIN_PACKAGE="$work/plain" \
  CBE_E2E_SUBMISSIONS="$work/submissions" CBE_E2E_BASE_DIGEST="$base_digest" CBE_E2E_BASE_IMAGE="${course%:*}" CBE_E2E_APT="${CBE_E2E_APT:-}" \
  "$work/e2e.test" -test.run '^TestRealServer$' -test.v -test.timeout "${TEST_TIMEOUT:-30m}" || status=$?

echo "== what the runs left (before they are cleaned up)"
left=0
checking=$(runtime ps -a --filter label=cbe.role=verify --format '{{.Names}}')
[ -z "$checking" ] || { echo "checking containers left:"; echo "$checking"; left=1; }
grading=$(runtime ps -a --filter label=cbe.role=grading --format '{{.Names}}')
[ -z "$grading" ] || { echo "grading containers left:"; echo "$grading"; left=1; }
new_external=$(for id in $(runtime ps -a --external --format '{{.ID}}' | sort | comm -13 <(echo "$containers_before") -); do runtime ps -a --external --filter "id=$id" --format '{{.ID}} {{.Names}}'; done)
[ -z "$new_external" ] || { echo "containers (a build's working containers, or any other) left:"; echo "$new_external"; left=1; }
pending=$(runtime images --format '{{.Repository}}:{{.Tag}}' | grep '^localhost/cbe-pending/' || true)
[ -z "$pending" ] || { echo "temporary images left: $pending"; left=1; }
works=$(sudo -n ls "$WORK_ROOT" 2>/dev/null | grep '^grade-' || true)
[ -z "$works" ] || { echo "/work folders of grading runs left: $works"; left=1; }
[ "$left" -eq 0 ] && echo "nothing left behind (the images the run built are removed next)"
[ "$status" -eq 0 ] || echo "the test failed (exit $status)"
exit $((left || status))
