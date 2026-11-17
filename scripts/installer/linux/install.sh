#!/bin/sh
# Installs the CBE Portal server's program files. This is the script the self-extracting installer (cbe-portal-VERSION-linux-ARCH.run) runs after it unpacks
# itself, and it can be run from the unpacked folder. It only puts files in place: the machine is made a server by `cbe-portalctl configure`, which this
# script runs when it is asked to (--config) or when a person is at the terminal.
#
#   sudo ./cbe-portal-0.17.0-linux-x64.run -- [options]
#     --prefix DIR    where the program files go (default /opt/cbe-portal)
#     --config FILE   configure the server from this file once the files are in place (unattended with --yes)
#     --yes           do not ask before changing the machine
#     --no-configure  only put the files in place
#     --no-link       do not link cbe-portalctl and examc into /usr/local/bin
#
# An upgrade (the prefix has an installation already) replaces the programs and keeps etc/, the saved answers and the settings; it prepares the
# units and says that the services are to be restarted (cbe-portalctl restart) when no class is working. Nothing running is stopped.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
prefix=/opt/cbe-portal
config=
yes=
configure=auto
link=yes

while [ $# -gt 0 ]; do
  case $1 in
    --prefix) prefix=${2:?--prefix needs a folder}; shift 2 ;;
    --config) config=${2:?--config needs a file}; shift 2 ;;
    --yes|-y) yes=--yes; shift ;;
    --no-configure) configure=no; shift ;;
    --no-link) link=no; shift ;;
    -h|--help) sed -n '2,/^set -eu/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "install.sh: $1 is not an option (--help lists them)" >&2; exit 2 ;;
  esac
done

version=$(cat "$here/VERSION")
case $(uname -m) in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) arch=$(uname -m) ;;
esac
if [ "$(cat "$here/ARCH")" != "$arch" ]; then
  echo "This installer is for $(cat "$here/ARCH") machines, and this one is $arch." >&2
  exit 1
fi
if [ "$(uname -s)" != Linux ]; then
  echo "The CBE Portal server installer is for Linux." >&2
  exit 1
fi
case $prefix in /*) ;; *) echo "--prefix must be a full path" >&2; exit 2 ;; esac

if [ "$(id -u)" -ne 0 ]; then
  if [ "$configure" != no ] || [ "$link" != no ]; then
    echo "Installing and configuring change the system: run it as root (sudo), or add --no-configure --no-link to only unpack the files into a folder you own (--prefix)." >&2
    exit 1
  fi
fi

upgrade=no
old=
if [ -f "$prefix/VERSION" ]; then
  upgrade=yes
  old=$(cat "$prefix/VERSION")
  echo "CBE Portal $old is installed in $prefix: upgrading it to $version."
else
  echo "Installing CBE Portal $version in $prefix."
fi

# Replace each file in one step, so that a program that is running is never half replaced (the running one goes on with the old inode).
put() { # mode source destination
  mkdir -p "$(dirname "$3")"
  cp "$2" "$3.new"
  chmod "$1" "$3.new"
  mv -f "$3.new" "$3"
}
mkdir -p "$prefix" "$prefix/bin" "$prefix/etc" "$prefix/lib/systemd" "$prefix/share"
chmod 755 "$prefix" "$prefix/bin" "$prefix/etc" "$prefix/lib" "$prefix/lib/systemd" "$prefix/share"
for program in "$here"/bin/*; do
  put 755 "$program" "$prefix/bin/$(basename "$program")"
done
rm -rf "$prefix/share.new"
cp -R "$here/share" "$prefix/share.new"
chmod -R go-w,a+rX "$prefix/share.new"
rm -rf "$prefix/share.old"
[ -d "$prefix/share" ] && mv "$prefix/share" "$prefix/share.old"
mv "$prefix/share.new" "$prefix/share"
rm -rf "$prefix/share.old"
put 755 "$here/uninstall.sh" "$prefix/uninstall.sh"
put 644 "$here/ARCH" "$prefix/ARCH"
put 644 "$here/VERSION" "$prefix/VERSION"

if [ "$link" = yes ] && [ "$prefix" = /opt/cbe-portal ] || { [ "$link" = yes ] && [ -d /usr/local/bin ] && [ "$(id -u)" -eq 0 ]; }; then
  for program in cbe-portalctl examc; do
    [ -e "$prefix/bin/$program" ] || continue
    target=/usr/local/bin/$program
    if [ -L "$target" ] && [ "$(readlink "$target")" = "$prefix/bin/$program" ]; then
      continue
    elif [ -e "$target" ] || [ -L "$target" ]; then
      echo "warning: $target exists and is not this installation's, so it is left alone (use $prefix/bin/$program)." >&2
    else
      ln -s "$prefix/bin/$program" "$target"
    fi
  done
fi

echo "The program files are in $prefix ($("$prefix/bin/cbe-portalctl" version 2>/dev/null || echo "$version"))."

if [ "$configure" = no ]; then
  echo "Next: $prefix/bin/cbe-portalctl configure  (cbe-portalctl sample-config prints a configuration file to start from)."
  exit 0
fi

ctl="$prefix/bin/cbe-portalctl"
if [ "$upgrade" = yes ] && [ -f "$prefix/etc/cbe-portal.conf" ] && [ -z "$config" ]; then
  # What was answered before is applied again: units are written anew, nothing running is stopped.
  echo "Preparing the services for the new version with the answers saved in $prefix/etc/cbe-portal.conf."
  "$ctl" configure --yes
  echo
  echo "The services still run the old version. When no class is working: sudo cbe-portalctl restart"
  exit 0
fi
if [ -n "$config" ]; then
  # shellcheck disable=SC2086
  exec "$ctl" configure --config "$config" $yes
fi
if [ -t 0 ] && [ -t 1 ]; then
  printf 'Configure this machine as a CBE Portal server now? [Y/n] '
  read -r answer || answer=n
  case $answer in
    ""|y|Y|yes|YES) exec "$ctl" configure $yes ;;
  esac
fi
echo "Next: sudo $ctl configure  (or  --config FILE  for an unattended configuration; $ctl sample-config prints one)."
