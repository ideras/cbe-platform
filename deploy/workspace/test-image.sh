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
# Needs deploy/workspace/install.sh to have been run (the accounts, the quota helper, the folders) and the image (images/build.sh cpp20).
# It uses sudo. Takes about three minutes (about six with CBE_E2E_APT=1).
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
BASE=/data/cbe-platform
work=$(mktemp -d)
state=$BASE/server/e2e-image-$$
tag=e2e-$$
runtime() { sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" podman "$@"; }
containers_before=$(runtime ps -a --external --format '{{.ID}}' | sort)
images_before=$(runtime images -a --format '{{.ID}}' | sort)
cleanup() {
  sudo -n pkill -u "$RUNNER" -f "^$work/cbe-runner -config" || true
  sleep 1
  # Any container the run made and left (whoever's it was) goes; what was there before it began stays.
  for id in $(runtime ps -a --external --format '{{.ID}}' | sort | comm -13 <(echo "$containers_before") -); do runtime rm -f "$id" >/dev/null 2>&1 || true; done
  # The images this run built: by name, and only those that were not there before it.
  for ref in $(runtime images --format '{{.Repository}}:{{.Tag}}' | grep -E '^localhost/cbe-(exam|pending)/' || true); do
    id=$(runtime image inspect "$ref" --format '{{.Id}}' 2>/dev/null | cut -c1-12 || true)
    echo "$images_before" | grep -q "^$id" || runtime rmi -f "$ref" >/dev/null 2>&1 || true
  done
  sudo -n -u cbe-server rm -rf "$state" || true
  sudo -n rm -rf "$BASE/runner-$tag" "$BASE/run-$tag" "$BASE/grading-$tag" "$BASE/ipc/runner-$tag.sock" "$BASE/ipc/runner-$tag.secret" || true
  rm -rf "$work"
}
trap cleanup EXIT

(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-portal-server" ./cmd/server && go build -trimpath -o "$work/cbe-runner" ./cmd/cbe-runner)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
mkdir -p "$work/pkg" "$work/plain"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-image" > "$work/pkg/document.json"
"$work/examc" -json -bundles "$work/plain" "$repo/testdata/workspace/lab-listas" > "$work/plain/document.json"
cp -r "$repo/testdata/workspace/lab-image-submissions" "$work/submissions"
chmod -R a+rX "$work"; chmod a+rx "$work"
base_digest=$(runtime image inspect localhost/cbe/cpp20:latest --format '{{index .RepoDigests 0}}' | cut -d@ -f2)
[ -n "$base_digest" ] || { echo "the course image cbe/cpp20 has no digest on this host" >&2; exit 1; }
echo "the course image's digest: $base_digest"

# The private runner: the installed configuration with a socket, a secret and folders of its own, and a memory limit for builds that a test can reach.
python3 - "$work/runner.json" "$BASE" "$tag" <<'PY'
import json, sys
out, base, tag = sys.argv[1:]
cfg = json.load(open('/etc/cbe-platform/runner.json'))
cfg.update({'socket': f'{base}/ipc/runner-{tag}.sock', 'secret_file': f'{base}/ipc/runner-{tag}.secret', 'state_root': f'{base}/runner-{tag}',
            'run_root': f'{base}/run-{tag}', 'grade_root': f'{base}/grading-{tag}', 'build_memory_mb': 256})
json.dump(cfg, open(out, 'w'), indent=2)
PY
head -c 32 /dev/urandom | base64 | tr -d '\n=' > "$work/secret"
sudo -n install -o "$RUNNER" -g cbe-ipc -m 0640 "$work/secret" "$BASE/ipc/runner-$tag.secret"
sudo -n install -d -o "$RUNNER" -g "$RUNNER" -m 0700 "$BASE/runner-$tag" "$BASE/run-$tag" "$BASE/grading-$tag"
sudo -n install -d -o cbe-server -g cbe-server -m 0700 "$state"
sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" nohup "$work/cbe-runner" -config "$work/runner.json" > "$work/runner.log" 2>&1 &
for _ in $(seq 1 50); do sudo -n test -S "$BASE/ipc/runner-$tag.sock" && break; sleep 0.2; done
sudo -n test -S "$BASE/ipc/runner-$tag.sock" || { cat "$work/runner.log" >&2; echo "the private runner did not start" >&2; exit 1; }

status=0
sudo -n -u cbe-server env CBE_RUNNER_SOCKET="$BASE/ipc/runner-$tag.sock" CBE_RUNNER_SECRET_FILE="$BASE/ipc/runner-$tag.secret" \
  CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE=I CBE_E2E_PACKAGE="$work/pkg" CBE_E2E_PLAIN_PACKAGE="$work/plain" \
  CBE_E2E_SUBMISSIONS="$work/submissions" CBE_E2E_BASE_DIGEST="$base_digest" CBE_E2E_APT="${CBE_E2E_APT:-}" \
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
works=$(sudo -n ls "$BASE/work" 2>/dev/null | grep '^grade-' || true)
[ -z "$works" ] || { echo "/work folders of grading runs left: $works"; left=1; }
[ "$left" -eq 0 ] && echo "nothing left behind (the images the run built are removed next)"
[ "$status" -eq 0 ] || echo "the test failed (exit $status)"
exit $((left || status))
