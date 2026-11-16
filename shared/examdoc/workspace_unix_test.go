//go:build unix

package examdoc

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestARealNamedPipeIsRefusedWithoutBeingOpened(t *testing.T) {
	dir := copyPackage(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "project", "pipe"), 0o644); err != nil {
		t.Skipf("cannot make a named pipe: %v", err)
	}
	// Opening the pipe would block for ever: the compiler looks at what the file is first.
	if problems := dirProblems(t, dir); !strings.Contains(problems, "project/pipe: it is a named pipe") {
		t.Fatalf("problems:\n%s", problems)
	}
}

func TestAnExecutableBitOnTheAuthorsComputerIsKept(t *testing.T) {
	dir := copyPackage(t)
	for name, mode := range map[string]os.FileMode{"build-it": 0o755, "plain": 0o644} {
		if err := os.WriteFile(filepath.Join(dir, "project", name), []byte("no shebang"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, "project", name), mode); err != nil {
			t.Fatal(err)
		}
	}
	_, bundles, err := CompileWorkspaceDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]int64{}
	for _, header := range readTar(t, bundles.Starter.Data) {
		if header.Typeflag == tar.TypeReg {
			modes[header.Name] = header.Mode
		}
	}
	if modes["build-it"] != 0o755 || modes["plain"] != 0o644 {
		t.Fatalf("modes = %v", modes)
	}
}
