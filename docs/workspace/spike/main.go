// Command spike is the Phase 0 throwaway that decides rootless Podman versus rootful Docker (plan D10).
//
// It has two faces, built into one static binary:
//
//	spike check [flags]   the orchestrator. It drives the container runtime ONLY through the Docker Engine API with the
//	                      moby client SDK, the way cmd/cbe-runner will, and runs the five checks of the plan plus a few
//	                      extras (agent socket, isolation of neighbours, reboot simulation).
//	spike load <mode>     the hostile or measured workload the orchestrator starts INSIDE a container: a memory hog, a CPU
//	                      burner, a disk filler, a file creator, a socket listener and so on.
//
// It is NOT product code and is never merged into it. See docs/workspace/DISCOVERY.md for what it found.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "load" {
		os.Exit(load(os.Args[2:]))
	}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "check" {
		args = args[1:]
	}
	os.Exit(orchestrate(args))
}

// ---------------------------------------------------------------------------------------------------------------------
// Workloads that run inside the containers.
// ---------------------------------------------------------------------------------------------------------------------

func readCgroup(name string) string {
	data, err := os.ReadFile("/sys/fs/cgroup/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func cgroupStat(name string) map[string]int64 {
	out := map[string]int64{}
	for _, line := range strings.Split(readCgroup(name), "\n") {
		if key, value, ok := strings.Cut(line, " "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			out[key] = n
		}
	}
	return out
}

func printJSON(value any) {
	data, _ := json.Marshal(value)
	fmt.Println(string(data))
}

var sink int

func spin(done <-chan struct{}) {
	runtime.LockOSThread()
	x := 0
	for {
		select {
		case <-done:
			sink = x
			return
		default:
		}
		for k := 0; k < 1_000_000; k++ {
			x += k
		}
	}
}

// fixtureMain is the content of src/main.cpp in the archive test. The orchestrator regenerates it to compare hashes.
func fixtureMain() []byte {
	rng := rand.New(rand.NewPCG(1, 2))
	data := make([]byte, 200<<10)
	for i := range data {
		data[i] = byte('a' + rng.IntN(26))
	}
	return data
}

func load(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: spike load <mem|cpu|fill|inodes|tick|hold|files|sock|sleep> ...")
		return 2
	}
	arg := func(i int, fallback string) string {
		if len(args) > i {
			return args[i]
		}
		return fallback
	}
	switch args[0] {
	case "mem":
		limit := readCgroup("memory.max")
		if limit == "" || limit == "max" {
			fmt.Println("NOT-ENFORCED memory.max=" + limit)
			return 3
		}
		fmt.Println("memory.max=" + limit)
		want, _ := strconv.Atoi(arg(1, "512"))
		var chunks [][]byte
		for i := 0; i < want; i += 8 {
			chunk := make([]byte, 8<<20)
			for j := 0; j < len(chunk); j += 4096 {
				chunk[j] = 1
			}
			chunks = append(chunks, chunk)
			fmt.Printf("alloc=%d\n", len(chunks)*8)
		}
		fmt.Println("SURVIVED")
		runtime.KeepAlive(chunks)
		time.Sleep(time.Minute)
	case "cpu":
		spinners, _ := strconv.Atoi(arg(1, "4"))
		seconds, _ := strconv.Atoi(arg(2, "6"))
		max := readCgroup("cpu.max")
		if max == "" || strings.HasPrefix(max, "max") {
			fmt.Println("NOT-ENFORCED cpu.max=" + max)
			return 3
		}
		// Go 1.25+ sizes GOMAXPROCS from the cgroup quota. Override it, or this test would pass by itself.
		runtime.GOMAXPROCS(spinners)
		before, start := cgroupStat("cpu.stat"), time.Now()
		done := make(chan struct{})
		for i := 0; i < spinners; i++ {
			go spin(done)
		}
		time.Sleep(time.Duration(seconds) * time.Second)
		close(done)
		wall, after := time.Since(start), cgroupStat("cpu.stat")
		printJSON(map[string]any{
			"cpu_max": max, "spinners": spinners, "wall_s": wall.Seconds(),
			"cores":        float64(after["usage_usec"]-before["usage_usec"]) / float64(wall.Microseconds()),
			"nr_throttled": after["nr_throttled"] - before["nr_throttled"],
		})
	case "fill":
		dir := filepath.Join(arg(1, "/work"), "fill")
		var st syscall.Statfs_t
		_ = syscall.Statfs(arg(1, "/work"), &st)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			printJSON(map[string]any{"err": err.Error()})
			return 1
		}
		block := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
		var total int64
		var failure error
		for n := 0; failure == nil; n++ {
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("f%04d", n)))
			if err != nil {
				failure = err
				break
			}
			for k := 0; k < 10 && failure == nil; k++ {
				w, err := f.Write(block)
				total += int64(w)
				failure = err
			}
			if err := f.Sync(); failure == nil {
				failure = err
			}
			f.Close()
		}
		printJSON(map[string]any{
			"bytes": total, "err": failure.Error(), "edquot": errors.Is(failure, syscall.EDQUOT),
			"enospc": errors.Is(failure, syscall.ENOSPC), "statfs_total": st.Blocks * uint64(st.Bsize),
		})
	case "inodes":
		dir := filepath.Join(arg(1, "/work"), "ino")
		limit, _ := strconv.Atoi(arg(2, "100000"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			printJSON(map[string]any{"err": err.Error()})
			return 1
		}
		created, msg := 0, ""
		var edquot bool
		for ; created < limit; created++ {
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("f%06d", created)))
			if err != nil {
				msg, edquot = err.Error(), errors.Is(err, syscall.EDQUOT)
				break
			}
			f.Close()
		}
		printJSON(map[string]any{"created": created, "err": msg, "edquot": edquot})
	case "tick":
		file := arg(1, "/work/tick")
		starts, _ := os.OpenFile(file+".starts", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if starts != nil {
			fmt.Fprintf(starts, "start %d\n", time.Now().UnixNano())
			starts.Close()
		}
		for n := 1; ; n++ {
			_ = os.WriteFile(file, []byte(strconv.Itoa(n)), 0o644)
			time.Sleep(100 * time.Millisecond)
		}
	case "hold":
		mb, _ := strconv.Atoi(arg(1, "80"))
		spinners, _ := strconv.Atoi(arg(2, "1"))
		runtime.GOMAXPROCS(spinners + 1)
		chunk := make([]byte, mb<<20)
		for j := 0; j < len(chunk); j += 4096 {
			chunk[j] = 1
		}
		done := make(chan struct{})
		for i := 0; i < spinners; i++ {
			go spin(done)
		}
		time.Sleep(25 * time.Second)
		close(done)
		runtime.KeepAlive(chunk)
	case "files":
		dir := arg(1, "/work")
		must := func(err error) {
			if err != nil {
				fmt.Println("ERR", err)
				os.Exit(1)
			}
		}
		must(os.MkdirAll(filepath.Join(dir, "src"), 0o755))
		must(os.MkdirAll(filepath.Join(dir, "empty"), 0o755))
		must(os.MkdirAll(filepath.Join(dir, "a b"), 0o755))
		must(os.WriteFile(filepath.Join(dir, "src/main.cpp"), fixtureMain(), 0o644))
		must(os.WriteFile(filepath.Join(dir, "src/util.h"), []byte("#pragma once\n"), 0o644))
		must(os.WriteFile(filepath.Join(dir, "a b", "ñ.txt"), []byte("unicode name\n"), 0o644))
		must(os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755))
		must(os.Symlink("/etc/passwd", filepath.Join(dir, "link")))
		must(os.Symlink("../../../..", filepath.Join(dir, "up")))
		must(os.Link(filepath.Join(dir, "src/main.cpp"), filepath.Join(dir, "hard")))
		must(syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644))
		must(os.WriteFile(filepath.Join(dir, "READY"), []byte("ok\n"), 0o644))
		for {
			time.Sleep(time.Hour)
		}
	case "sock":
		path := arg(1, "/run/cbe/agent.sock")
		_ = os.Remove(path)
		listener, err := net.Listen("unix", path)
		if err != nil {
			fmt.Println("ERR", err)
			return 1
		}
		_ = os.Chmod(path, 0o666)
		fmt.Println("listening", path)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return 1
			}
			go func() {
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadString('\n')
				fmt.Fprintf(conn, "pong:%s", line)
			}()
		}
	case "sleep":
		for {
			time.Sleep(time.Hour)
		}
	case "fsync":
		// Autosave-shaped writes: write a small file next to the target, fsync it, rename it over the target.
		dir, count := arg(1, "/work"), 0
		count, _ = strconv.Atoi(arg(2, "200"))
		pause, _ := strconv.Atoi(arg(3, "50"))
		var millis []float64
		for i := 0; i < count; i++ {
			content := bytes.Repeat([]byte("int main() { return 0; }\n"), 40) // about 1 KiB
			started := time.Now()
			tmp := filepath.Join(dir, ".save.tmp")
			f, err := os.Create(tmp)
			if err == nil {
				_, err = f.Write(content)
				if err == nil {
					err = f.Sync()
				}
				f.Close()
			}
			if err == nil {
				err = os.Rename(tmp, filepath.Join(dir, "main.cpp"))
			}
			if err != nil {
				printJSON(map[string]any{"err": err.Error()})
				return 1
			}
			millis = append(millis, float64(time.Since(started).Microseconds())/1000)
			time.Sleep(time.Duration(pause) * time.Millisecond)
		}
		sort.Float64s(millis)
		at := func(p float64) float64 { return millis[min(len(millis)-1, int(p*float64(len(millis))))] }
		printJSON(map[string]any{"n": len(millis), "p50_ms": at(0.50), "p95_ms": at(0.95), "p99_ms": at(0.99), "max_ms": millis[len(millis)-1]})
	default:
		fmt.Fprintln(os.Stderr, "unknown load mode", args[0])
		return 2
	}
	return 0
}

