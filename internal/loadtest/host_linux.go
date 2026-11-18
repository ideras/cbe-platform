//go:build linux

package loadtest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Host reads what a Linux machine is doing, for the load test's time series: the whole machine, some processes (by pid or by name) and some
// control groups (the slice the containers run in). Everything is read from /proc and /sys/fs/cgroup, which any user may read.
type Host struct {
	// Pids and Names say which processes to follow: a label and a pid, or a label and a program's name (the first process of that name).
	Pids   map[string]int
	Names  map[string]string
	Groups map[string]string // a label and a control group's folder, such as /sys/fs/cgroup/user.slice/user-<uid>.slice
	// Extra adds readings of the caller's own (the runner's statistics of the containers, for one).
	Extra func() map[string]float64

	lastAt    time.Time
	lastStat  [2]uint64 // busy and total ticks of the machine
	lastProc  map[string]uint64
	lastGroup map[string]uint64
}

var tickSeconds = 0.01 // USER_HZ is 100 on every Linux the course runs on

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func machineTicks() (busy, total uint64) {
	fields := strings.Fields(strings.SplitN(readFile("/proc/stat"), "\n", 2)[0])
	if len(fields) < 8 || fields[0] != "cpu" {
		return 0, 0
	}
	for i, field := range fields[1:] {
		value, _ := strconv.ParseUint(field, 10, 64)
		if i > 7 {
			break
		}
		total += value
		if i != 3 && i != 4 { // idle and iowait are not work
			busy += value
		}
	}
	return busy, total
}

func processTicks(pid int) (ticks uint64, rssMB float64, ok bool) {
	stat := readFile(fmt.Sprintf("/proc/%d/stat", pid))
	end := strings.LastIndex(stat, ")")
	if end < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 22 {
		return 0, 0, false
	}
	user, _ := strconv.ParseUint(fields[11], 10, 64)
	system, _ := strconv.ParseUint(fields[12], 10, 64)
	pages, _ := strconv.ParseUint(fields[21], 10, 64)
	return user + system, float64(pages) * float64(os.Getpagesize()) / (1 << 20), true
}

func findProcess(name string) int {
	entries, _ := filepath.Glob("/proc/[0-9]*/comm")
	best := 0
	for _, entry := range entries {
		if strings.TrimSpace(readFile(entry)) != name {
			continue
		}
		pid, _ := strconv.Atoi(strings.Split(entry, "/")[2])
		if best == 0 || pid < best {
			best = pid
		}
	}
	return best
}

func groupUsage(dir string) (usec uint64, ok bool) {
	for _, line := range strings.Split(readFile(filepath.Join(dir, "cpu.stat")), "\n") {
		if rest, found := strings.CutPrefix(line, "usage_usec "); found {
			value, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 64)
			return value, err == nil
		}
	}
	return 0, false
}

func number(path string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(readFile(path)), 64)
	return value
}

// Sample reads everything once. CPU figures are percentages of one core (200 is two cores busy) over the time since the last reading, so the
// first reading has none.
func (h *Host) Sample() map[string]float64 {
	values := map[string]float64{}
	now := time.Now()
	elapsed := now.Sub(h.lastAt).Seconds()
	first := h.lastAt.IsZero()
	if h.lastProc == nil {
		h.lastProc, h.lastGroup = map[string]uint64{}, map[string]uint64{}
	}
	if fields := strings.Fields(readFile("/proc/loadavg")); len(fields) >= 3 {
		values["load1"], _ = strconv.ParseFloat(fields[0], 64)
	}
	busy, total := machineTicks()
	if !first && total > h.lastStat[1] {
		values["machine_cpu_busy_percent"] = 100 * float64(busy-h.lastStat[0]) / float64(total-h.lastStat[1])
	}
	h.lastStat = [2]uint64{busy, total}
	for _, line := range strings.Split(readFile("/proc/pressure/cpu"), "\n") {
		if rest, found := strings.CutPrefix(line, "some "); found {
			for _, field := range strings.Fields(rest) {
				if value, found := strings.CutPrefix(field, "avg10="); found {
					values["cpu_pressure_some_avg10"], _ = strconv.ParseFloat(value, 64)
				}
			}
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(readFile("/proc/meminfo")))
	for scanner.Scan() {
		if rest, found := strings.CutPrefix(scanner.Text(), "MemAvailable:"); found {
			kb, _ := strconv.ParseFloat(strings.Fields(rest)[0], 64)
			values["mem_available_mb"] = kb / 1024
		}
	}
	pids := map[string]int{}
	for label, pid := range h.Pids {
		pids[label] = pid
	}
	for label, name := range h.Names {
		pids[label] = findProcess(name)
	}
	for label, pid := range pids {
		ticks, rss, ok := processTicks(pid)
		if !ok {
			continue
		}
		values[label+"_rss_mb"] = rss
		if last, seen := h.lastProc[label]; seen && !first && elapsed > 0 && ticks >= last {
			values[label+"_cpu_percent"] = 100 * float64(ticks-last) * tickSeconds / elapsed
		}
		h.lastProc[label] = ticks
	}
	for label, dir := range h.Groups {
		usage, ok := groupUsage(dir)
		if !ok {
			continue
		}
		if last, seen := h.lastGroup[label]; seen && !first && elapsed > 0 && usage >= last {
			values[label+"_cpu_percent"] = 100 * float64(usage-last) / 1e6 / elapsed
		}
		h.lastGroup[label] = usage
		values[label+"_mem_mb"] = number(filepath.Join(dir, "memory.current")) / (1 << 20)
		values[label+"_pids"] = number(filepath.Join(dir, "pids.current"))
	}
	if h.Extra != nil {
		for name, value := range h.Extra() {
			values[name] = value
		}
	}
	h.lastAt = now
	return values
}
