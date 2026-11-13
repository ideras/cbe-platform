#!/usr/bin/env bash
# The real-reboot check for Phase 0 (DECISIONS P0-12), in two steps around ONE reboot of this machine.
# Run it as your normal user; it calls sudo. It never reboots anything: you do that.
#
#   docs/workspace/spike/reboot-check.sh prep      BEFORE the reboot: writes down the boot ID and the container's start count,
#                                                  and starts a container with a restart policy that writes a counter to /work
#   (reboot this machine yourself, log in, wait a minute)
#   docs/workspace/spike/reboot-check.sh verify    AFTER the reboot: checks the machine really rebooted, the XFS volume is back
#                                                  with its quotas, cbe-runner's user manager is up with nobody logged in as
#                                                  cbe-runner, and the container came back by itself with its files
#
# Do not use run-spike.sh for the second step: it recreates the session folders first, which destroys the evidence.
set -euo pipefail

RUNNER=cbe-runner
RUNNER_UID=$(id -u "$RUNNER")
HOME_R=/home/$RUNNER
BIN=$HOME_R/spike/bin/spike
STATE=$HOME_R/spike/reboot-before
SOCK=unix:///run/user/$RUNNER_UID/podman/podman.sock
WORK=/data/cbe-platform/work
SESSION=spike-reboot

as_runner() {
  sudo -n -u "$RUNNER" env HOME="$HOME_R" XDG_RUNTIME_DIR=/run/user/"$RUNNER_UID" \
    DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/"$RUNNER_UID"/bus \
    sh -c 'cd "$HOME" && exec "$@"' sh "$@"
}
starts_now() { sudo -n cat "$WORK/$SESSION/tick.starts" 2>/dev/null | grep -c start || true; }
ok()   { printf '  PASS  %s\n' "$*"; }
bad()  { printf '  FAIL  %s\n' "$*"; failed=1; }
failed=0

case "${1:-}" in
  prep)
    findmnt -n -t xfs -O prjquota "$WORK" >/dev/null || { echo "$WORK is not mounted as XFS with prjquota; stop." >&2; exit 1; }
    [ -x "$BIN" ] || sudo -n test -x "$BIN" || { echo "$BIN is missing: run docs/workspace/spike/run-spike.sh once to build it." >&2; exit 1; }
    sudo -n test -d "$WORK/$SESSION" || sudo -n env CBE_FOLDER_MODE=0755 cbe-quota-helper create "$SESSION" 20 20000
    as_runner "$BIN" check -sock "$SOCK" -only reboot-prep -out "$HOME_R/spike/reboot-prep.json"
    # Written after the container has started, so the count is the one the reboot has to exceed.
    printf 'boot_id=%s\nstarts=%s\nwhen=%s\n' "$(cat /proc/sys/kernel/random/boot_id)" "$(starts_now)" "$(date -Is)" | sudo -n -u "$RUNNER" tee "$STATE" >/dev/null
    echo
    echo "Recorded in $STATE:"; sudo -n cat "$STATE"
    echo
    echo "Ready. Reboot this machine when convenient, log in, wait about a minute, then run:"
    echo "  docs/workspace/spike/reboot-check.sh verify"
    ;;
  verify)
    sudo -n test -f "$STATE" || { echo "No $STATE: run 'prep' before the reboot." >&2; exit 1; }
    before_boot=$(sudo -n sed -n 's/^boot_id=//p' "$STATE"); before_starts=$(sudo -n sed -n 's/^starts=//p' "$STATE")
    echo "Booted: $(uptime -s)   Now: $(date -Is)"
    echo "== the checks =="
    [ "$(cat /proc/sys/kernel/random/boot_id)" != "$before_boot" ] && ok "the machine rebooted since 'prep' (boot ID changed)" || bad "same boot ID as at 'prep': no reboot happened"
    findmnt -n -t xfs -O prjquota "$WORK" >/dev/null && ok "$WORK is mounted by itself, XFS with prjquota ($(findmnt -n -o SOURCE "$WORK"))" || bad "$WORK is not mounted as XFS with prjquota"
    systemctl is-active --quiet "user@$RUNNER_UID.service" && ok "user@$RUNNER_UID.service is active (lingering)" || bad "user@$RUNNER_UID.service is not active"
    sessions=$(loginctl list-sessions --no-legend | awk -v u="$RUNNER" '$3==u' | wc -l)
    [ "$sessions" -eq 0 ] && ok "no login session for $RUNNER: the container survived without one" || bad "$RUNNER has $sessions login session(s): this does not prove survival without a login"
    as_runner "$BIN" check -sock "$SOCK" -only reboot-verify -out "$HOME_R/spike/reboot-verify.json" && ok "the container is running again (the spike's own check passed)" || bad "the spike's reboot-verify failed (see above)"
    after_starts=$(starts_now)
    [ "$after_starts" -gt "$before_starts" ] && ok "its process started again after the reboot (start count $before_starts -> $after_starts) and /work survived" || bad "start count did not grow ($before_starts -> $after_starts)"
    echo
    if [ "$failed" -eq 0 ]; then echo "REAL REBOOT CHECK: PASS"; else echo "REAL REBOOT CHECK: FAIL"; exit 1; fi
    ;;
  *)
    echo "usage: $0 prep|verify" >&2; exit 2 ;;
esac
