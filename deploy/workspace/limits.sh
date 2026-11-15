#!/usr/bin/env bash
# The limits of the control group the containers run in (the cbe-runner user's slice), for production: they replace the spike's backstop
# (docs/workspace/DECISIONS.md P0-11, P8-3). Idempotent. Run as root; install.sh runs it.
#
#   sudo deploy/workspace/limits.sh [MEMORY_BUDGET_MB]
#
# MEMORY_BUDGET_MB is what the runner's host_memory_mb says (default: the runner's /etc/cbe-platform/runner.json, or 70% of the machine's memory).
# What it sets, on user-<uid>.slice of the cbe-runner user (every container, the runner, the runtime and the image builds are in it):
#
#   CPUWeight=50     Under contention the exam server, the proctor application and the desktop of the machine's owner (weight 100) win two to one;
#                    an idle machine still gives the students every core. (The spike capped the slice at 400%, which made a class wait for
#                    cores the machine was not using, and kept nothing from a busy desktop.)
#   MemoryMax        The budget the runner keeps the containers' limits under, and room for what is not a student's container: two grading
#                    runs, one image build and the runtime. Over it the kernel's out-of-memory killer acts inside the slice, not on the machine.
#   TasksMax=12000   Forty containers at their 256 processes each, and the runtime.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
RUNNER=cbe-runner
RUNNER_UID=$(id -u "$RUNNER")
CONFIG=/etc/cbe-platform/runner.json
total_mb=$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo)
budget="${1:-}"
if [ -z "$budget" ] && [ -f "$CONFIG" ]; then
  budget=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("host_memory_mb", 0))' "$CONFIG")
fi
if [ -z "$budget" ] || [ "$budget" -eq 0 ]; then budget=$((total_mb * 7 / 10)); fi
# Room for what is not a student's container: two grading runs of the largest size, a build, the runtime and the runner.
max_mb=$((budget + 2 * 2048 + 2048 + 3072))
dropin=/etc/systemd/system/user-"$RUNNER_UID".slice.d
install -d "$dropin"
if [ -f "$dropin/spike-backstop.conf" ]; then
  install -d /etc/cbe-platform
  mv "$dropin/spike-backstop.conf" /etc/cbe-platform/spike-backstop.conf.removed
  echo "the spike's backstop is removed (kept as /etc/cbe-platform/spike-backstop.conf.removed)"
fi
cat > "$dropin/cbe-limits.conf" <<CONF
# Written by deploy/workspace/limits.sh. The runner keeps the containers' memory limits under host_memory_mb ($budget MiB).
[Slice]
CPUWeight=50
MemoryMax=${max_mb}M
MemorySwapMax=0
TasksMax=12000
CONF
systemctl daemon-reload
echo "limits of user-$RUNNER_UID.slice: $(systemctl show "user-$RUNNER_UID.slice" -p CPUWeight -p MemoryMax -p MemorySwapMax -p TasksMax | tr '\n' ' ')"