// ---------------------------------------------------------------------------------------------------------------------
// The orchestrator.
// ---------------------------------------------------------------------------------------------------------------------

type result struct {
	Name   string         `json:"name"`
	Pass   bool           `json:"pass"`
	Detail string         `json:"detail"`
	Data   map[string]any `json:"data,omitempty"`
}

type env struct {
	cli      *client.Client
	image    string
	workroot string // where the quota helper makes the per-session folders
	binDir   string // holds this binary; mounted read-only at /spike inside the containers
	runDir   string // per-container socket folders
	density  int    // how many containers the density check starts
}

// spec describes a container the way Phase 3's runner will: the hardening is fixed here, the caller picks limits only.
type spec struct {
	name    string
	cmd     []string
	memMB   int64
	cpus    float64
	pids    int64
	cpuset  string
	session string // quota folder to mount at /work
	runSock string // host folder to mount at /run/cbe
	restart bool
}

func (e *env) create(ctx context.Context, s spec) (string, error) {
	_, _ = e.cli.ContainerRemove(ctx, "cbe-spike-"+s.name, client.ContainerRemoveOptions{Force: true})
	binds := []string{e.binDir + ":/spike:ro"}
	workdir := "/"
	if s.session != "" {
		binds = append(binds, filepath.Join(e.workroot, s.session)+":/work")
		workdir = "/work"
	}
	if s.runSock != "" {
		binds = append(binds, s.runSock+":/run/cbe")
	}
	pids := s.pids
	host := &container.HostConfig{
		Binds:          binds,
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
		Tmpfs:          map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=16m"},
		Resources: container.Resources{
			Memory:     s.memMB << 20,
			MemorySwap: s.memMB << 20, // no swap
			NanoCPUs:   int64(s.cpus * 1e9),
			PidsLimit:  &pids,
			CpusetCpus: s.cpuset,
		},
	}
	if s.restart {
		host.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyAlways}
	}
	created, err := e.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "cbe-spike-" + s.name,
		Config: &container.Config{
			Image: e.image, User: "1000:1000", Cmd: s.cmd, WorkingDir: workdir,
			Labels: map[string]string{"cbe.spike": "1", "cbe.check": s.name},
		},
		HostConfig: host,
	})
	return created.ID, err
}

