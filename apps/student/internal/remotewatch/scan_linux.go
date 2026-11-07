//go:build linux

package remotewatch

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// processNames are the names of the running programs, from /proc: the program a process was started as and the short name the
// kernel keeps for it. The first is the whole name; the second is cut to 15 characters but is there when the first is not
// readable.
func processNames() ([]string, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	var names []string
	// One buffer serves every file of the scan, as there are hundreds of processes and the scan is made every few seconds.
	buffer := make([]byte, 4096)
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		if data, err := readHead(filepath.Join(dir, "cmdline"), buffer); err == nil {
			if first, _, _ := bytes.Cut(data, []byte{0}); len(first) > 0 {
				names = append(names, filepath.Base(string(first)))
			}
		}
		if data, err := readHead(filepath.Join(dir, "comm"), buffer); err == nil {
			if name := string(bytes.TrimSpace(data)); name != "" {
				names = append(names, name)
			}
		}
	}
	return names, true
}

// readHead reads at most len(buffer) bytes of a file into buffer, and answers what it read, which is only good until the buffer is
// used again.
func readHead(path string, buffer []byte) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	read, err := file.Read(buffer)
	if read > 0 {
		return buffer[:read], nil
	}
	return nil, err
}
