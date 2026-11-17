#!/usr/bin/env bash
# The real test of `cbe-portalctl image check` (internal/imagecheck): the author's validator, run against the real container runtime of this host, as the
# cbe-runner account (the owner's own Podman storage is not touched: Podman shares a lock file per user, even with another --root). The images it makes are
# removed by the command itself, and whatever else the run left (a pulled base) is removed at the end, by ID, against what was there before.
#
#   deploy/workspace/test-imagecheck.sh                   a package on alpine:3.20 (on the host already), the rules, the server's registries: no network
#   CBE_IMAGECHECK_NET=1 deploy/workspace/test-imagecheck.sh   also a Redis base, which is refused for its volume (pulls redis:7-alpine)
#
# Needs deploy/workspace/install.sh to have been run (the cbe-runner account). It uses sudo.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
work=$(mktemp -d)
as_runner() { sudo -n -u "$RUNNER" env HOME="/home/$RUNNER" XDG_RUNTIME_DIR="/run/user/$RUID" "$@"; }
images_before=$(as_runner podman images -a --format '{{.ID}}' | sort)
cleanup() {
  for id in $(as_runner podman images -a --format '{{.ID}}' | sort | comm -13 <(echo "$images_before") -); do as_runner podman rmi -f "$id" >/dev/null 2>&1 || true; done
  rm -rf "$work"
}
trap cleanup EXIT
fail() { echo "FAILED: $*" >&2; exit 1; }

mkdir -p "$work/bin"
(cd "$repo" && go build -o "$work/bin/cbe-portalctl" ./cmd/cbe-portalctl && CGO_ENABLED=0 go build -o "$work/bin/cbe-agent" ./cmd/cbe-agent)
package() { # NAME CONTAINERFILE
  mkdir -p "$work/$1/image"; printf '%b' "$2" > "$work/$1/image/Containerfile"; echo "the starter" > "$work/$1/image/hello.txt"
}
package alpine 'FROM docker.io/library/alpine:3.20\nCOPY hello.txt /srv/hello.txt\n'
package volume 'FROM docker.io/library/alpine:3.20\nVOLUME /data\n'
package redis 'FROM docker.io/library/redis:7-alpine\n'
echo '{}' > "$work/any.json"
echo '{"base_registries": ["ghcr.io"]}' > "$work/ghcr-only.json"
echo '{"custom_base": false}' > "$work/no-custom.json"
chmod -R a+rX "$work"; chmod a+rx "$work"
check() { # SETTINGS PACKAGE [OPTIONS]
  local settings=$1 package=$2; shift 2
  (cd "$work" && as_runner "$work/bin/cbe-portalctl" image check "$work/$package" --agent "$work/bin/cbe-agent" --runner-config "$work/$settings" "$@") 2>&1
}

echo "== a package on a base the host has"
out=$(check any.json alpine) || { echo "$out"; fail "a good package was not accepted"; }
echo "$out" | grep -q '^OK: the image keeps the contract' || { echo "$out"; fail "no OK"; }
for name in user account shell work tmp root-read-only capabilities no-new-privileges network no-sudo no-setuid-files; do
  echo "$out" | grep -q "self test   ok    $name\$" || { echo "$out"; fail "the self test did not pass $name"; }
done
echo "ok"

echo "== the Containerfile's rules are checked first"
if out=$(check any.json volume); then echo "$out"; fail "a VOLUME was accepted"; fi
echo "$out" | grep -q 'NOT ACCEPTED: the Containerfile is not accepted: line 2: VOLUME is not allowed' || { echo "$out"; fail "the rule was not said"; }
echo "ok"

echo "== the server's decisions"
if out=$(check ghcr-only.json alpine); then echo "$out"; fail "a registry that is not allowed was accepted"; fi
echo "$out" | grep -q 'does not take images from docker.io; it allows ghcr.io' || { echo "$out"; fail "the registry rule was not said"; }
if out=$(check no-custom.json alpine); then echo "$out"; fail "custom_base off was ignored"; fi
echo "$out" | grep -q 'custom_base is off' || { echo "$out"; fail "custom_base off was not said"; }
echo "ok"

if [ -n "${CBE_IMAGECHECK_NET:-}" ]; then
  echo "== a base that declares a volume (Redis) is refused"
  if out=$(check any.json redis); then echo "$out"; fail "a Redis base was accepted"; fi
  echo "$out" | grep -q 'NOT ACCEPTED: the image declares the volume(s) /data' || { echo "$out"; fail "the volume was not said"; }
  echo "ok"
fi

echo "== what the run left"
left=$(as_runner podman images -a --format '{{.Repository}}' | grep -c '^localhost/cbe-check/' || true)
[ "$left" = 0 ] && echo "no images of the check left" || fail "$left images of the check were left"
