#!/usr/bin/env bash
# Undo setup-host.sh. Run as root: sudo docs/workspace/spike/teardown-host.sh [--purge]
#
# Without --purge it stops the runner's containers and Podman socket, removes the spike backstop and the quota helper,
# and leaves the user, the XFS image and its fstab line alone. With --purge it also deletes the user (and its home with
# every image and container), unmounts the volume, deletes the image file and restores /etc/fstab.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root: sudo $0" >&2; exit 1; }

RUNNER=cbe-runner
BASE=/data/cbe-platform
RUNNER_UID=$(id -u "$RUNNER")
RUNTIME_DIR=/run/user/$RUNNER_UID

runuser -u "$RUNNER" -- env XDG_RUNTIME_DIR="$RUNTIME_DIR" DBUS_SESSION_BUS_ADDRESS="unix:path=$RUNTIME_DIR/bus" \
  sh -c 'podman rm -f --all >/dev/null 2>&1; systemctl --user disable --now podman.socket' || true
rm -f /usr/local/sbin/cbe-quota-helper
rm -rf /etc/systemd/system/user-"$RUNNER_UID".slice.d
systemctl daemon-reload

if [ "${1:-}" = "--purge" ]; then
  loginctl disable-linger "$RUNNER" || true
  loginctl terminate-user "$RUNNER" || true
  rm -rf /etc/systemd/system/user@"$RUNNER_UID".service.d
  systemctl daemon-reload
  umount "$BASE/work" || true
  [ -f /etc/fstab.bak-cbe ] && cp /etc/fstab.bak-cbe /etc/fstab && rm /etc/fstab.bak-cbe
  rm -f "$BASE/work.img" "$BASE/projects.next" "$BASE/projects.lock"
  rmdir "$BASE/work" "$BASE" 2>/dev/null || true
  userdel --remove "$RUNNER"
  echo "purged"
else
  echo "backstop and helper removed; user, volume and delegation kept (use --purge to remove them)"
fi
