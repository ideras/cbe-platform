#!/usr/bin/env bash
# Phase 0, task 4: size the host for a class of 40 students compiling C++ at the same moment.
#
# It measures, on this machine:
#   1. one compile alone in a hardened container (CPU seconds, wall time, peak memory of the biggest process);
#   2. 40 containers, each limited to 1 core and 1 GiB, all pressing "make" at the same instant;
#   3. autosave-shaped write latency (write + fsync + rename, ~1 KiB) in /work before and during that storm,
#      because the plan wants editor saves under 300 ms at the 95th percentile (Phase 8);
#   4. the host memory and CPU the storm cost, taken from the cbe-runner user slice.
#
# Run after setup-host.sh, as your normal user, on an otherwise idle machine (the numbers include whatever else runs).
#   docs/workspace/spike/sizing.sh [students]        default 40
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
N=${1:-40}
RUNNER=cbe-runner
RUNNER_UID=$(id -u "$RUNNER")
HOME_R=/home/$RUNNER
BIN=$HOME_R/spike/bin
SRC=$HOME_R/spike/cpp-src
SLICE=/sys/fs/cgroup/user.slice/user-$RUNNER_UID.slice
IMAGE=localhost/cbe-spike-cpp:1
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

as_runner() {
  sudo -n -u "$RUNNER" env HOME="$HOME_R" XDG_RUNTIME_DIR=/run/user/"$RUNNER_UID" \
    DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/"$RUNNER_UID"/bus \
    sh -c 'cd "$HOME" && exec "$@"' sh "$@"
}

echo "== build the driver (it carries the fsync probe and the density check) =="
BUILD=$(mktemp -d)
(cd "$HERE" && CGO_ENABLED=0 go build -trimpath -o "$BUILD/spike" .)
sudo install -d -o "$RUNNER" -g "$RUNNER" -m 0755 "$HOME_R/spike" "$BIN"
sudo install -o "$RUNNER" -g "$RUNNER" -m 0755 "$BUILD/spike" "$BIN/spike"
rm -rf "$BUILD"

echo "== host =="
echo "cpu: $(lscpu | awk -F: '/Model name/ {gsub(/^ +/,"",$2); print $2}'), $(nproc) threads; memory: $(free -m | awk '/Mem:/ {print $2}') MiB; load: $(cut -d' ' -f1-3 /proc/loadavg)"

echo "== image (network is allowed while an image is built, D11) =="
if ! as_runner podman image exists "$IMAGE"; then
  as_runner sh -c "cat > $HOME_R/spike/Containerfile" <<'EOF'
