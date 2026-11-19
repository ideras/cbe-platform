package imagecheck

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"exam-platform/internal/runner"
)

// LabelAgentVersion is the label of a finished image that says which release its agent is (runner.LabelAgent has its hash).
const LabelAgentVersion = "io.cbe.agent.version"

// Agent is an agent file that may go in an image (decision D-CB-9). It is the first process of every student's container, so it is looked at before
// anything is built: a Linux program for the machine the images are built on, static (it runs in any base, Alpine's too), and cbe-agent itself, not
// another program named by mistake.
type Agent struct {
	Data []byte
	// Sum is its SHA-256, as the image's label has it, and Arch the machine it is for in Go's words (amd64, arm64).
	Sum, Arch string
	// Version is what the agent says it is when asked (-version). It is empty when it could not be asked, and VersionProblem says why.
	Version, VersionProblem string
}

var elfArch = map[elf.Machine]string{elf.EM_X86_64: "amd64", elf.EM_AARCH64: "arm64", elf.EM_386: "386", elf.EM_ARM: "arm", elf.EM_RISCV: "riscv64",
	elf.EM_PPC64: "ppc64le", elf.EM_S390: "s390x", elf.EM_LOONGARCH: "loong64"}

// InspectAgent says whether data is an agent that can go in the images of a machine of arch, without running it.
func InspectAgent(data []byte, arch string) (Agent, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return Agent{}, errors.New("it is not a Linux program (not an ELF file)")
	}
	defer f.Close()
	got, known := elfArch[f.Machine]
	if !known {
		got = strings.ToLower(strings.TrimPrefix(f.Machine.String(), "EM_"))
	}
	if got != arch {
		return Agent{}, fmt.Errorf("it is a program for %s, and the images of this machine are %s: build one for this machine (make agent, or GOARCH=%s)", got, arch, arch)
	}
	static := errors.New("it is not static: it needs a dynamic loader and libraries, which a base such as Alpine does not have (build it with CGO_ENABLED=0, as make agent does)")
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return Agent{}, static
		}
	}
	if libraries, _ := f.ImportedLibraries(); len(libraries) > 0 {
		return Agent{}, static
	}
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return Agent{}, errors.New("it is not a Go program, so it is not cbe-agent")
	}
	if !strings.HasSuffix(info.Path, "/cmd/cbe-agent") {
		return Agent{}, fmt.Errorf("it is %s, not cbe-agent", info.Path)
	}
	for _, s := range info.Settings {
		switch {
		case s.Key == "GOOS" && s.Value != "linux":
			return Agent{}, fmt.Errorf("it is cbe-agent for %s, and an image is Linux", s.Value)
		case s.Key == "CGO_ENABLED" && s.Value != "0":
			return Agent{}, static
		}
	}
	return Agent{Data: data, Sum: runner.AgentSum(data), Arch: got}, nil
}

// ReadAgent reads the agent at path, inspects it (InspectAgent) and asks it its version.
func ReadAgent(ctx context.Context, path, arch string) (Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Agent{}, err
	}
	a, err := InspectAgent(data, arch)
	if err != nil {
		return Agent{}, fmt.Errorf("%s cannot go in an image: %w", path, err)
	}
	a.Version, a.VersionProblem = askVersion(ctx, data)
	return a, nil
}

// askVersion runs a copy of the agent with -version: the copy is what was inspected and what goes in the image. As root, the copy runs as nobody, for
// a program that has not run yet is not trusted yet. Not being able to ask is not the agent's fault (a temporary folder may not let programs run): the
// agent's self test in the finished image is what runs it for sure.
func askVersion(ctx context.Context, data []byte) (string, string) {
	dir, err := os.MkdirTemp("", "cbe-agent-")
	if err != nil {
		return "", err.Error()
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "cbe-agent")
	if err := os.WriteFile(file, data, 0o755); err != nil {
		return "", err.Error()
	}
	// nobody must reach the copy.
	if err := os.Chmod(dir, 0o755); err != nil {
		return "", err.Error()
	}
	if err := os.Chmod(file, 0o755); err != nil {
		return "", err.Error()
	}
	var out bytes.Buffer
	var errs strings.Builder
	for attempt := 0; ; attempt++ {
		asking, cancel := context.WithTimeout(ctx, 10*time.Second)
		cmd := exec.CommandContext(asking, file, "-version")
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = &out, &errs
		asNobody(cmd)
		err = cmd.Run()
		cancel()
		// A file just written can be busy for a moment while another thread of this program starts a process (it inherited the file open).
		if err != nil && attempt < 3 && strings.Contains(err.Error(), "text file busy") {
			out.Reset()
			errs.Reset()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		break
	}
	if err != nil {
		return "", strings.TrimSpace("it did not answer -version: " + err.Error() + " " + strings.TrimSpace(errs.String()))
	}
	version := strings.TrimSpace(out.String())
	if version == "" || len(version) > 100 || strings.IndexFunc(version, func(r rune) bool { return !unicode.IsPrint(r) || unicode.IsSpace(r) }) >= 0 {
		return "", "it did not answer -version with a version"
	}
	return version, ""
}
