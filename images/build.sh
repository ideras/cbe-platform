#!/usr/bin/env bash
# Build a course image with the agent in it.
#
#   images/build.sh cpp20             build cbe/cpp20:latest
#   images/build.sh cpp20 --check     build it twice from nothing and say whether the two images are the same
#
# The container host's images belong to the cbe-runner user, so on that host:
#
#   PODMAN='sudo -u cbe-runner env XDG_RUNTIME_DIR=/run/user/1001 podman' images/build.sh cpp20
#
# The agent is built here (static, no cgo, no paths, no build ID) and handed to the build as a file, so the image holds exactly
# the binary the tests ran. SOURCE_DATE_EPOCH (default 2000-01-01) stamps every file and the image itself.
set -euo pipefail

course=${1:?usage: images/build.sh COURSE [--check]}
mode=${2:-}
here=$(cd "$(dirname "$0")" && pwd)
repo=$(dirname "$here")
[ -f "$here/$course/Containerfile" ] || { echo "no $here/$course/Containerfile" >&2; exit 1; }
podman=${PODMAN:-podman}
epoch=${SOURCE_DATE_EPOCH:-946684800}
case $(uname -m) in x86_64) goarch=amd64 ;; aarch64|arm64) goarch=arm64 ;; *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;; esac
version=${AGENT_VERSION:-$(git -C "$repo" describe --tags --always --dirty 2>/dev/null || echo dev)}

context=$(mktemp -d)
trap 'rm -rf "$context"' EXIT
cp "$here/$course/Containerfile" "$context/"
for extra in "$here/$course"/*; do
  case $(basename "$extra") in Containerfile|test*) ;; *) cp -r "$extra" "$context/" ;; esac
done
(cd "$repo" && CGO_ENABLED=0 GOOS=linux GOARCH=$goarch go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X main.version=$version" -o "$context/cbe-agent" ./cmd/cbe-agent)
cp /etc/ssl/certs/ca-certificates.crt "$context/ca.crt"
chmod -R a+rX "$context"
chmod a+rx "$context"
agent_sha=$(sha256sum "$context/cbe-agent" | cut -d' ' -f1)
echo "agent: $version, sha256 $agent_sha"

build() { # tag, extra options
  local tag=$1; shift
  # shellcheck disable=SC2086
  $podman build "$@" --timestamp "$epoch" --build-arg SOURCE_DATE_EPOCH="$epoch" --build-arg AGENT_SHA256="$agent_sha" \
    --build-arg AGENT_VERSION="$version" -t "$tag" -f "$context/Containerfile" "$context"
}

if [ "$mode" != "--check" ]; then
  build "cbe/$course:latest" --layers
  $podman image inspect --format 'image {{.Id}}' "cbe/$course:latest"
  exit 0
fi

# Two builds from nothing. The images are the same when their IDs are, which means every layer is.
build "localhost/cbe-check-$course:a" --no-cache
build "localhost/cbe-check-$course:b" --no-cache
id_a=$($podman image inspect --format '{{.Id}}' "localhost/cbe-check-$course:a")
id_b=$($podman image inspect --format '{{.Id}}' "localhost/cbe-check-$course:b")
echo "build a: $id_a"
echo "build b: $id_b"
if [ "$id_a" = "$id_b" ]; then
  echo "REPRODUCIBLE: two builds from nothing gave the same image"
  $podman tag "localhost/cbe-check-$course:a" "cbe/$course:latest"
  exit 0
fi
echo "NOT REPRODUCIBLE: the layers differ:" >&2
diff <($podman image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "localhost/cbe-check-$course:a") \
     <($podman image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "localhost/cbe-check-$course:b") >&2 || true
exit 1
