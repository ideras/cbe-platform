#!/bin/sh
# Removes the CBE Portal server's program files and its services. THE DATA STAYS (the database, the screenshots, the students' folders, the XFS volume and the
# accounts that own them) unless --remove-data says that all of it is to be deleted too. The answers given to the configuration are kept with the data, so that a
# new installation finds it again:  cbe-portalctl configure --config DATA_DIR/cbe-portal.conf.last
#
#   sudo /opt/cbe-portal/uninstall.sh [--remove-data] [--yes]
set -eu

prefix=$(cd "$(dirname "$0")" && pwd)
flags=
while [ $# -gt 0 ]; do
  case $1 in
    --remove-data) flags="$flags --remove-data"; shift ;;
    --yes|-y) flags="$flags --yes"; shift ;;
    -h|--help) sed -n '2,/^set -eu/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "uninstall.sh: $1 is not an option (--help lists them)" >&2; exit 2 ;;
  esac
done
if [ "$(id -u)" -ne 0 ]; then
  echo "Run it as root (sudo)." >&2
  exit 1
fi

if [ -f "$prefix/etc/cbe-portal.conf" ] && [ -x "$prefix/bin/cbe-portalctl" ]; then
  # shellcheck disable=SC2086
  "$prefix/bin/cbe-portalctl" unconfigure $flags || { echo "The services could not be removed: the program files are left where they are, so that this can be run again." >&2; exit 1; }
else
  echo "This installation was never configured: there are no services to remove."
fi

for program in cbe-portalctl examc; do
  target=/usr/local/bin/$program
  if [ -L "$target" ] && [ "$(readlink "$target")" = "$prefix/bin/$program" ]; then
    rm -f "$target"
  fi
done
rm -rf "$prefix"
echo "The program files are removed from $prefix."