func (e *env) start(ctx context.Context, s spec) (string, error) {
	id, err := e.create(ctx, s)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	if _, err := e.cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return id, fmt.Errorf("start: %w", err)
	}
	return id, nil
}

func (e *env) remove(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = e.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
}

func (e *env) waitExit(ctx context.Context, id string, timeout time.Duration) (container.InspectResponse, error) {
	wait := e.cli.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case <-wait.Result:
	case err := <-wait.Error:
		return container.InspectResponse{}, err
	case <-time.After(timeout):
		return container.InspectResponse{}, errors.New("timed out waiting for the container to exit")
	}
	inspected, err := e.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	return inspected.Container, err
}

func (e *env) logs(ctx context.Context, id string) string {
	rc, err := e.cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "(logs: " + err.Error() + ")"
	}
	defer rc.Close()
	var out, errs bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &errs, rc); err != nil {
		return "(logs decode: " + err.Error() + ")"
	}
	return out.String() + errs.String()
}

// lastJSON returns the last line of the container log that is a JSON object.
func lastJSON(logs string) map[string]any {
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "{") {
			var out map[string]any
			if json.Unmarshal([]byte(lines[i]), &out) == nil {
				return out
			}
		}
	}
	return nil
}

// hostCgroup finds the cgroup directory of a container on the host through its init process.
func hostCgroup(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			return "/sys/fs/cgroup" + strings.TrimSpace(rest)
		}
	}
	return ""
}

func fileText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(" + err.Error() + ")"
	}
	return strings.TrimSpace(string(data))
}

func fail(name, format string, args ...any) result {
	return result{Name: name, Detail: fmt.Sprintf(format, args...)}
}

func pass(name string, data map[string]any, format string, args ...any) result {
	return result{Name: name, Pass: true, Detail: fmt.Sprintf(format, args...), Data: data}
}

func orchestrate(args []string) int {
	flags := flag.NewFlagSet("check", flag.ExitOnError)
	sock := flags.String("sock", "unix:///run/user/1001/podman/podman.sock", "runtime API socket")
	image := flags.String("image", "docker.io/library/alpine:3.20", "image used by every container")
	workroot := flags.String("workroot", "/data/cbe-platform/work", "folder of the XFS quota volume")
	only := flags.String("only", "", "comma-separated checks to run (default: all)")
	out := flags.String("out", "", "write the results as JSON to this file")
	density := flags.Int("n", 40, "containers started by the density check")
	_ = flags.Parse(args)

	self, _ := os.Executable()
	home, _ := os.UserHomeDir()
	cli, err := client.New(client.WithHost(*sock), client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Fprintln(os.Stderr, "client:", err)
		return 2
	}
	defer cli.Close()
	e := &env{cli: cli, image: *image, workroot: *workroot, binDir: filepath.Dir(self), runDir: filepath.Join(home, "spike", "run"), density: *density}

	checks := []struct {
		name string
		run  func(context.Context) result
	}{
		{"api", e.checkAPI},
		{"delegation", e.checkDelegation},
		{"pids", e.checkPIDs},
		{"memory", e.checkMemory},
		{"cpu", e.checkCPU},
		{"disk", e.checkDisk},
		{"inodes", e.checkInodes},
		{"pause", e.checkPause},
		{"stats", e.checkStats},
		{"archive", e.checkArchive},
		{"socket", e.checkSocket},
		{"reboot-prep", e.rebootPrep},
		{"reboot-verify", e.rebootVerify},
		{"density", e.checkDensity},
	}
	wanted := map[string]bool{}
	for _, name := range strings.Split(*only, ",") {
		if name = strings.TrimSpace(name); name != "" {
			wanted[name] = true
		}
	}
	// The reboot steps and the density run are asked for by name, never as part of "all".
	selected := func(name string) bool {
		return len(wanted) == 0 && !strings.HasPrefix(name, "reboot") && name != "density" || wanted[name]
	}

	// A neighbour that must not notice any of the abuse below.
	ctx := context.Background()
	var canaryID string
	if selected("pids") || selected("memory") || selected("cpu") || selected("disk") || selected("inodes") {
		id, err := e.start(ctx, spec{name: "canary", cmd: []string{"sh", "-c", "i=0; while true; do echo tick; sleep 1; done"}, memMB: 32, cpus: 0.5, pids: 32})
		if err != nil {
			fmt.Fprintln(os.Stderr, "canary:", err)
			return 2
		}
		canaryID = id
		defer e.remove(id)
		time.Sleep(2 * time.Second)
	}
	canaryLines := func() int { return strings.Count(e.logs(ctx, canaryID), "tick") }
	canaryStart, canaryAt := 0, time.Now()
	if canaryID != "" {
		canaryStart, canaryAt = canaryLines(), time.Now()
	}

	var results []result
	failed := 0
	for _, check := range checks {
		if !selected(check.name) {
			continue
		}
		started := time.Now()
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		r := check.run(checkCtx)
		cancel()
		results = append(results, r)
		verdict := "PASS"
		if !r.Pass {
			verdict, failed = "FAIL", failed+1
		}
		fmt.Printf("%s  %-14s %s  (%.1fs)\n", verdict, r.Name, r.Detail, time.Since(started).Seconds())
	}

	if canaryID != "" {
		grown, elapsed := canaryLines()-canaryStart, time.Since(canaryAt).Seconds()
		inspected, err := cli.ContainerInspect(ctx, canaryID, client.ContainerInspectOptions{})
		r := fail("isolation", "the neighbour container was disturbed: %v", err)
		if err == nil && inspected.Container.State.Running && !inspected.Container.State.OOMKilled && float64(grown) >= elapsed*0.6 {
			r = pass("isolation", map[string]any{"ticks": grown, "seconds": elapsed},
				"a neighbour kept running through the abuse: %d of ~%.0f one-second ticks, not OOM-killed", grown, elapsed)
		} else if err == nil {
			r = fail("isolation", "neighbour ticks=%d over %.0fs running=%v oomkilled=%v", grown, elapsed, inspected.Container.State.Running, inspected.Container.State.OOMKilled)
		}
		results = append(results, r)
		verdict := "PASS"
		if !r.Pass {
			verdict, failed = "FAIL", failed+1
		}
		fmt.Printf("%s  %-14s %s\n", verdict, r.Name, r.Detail)
	}

	if *out != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		_ = os.WriteFile(*out, data, 0o644)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------------------------------------------------
// Checks.
// ---------------------------------------------------------------------------------------------------------------------

func (e *env) checkAPI(ctx context.Context) result {
	ping, err := e.cli.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true})
	if err != nil {
		return fail("api", "ping: %v", err)
	}
	inspected, err := e.cli.ImageInspect(ctx, e.image)
	if err != nil {
		return fail("api", "image inspect %s: %v", e.image, err)
	}
	list, err := e.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", "cbe.spike=1")})
	if err != nil {
		return fail("api", "container list with a label filter: %v", err)
	}
	return pass("api", map[string]any{"api_version": ping.APIVersion, "image_id": inspected.ID, "labelled_containers": len(list.Items)},
		"moby client v0.6.3 talks to the compatible API (version %s); image inspect and label-filtered list work", ping.APIVersion)
}

