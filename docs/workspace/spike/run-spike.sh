#!/usr/bin/env bash
# Run the Phase 0 spike end to end. Needs setup-host.sh to have been run first. Run as your normal user: it calls sudo.
#
#   docs/workspace/spike/run-spike.sh            all checks, then the reboot simulation
#   docs/workspace/spike/run-spike.sh pids,disk  only these checks (no reboot simulation)
#
# Results land in $RESULTS (JSON) and on the terminal.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
RUNNER=cbe-runner
RUNNER_UID=$(id -u "$RUNNER")
HOME_R=/home/$RUNNER
BIN=$HOME_R/spike/bin
RESULTS=$HOME_R/spike/results.json
SOCK=unix:///run/user/$RUNNER_UID/podman/podman.sock
ONLY=${1:-}

as_runner() {
  sudo -n -u "$RUNNER" env HOME="$HOME_R" XDG_RUNTIME_DIR=/run/user/"$RUNNER_UID" \
    DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/"$RUNNER_UID"/bus \
    sh -c 'cd "$HOME" && exec "$@"' sh "$@"
}

echo "== build =="
OUT=$(mktemp -d)
(cd "$HERE" && CGO_ENABLED=0 go build -trimpath -o "$OUT/spike" .)
sudo install -d -o "$RUNNER" -g "$RUNNER" -m 0755 "$HOME_R/spike" "$BIN" "$HOME_R/spike/run"
sudo install -o "$RUNNER" -g "$RUNNER" -m 0755 "$OUT/spike" "$BIN/spike"
rm -rf "$OUT"

echo "== quota folders =="
make_session() { # name, size MiB, max files
  sudo cbe-quota-helper remove "$1" >/dev/null 2>&1 || true
  sudo env CBE_FOLDER_MODE=0755 cbe-quota-helper create "$1" "$2" "$3"   # 0755: this harness reads the folders from the host
}
make_session spike-fill 50 20000
make_session spike-ino 50 2000
make_session spike-misc 20 20000
make_session spike-arch 20 20000
make_session spike-reboot 20 20000

echo "== checks =="
status=0
as_runner "$BIN/spike" check -sock "$SOCK" -out "$RESULTS" ${ONLY:+-only "$ONLY"} || status=$?

echo "== quota usage after the run =="
sudo cbe-quota-helper report || true

if [ -z "$ONLY" ]; then
  echo "== reboot simulation: restart the user manager, as a boot would =="
  as_runner "$BIN/spike" check -sock "$SOCK" -only reboot-prep -out "$HOME_R/spike/reboot-prep.json" || status=$?
  sudo systemctl restart user@"$RUNNER_UID".service
  sleep 5
  as_runner "$BIN/spike" check -sock "$SOCK" -only reboot-verify -out "$HOME_R/spike/reboot-verify.json" || status=$?
fi
exit $status
