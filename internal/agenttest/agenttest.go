// Package agenttest builds the programs the tests of the image commands put in images: a real cbe-agent (static, for Linux), so that what checks an
// agent before a build is tested on the file it will be given. Only tests import it.
package agenttest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// Version is the version the programs are built with, which the agent says when it is asked (-version).
const Version = "v0.0.0-test"

var (
	mu    sync.Mutex
	built = map[string][]byte{}
)

// Agent is cbe-agent built for Linux on arch (amd64, arm64), as the installer builds it. It is built once per test program and architecture.
func Agent(t testing.TB, arch string) []byte {
	return Program(t, "cbe-agent", arch)
}

// Program is the command ./cmd/<name> of this repository built static for Linux on arch.
func Program(t testing.TB, name, arch string) []byte {
	t.Helper()
	data, err := Build(name, arch)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Build is Program for a caller that has no test at hand (a fixture made outside a test function).
func Build(name, arch string) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	key := name + "/" + arch
	if data, ok := built[key]; ok {
		return data, nil
	}
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("agenttest: where the repository is cannot be told")
	}
	root := filepath.Join(filepath.Dir(here), "..", "..")
	goCommand, err := exec.LookPath("go")
	if err != nil {
		goCommand = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	dir, err := os.MkdirTemp("", "agenttest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, name)
	cmd := exec.Command(goCommand, "build", "-trimpath", "-ldflags", "-s -w -buildid= -X main.version="+Version, "-o", out, "./cmd/"+name)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch, "GOFLAGS=")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("agenttest: building %s for linux/%s: %v\n%s", name, arch, err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	built[key] = data
	return data, nil
}
