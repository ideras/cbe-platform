#!/usr/bin/env bash
# The real test of `cbe-portalctl image install` and `image list` (decision D-CB-7), end to end against the container runtime of this host:
#
#   1. a class image is built by the runner account's own Podman (the author's Containerfile, the runner's finishing layer, the verification and the self test),
#      named localhost/cbe-course/<name>:<id> and :latest, labelled, with the check's own names taken off and nothing else removed;
#   2. its name is approved in the runner's settings, every other setting kept; the same name again needs --replace and then needs no change to the settings;
#   3. an image that is not accepted is not named and changes nothing;
#   4. a PRIVATE runner that approves the name makes a real container of it by the course-image path (internal/runner/courseimage_real_test.go): the student,
#      the agent, a read-only root, no capabilities, and what the class image holds.
#
# It installs into a scratch INSTALL_DIR (the program is run from its bin/, so that is where it looks for its settings): the installed server's settings and
# services are not touched, and nothing is restarted. Whatever images the run made are removed at the end, by ID, against what was there before.
#
#   deploy/workspace/test-imageinstall.sh        uses alpine:3.20, which the host has: no network
#
# Needs deploy/workspace/install.sh to have been run (the accounts, the quota helper, the folders). It uses sudo.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
BASE=/data/cbe-platform
NAME=it-$$
work=$(mktemp -d)
as_runner() { sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" "$@"; }
images_before=$(as_runner podman images -a --format '{{.ID}}' | sort)
# shellcheck source=private-runner.sh
. "$repo/deploy/workspace/private-runner.sh"
cleanup() {
  private_runner_stop || true
  for id in $(as_runner podman images -a --format '{{.ID}}' | sort | comm -13 <(echo "$images_before") -); do as_runner podman rmi -f "$id" >/dev/null 2>&1 || true; done
  sudo -n rm -rf "$work"
}
trap cleanup EXIT
fail() { echo "FAILED: $*" >&2; exit 1; }

inst="$work/inst"
mkdir -p "$inst/bin" "$inst/etc"
(cd "$repo" && go build -o "$inst/bin/cbe-portalctl" ./cmd/cbe-portalctl && CGO_ENABLED=0 go build -o "$inst/bin/cbe-agent" ./cmd/cbe-agent)
cat > "$inst/etc/cbe-portal.conf" <<EOF
INSTALL_DIR=$inst
WORKSPACE_ENABLED=yes
RUNNER_USER=$RUNNER
EOF
echo '{"images": {"cbe/other": "localhost/cbe/other:latest"}, "max": {"cpu": 4, "memory_mb": 4096, "disk_mb": 800}}' > "$inst/etc/runner.json"
ctl() { sudo -n "$inst/bin/cbe-portalctl" "$@" 2>&1; }

pkg="$work/class"
mkdir -p "$pkg/image"
printf 'FROM docker.io/library/alpine:3.20\nCOPY from-the-class-image.txt /srv/from-the-class-image.txt\n' > "$pkg/image/Containerfile"
echo "in the class image" > "$pkg/image/from-the-class-image.txt"
bad="$work/bad"
mkdir -p "$bad/image"
printf 'FROM docker.io/library/alpine:3.20\nVOLUME /data\n' > "$bad/image/Containerfile"
chmod -R a+rX "$work"; chmod a+rx "$work"

echo "== install: the runner's own Podman builds it, names it, approves its name"
out=$(ctl image install "$NAME" "$pkg") || { echo "$out"; fail "the install failed"; }
echo "$out" | grep -q "Installed $NAME: localhost/cbe-course/$NAME:latest" || { echo "$out"; fail "no 'Installed'"; }
echo "$out" | grep -q 'cbe-portalctl restart' || { echo "$out"; fail "a new name was not said to need a restart"; }
python3 - "$inst/etc/runner.json" "$NAME" <<'PY' || fail "the runner's settings"
import json, sys
cfg = json.load(open(sys.argv[1]))
assert cfg["images"][sys.argv[2]] == f"localhost/cbe-course/{sys.argv[2]}:latest", cfg
assert cfg["images"]["cbe/other"] == "localhost/cbe/other:latest", "an approved image was lost"
assert cfg["max"] == {"cpu": 4, "memory_mb": 4096, "disk_mb": 800}, "a setting was lost"
PY
names=$(as_runner podman images --format '{{.Repository}}:{{.Tag}}' | grep "^localhost/cbe-course/$NAME:" | sort)
[ "$(echo "$names" | wc -l)" = 2 ] && echo "$names" | grep -q ":latest$" || { echo "$names"; fail "the image should have two names: its id and latest"; }
repos=$(as_runner podman images --format '{{.Repository}}')
if grep -q '^localhost/cbe-check/' <<<"$repos"; then fail "the check's own names were left"; fi
label=$(as_runner podman image inspect --format '{{index .Labels "io.cbe.course"}}' "localhost/cbe-course/$NAME:latest")
[ "$label" = "$NAME" ] || fail "the label is '$label'"
echo "ok"

echo "== list"
out=$(ctl image list) || true # it ends 1 on purpose when an approved image is missing, and cbe/other is
echo "$out" | grep -q "^$NAME  *localhost/cbe-course/$NAME:latest  present, installed by image install" || { echo "$out"; fail "list does not show it as installed"; }
echo "$out" | grep -q '^cbe/other  .*MISSING' || { echo "$out"; fail "list does not say that an approved image is missing"; }
echo "ok"

echo "== the same name again needs --replace, and then changes no setting"
if out=$(ctl image install "$NAME" "$pkg"); then echo "$out"; fail "an installed name was built again without --replace"; fi
echo "$out" | grep -q 'is approved already' || { echo "$out"; fail "no explanation"; }
cp "$inst/etc/runner.json" "$work/runner.before"
first=$(as_runner podman image inspect --format '{{.Id}}' "localhost/cbe-course/$NAME:latest")
out=$(ctl image install "$NAME" "$pkg" --replace) || { echo "$out"; fail "--replace failed"; }
echo "$out" | grep -q 'no restart is needed' || { echo "$out"; fail "a re-install should say it needs no restart"; }
cmp -s "$inst/etc/runner.json" "$work/runner.before" || fail "the settings were changed by a re-install"
as_runner podman image inspect "$first" >/dev/null || fail "the first image was removed by the re-install"
echo "ok"

echo "== an image that is not accepted is not named and changes nothing"
cp "$inst/etc/runner.json" "$work/runner.before"
if out=$(ctl image install "bad-$$" "$bad"); then echo "$out"; fail "a VOLUME was installed"; fi
echo "$out" | grep -q 'NOT INSTALLED: the Containerfile is not accepted: line 2: VOLUME is not allowed' || { echo "$out"; fail "the rule was not said"; }
cmp -s "$inst/etc/runner.json" "$work/runner.before" || fail "the settings were changed by a refusal"
repos=$(as_runner podman images --format '{{.Repository}}')
if grep -q "bad-$$" <<<"$repos"; then fail "a refused image was named"; fi
echo "ok"

echo "== a runner that approves the name runs a real container of it (the course-image path)"
export PR_IMAGES="$NAME=localhost/cbe-course/$NAME:latest"
(cd "$repo" && go test -c -o "$work/runner.test" ./internal/runner)
mkdir -p "$work/internal/runner" "$work/private"; sudo -n chmod -R a+rX "$work"  # (the install, run as root, wrote some of what is in it)
private_runner_start "$work/private"
echo "the private runner is up ($PR_TAG)"
sudo -n -u cbe-server env CBE_RUNNER_SOCKET="$PR_SOCKET" CBE_RUNNER_SECRET_FILE="$PR_SECRET" CBE_COURSE_IMAGE="$NAME" \
  bash -c "cd '$work/internal/runner' && ../../runner.test -test.run '^TestRealInstalledCourseImage\$' -test.v -test.timeout 10m" | tee "$work/course.log" | grep -E '^(--- |ok|PASS|FAIL)'
grep -q -- '--- PASS: TestRealInstalledCourseImage' "$work/course.log" || fail "the course-image path did not run the installed image"
echo "ok"

echo "== what the run left"
left=$(runtime ps -a --format '{{.Names}}' | sort | comm -13 <(echo "$PR_BEFORE") -)
[ -z "$left" ] && echo "no containers left" || { echo "containers left: $left"; exit 1; }