FROM docker.io/library/ubuntu:24.04
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends g++ make time && rm -rf /var/lib/apt/lists/*
EOF
  as_runner podman build -q -t "$IMAGE" -f "$HOME_R/spike/Containerfile" "$HOME_R/spike" >/dev/null
fi
as_runner podman images --format '{{.Repository}}:{{.Tag}} {{.Size}}' "$IMAGE"

echo "== sample project: a C++20 lab with STL headers, three translation units =="
sudo -u "$RUNNER" mkdir -p "$SRC"
sudo -u "$RUNNER" tee "$SRC/list.h" >/dev/null <<'EOF'
#pragma once
#include <algorithm>
#include <functional>
#include <iostream>
#include <map>
#include <memory>
#include <optional>
#include <ranges>
#include <sstream>
#include <string>
#include <vector>

template <typename T>
class SortedList {
public:
    struct Node {
        T value;
        std::unique_ptr<Node> next;
        explicit Node(T v) : value(std::move(v)) {}
    };
    void push_front(T v) {
        auto node = std::make_unique<Node>(std::move(v));
        node->next = std::move(head_);
        head_ = std::move(node);
        ++size_;
    }
    void insert_sorted(T v) {
        auto node = std::make_unique<Node>(std::move(v));
        Node* cursor = nullptr;
        for (Node* it = head_.get(); it && it->value < node->value; it = it->next.get()) cursor = it;
        if (!cursor) { node->next = std::move(head_); head_ = std::move(node); }
        else { node->next = std::move(cursor->next); cursor->next = std::move(node); }
        ++size_;
    }
    std::optional<T> find_if(const std::function<bool(const T&)>& pred) const {
        for (Node* it = head_.get(); it; it = it->next.get()) if (pred(it->value)) return it->value;
        return std::nullopt;
    }
    std::vector<T> to_vector() const {
        std::vector<T> out;
        for (Node* it = head_.get(); it; it = it->next.get()) out.push_back(it->value);
        return out;
    }
    std::size_t size() const { return size_; }
    std::string str() const {
        std::ostringstream os;
        for (const auto& v : to_vector()) os << v << ' ';
        return os.str();
    }
private:
    std::unique_ptr<Node> head_;
    std::size_t size_ = 0;
};

std::map<std::string, int> word_count(const std::string& text);
std::vector<int> evens(const std::vector<int>& values);
EOF
sudo -u "$RUNNER" tee "$SRC/list.cpp" >/dev/null <<'EOF'
#include "list.h"

std::map<std::string, int> word_count(const std::string& text) {
    std::map<std::string, int> counts;
    std::istringstream in(text);
    for (std::string word; in >> word;) ++counts[word];
    return counts;
}

std::vector<int> evens(const std::vector<int>& values) {
    std::vector<int> out;
    for (int v : values | std::views::filter([](int x) { return x % 2 == 0; })) out.push_back(v);
    std::ranges::sort(out);
    return out;
}

template class SortedList<int>;
template class SortedList<std::string>;
EOF
sudo -u "$RUNNER" tee "$SRC/main.cpp" >/dev/null <<'EOF'
#include "list.h"

int main() {
    SortedList<int> list;
    for (int v : {5, 3, 9, 1, 7}) list.insert_sorted(v);
    list.push_front(0);
    std::cout << list.str() << "(" << list.size() << ")\n";
    for (const auto& [word, n] : word_count("a b a c b a")) std::cout << word << "=" << n << "\n";
    return 0;
}
EOF
sudo -u "$RUNNER" tee "$SRC/tests.cpp" >/dev/null <<'EOF'
#include <cassert>
#include "list.h"

int main() {
    SortedList<int> list;
    for (int v : {4, 2, 8}) list.insert_sorted(v);
    assert((list.to_vector() == std::vector<int>{2, 4, 8}));
    assert(list.find_if([](int v) { return v > 3; }) == 4);
    assert(evens({1, 2, 3, 4}) == (std::vector<int>{2, 4}));
    assert(word_count("x y x").at("x") == 2);
    std::cout << "ok\n";
    return 0;
}
EOF
sudo -u "$RUNNER" tee "$SRC/Makefile" >/dev/null <<'EOF'
CXX = g++
CXXFLAGS = -std=c++20 -g -O0 -Wall -Wextra
all: main tests
main: main.o list.o
	$(CXX) -o $@ $^
tests: tests.o list.o
	$(CXX) -o $@ $^
%.o: %.cpp list.h
	$(CXX) $(CXXFLAGS) -c $<
check: tests
	./tests
EOF
sudo chmod -R a+rX "$SRC"

# What each container does: copy the project into /work, run make under time -v, print the numbers.
SCRIPT='cp -r /src/. /work/ && cd /work && /usr/bin/time -v -o /work/time.txt make -s >/work/make.log 2>&1; echo "rc=$?"; cat /work/time.txt'

run_container() { # <name> <session>   (hardened the way Phase 3's runner will do it; limits are the plan defaults)
  as_runner podman run --rm --name "$1" --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,size=64m --cpus 1 --memory 1g --memory-swap 1g --pids-limit 256 --user 1000:1000 \
    -v "$SRC":/src:ro -v /data/cbe-platform/work/"$2":/work -w /work "$IMAGE" sh -c "$SCRIPT"
}

summarize() { # reads container outputs on stdin -> "user sys wall maxrss_kb rc"
  awk '
    /^rc=/ {rc=substr($0,4)}
    /User time/ {u=$NF}
    /System time/ {s=$NF}
    /Elapsed/ {n=split($NF,a,":"); w=0; for(i=1;i<=n;i++) w=w*60+a[i]}
    /Maximum resident/ {m=$NF}
    END {printf "%s %s %s %s %s\n", u, s, w, m, rc}'
}

echo "== quota folders for $N students + a probe =="
make_session() { sudo cbe-quota-helper remove "$1" >/dev/null 2>&1 || true; sudo cbe-quota-helper create "$1" "$2" "$3" >/dev/null; }
for i in $(seq -w 1 "$N"); do make_session "sz-$i" 100 5000; done
make_session sz-probe 20 5000
trap 'for i in $(seq -w 1 "$N") probe; do sudo cbe-quota-helper remove "sz-$i" >/dev/null 2>&1 || true; done; rm -rf "$OUT"' EXIT

echo "== 1. one compile alone =="
for round in 1 2 3; do
  make_session sz-01 100 5000
  printf "run %s: " "$round"; run_container sz-solo sz-01 | summarize | awk '{printf "user %ss, system %ss, wall %.2fs, biggest process %.0f MiB, rc=%s\n", $1, $2, $3, $4/1024, $5}'
done

echo "== 3a. autosave latency, machine idle =="
as_runner podman run --rm --network none --user 1000:1000 -v "$BIN":/spike:ro -v /data/cbe-platform/work/sz-probe:/work docker.io/library/alpine:3.20 /spike/spike load fsync /work 300 50

echo "== 2. $N containers press make at the same instant =="
for i in $(seq -w 1 "$N"); do make_session "sz-$i" 100 5000; done
START=$SLICE
cpu0=$(awk '/usage_usec/ {print $2}' $START/cpu.stat)
stop=$OUT/stop; peakfile=$OUT/peak
( max=0; while [ ! -f "$stop" ]; do m=$(cat $START/memory.current); [ "$m" -gt "$max" ] && max=$m; sleep 0.2; done; echo "$max" > "$peakfile" ) &
poller=$!
# the probe starts first so it is running when the storm begins; it lasts about 15 s
as_runner podman run --rm --network none --user 1000:1000 -v "$BIN":/spike:ro -v /data/cbe-platform/work/sz-probe:/work docker.io/library/alpine:3.20 /spike/spike load fsync /work 300 50 > "$OUT/probe.json" &
probe=$!
sleep 1
t0=$(date +%s.%N)
workers=()
for i in $(seq -w 1 "$N"); do
  ( run_container "sz-$i" "sz-$i" > "$OUT/c$i.txt" 2>&1 ) &
  workers+=($!)
done
wait "${workers[@]}" 2>/dev/null || true
t1=$(date +%s.%N)
touch "$stop"; wait "$poller" 2>/dev/null || true; wait "$probe" 2>/dev/null || true
cpu1=$(awk '/usage_usec/ {print $2}' $START/cpu.stat)
wall=$(echo "$t1 - $t0" | bc -l)

echo "== results: $N concurrent compiles =="
for i in $(seq -w 1 "$N"); do summarize < "$OUT/c$i.txt"; done > "$OUT/all.txt"
awk -v wall="$wall" -v n="$N" -v cpu="$(( cpu1 - cpu0 ))" -v peak="$(cat "$peakfile")" '
  { u[NR]=$1; s[NR]=$2; w[NR]=$3; m[NR]=$4; if ($5 != 0) bad++ ; cpusum += $1 + $2 }
  END {
    asort(w, ws); asort(m, ms);
    printf "all %d finished in %.1fs wall (slowest container %.1fs, median %.1fs), failures: %d\n", n, wall, ws[n], ws[int((n+1)/2)], bad+0;
    printf "CPU per compile: %.2fs (user+system); host average %.1f cores busy during the storm\n", cpusum/n, (cpu/1e6)/wall;
    printf "biggest single process: median %.0f MiB, max %.0f MiB; the user slice peaked at %.0f MiB for %d containers\n", ms[int((n+1)/2)]/1024, ms[n]/1024, peak/1048576, n
  }' "$OUT/all.txt"
echo "autosave latency while the storm ran: $(cat "$OUT/probe.json")"
echo "== host after: load $(cut -d' ' -f1-3 /proc/loadavg), free $(free -m | awk '/Mem:/ {print $7}') MiB available =="
