#!/bin/bash
# Makes the self-extracting installer of the CBE Portal server for Linux, with makeself:  cbe-portal-VERSION-linux-x64.run (or -arm64)
#
#   scripts/installer/linux/make_installer.sh --version v0.17.0 --arch amd64 --bin-dir DIR --output DIR
#
# DIR holds the Linux programs built for ARCH: cbe-portal-server, cbe-runner, cbe-quota-helperd, cbe-portalctl and examc. What the installer holds is the
# layout of /opt/cbe-portal (bin, share) and the two scripts next to this one; install.sh and uninstall.sh are what it runs. See docs/deployment-linux.md.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
version=
arch=
bin_dir=
output=
while [ $# -gt 0 ]; do
  case $1 in
    --version) version=${2:?}; shift 2 ;;
    --arch) arch=${2:?}; shift 2 ;;
    --bin-dir) bin_dir=${2:?}; shift 2 ;;
    --output) output=${2:?}; shift 2 ;;
    *) echo "make_installer.sh: $1 is not an option" >&2; exit 2 ;;
  esac
done
[ -n "$version" ] && [ -n "$arch" ] && [ -n "$bin_dir" ] && [ -n "$output" ] || { echo "usage: $0 --version V --arch amd64|arm64 --bin-dir DIR --output DIR" >&2; exit 2; }
case $arch in amd64|arm64) ;; *) echo "--arch is amd64 or arm64" >&2; exit 2 ;; esac
[[ $version =~ ^[A-Za-z0-9._+-]{1,64}$ ]] || { echo "--version is not a version word: $version" >&2; exit 2; }

makeself=${MAKESELF:-}
if [ -z "$makeself" ]; then
  makeself=$(command -v makeself.sh || command -v makeself || true)
fi
[ -n "$makeself" ] && [ -x "$makeself" ] || { echo "makeself is not installed (apt install makeself), or say where it is with MAKESELF=" >&2; exit 1; }

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/bin" "$stage/share/docs" "$stage/share/examples"
for program in cbe-portal-server cbe-runner cbe-quota-helperd cbe-portalctl examc; do
  [ -f "$bin_dir/$program" ] || { echo "$bin_dir/$program is missing" >&2; exit 1; }
  install -m 0755 "$bin_dir/$program" "$stage/bin/$program"
done
install -m 0644 "$repo/docs/deployment-linux.md" "$stage/share/docs/deployment-linux.md"
for doc in TEACHER-GUIDE.md RELEASE.md deployment.md; do
  install -m 0644 "$repo/docs/workspace/$doc" "$stage/share/docs/$doc"
done
cp -R "$repo/testdata/workspace/lab-listas" "$stage/share/examples/lab-listas"
install -m 0755 "$here/install.sh" "$stage/install.sh"
install -m 0755 "$here/uninstall.sh" "$stage/uninstall.sh"
printf '%s\n' "$version" > "$stage/VERSION"
printf '%s\n' "$arch" > "$stage/ARCH"
chmod -R go-w,a+rX "$stage"

mkdir -p "$output"
# The release names the two architectures x64 and arm64 (cbe-portal-server-linux-x64.tar.gz); the version is without its v.
label=$arch
[ "$arch" = amd64 ] && label=x64
file="$output/cbe-portal-${version#v}-linux-$label.run"
# --needroot: the installer changes the system. --keep-umask: the files keep the modes they are given here, not the umask of whoever runs it.
"$makeself" --sha256 --needroot --keep-umask --nox11 "$stage" "$file" "CBE Portal server $version ($arch)" ./install.sh
(cd "$output" && sha256sum "$(basename "$file")" > "$(basename "$file").sha256")
echo "$file"
