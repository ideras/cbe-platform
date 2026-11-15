#!/usr/bin/env bash
# Phase 6's real test of grading (internal/workspace/e2e, phase E): the exam server's binary as its own account, real containers from the
# course image, the example package's own grade script, and grade programs that misbehave. It starts a PRIVATE cbe-runner (its own socket,
# secret and folders; the same container runtime and quota helper), so the installed runner, and whatever containers anyone has made
# with it, are not touched: nothing here restarts a service or the runtime's user manager. What it made is removed at the end, and what
# it finds left is reported.
#
#   deploy/workspace/test-grading.sh
#
# Needs deploy/workspace/install.sh to have been run (the accounts, the quota helper, the folders) and the image (images/build.sh cpp20).
# It uses sudo. Takes about five minutes.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
BASE=/data/cbe-platform
work=$(mktemp -d)
state=$BASE/server/e2e-grading-$$
tag=e2e-$$
runtime() { sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" podman "$@"; }
before=$(runtime ps -a --format '{{.Names}}' | sort)
cleanup() {
  sudo -n pkill -u "$RUNNER" -f "^$work/cbe-runner -config" || true
  sleep 1
  # Any container the run made and left (whoever's it was) goes; what was there before it began stays.
  for name in $(runtime ps -a --format '{{.Names}}' | sort | comm -13 <(echo "$before") -); do runtime rm -f "$name" >/dev/null 2>&1 || true; done
  sudo -n -u cbe-server rm -rf "$state" || true
  sudo -n rm -rf "$BASE/runner-$tag" "$BASE/run-$tag" "$BASE/grading-$tag" "$BASE/ipc/runner-$tag.sock" "$BASE/ipc/runner-$tag.secret" || true
  rm -rf "$work"
}
trap cleanup EXIT

(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath -o "$work/cbe-portal-server" ./cmd/server && go build -trimpath -o "$work/cbe-runner" ./cmd/cbe-runner)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
mkdir -p "$work/pkg"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-listas" > "$work/pkg/document.json"
cp -r "$repo/testdata/workspace/lab-listas-submissions" "$work/submissions"
chmod -R a+rX "$work"; chmod a+rx "$work"

# The private runner: the installed configuration with a socket, a secret and folders of its own.
python3 - "$work/runner.json" "$BASE" "$tag" <<'PY'
import json, sys
out, base, tag = sys.argv[1:]
cfg = json.load(open('/etc/cbe-platform/runner.json'))
cfg.update({'socket': f'{base}/ipc/runner-{tag}.sock', 'secret_file': f'{base}/ipc/runner-{tag}.secret', 'state_root': f'{base}/runner-{tag}',
            'run_root': f'{base}/run-{tag}', 'grade_root': f'{base}/grading-{tag}', 'grade_parallel': 2})
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
  CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE=E CBE_E2E_PACKAGE="$work/pkg" CBE_E2E_SUBMISSIONS="$work/submissions" \
  "$work/e2e.test" -test.run '^TestRealServer$' -test.v -test.timeout "${TEST_TIMEOUT:-20m}" || status=$?

echo "== what the runs left"
left=0
containers=$(runtime ps -a --filter label=cbe.role=grading --format '{{.Names}}')
[ -z "$containers" ] || { echo "grading containers left:"; echo "$containers"; left=1; }
folders=$(sudo -n ls "$BASE/grading-$tag" 2>/dev/null || true)
[ -z "$folders" ] || { echo "grading bundles left: $folders"; left=1; }
works=$(sudo -n ls "$BASE/work" 2>/dev/null | grep '^grade-' || true)
[ -z "$works" ] || { echo "/work folders of grading runs left: $works"; left=1; }
notes=$(sudo -n ls "$BASE/runner-$tag/grading" 2>/dev/null || true)
[ -z "$notes" ] || { echo "run notes left: $notes"; left=1; }
[ "$left" -eq 0 ] && echo "nothing left behind"
[ "$status" -eq 0 ] || echo "the test failed (exit $status)"
exit $((left || status))