func (e *env) checkDelegation(ctx context.Context) result {
	id, err := e.start(ctx, spec{name: "delegation", cmd: []string{"sh", "-c", "cat /sys/fs/cgroup/cgroup.controllers; cat /sys/fs/cgroup/cpuset.cpus.effective"}, memMB: 64, cpus: 1, pids: 64, cpuset: "0-1"})
	if err != nil {
		return fail("delegation", "%v", err)
	}
	defer e.remove(id)
	if _, err := e.waitExit(ctx, id, 30*time.Second); err != nil {
		return fail("delegation", "%v", err)
	}
	logs := strings.Fields(e.logs(ctx, id))
	text := strings.Join(logs, " ")
	for _, controller := range []string{"cpu", "cpuset", "io", "memory", "pids"} {
		if !strings.Contains(" "+text+" ", " "+controller+" ") {
			return fail("delegation", "controller %s is not available inside the container: %q", controller, text)
		}
	}
	if len(logs) == 0 || logs[len(logs)-1] != "0-1" {
		return fail("delegation", "cpuset.cpus.effective is not 0-1: %q", text)
	}
	return pass("delegation", map[string]any{"controllers": text}, "the container sees cpu, cpuset, io, memory and pids, and a cpuset of 0-1 is applied")
}

func (e *env) checkPIDs(ctx context.Context) result {
	const limit = 64
	script := fmt.Sprintf(`[ "$(cat /sys/fs/cgroup/pids.max)" = "%d" ] || { echo "NOT-ENFORCED pids.max=$(cat /sys/fs/cgroup/pids.max)"; exit 3; }
echo "pids.max=%d"
timeout 3 sh -c 'b() { b | b & }; b' >/dev/null 2>&1
sleep 30`, limit, limit)
	id, err := e.start(ctx, spec{name: "pids", cmd: []string{"sh", "-c", script}, memMB: 128, cpus: 1, pids: limit})
	if err != nil {
		return fail("pids", "%v", err)
	}
	defer e.remove(id)
	time.Sleep(1 * time.Second)
	if logs := e.logs(ctx, id); strings.Contains(logs, "NOT-ENFORCED") {
		return fail("pids", "the limit was not applied, the fork bomb never ran: %s", strings.TrimSpace(logs))
	}
	var peak uint64
	stats, err := e.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: true})
	if err == nil {
		decoder := json.NewDecoder(stats.Body)
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			var frame container.StatsResponse
			if decoder.Decode(&frame) != nil {
				break
			}
			peak = max(peak, frame.PidsStats.Current)
		}
		stats.Body.Close()
	}
	inspected, ierr := e.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if ierr != nil {
		return fail("pids", "inspect: %v", ierr)
	}
	dir := hostCgroup(inspected.Container.State.Pid)
	scope := strings.TrimSuffix(dir, "/container")
	hostPeak, events := fileText(scope+"/pids.peak"), strings.ReplaceAll(fileText(scope+"/pids.events"), "\n", " ")
	started := time.Now()
	_, _ = e.cli.ContainerKill(ctx, id, client.ContainerKillOptions{})
	_, _ = e.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	data := map[string]any{"limit": limit, "api_peak": peak, "cgroup_peak": hostPeak, "pids_events": events, "cleanup_ms": time.Since(started).Milliseconds()}
	hostPeakN, _ := strconv.Atoi(hostPeak)
	if hostPeakN == 0 || hostPeakN > limit || !strings.Contains(events, "max ") || strings.Contains(events, "max 0") {
		return result{Name: "pids", Data: data, Detail: fmt.Sprintf("the fork bomb was not stopped at %d: cgroup pids.peak=%s events=%q", limit, hostPeak, events)}
	}
	return pass("pids", data, "fork bomb stopped at the limit: pids.peak=%d of %d, fork refused (pids.events: %s), killed and removed in %dms", hostPeakN, limit, events, time.Since(started).Milliseconds())
}

