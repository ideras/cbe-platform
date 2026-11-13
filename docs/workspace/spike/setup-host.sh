#!/usr/bin/env bash
# Phase 0 spike: prepare this host for rootless Podman under a dedicated "cbe-runner" user.
#
# Run once, as root:  sudo docs/workspace/spike/setup-host.sh
# It is idempotent. Undo it with teardown-host.sh in this folder.
#
# What it sets up (plan section 7, Phase 0, task 2):
#   1. the cbe-runner system user, with lingering, so its containers outlive any login;
#   2. systemd cgroup v2 delegation of cpu, cpuset, io, memory and pids to that user;
#   3. a SPIKE-ONLY backstop on the user's slice, so a limit that fails to hold cannot take the whole
#      machine down while hostile workloads (fork bomb, memory hog, CPU loop) are being tested;
#   4. a 16 GiB sparse image file, formatted XFS, loop-mounted with project quotas under /data/cbe-platform;
#   5. the quota helper prototype (see quota-helper.sh);
#   6. the rootless Podman API socket (podman.socket) for cbe-runner.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run as root: sudo $0" >&2; exit 1; }

RUNNER=cbe-runner
BASE=/data/cbe-platform
IMG=$BASE/work.img
WORK=$BASE/work
SIZE=16G
HERE=$(cd "$(dirname "$0")" && pwd)

echo "== 1. user =="
if ! id "$RUNNER" >/dev/null 2>&1; then
  useradd --create-home --shell /bin/bash --comment "CBE runner (container host)" "$RUNNER"
fi
# useradd allocates subordinate ID ranges from /etc/login.defs; check that it did.
grep -q "^$RUNNER:" /etc/subuid || { echo "no /etc/subuid entry for $RUNNER" >&2; exit 1; }
grep -q "^$RUNNER:" /etc/subgid || { echo "no /etc/subgid entry for $RUNNER" >&2; exit 1; }
RUNNER_UID=$(id -u "$RUNNER")
echo "$RUNNER uid=$RUNNER_UID subuid=$(grep "^$RUNNER:" /etc/subuid) subgid=$(grep "^$RUNNER:" /etc/subgid)"

echo "== 2. cgroup delegation =="
install -d /etc/systemd/system/user@"$RUNNER_UID".service.d
cat > /etc/systemd/system/user@"$RUNNER_UID".service.d/delegate.conf <<EOF
# Phase 0 spike: rootless Podman needs these controllers to enforce container limits.
[Unit]
# Fail closed: without the XFS volume a container would bind-mount the EMPTY folder underneath it, and a student
# would see an empty /work after a reboot. The user manager (and so every container) waits for the mount instead.
RequiresMountsFor=$WORK

[Service]
Delegate=cpu cpuset io memory pids
EOF

echo "== 3. spike-only backstop on the user's slice =="
install -d /etc/systemd/system/user-"$RUNNER_UID".slice.d
cat > /etc/systemd/system/user-"$RUNNER_UID".slice.d/spike-backstop.conf <<'EOF'
# SPIKE ONLY. Caps everything cbe-runner runs, in case a per-container limit does not hold.
# Remove before production: 40 students at 256 processes each need far more than this.
[Slice]
TasksMax=6000
MemoryMax=16G
MemorySwapMax=0
CPUQuota=400%
EOF
systemctl daemon-reload
loginctl enable-linger "$RUNNER"
# Restart the user manager so it picks the delegation up (nothing of cbe-runner is running yet).
systemctl restart user@"$RUNNER_UID".service
sleep 1
echo "controllers delegated to user@$RUNNER_UID.service: $(cat /sys/fs/cgroup/user.slice/user-"$RUNNER_UID".slice/user@"$RUNNER_UID".service/cgroup.controllers)"

echo "== 4. XFS work volume =="
install -d -m 0755 "$BASE"
if [ ! -e "$IMG" ]; then
  touch "$IMG"
  chattr +C "$IMG" || true        # /data is btrfs: no copy-on-write for a loop image (must be set while empty)
  truncate -s "$SIZE" "$IMG"
  mkfs.xfs -q -L cbework "$IMG"
fi
chmod 0600 "$IMG"                 # the raw image holds every student's files: root only
install -d -m 0711 "$WORK"
mountpoint -q "$WORK" || mount -o loop,prjquota "$IMG" "$WORK"
chmod 0711 "$WORK"
if ! grep -qs "^$IMG " /etc/fstab; then
  cp /etc/fstab /etc/fstab.bak-cbe
  echo "$IMG $WORK xfs loop,prjquota,nofail,x-systemd.requires-mounts-for=$BASE 0 0" >> /etc/fstab
fi
findmnt --verify --tab-file /etc/fstab >/dev/null 2>&1 || echo "WARNING: findmnt --verify reports problems in /etc/fstab" >&2
findmnt -no SOURCE,FSTYPE,OPTIONS "$WORK"

echo "== 5. quota helper prototype =="
install -m 0755 -o root -g root "$HERE/quota-helper.sh" /usr/local/sbin/cbe-quota-helper

echo "== 6. rootless Podman API socket =="
RUNTIME_DIR=/run/user/$RUNNER_UID
runuser -u "$RUNNER" -- env XDG_RUNTIME_DIR="$RUNTIME_DIR" DBUS_SESSION_BUS_ADDRESS="unix:path=$RUNTIME_DIR/bus" \
  systemctl --user enable --now podman.socket
# After a reboot the user manager starts (lingering) and this unit restarts the containers whose restart policy is "always".
runuser -u "$RUNNER" -- env XDG_RUNTIME_DIR="$RUNTIME_DIR" DBUS_SESSION_BUS_ADDRESS="unix:path=$RUNTIME_DIR/bus" \
  systemctl --user enable podman-restart.service
ls -l "$RUNTIME_DIR/podman/podman.sock"
echo "done"
