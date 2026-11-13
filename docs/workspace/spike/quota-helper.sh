#!/usr/bin/env bash
# PROTOTYPE of the plan's quota helper (section 4): the only root-owned piece under rootless Podman.
# Installed as /usr/local/sbin/cbe-quota-helper. It creates and removes size-limited folders for /work.
#
#   cbe-quota-helper create <session-id> <size-mb> [max-files]   make the folder, set its XFS project quota
#   cbe-quota-helper remove <session-id>                         delete the folder and release its quota
#   cbe-quota-helper report                                      print usage per project
#
# It accepts a session ID and sizes, nothing else: no paths, no owners. The production helper is a small
# service on a socket; this shell version only proves the quota mechanism works for the spike.
set -euo pipefail

BASE=/data/cbe-platform
WORK=$BASE/work
RUNNER=cbe-runner
CONTAINER_UID=1000      # the "student" user inside the image
CONTAINER_GID=1000

die() { echo "cbe-quota-helper: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "must run as root"
mountpoint -q "$WORK" || die "$WORK is not mounted"

valid_id() { [[ "$1" =~ ^[A-Za-z0-9_-]{1,64}$ ]]; }
valid_num() { [[ "$1" =~ ^[0-9]{1,7}$ ]] && [ "$1" -ge 1 ]; }

# A container user N (N >= 1) is host user (first subuid + N - 1) under rootless Podman.
mapped() { # <file> <id>
  local start
  start=$(awk -F: -v u="$RUNNER" '$1==u {print $2; exit}' "$1")
  [ -n "$start" ] || die "no $1 entry for $RUNNER"
  echo $(( start + $2 - 1 ))
}

next_project() {
  local counter=$BASE/projects.next
  exec 9>"$BASE/projects.lock"
  flock 9
  local n
  n=$(cat "$counter" 2>/dev/null || echo 100000)
  echo $(( n + 1 )) > "$counter"
  echo "$n"
}

case "${1:-}" in
  create)
    sid=${2:-}; size=${3:-}; files=${4:-20000}
    valid_id "$sid" || die "bad session id"
    valid_num "$size" || die "bad size"
    valid_num "$files" || die "bad file limit"
    [ "$size" -le 4096 ] || die "size over the 4096 MiB maximum"
    dir=$WORK/$sid
    [ ! -e "$dir" ] || die "$sid already exists"
    mkdir "$dir"
    project=$(next_project)
    xfs_quota -x -c "project -s -p $dir $project" "$WORK" >/dev/null
    xfs_quota -x -c "limit -p bhard=${size}m ihard=$files $project" "$WORK"
    chown "$(mapped /etc/subuid "$CONTAINER_UID"):$(mapped /etc/subgid "$CONTAINER_GID")" "$dir"
    # 0700 by default: only the container's own user (the mapped uid) can read the folder, so neither cbe-runner nor the
    # exam-server user can read a student's files from the host; snapshots go through the archive API instead (D14).
    # The spike's own checks read counters from the host, so run-spike.sh asks for 0755 with CBE_FOLDER_MODE.
    chmod "${CBE_FOLDER_MODE:-0700}" "$dir"
    echo "$dir project=$project bhard=${size}m ihard=$files"
    ;;
  remove)
    sid=${2:-}
    valid_id "$sid" || die "bad session id"
    dir=$WORK/$sid
    [ -d "$dir" ] || die "$sid does not exist"
    project=$(lsattr -dp "$dir" | awk '{print $1}')
    rm -rf --one-file-system "$dir"
    [ "$project" != 0 ] && xfs_quota -x -c "limit -p bhard=0 ihard=0 $project" "$WORK" || true
    echo "removed $sid"
    ;;
  report)
    xfs_quota -x -c "report -p -b -i -h" "$WORK"
    ;;
  *)
    die "usage: create <session-id> <size-mb> [max-files] | remove <session-id> | report"
    ;;
esac