// checkMemory runs the hog as a CHILD of a shell, which is the real shape: the agent is PID 1 and the student's program is
// a child. The evidence that the limit held is the cgroup's own memory.events (oom_kill) and the last allocation the hog
// reached, not State.OOMKilled: Podman 4.9.3 does not set that flag (see DISCOVERY.md), so it is only recorded.
func (e *env) checkMemory(ctx context.Context) result {
	const limitMB = 128
	script := `/spike/spike load mem 1024 >/tmp/out 2>&1; echo "workload-exit=$?"; tail -n 2 /tmp/out
cat /sys/fs/cgroup/memory.events; echo "memory.max=$(cat /sys/fs/cgroup/memory.max) swap.max=$(cat /sys/fs/cgroup/memory.swap.max)"`
	id, err := e.start(ctx, spec{name: "memory", cmd: []string{"sh", "-c", script}, memMB: limitMB, cpus: 1, pids: 64})
	if err != nil {
		return fail("memory", "%v", err)
	}
	defer e.remove(id)
	inspected, err := e.waitExit(ctx, id, 60*time.Second)
	if err != nil {
		return fail("memory", "%v", err)
	}
	logs := e.logs(ctx, id)
	field := func(prefix string) string {
		for _, line := range strings.Split(logs, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
				return rest
			}
		}
		return ""
	}
	oomKills, _ := strconv.Atoi(strings.TrimSpace(field("oom_kill ")))
	lastAlloc := 0
	for _, line := range strings.Split(logs, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "alloc="); ok {
			lastAlloc, _ = strconv.Atoi(rest)
		}
	}
	data := map[string]any{"limit_mb": limitMB, "workload_exit": field("workload-exit="), "oom_kill_events": oomKills, "last_alloc_mb": lastAlloc,
		"api_oom_killed_flag": inspected.State.OOMKilled, "limits": field("memory.max=")}
	if strings.Contains(logs, "NOT-ENFORCED") || strings.Contains(logs, "SURVIVED") {
		return result{Name: "memory", Data: data, Detail: "the memory limit was not enforced: " + strings.TrimSpace(logs)}
	}
	if field("workload-exit=") != "137" || oomKills < 1 || lastAlloc > limitMB || !strings.Contains(logs, "swap.max=0") {
		return result{Name: "memory", Data: data, Detail: fmt.Sprintf("expected exit 137, oom_kill>=1, last alloc <= %d MiB and no swap; got %+v", limitMB, data)}
	}
	return pass("memory", data, "memory hog killed at the %d MiB limit (last allocation %d MiB, memory.events oom_kill=%d, exit 137, swap off); API OOMKilled flag=%v",
		limitMB, lastAlloc, oomKills, inspected.State.OOMKilled)
}

func (e *env) checkCPU(ctx context.Context) result {
	id, err := e.start(ctx, spec{name: "cpu", cmd: []string{"/spike/spike", "load", "cpu", "4", "6"}, memMB: 128, cpus: 1, pids: 64})
	if err != nil {
		return fail("cpu", "%v", err)
	}
	defer e.remove(id)
	if _, err := e.waitExit(ctx, id, 60*time.Second); err != nil {
		return fail("cpu", "%v", err)
	}
	logs := e.logs(ctx, id)
	got := lastJSON(logs)
	if got == nil {
		return fail("cpu", "no result from the workload: %s", strings.TrimSpace(logs))
	}
	cores, _ := got["cores"].(float64)
	throttled, _ := got["nr_throttled"].(float64)
	if cores < 0.8 || cores > 1.25 || throttled == 0 {
		return result{Name: "cpu", Data: got, Detail: fmt.Sprintf("4 busy threads used %.2f cores (throttled %.0f periods); expected about 1.0", cores, throttled)}
	}
	return pass("cpu", got, "4 busy threads held to %.2f cores of a 1-core limit (throttled in %.0f periods)", cores, throttled)
}

func (e *env) session(name string) string { return filepath.Join(e.workroot, name) }

func (e *env) checkDisk(ctx context.Context) result {
	id, err := e.start(ctx, spec{name: "disk", cmd: []string{"/spike/spike", "load", "fill", "/work"}, memMB: 128, cpus: 1, pids: 64, session: "spike-fill"})
	if err != nil {
		return fail("disk", "%v", err)
	}
	defer e.remove(id)
	if _, err := e.waitExit(ctx, id, 90*time.Second); err != nil {
		return fail("disk", "%v", err)
	}
	got := lastJSON(e.logs(ctx, id))
	if got == nil {
		return fail("disk", "no result from the workload")
	}
	written, _ := got["bytes"].(float64)
	edquot, _ := got["edquot"].(bool)
	enospc, _ := got["enospc"].(bool)
	const quotaMB = 50
	// XFS reports an exhausted PROJECT quota as ENOSPC, not EDQUOT (user and group quotas give EDQUOT). The agent must map both to QUOTA.
	if !(edquot || enospc) || written > quotaMB<<20 || written < 0.9*quotaMB*(1<<20) {
		return result{Name: "disk", Data: got, Detail: fmt.Sprintf("expected ENOSPC/EDQUOT near %d MiB, wrote %.1f MiB, err=%v", quotaMB, written/(1<<20), got["err"])}
	}
	return pass("disk", got, "filling /work stopped at the %d MiB project quota after %.1f MiB with %q (ENOSPC=%v EDQUOT=%v); statfs inside reports %.0f MiB total",
		quotaMB, written/(1<<20), got["err"], enospc, edquot, got["statfs_total"].(float64)/(1<<20))
}

