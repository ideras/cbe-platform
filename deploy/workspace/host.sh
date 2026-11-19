# Sourced by the tests of this folder that need the container host: what the installer and `cbe-portalctl configure` made there, read from the
# installation itself (docs/deployment-linux.md), so that no test names a folder or an account of its own. The caller sets repo first.
#
#   INSTALL_DIR                 the installation: CBE_INSTALL_DIR, or /opt/cbe-portal
#   SETTINGS                    its runner's settings, INSTALL_DIR/etc/runner.json
#   RUNNER SERVER IPC_GROUP     the accounts and the group (RUNNER_USER, SERVER_USER, IPC_GROUP of INSTALL_DIR/etc/cbe-portal.conf)
#   RUID RUNNER_HOME            the runner account's uid and home
#   SOCKET SECRET               the installed runner's socket and the file of its secret
#   IPC_DIR DATA_DIR WORK_ROOT  the folder of the socket, the folder of the runner's folders (DATA_DIR), and the students' /work folders
#
#   runtime ARGS...             the runner account's Podman (the server's images and containers are there)
#   setting KEY                 a key of SETTINGS; images.cbe/cpp20 reads a key of a key
#   course_image NAME           the reference of a course image the runner approves and the runtime has, or a failure that says how to install it
#   server_state NAME           makes a private folder of the exam server's account for a test's own server, and prints it
#   require_quiet SCRIPT        stops a test unless no container of the installed runner is running and no image is being built
#   allow_restarts SCRIPT WHAT  stops a test that restarts the installed runner unless CBE_ALLOW_RESTART=1 and no container of the runner is running
#
# The tests use the server's own container runtime, quota helper and volume. They start only on a quiet server (require_quiet), and whoever runs
# one makes sure that no class, grading or image build starts on it until the test ends: what a test removes at the end is what was not there
# when it began, and the private runner's cleanup never touches what the installed runner has a note of.
#
# It uses sudo without a password.

INSTALL_DIR=${CBE_INSTALL_DIR:-/opt/cbe-portal}
SETTINGS=$INSTALL_DIR/etc/runner.json
if [ ! -f "$SETTINGS" ] || [ ! -f "$INSTALL_DIR/etc/cbe-portal.conf" ]; then
  echo "no server with workspace exams is installed in $INSTALL_DIR: install it (the installer, then sudo cbe-portalctl configure with WORKSPACE_ENABLED=yes)," >&2
  echo "or say where it is with CBE_INSTALL_DIR" >&2
  exit 1
fi

# conf_get KEY DEFAULT: a key of the configuration the configurator saved, as it wrote it.
conf_get() {
  local value
  value=$(sed -n "s/^$1=//p" "$INSTALL_DIR/etc/cbe-portal.conf" | tail -1 | sed -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'\$/\1/")
  echo "${value:-$2}"
}

setting() {
  python3 - "$SETTINGS" "$1" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
for key in sys.argv[2].split('.', 1) if sys.argv[2].startswith('images.') else [sys.argv[2]]:
    value = value.get(key, '') if isinstance(value, dict) else ''
print(value if isinstance(value, str) else json.dumps(value))
PY
}

RUNNER=$(conf_get RUNNER_USER cbe-runner)
SERVER=$(conf_get SERVER_USER cbe-server)
IPC_GROUP=$(conf_get IPC_GROUP cbe-ipc)
RUID=$(id -u "$RUNNER") || { echo "the account $RUNNER does not exist: configure the server with WORKSPACE_ENABLED=yes" >&2; exit 1; }
RUNNER_HOME=$(getent passwd "$RUNNER" | cut -d: -f6)
SOCKET=$(setting socket)
SECRET=$(setting secret_file)
IPC_DIR=$(dirname "$SOCKET")
DATA_DIR=$(dirname "$(setting state_root)")
WORK_ROOT=$(setting work_root)

runtime() { sudo -n -u "$RUNNER" env HOME="$RUNNER_HOME" XDG_RUNTIME_DIR="/run/user/$RUID" podman "$@"; }

course_image() {
  local reference
  reference=$(setting "images.$1")
  if [ -z "$reference" ] || ! runtime image exists "$reference"; then
    echo "the runner does not have the course image $1 (${reference:-not approved in $SETTINGS}): sudo cbe-portalctl image install $1 images/${1#*/} --restart" >&2
    return 1
  fi
  echo "$reference"
}

server_state() {
  local dir="$DATA_DIR/test-$1-$$"
  sudo -n install -d -o "$SERVER" -g "$SERVER" -m 0700 "$dir"
  echo "$dir"
}

require_quiet() {
  local running building
  running=$(runtime ps --format '{{.Names}}')
  building=$(runtime ps -a --external --format '{{.Names}} {{.Status}}' | grep -i ' storage' || true)
  if [ -n "$running$building" ]; then
    echo "$1 uses this server's container runtime, and the installed runner is busy (students' workspaces, gradings or an image build): it does not run now." >&2
    printf '%s\n' "$running" "$building" | sed '/^$/d' >&2
    exit 1
  fi
}

allow_restarts() {
  if [ "${CBE_ALLOW_RESTART:-}" != 1 ]; then
    echo "$1 restarts $2: it is for a test host, not a server a class may be using. When nobody works on this one, run it with CBE_ALLOW_RESTART=1." >&2
    exit 1
  fi
  local running
  running=$(runtime ps --format '{{.Names}}')
  if [ -n "$running" ]; then
    echo "$1 restarts $2, and containers of the runner are running (students' workspaces, gradings or builds): it does not run now." >&2
    echo "$running" >&2
    exit 1
  fi
}
