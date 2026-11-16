#!/usr/bin/env bash
# Check a course image the way the runner will use it: start it with the hardening of Phase 3 (no network, read-only root, a tmpfs at
# /tmp, no capabilities, no new privileges, limits on processes, memory and CPU, a quota-limited /work), then drive its agent through
# the socket with cbe-agent-cli, as the cbe-runner user, from outside.
#
#   images/test.sh cpp20
#
# It needs the container host's setup (docs/workspace/spike/setup-host.sh: the cbe-runner user, the XFS volume with project quotas,
# the quota helper) and the image (images/build.sh). It uses sudo and cleans up after itself, even when a check fails.
set -uo pipefail

course=${1:?usage: images/test.sh COURSE}
image=cbe/$course:latest
here=$(cd "$(dirname "$0")" && pwd)
repo=$(dirname "$here")
RUNNER=cbe-runner
RUID=$(id -u "$RUNNER")
WORK=/data/cbe-platform/work
session=p2-test-$course
name=cbe-$session
run_dir=$WORK/$session-run
bin=/home/$RUNNER/phase2/bin
tmp=$(mktemp -d)

as_runner() { sudo -n -u "$RUNNER" env HOME=/home/$RUNNER XDG_RUNTIME_DIR=/run/user/"$RUID" "$@"; }
podman() { as_runner podman "$@"; }
agent() { as_runner "$bin/cbe-agent-cli" -socket "$run_dir/agent.sock" "$@"; }
x() { agent exec "$@" | tr -d '\r'; } # run a command in a terminal and print what it printed, without the terminal's carriage returns

pass=0 fail=0
check() { # description, then the function that succeeds when the check does
  local what=$1; shift
  if ( "$@" ) >"$tmp/last" 2>&1; then printf '  PASS  %s\n' "$what"; pass=$((pass + 1)); else printf '  FAIL  %s\n' "$what"; sed 's/^/        /' "$tmp/last" | tail -8; fail=$((fail + 1)); fi
}