func (e *env) checkInodes(ctx context.Context) result {
	id, err := e.start(ctx, spec{name: "inodes", cmd: []string{"/spike/spike", "load", "inodes", "/work", "100000"}, memMB: 128, cpus: 1, pids: 64, session: "spike-ino"})
	if err != nil {
		return fail("inodes", "%v", err)
	}
	defer e.remove(id)
	if _, err := e.waitExit(ctx, id, 90*time.Second); err != nil {
		return fail("inodes", "%v", err)
	}
	got := lastJSON(e.logs(ctx, id))
	if got == nil {
		return fail("inodes", "no result from the workload")
	}
	created, _ := got["created"].(float64)
	failure, _ := got["err"].(string)
	if failure == "" || created > 2000 || created < 1900 || !(strings.Contains(failure, "no space left") || strings.Contains(failure, "quota exceeded")) {
		return result{Name: "inodes", Data: got, Detail: fmt.Sprintf("expected a quota error near 2000 files, created %.0f, err=%v", created, got["err"])}
	}
	return pass("inodes", got, "creating empty files stopped at the 2000-inode project quota after %.0f files with %q", created, failure)
}

func counter(path string) int {
	n, _ := strconv.Atoi(fileText(path))
	return n
}

func (e *env) checkPause(ctx context.Context) result {
	tick := filepath.Join(e.session("spike-misc"), "tick")
	id, err := e.start(ctx, spec{name: "pause", cmd: []string{"/spike/spike", "load", "tick", "/work/tick"}, memMB: 64, cpus: 0.5, pids: 32, session: "spike-misc"})
	if err != nil {
		return fail("pause", "%v", err)
	}
	defer e.remove(id)
	time.Sleep(1500 * time.Millisecond)
	before := counter(tick)
	t0 := time.Now()
	if _, err := e.cli.ContainerPause(ctx, id, client.ContainerPauseOptions{}); err != nil {
		return fail("pause", "pause: %v", err)
	}
	pauseMS := time.Since(t0).Milliseconds()
	time.Sleep(300 * time.Millisecond)
	inspected, _ := e.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	frozenAt := counter(tick)
	time.Sleep(2500 * time.Millisecond)
	stillFrozen := counter(tick)
	// Podman applies the freezer to the container's own cgroup (the "container" child of the libpod scope).
	freezer := fileText(hostCgroup(inspected.Container.State.Pid) + "/cgroup.freeze")
	t1 := time.Now()
	if _, err := e.cli.ContainerUnpause(ctx, id, client.ContainerUnpauseOptions{}); err != nil {
		return fail("pause", "unpause: %v", err)
	}
	unpauseMS := time.Since(t1).Milliseconds()
	time.Sleep(1500 * time.Millisecond)
	resumed := counter(tick)
	data := map[string]any{"before": before, "frozen_at": frozenAt, "after_2_5s": stillFrozen, "resumed": resumed, "state": string(inspected.Container.State.Status), "cgroup.freeze": freezer, "pause_ms": pauseMS, "unpause_ms": unpauseMS}
	if inspected.Container.State.Status != "paused" || !inspected.Container.State.Paused || stillFrozen != frozenAt || resumed <= stillFrozen+5 {
		return result{Name: "pause", Data: data, Detail: fmt.Sprintf("state=%s, counter %d -> %d -> %d -> %d", inspected.Container.State.Status, before, frozenAt, stillFrozen, resumed)}
	}
	return pass("pause", data, "paused in %dms (cgroup.freeze=%s), counter frozen at %d for 2.5s, resumed to %d after unpause (%dms)", pauseMS, freezer, frozenAt, resumed, unpauseMS)
}

func (e *env) checkStats(ctx context.Context) result {
	id, err := e.start(ctx, spec{name: "stats", cmd: []string{"/spike/spike", "load", "hold", "80", "1"}, memMB: 256, cpus: 1, pids: 64})
	if err != nil {
		return fail("stats", "%v", err)
	}
	defer e.remove(id)
	time.Sleep(2 * time.Second)
	stream, err := e.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return fail("stats", "stream: %v", err)
	}
	decoder := json.NewDecoder(stream.Body)
	var frames []container.StatsResponse
	for len(frames) < 5 {
		var frame container.StatsResponse
		if err := decoder.Decode(&frame); err != nil {
			break
		}
		frames = append(frames, frame)
	}
	stream.Body.Close()
	if len(frames) < 4 {
		return fail("stats", "only %d frames arrived from the stream", len(frames))
	}
	first, last := frames[1], frames[len(frames)-1]
	seconds := last.Read.Sub(first.Read).Seconds()
	cores := float64(last.CPUStats.CPUUsage.TotalUsage-first.CPUStats.CPUUsage.TotalUsage) / 1e9 / seconds
	oneShot, oerr := e.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	var single container.StatsResponse
	if oerr == nil {
		_ = json.NewDecoder(oneShot.Body).Decode(&single)
		oneShot.Body.Close()
	}
	data := map[string]any{"frames": len(frames), "cores": cores, "memory_mb": last.MemoryStats.Usage >> 20, "memory_limit_mb": last.MemoryStats.Limit >> 20, "pids": last.PidsStats.Current, "one_shot_error": fmt.Sprint(oerr), "one_shot_memory_mb": single.MemoryStats.Usage >> 20}
	if cores < 0.5 || cores > 1.3 || last.MemoryStats.Usage>>20 < 70 || last.PidsStats.Current == 0 || oerr != nil {
		return result{Name: "stats", Data: data, Detail: fmt.Sprintf("cores=%.2f memory=%d MiB pids=%d one-shot error=%v", cores, last.MemoryStats.Usage>>20, last.PidsStats.Current, oerr)}
	}
	return pass("stats", data, "streamed %d frames: %.2f cores, %d MiB of a %d MiB limit, %d pids; a one-shot sample works too", len(frames), cores, last.MemoryStats.Usage>>20, last.MemoryStats.Limit>>20, last.PidsStats.Current)
}

