#!/bin/bash
# Builds the programs of the CBE Portal server for Linux and the installer that holds them (make_installer.sh), the way CI does:
#
#   scripts/installer/linux/build.sh [--version V] [--arch amd64|arm64] [--output DIR]
#
# Defaults: the version git says (the tag, or the commit after it), this machine's architecture, and build/installer. Needs Go and makeself.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
version=${CBE_VERSION:-}
arch=
output="$repo/build/installer"
while [ $# -gt 0 ]; do
  case $1 in
    --version) version=${2:?}; shift 2 ;;
    --arch) arch=${2:?}; shift 2 ;;
    --output) output=${2:?}; shift 2 ;;
    *) echo "build.sh: $1 is not an option" >&2; exit 2 ;;
  esac
done
if [ -z "$version" ]; then
  version=$(git -C "$repo" describe --tags --always --dirty 2>/dev/null || echo dev)
fi
if [ -z "$arch" ]; then
  case $(uname -m) in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) echo "unsupported machine $(uname -m)" >&2; exit 1 ;; esac
fi

bins=$(mktemp -d)
trap 'rm -rf "$bins"' EXIT
export CGO_ENABLED=0 GOOS=linux GOARCH=$arch
flags=(-trimpath -ldflags "-X main.version=$version")
echo "Building the programs ($version, linux/$arch) ..."
(cd "$repo" && go build "${flags[@]}" -o "$bins/cbe-portal-server" ./cmd/server)
(cd "$repo" && go build "${flags[@]}" -o "$bins/cbe-runner" ./cmd/cbe-runner)
(cd "$repo" && go build "${flags[@]}" -o "$bins/cbe-quota-helperd" ./cmd/cbe-quota-helper)
(cd "$repo" && go build "${flags[@]}" -o "$bins/cbe-portalctl" ./cmd/cbe-portalctl)
# The agent the runner puts in the images of exams that bring a base of their own (runner.json agent_binary): a static program, built the way images/build.sh builds it.
(cd "$repo" && go build -trimpath -ldflags "-s -w -buildid= -X main.version=$version" -o "$bins/cbe-agent" ./cmd/cbe-agent)
(cd "$repo/shared/examdoc" && go build -trimpath -o "$bins/examc" ./cmd/examc)
"$here/make_installer.sh" --version "$version" --arch "$arch" --bin-dir "$bins" --output "$output"