cleanup() {
  podman rm -f "$name" >/dev/null 2>&1
  sudo -n cbe-quota-helper remove "$session" >/dev/null 2>&1
  sudo -n rm -f "$run_dir/agent.sock"
  sudo -n rmdir "$run_dir" >/dev/null 2>&1
  rm -rf "$tmp"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------------------------------------------------------------
# What the image holds, looked at without the agent.
t_label() { podman image inspect --format '{{index .Config.Labels "io.cbe.agent.sha256"}}' "$image" | grep -qE '^[0-9a-f]{64}$'; }
t_user() { [ "$(sed -n 1p "$tmp/info")" = 1000 ] && [ "$(sed -n 2p "$tmp/info")" = 1000 ] && [ "$(sed -n 3p "$tmp/info")" = student ]; }
t_locked() { sed -n 4p "$tmp/info" | grep -q '^!' && sed -n 5p "$tmp/info" | grep -q '^!'; }
t_nosudo() { sed -n 6p "$tmp/info" | grep -qx nosudo; }
t_nosuid() { [ "$(sed -n 7p "$tmp/info")" = 0 ]; }
t_workowner() { [ "$(sed -n 8p "$tmp/info")" = student ]; }

# What the agent is, from outside.
t_hello() { grep -q '"type":"agent.hello"' "$tmp/status" && grep -q '"proto":1' "$tmp/status"; }
t_pid1flag() { grep -q '"pid1":true' "$tmp/status"; }
t_openat2() { ! grep -q slow_paths "$tmp/status"; }
t_heartbeat() { grep -q '"type":"agent.hb"' "$tmp/status"; }
t_pid1() { x 'tr "\0" " " < /proc/1/cmdline; echo; grep ^Uid: /proc/1/status' | grep -q 'cbe-agent' && x 'grep ^Uid: /proc/1/status' | grep -qE 'Uid:[[:space:]]+1000[[:space:]]+1000'; }
t_readonly() { ! agent exec 'touch /usr/x'; }
t_nonetwork() { ! agent exec 'getent hosts example.com' && x 'ls /sys/class/net' | grep -qx lo; }
t_noroot() { ! agent exec 'sudo -n true' && agent exec 'test ! -u /usr/bin/su' && [ "$(x 'id -u')" = 1000 ]; }

# Files.
t_roundtrip() {
  agent mkdir src && echo 'int a;' | agent create src/a.c && [ "$(agent cat src/a.c)" = 'int a;' ] && echo 'int b;' | agent write src/a.c &&
    [ "$(agent cat src/a.c)" = 'int b;' ] && agent mv src/a.c src/b.c && agent ls src | grep -q b.c && agent rm src && ! agent ls src
}
t_hostblind() {
  echo secret | agent create hostcheck.txt && [ "$(x 'stat -c %u hostcheck.txt')" = 1000 ] && ! as_runner ls "$WORK/$session" && ! ls "$WORK/$session"
}
t_links() {
  agent exec 'ln -s /etc evil; ln -s ../../../../etc/passwd rel; ln -s loop loop' || return 1
  ! agent cat evil/passwd && ! agent cat rel && ! agent cat loop && ! agent ls evil && ! echo x | agent create evil/planted || return 1
  local refused
  refused=$(agent cat evil/passwd 2>&1)
  grep -q BAD_PATH <<<"$refused" && agent exec 'test ! -e /etc/planted'
}

# Terminals.
upload_project() {
  local source=$repo/testdata/workspace/lab-listas/project
  (cd "$source" && find . -type d | sed 's|^\./||' | grep -v '^\.$') | while read -r d; do agent mkdir "$d" 2>/dev/null; done
  (cd "$source" && find . -type f | sed 's|^\./||') | while read -r f; do agent create "$f" < "$source/$f" >/dev/null || return 1; done
}
t_make() {
  upload_project && x 'make build/list_demo' | grep -q 'g++ -std=c++20' && agent exec make build/list_demo >/dev/null &&
    x 'make check; echo exit=$?' | grep -q 'push_front: FAILED' && { agent exec make check >/dev/null; [ $? = 2 ]; }
}
t_toolchain() {
  cat > "$tmp/t.cpp" <<'CPP'
#include <format>
#include <iostream>
#include <ranges>
#include <vector>
int main() {
  std::vector v{1, 2, 3, 4};
  for (int x : v | std::views::filter([](int n) { return n % 2 == 0; })) std::cout << std::format("even {}\n", x);
}
CPP
  agent create t.cpp < "$tmp/t.cpp" >/dev/null && x 'g++ -std=c++20 -Wall -g -o t t.cpp && ./t' | grep -q 'even 4' || return 1
  [ "$(x 'cmake --version | head -1; gdb --version | head -1; flex --version; bison --version | head -1; make --version | head -1; nano --version | head -1' | grep -c -E 'cmake version|GNU gdb|flex 2|bison \(GNU|GNU Make|GNU nano')" = 6 ]
}
t_cmake() {
  agent mkdir cm && printf 'cmake_minimum_required(VERSION 3.20)\nproject(demo CXX)\nset(CMAKE_CXX_STANDARD 20)\nadd_executable(demo main.cpp)\n' | agent create cm/CMakeLists.txt >/dev/null &&
    printf '#include <iostream>\nint main() { std::cout << "cmake ok\\n"; }\n' | agent create cm/main.cpp >/dev/null &&
    x 'cd cm && cmake -S . -B build >/dev/null && cmake --build build >/dev/null && ./build/demo' | grep -q 'cmake ok'
}
t_flexbison() {
  agent mkdir fb && printf '%%%%\n[0-9]+ { return 1; }\n.|\\n ;\n%%%%\nint yywrap(void) { return 1; }\n' | agent create fb/s.l >/dev/null &&
    x 'cd fb && flex -o s.c s.l && gcc -c s.c -o s.o && echo compiled' | grep -q compiled &&
    printf '%%{\nint yylex(void) { return 0; }\nvoid yyerror(const char *s) {}\n%%}\n%%%%\nstart: ;\n' | agent create fb/p.y >/dev/null &&
    x 'cd fb && bison -o p.c p.y && gcc -c p.c -o p.o && echo parsed' | grep -q parsed
}
t_gdb() { x 'gdb -batch -ex run -ex bt ./t 2>&1 | tail -4' | grep -q 'exited normally'; }
t_resize() { x -cols 132 -rows 50 'stty size' | grep -q '50 132'; }
t_events() {
  ( sleep 0.5; agent exec 'touch from-terminal.txt' >/dev/null ) &
  agent watch -n 1 -for 10s | grep -q '"path":"from-terminal.txt"'
  wait
}
t_zombies() {
  agent exec 'sleep 0.3 & sleep 0.1 & exit 0' >/dev/null; sleep 1.5
  [ "$(x 'ps -eo stat= | grep -c ^Z; true' | head -1)" = 0 ]
}

# Limits.
t_quota() {
  x 'dd if=/dev/zero of=big bs=1M count=100 2>&1; echo done' | grep -q 'No space left' || return 1
  echo x | agent create small.txt 2>"$tmp/err"; grep -q QUOTA "$tmp/err" || return 1
  echo y | agent write hostcheck.txt 2>"$tmp/err"; grep -q QUOTA "$tmp/err" || return 1
  agent rm big && echo x | agent create small.txt >/dev/null && [ "$(agent cat hostcheck.txt)" = secret ]
}
t_memory() {
  printf '#include <stdlib.h>\n#include <string.h>\nint main(void) { for (int i = 0; i < 400; i++) { char *p = malloc(1 << 20); if (!p) return 2; memset(p, 1, 1 << 20); } return 0; }\n' | agent create hog.c >/dev/null &&
    agent exec 'gcc -O0 -o hog hog.c' >/dev/null || return 1
  agent exec ./hog >/dev/null 2>"$tmp/err"; code=$?
  [ "$code" = 137 ] && grep -q 'memory limit killed' "$tmp/err" && agent status | grep -q '"oom_kills":[1-9]'
}
t_forkstorm() {
  agent exec 'for i in $(seq 1 400); do sleep 2 & done 2>/dev/null; wait' >/dev/null 2>&1; sleep 2.5
  [ "$(x 'echo alive')" = alive ]
}

# ---------------------------------------------------------------------------------------------------------------------------------
echo "== the image"
podman image exists "$image" || { echo "no image $image for $RUNNER: run images/build.sh $course first" >&2; exit 1; }
podman run --rm --user 0 --network none --read-only --entrypoint /bin/sh "$image" -c \
  'id -u student; id -g student; id -un 1000; getent shadow root | cut -d: -f2; getent shadow student | cut -d: -f2; command -v sudo || echo nosudo; find / -xdev -type f \( -perm -4000 -o -perm -2000 \) | wc -l; stat -c %U /work' >"$tmp/info" 2>&1
check "the agent in it is labelled with its hash" t_label
check "the user is student, 1000:1000" t_user
check "root and student have no password" t_locked
check "there is no sudo" t_nosudo
check "no file is setuid or setgid" t_nosuid
check "/work belongs to the student" t_workowner

echo "== starting the container as the runner will"
sudo -n cbe-quota-helper remove "$session" >/dev/null 2>&1
sudo -n cbe-quota-helper create "$session" 20 20000 >/dev/null || { echo "the quota helper failed" >&2; exit 1; }
sudo -n install -d -m 0777 "$run_dir"
sudo -n install -d -o "$RUNNER" -m 0755 "$bin"
(cd "$repo" && CGO_ENABLED=0 go build -trimpath -o "$tmp/cbe-agent-cli" ./cmd/cbe-agent-cli) && sudo -n install -o "$RUNNER" -m 0755 "$tmp/cbe-agent-cli" "$bin/cbe-agent-cli" || { echo "cannot build the harness" >&2; exit 1; }
podman rm -f "$name" >/dev/null 2>&1
podman run -d --name "$name" --network none --read-only --tmpfs /tmp:rw,size=64m,mode=1777 --cap-drop all --security-opt no-new-privileges \
  --pids-limit 256 --memory 256m --memory-swap 256m --cpus 1 -v "$WORK/$session:/work" -v "$run_dir:/run/cbe" "$image" >/dev/null || { echo "the container did not start" >&2; exit 1; }
for _ in $(seq 100); do [ -S "$run_dir/agent.sock" ] && agent status >"$tmp/status" 2>&1 && break; sleep 0.1; done

echo "== the agent, from outside"
check "the agent answers on the socket in the runner's folder" t_hello
check "it is PID 1" t_pid1flag
check "openat2 works under the default seccomp profile (no slow paths)" t_openat2
check "a heartbeat arrives" t_heartbeat
check "PID 1 is the agent, and it runs as uid 1000" t_pid1
check "the root file system is read-only" t_readonly
check "there is no network" t_nonetwork
check "there is no way to become root" t_noroot

echo "== files, from outside"
check "create, read, write, move, delete" t_roundtrip
check "files are uid 1000 inside, and cannot be read from the host" t_hostblind
check "a link to /etc made in the terminal leads nowhere" t_links

echo "== terminals, from outside"
check "make builds the project, streams its output and returns the exit code" t_make
check "the toolchain: C++20 with ranges and format, cmake, gdb, flex, bison, nano" t_toolchain
check "cmake configures and builds" t_cmake
check "flex and bison generate code that compiles" t_flexbison
check "gdb runs a program" t_gdb
check "a terminal resizes" t_resize
check "files created from a terminal produce events" t_events
check "orphaned processes are reaped (no zombies)" t_zombies

echo "== limits, from outside"
check "a full disk is QUOTA, for a create and for a write, and frees up again" t_quota
check "the memory limit kills the program, not the container, and the agent says so" t_memory
check "a fork storm hits the process limit and the agent still answers" t_forkstorm

echo "== stopping"
start=$(date +%s.%N)
podman stop "$name" >/dev/null 2>&1
elapsed=$(echo "$(date +%s.%N) - $start" | bc)
t_stopfast() { [ "$(echo "$elapsed < 4" | bc)" = 1 ]; }
t_nosocket() { [ ! -e "$run_dir/agent.sock" ]; }
t_exitcode() { [ "$(podman inspect --format '{{.State.ExitCode}}' "$name")" = 0 ]; }
check "podman stop ends the agent on SIGTERM without waiting for the kill ($elapsed s)" t_stopfast
check "the socket is gone after a clean stop" t_nosocket
check "the agent exited by itself, with code 0" t_exitcode

echo
echo "$pass passed, $fail failed"
[ "$fail" = 0 ]