type entry struct {
	Type byte
	Mode int64
	Size int64
	Link string
	Hash string
}

func readTar(r io.Reader) (map[string]entry, error) {
	out := map[string]entry{}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		item := entry{Type: header.Typeflag, Mode: header.Mode & 0o7777, Size: header.Size, Link: header.Linkname}
		switch header.Typeflag {
		case tar.TypeReg:
			sum := sha256.New()
			if _, err := io.Copy(sum, reader); err != nil {
				return out, err
			}
			item.Hash = hex.EncodeToString(sum.Sum(nil))[:12]
		case tar.TypeLink:
			// A hard link carries no data: tar writes the first name of an inode as a file and the others as links to it.
			// Which name comes first is the order of the directory, so resolve the link to see the content.
			target := out[header.Linkname]
			item.Hash, item.Size = target.Hash, target.Size
		}
		out[header.Name] = item
	}
}

func (e *env) archive(ctx context.Context, id, path string) (map[string]entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	copied, err := e.cli.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: path})
	if err != nil {
		return nil, err
	}
	defer copied.Content.Close()
	return readTar(copied.Content)
}

func (e *env) checkArchive(ctx context.Context) result {
	id, err := e.start(ctx, spec{name: "archive", cmd: []string{"/spike/spike", "load", "files", "/work"}, memMB: 64, cpus: 0.5, pids: 32, session: "spike-arch"})
	if err != nil {
		return fail("archive", "%v", err)
	}
	defer e.remove(id)
	for i := 0; i < 50 && !strings.HasPrefix(fileText(filepath.Join(e.session("spike-arch"), "READY")), "ok"); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	// "/work/." (the contents of the directory, as docker cp reads it) names the entries the same way whatever state the
	// container is in. "/work" does not: a running container gives "work/src/main.cpp", a paused one "/src/main.cpp".
	running, err := e.archive(ctx, id, "/work/.")
	if err != nil {
		return fail("archive", "from the running container: %v", err)
	}
	runningPlain, _ := e.archive(ctx, id, "/work")
	if _, err := e.cli.ContainerPause(ctx, id, client.ContainerPauseOptions{}); err != nil {
		return fail("archive", "pause: %v", err)
	}
	paused, err := e.archive(ctx, id, "/work/.")
	if err != nil {
		return fail("archive", "from the paused container: %v", err)
	}
	pausedPlain, _ := e.archive(ctx, id, "/work")
	sample := func(entries map[string]entry) []string {
		var out []string
		for name := range entries {
			out = append(out, name)
		}
		sort.Strings(out)
		return out[:min(4, len(out))]
	}
	sum := sha256.Sum256(fixtureMain())
	want := hex.EncodeToString(sum[:])[:12]
	var names []string
	for name := range running {
		names = append(names, name)
	}
	sort.Strings(names)
	var problems []string
	need := func(ok bool, what string) {
		if !ok {
			problems = append(problems, what)
		}
	}
	main := running["src/main.cpp"]
	need(main.Hash == want && main.Size == 200<<10, fmt.Sprintf("main.cpp content/size wrong: %+v", main))
	need(running["run.sh"].Mode == 0o755, fmt.Sprintf("run.sh mode %o", running["run.sh"].Mode))
	need(running["link"].Type == tar.TypeSymlink && running["link"].Link == "/etc/passwd", fmt.Sprintf("link is %+v", running["link"]))
	need(running["up"].Type == tar.TypeSymlink, fmt.Sprintf("up is %+v", running["up"]))
	need(running["pipe"].Type == tar.TypeFifo, fmt.Sprintf("pipe is %+v", running["pipe"]))
	hard := running["hard"]
	need(hard.Type == tar.TypeLink || hard.Hash == want, fmt.Sprintf("hard is %+v", hard))
	need(running["empty"].Type == tar.TypeDir, "empty dir missing")
	need(running["a b/ñ.txt"].Hash != "", "unicode name missing")
	for name := range running {
		need(!strings.Contains(name, "..") && !strings.HasPrefix(name, "/"), "unsafe name "+name)
	}
	var differences []string
	for name, item := range running {
		if other, ok := paused[name]; !ok || other != item {
			differences = append(differences, fmt.Sprintf("%s: running %+v, paused %+v", name, item, other))
		}
	}
	for name := range paused {
		if _, ok := running[name]; !ok {
			differences = append(differences, name+": only in the paused archive")
		}
	}
	sort.Strings(differences)
	need(len(differences) == 0, "the archive of the paused container differs from the running one: "+strings.Join(differences, " | "))
	// What does the API do with a path that goes through a hostile symlink? It must stay inside the container.
	viaLink, linkErr := e.archive(ctx, id, "/work/link")
	var viaLinkNames []string
	for name := range viaLink {
		viaLinkNames = append(viaLinkNames, name)
	}
	data := map[string]any{"entries": len(running), "names": names, "via_symlink_names": viaLinkNames, "via_symlink_error": fmt.Sprint(linkErr),
		"path_/work_running_names": sample(runningPlain), "path_/work_paused_names": sample(pausedPlain)}
	if len(problems) > 0 {
		return result{Name: "archive", Data: data, Detail: strings.Join(problems, "; ")}
	}
	return pass("archive", data, "tar of /work/. from a running and a paused container are identical (%d entries); content hash and modes survive; symlinks, a hard link and a FIFO come through as entries, never followed. GOTCHA: path /work names entries %v when running but %v when paused",
		len(running), sample(runningPlain)[:2], sample(pausedPlain)[:2])
}

