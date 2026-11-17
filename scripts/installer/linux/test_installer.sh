#!/bin/bash
# Tests an installer that make_installer.sh made:  scripts/installer/linux/test_installer.sh cbe-portal-server-VERSION-linux-x64-installer.run
#
# It installs into a folder of its own (--prefix) with --no-configure --no-link, so nothing of the machine outside that folder is changed, upgrades it,
# and uninstalls it. The installer needs root to run: with sudo (passwordless) when it is not root; without either the test is skipped.
set -euo pipefail

run=$(readlink -f "${1:?usage: $0 INSTALLER.run}")
sudo_cmd=()
if [ "$(id -u)" -ne 0 ]; then
  if sudo -n true 2>/dev/null; then sudo_cmd=(sudo -n); else echo "skipped: needs root, or sudo without a password"; exit 0; fi
fi
work=$(mktemp -d)
trap '"${sudo_cmd[@]}" rm -rf "$work"' EXIT
fail() { echo "FAILED: $*" >&2; exit 1; }
as_root() { "${sudo_cmd[@]}" "$@"; }

(cd "$(dirname "$run")" && sha256sum -c "$(basename "$run").sha256") || fail "the checksum"
sh "$run" --info | grep -q "^Identification: CBE Portal server" || fail "--info does not say what it is"
# Not root: refused, before anything is unpacked.
if [ "${#sudo_cmd[@]}" -gt 0 ]; then
  if sh "$run" -- --prefix "$work/refused" --no-configure --no-link >/dev/null 2>&1; then fail "a person who is not root could run it"; fi
  [ ! -e "$work/refused" ] || fail "something was installed by a person who is not root"
fi

prefix=$work/opt
as_root sh "$run" -- --prefix "$prefix" --no-configure --no-link >"$work/install.log" 2>&1 || { cat "$work/install.log"; fail "the install"; }
for file in bin/cbe-portal-server bin/cbe-runner bin/cbe-quota-helperd bin/cbe-portalctl bin/cbe-agent bin/examc uninstall.sh VERSION ARCH share/docs/deployment-linux.md share/examples/lab-listas/workspace.md share/examples/lab-listas-own-base/image/Containerfile; do
  [ -e "$prefix/$file" ] || fail "$file was not installed"
done
[ -x "$prefix/bin/cbe-portalctl" ] && [ -x "$prefix/uninstall.sh" ] || fail "the programs are not executable"
[ "$(stat -c %U "$prefix/bin/cbe-portal-server")" = root ] || fail "the programs are not root's"
version=$(cat "$prefix/VERSION")
"$prefix/bin/cbe-portalctl" version | grep -q "$version" || fail "cbe-portalctl does not report $version"
"$prefix/bin/cbe-portal-server" -version | grep -q "$version" || fail "the server does not report $version"
"$prefix/bin/cbe-agent" -version | grep -q "$version" || fail "the agent does not report $version"
if command -v ldd >/dev/null 2>&1 && ldd "$prefix/bin/cbe-agent" >/dev/null 2>&1; then fail "the agent is not a static program: it could not run in every image"; fi
"$prefix/bin/cbe-portalctl" sample-config | grep -q '^WORKSPACE_ENABLED=yes' || fail "sample-config"
# A configuration that is wrong is refused with the line, before the machine is looked at (it needs no root and changes nothing).
printf 'WORKSPACE_ENABLD=yes\n' > "$work/bad.conf"
if "$prefix/bin/cbe-portalctl" configure --config "$work/bad.conf" --dry-run >"$work/bad.log" 2>&1; then fail "a wrong configuration was taken"; fi
grep -q 'WORKSPACE_ENABLD is not a setting (did you mean WORKSPACE_ENABLED?)' "$work/bad.log" || { cat "$work/bad.log"; fail "the wrong key was not named"; }

# An upgrade replaces the programs and keeps what is in etc/.
echo "mine" | as_root tee "$prefix/etc/keep.txt" >/dev/null
as_root sh "$run" -- --prefix "$prefix" --no-configure --no-link >"$work/upgrade.log" 2>&1 || { cat "$work/upgrade.log"; fail "the upgrade"; }
grep -q "upgrading it to $version" "$work/upgrade.log" || fail "the second run was not an upgrade"
[ "$(cat "$prefix/etc/keep.txt")" = mine ] || fail "the upgrade took what was in etc/"

# The uninstaller removes the program files (nothing was ever configured, so there are no services or data).
as_root "$prefix/uninstall.sh" >"$work/uninstall.log" 2>&1 || { cat "$work/uninstall.log"; fail "the uninstall"; }
[ ! -e "$prefix" ] || fail "the program files are still there"
echo "the installer is as it should be ($version)"
