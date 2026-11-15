#!/usr/bin/env bash
# The Phase 8 load test on the container host: a class of simulated students (internal/loadtest) takes a workspace exam on a throwaway exam
# server (its own account, its own database), the installed runner and quota helper, rootless Podman: forty containers, each student opening
# files, typing (autosave), running make and submitting, for half an hour; the machine is read every ten seconds.
#
#   deploy/workspace/test-load.sh [results-folder]
#
#   CBE_LOAD_STUDENTS=40   CBE_LOAD_DURATION=30m   how big and how long (a short run: CBE_LOAD_STUDENTS=8 CBE_LOAD_DURATION=2m)
#   CBE_E2E_SNAPSHOT_EVERY=60s   the server's periodic snapshot (the default of the real server)
#
# Needs deploy/workspace/install.sh to have been run and the image (images/build.sh cpp20). It uses sudo. It starts a PRIVATE runner of this tree's
# build (deploy/workspace/private-runner.sh: its own socket and folders; the same runtime and quota helper), so the installed runner is not touched.
# The students use every core the containers are allowed: the machine is not for anything else for that half hour. Whatever happens, the test's
# containers and folders are removed.
# The results (load-report.md, load-summary.json, load-machine.csv) are kept in the folder given (default: docs/workspace/reports/phase-8-load).
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
out="${1:-$repo/docs/workspace/reports/phase-8-load}"
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
BASE=/data/cbe-platform
work=$(mktemp -d)
state=$BASE/server/e2e-$$
# shellcheck source=private-runner.sh
. "$repo/deploy/workspace/private-runner.sh"
cleanup() {
  run cleanup || true
  private_runner_stop
  sudo -n -u cbe-server rm -rf "$state" || true
  rm -rf "$work"
}
run() { # phase
  sudo -n -u cbe-server env CBE_RUNNER_SOCKET="$PR_SOCKET" CBE_RUNNER_SECRET_FILE="$PR_SECRET" CBE_E2E_RUNNER_PID="$PR_PID" \
    CBE_E2E_SERVER_BIN="$work/cbe-portal-server" CBE_E2E_STATE="$state" CBE_E2E_PHASE="$1" CBE_E2E_LOAD_PACKAGE="$work/pkg" CBE_E2E_PACKAGE="$work/pkg" \
    CBE_E2E_RUNTIME_SOCKET="/run/user/$RUID/podman/podman.sock" CBE_E2E_SNAPSHOT_EVERY="${CBE_E2E_SNAPSHOT_EVERY:-60s}" \
    ${CBE_LOAD_PROFILE:+CBE_LOAD_PROFILE=1 CBE_PPROF=127.0.0.1:6061} CBE_LOAD_STUDENTS="${CBE_LOAD_STUDENTS:-40}" CBE_LOAD_DURATION="${CBE_LOAD_DURATION:-30m}" "$work/e2e.test" -test.run '^TestRealServer$' -test.v -test.timeout 3h
}
(cd "$repo" && go test -c -o "$work/e2e.test" ./internal/workspace/e2e && CGO_ENABLED=0 go build -trimpath ${CBE_LOAD_PROFILE:+-tags pprof} -o "$work/cbe-portal-server" ./cmd/server)
(cd "$repo/shared/examdoc" && CGO_ENABLED=0 go build -o "$work/examc" ./cmd/examc)
mkdir -p "$work/pkg"
"$work/examc" -json -bundles "$work/pkg" "$repo/testdata/workspace/lab-carga" > "$work/pkg/document.json"
chmod -R a+rX "$work"; chmod a+rx "$work"
sudo -n -u cbe-server install -d -m 0700 "$state"
trap cleanup EXIT
private_runner_start "$work"
echo "== the load test: ${CBE_LOAD_STUDENTS:-40} students for ${CBE_LOAD_DURATION:-30m}  ($(date +%T))"
status=0
run L || status=$?
mkdir -p "$out"
for name in load-report.md load-summary.json load-machine.csv load-server-warnings.log profile-early.pb.gz profile-late.pb.gz; do
  for file in $(sudo -n -u cbe-server find "$state" -name "$name" 2>/dev/null); do sudo -n cat "$file" > "$out/$name"; done
done
echo "== results in $out ($(date +%T))"
exit "$status"
