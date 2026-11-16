package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The student's application never talks to the container runtime, a container or its agent (plan, rule 9 and the Phase 8 checklist): its only way
// to a workspace is the exam server's WebSocket. Two things keep it so, and this test checks both: the application cannot even link the code that
// would (no container runtime, no agent protocol, nothing of the exam server's internals), and its source opens no Unix socket and no network
// address of its own.
func TestTheApplicationCannotReachTheRuntimeOrTheAgent(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/student")
	command.Dir = root
	out, err := command.Output()
	if err != nil {
		t.Skipf("go list is not available here: %v", err)
	}
	forbidden := regexp.MustCompile(`docker|moby|podman|containerd|libpod|buildah|opencontainers|exam-platform/internal/|exam-platform/cmd/`)
	linked := 0
	for _, pkg := range strings.Fields(string(out)) {
		linked++
		if forbidden.MatchString(pkg) {
			t.Errorf("the student application links %s, which has to do with containers or the exam server's inside", pkg)
		}
	}
	t.Logf("the student application links %d packages, none a container runtime or the agent's protocol", linked)

	// It dials the exam server (a URL it was given) and nothing else: no Unix socket, no fixed address.
	dials := regexp.MustCompile(`net\.Dial(Timeout)?\(|net\.Dialer|"unix"|"unixpacket"|\.sock"`)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "frontend" || entry.Name() == "bindings") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for number, line := range strings.Split(string(data), "\n") {
			if dials.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d opens a connection of its own: %s", path, number+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