func (e *env) checkSocket(ctx context.Context) result {
	dir := filepath.Join(e.runDir, "sock1")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fail("socket", "%v", err)
	}
	_ = os.Chmod(dir, 0o777)
	id, err := e.start(ctx, spec{name: "socket", cmd: []string{"/spike/spike", "load", "sock", "/run/cbe/agent.sock"}, memMB: 64, cpus: 0.5, pids: 32, runSock: dir})
	if err != nil {
		return fail("socket", "%v", err)
	}
	defer e.remove(id)
	path := filepath.Join(dir, "agent.sock")
	var conn net.Conn
	for i := 0; i < 50; i++ {
		if conn, err = net.DialTimeout("unix", path, time.Second); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return fail("socket", "the host cannot connect to the socket the container's user created: %v", err)
	}
	defer conn.Close()
	_, _ = fmt.Fprint(conn, "ping\n")
	reply, _ := bufio.NewReader(conn).ReadString('\n')
	info, _ := os.Stat(path)
	data := map[string]any{"reply": strings.TrimSpace(reply), "socket_mode": info.Mode().String(), "dir_mode": "0777"}
	if strings.TrimSpace(reply) != "pong:ping" {
		return result{Name: "socket", Data: data, Detail: fmt.Sprintf("unexpected reply %q", reply)}
	}
	return pass("socket", data, "the host (cbe-runner) talks to a Unix socket made by the container's uid 1000 through a bind-mounted folder; reply %q", strings.TrimSpace(reply))
}

func cgroupNumber(path string) int64 {
	n, _ := strconv.ParseInt(fileText(path), 10, 64)
	return n
}

// checkDensity measures what N idle, fully limited containers cost the host and how fast the runner can bring them up.
// It is the "idle" half of the sizing estimate; sizing.sh measures the compile storm.
func (e *env) checkDensity(ctx context.Context) result {
	slice := fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice", os.Getuid())
	before := cgroupNumber(slice + "/memory.current")
	var ids []string
	var creates, starts []float64
	for i := 0; i < e.density; i++ {
		s := spec{name: fmt.Sprintf("density-%02d", i), cmd: []string{"/spike/spike", "load", "sleep"}, memMB: 1024, cpus: 1, pids: 256}
		t0 := time.Now()
		id, err := e.create(ctx, s)
		if err != nil {
			return fail("density", "container %d: create: %v", i, err)
		}
		creates = append(creates, time.Since(t0).Seconds()*1000)
		ids = append(ids, id)
		t1 := time.Now()
		if _, err := e.cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
			return fail("density", "container %d: start: %v", i, err)
		}
		starts = append(starts, time.Since(t1).Seconds()*1000)
	}
	time.Sleep(3 * time.Second)
	after := cgroupNumber(slice + "/memory.current")
	list, _ := e.cli.ContainerList(ctx, client.ContainerListOptions{Filters: client.Filters{}.Add("label", "cbe.spike=1")})
	running := 0
	for _, item := range list.Items {
		if item.State == "running" {
			running++
		}
	}
	removeStart := time.Now()
	for _, id := range ids {
		e.remove(id)
	}
	removeMS := float64(time.Since(removeStart).Milliseconds())
	sort.Float64s(creates)
	sort.Float64s(starts)
	median := func(v []float64) float64 { return v[len(v)/2] }
	n := float64(e.density)
	data := map[string]any{"containers": e.density, "running": running, "memory_before_mb": before >> 20, "memory_after_mb": after >> 20,
		"idle_overhead_mb_per_container": float64(after-before) / n / (1 << 20), "create_ms_median": median(creates), "create_ms_max": creates[len(creates)-1],
		"start_ms_median": median(starts), "start_ms_max": starts[len(starts)-1], "remove_all_ms": removeMS}
	if running != e.density {
		return result{Name: "density", Data: data, Detail: fmt.Sprintf("only %d of %d containers are running", running, e.density)}
	}
	return pass("density", data, "%d limited containers up: %.1f MiB of host memory each while idle, create %.0fms and start %.0fms (median), all removed in %.1fs",
		e.density, float64(after-before)/n/(1<<20), median(creates), median(starts), removeMS/1000)
}

func (e *env) rebootPrep(ctx context.Context) result {
	if _, err := e.start(ctx, spec{name: "reboot", cmd: []string{"/spike/spike", "load", "tick", "/work/tick"}, memMB: 64, cpus: 0.5, pids: 32, session: "spike-reboot", restart: true}); err != nil {
		return fail("reboot-prep", "%v", err)
	}
	time.Sleep(2 * time.Second)
	return pass("reboot-prep", nil, "container with restart policy 'always' is running and has written /work/tick (counter %d)", counter(filepath.Join(e.session("spike-reboot"), "tick")))
}

func (e *env) rebootVerify(ctx context.Context) result {
	deadline := time.Now().Add(60 * time.Second)
	var inspected client.ContainerInspectResult
	var err error
	for time.Now().Before(deadline) {
		if inspected, err = e.cli.ContainerInspect(ctx, "cbe-spike-reboot", client.ContainerInspectOptions{}); err == nil && inspected.Container.State.Running {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil || !inspected.Container.State.Running {
		return fail("reboot-verify", "the container did not come back after the user manager restarted: err=%v", err)
	}
	time.Sleep(2 * time.Second)
	starts := strings.Count(fileText(filepath.Join(e.session("spike-reboot"), "tick.starts")), "start")
	defer e.remove("cbe-spike-reboot")
	if starts < 2 {
		return fail("reboot-verify", "the container runs again but its process did not restart (starts=%d)", starts)
	}
	return pass("reboot-verify", map[string]any{"starts": starts}, "after the user manager restarted, the container came back by itself, /work survived and the process started %d times", starts)
}
