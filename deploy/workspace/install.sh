#!/usr/bin/env bash
# Install the workspace-exam services on the container host: accounts, folders, the quota helper (root) and the runner (the cbe-runner
# user's systemd unit). Idempotent. Run as root, after docs/workspace/spike/setup-host.sh (the cbe-runner user, rootless Podman, the XFS
# volume), with the binaries built:
#
#   sudo deploy/workspace/install.sh BIN_DIR       BIN_DIR holds cbe-runner and cbe-quota-helper
#
# docs/workspace/deployment.md explains what it makes and why.
set -euo pipefail

BIN=${1:?usage: install.sh BIN_DIR}
HERE=$(cd "$(dirname "$0")" && pwd)
RUNNER=cbe-runner
SERVER=cbe-server
GROUP=cbe-ipc
BASE=/data/cbe-platform
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
id "$RUNNER" >/dev/null || { echo "the $RUNNER user does not exist: run docs/workspace/spike/setup-host.sh first" >&2; exit 1; }
RUNNER_UID=$(id -u "$RUNNER")

echo "== accounts"
getent group "$GROUP" >/dev/null || groupadd --system "$GROUP"
id "$SERVER" >/dev/null 2>&1 || useradd --system --no-create-home --home-dir "$BASE/server" --shell /usr/sbin/nologin --comment "CBE exam server" "$SERVER"
usermod -aG "$GROUP" "$RUNNER"
usermod -aG "$GROUP" "$SERVER"

echo "== folders"
# ipc: the runner's socket and its secret; the exam server's account reaches them through the group, and cannot list the folder.
install -d -o "$RUNNER" -g "$GROUP" -m 2750 "$BASE/ipc"
# run: the agents' socket folders (one per session, open to the student) live under a folder only the runner can enter.
install -d -o "$RUNNER" -g "$RUNNER" -m 0700 "$BASE/run" "$BASE/runner"
# server: the exam server's own data, in its own account.
install -d -o "$SERVER" -g "$SERVER" -m 0700 "$BASE/server"

echo "== binaries"
install -m 0755 -o root -g root "$BIN/cbe-quota-helper" /usr/local/sbin/cbe-quota-helperd
install -m 0755 -o root -g root "$BIN/cbe-runner" /usr/local/bin/cbe-runner
install -d -m 0755 /etc/cbe-platform
[ -f /etc/cbe-platform/runner.json ] || install -m 0644 "$HERE/runner.json" /etc/cbe-platform/runner.json

echo "== the quota helper (system service)"
install -m 0644 "$HERE/cbe-quota-helper.service" /etc/systemd/system/cbe-quota-helper.service
systemctl daemon-reload
systemctl enable cbe-quota-helper.service >/dev/null 2>&1
systemctl restart cbe-quota-helper.service

echo "== the runner (the $RUNNER user's service, with lingering)"
loginctl enable-linger "$RUNNER"
install -d -o "$RUNNER" -g "$RUNNER" -m 0755 "/home/$RUNNER/.config/systemd/user"
install -m 0644 -o "$RUNNER" -g "$RUNNER" "$HERE/cbe-runner.service" "/home/$RUNNER/.config/systemd/user/cbe-runner.service"
user_systemctl() { sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUNNER_UID" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$RUNNER_UID/bus" systemctl --user "$@"; }
user_systemctl daemon-reload
user_systemctl enable cbe-runner.service >/dev/null 2>&1
user_systemctl restart cbe-runner.service
sleep 1
user_systemctl is-active cbe-runner.service
systemctl is-active cbe-quota-helper.service
echo "done"
