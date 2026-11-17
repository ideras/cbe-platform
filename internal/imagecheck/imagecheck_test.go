package imagecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exam-platform/internal/runner"
)

const pythonFile = "FROM docker.io/library/python:3.12-slim@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\nRUN pip install numpy==2.1.3\n"

// fakePodman is a container runtime that records what it is asked, reads the build contexts while they exist, and answers from what a test sets.
type fakePodman struct {
	calls     [][]string
	contexts  map[string]map[string]string // the files of each build context, by its last argument's base name
	buildErr  map[int]error                // a build (the first is 1) that fails
	builds    int
	inspected string // what `image inspect` prints; empty: an image that is as the runner finishes it
	selftest  string // what the agent prints
	runErr    error
	noPodman  bool
	agentSum  string
}

func (f *fakePodman) run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "version":
		if f.noPodman {
			return errors.New("executable file not found")
		}
		fmt.Fprint(stdout, "4.9.3")
	case "build":
		f.builds++
		dir := args[len(args)-1]
		files := map[string]string{}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			files[e.Name()] = string(data)
		}
		if f.contexts == nil {
			f.contexts = map[string]map[string]string{}
		}
		f.contexts[filepath.Base(dir)] = files
		fmt.Fprintf(stdout, "STEP 1/1: build %d\n", f.builds)
		return f.buildErr[f.builds]
	case "image":
		if f.inspected != "" {
			fmt.Fprint(stdout, f.inspected)
			return nil
		}
		fmt.Fprint(stdout, finishedImage(f.agentSum, "1000:1000", "", nil))
	case "run":
		if f.selftest != "" {
			fmt.Fprint(stdout, f.selftest)
		} else {
			fmt.Fprint(stdout, passing)
		}
		return f.runErr
	case "rmi":
	default:
		return fmt.Errorf("unexpected %v", args)
	}
	return nil
}

func (f *fakePodman) called(name string) [][]string {
	var found [][]string
	for _, c := range f.calls {
		if c[0] == name {
			found = append(found, c)
		}
	}
	return found
}

const passing = `{"version":"test","ok":true,"checks":[{"name":"user","ok":true},{"name":"account","ok":true},{"name":"shell","ok":true,"detail":"/bin/sh"},{"name":"no-setuid-files","ok":true,"detail":"900 files looked at"}]}`

// finishedImage is `podman image inspect` of an image the runner finished.
func finishedImage(agentSum, user, workdir string, volumes []string) string {
	if workdir == "" {
		workdir = "/work"
	}
	vols := map[string]struct{}{}
	for _, v := range volumes {
		vols[v] = struct{}{}
	}
	data, _ := json.Marshal([]map[string]any{{"Id": "abc123", "Config": map[string]any{"User": user, "Entrypoint": []string{runner.AgentPath}, "WorkingDir": workdir,
		"Labels": map[string]string{runner.LabelAgent: agentSum}, "Volumes": vols}}})
	return string(data)
}

type rig struct {
	dir, agent string
	fake       *fakePodman
	out        *bytes.Buffer
	options    Options
}

func newRig(t *testing.T, containerfile string) *rig {
	t.Helper()
	dir := t.TempDir()
	image := filepath.Join(dir, "image")
	if err := os.MkdirAll(filepath.Join(image, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"Containerfile": containerfile, "data/train.csv": "a,b\n1,2\n"} {
		if err := os.WriteFile(filepath.Join(image, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agent := filepath.Join(t.TempDir(), "cbe-agent")
	if err := os.WriteFile(agent, []byte("THE AGENT"), 0o755); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(ca, []byte("CERTS"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakePodman{agentSum: runner.AgentSum([]byte("THE AGENT"))}
	out := &bytes.Buffer{}
	return &rig{dir: dir, agent: agent, fake: fake, out: out, options: Options{Dir: dir, Agent: agent, Podman: fake.run, CABundle: ca, Out: out}}
}

func (r *rig) check(t *testing.T) Result {
	t.Helper()
	result, err := Check(context.Background(), r.options)
	if err != nil {
		t.Fatalf("Check: %v\n%s", err, r.out)
	}
	return result
}

func TestAnImageThatKeepsTheContractPassesAndLeavesNothingBehind(t *testing.T) {
	r := newRig(t, pythonFile)
	result := r.check(t)
	if !result.OK || result.Problem != "" || len(result.Checks) != 4 {
		t.Fatalf("%+v\n%s", result, r.out)
	}
	if result.Image != "" {
		t.Errorf("the image was removed, and the result still names it: %q", result.Image)
	}
	// The author's build gets the folder as it is, and the host's certificates as ca.crt.
	author := r.fake.contexts["author"]
	if author["Containerfile"] != pythonFile || author["ca.crt"] != "CERTS" || author["data"] != "" {
		t.Errorf("the author's context: %v", author)
	}
	if data, err := os.ReadFile(filepath.Join(r.dir, "image", "data", "train.csv")); err != nil || string(data) != "a,b\n1,2\n" {
		t.Error("the author's folder was changed")
	}
	// The finishing is the runner's: its Containerfile, its script, the agent, on top of the author's image.
	finish := r.fake.contexts["finish"]
	built := r.fake.called("build")
	if len(built) != 2 || !strings.Contains(finish["Containerfile"], "FROM localhost/cbe-check/") || !strings.Contains(finish["Containerfile"], ":author\n") ||
		finish["cbe-agent"] != "THE AGENT" || finish["cbe-finalize.sh"] == "" || !strings.Contains(finish["Containerfile"], "USER 1000:1000") {
		t.Errorf("the finishing context: %v", finish)
	}
	for _, b := range built {
		if !strings.Contains(strings.Join(b, " "), "--format docker") {
			t.Errorf("a build in OCI format would ignore HEALTHCHECK NONE: %v", b)
		}
	}
	// The self test runs as a student's container is run.
	runs := r.fake.called("run")
	if len(runs) != 1 {
		t.Fatalf("runs: %v", runs)
	}
	line := strings.Join(runs[0], " ")
	for _, want := range []string{"--user 1000:1000", "--network none", "--read-only", "--cap-drop all", "--security-opt no-new-privileges", "--entrypoint " + runner.AgentPath, "-selftest"} {
		if !strings.Contains(line, want) {
			t.Errorf("the self test run lacks %q: %s", want, line)
		}
	}
	// Both images are removed.
	removed := r.fake.called("rmi")
	if len(removed) != 1 || !strings.Contains(strings.Join(removed[0], " "), ":finished") || !strings.Contains(strings.Join(removed[0], " "), ":author") {
		t.Errorf("rmi: %v", removed)
	}
	for _, want := range []string{"rules       ok", "image       ok", "self test   ok    user"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, r.out)
		}
	}
}

func TestKeepLeavesTheImagesToLookInside(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Keep = true
	result := r.check(t)
	if !result.OK || !strings.HasPrefix(result.Image, "localhost/cbe-check/") || len(r.fake.called("rmi")) != 0 {
		t.Errorf("%+v %v", result, r.fake.called("rmi"))
	}
}

func TestTheRulesOfTheContainerfileAreCheckedBeforeAnythingIsBuilt(t *testing.T) {
	for name, text := range map[string]string{
		"a volume":   pythonFile + "VOLUME /data\n",
		"a variable": "ARG V=1\nFROM python:$V\n",
		"a flag":     "FROM --platform=linux/amd64 python:3.12\n",
	} {
		r := newRig(t, text)
		result := r.check(t)
		if result.OK || !strings.Contains(result.Problem, "the Containerfile is not accepted") || len(r.fake.calls) != 0 {
			t.Errorf("%s: %+v, podman was called %v", name, result, r.fake.calls)
		}
	}
	// Warnings are said, and do not fail.
	r := newRig(t, "FROM python:3.12-slim\nRUN pip install numpy\n")
	result := r.check(t)
	if !result.OK || len(result.Warnings) == 0 || !strings.Contains(r.out.String(), "warning: Containerfile") {
		t.Errorf("warnings: %+v\n%s", result, r.out)
	}
}

func TestTheDecisionsOfTheServerAreKept(t *testing.T) {
	r := newRig(t, "FROM ghcr.io/acme/lab@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n")
	r.options.Config = &runner.Config{CustomBase: false}
	if result := r.check(t); result.OK || !strings.Contains(result.Problem, "custom_base is off") || len(r.fake.calls) != 0 {
		t.Errorf("custom_base off: %+v", result)
	}
	r.options.Config = &runner.Config{CustomBase: true, BaseRegistries: []string{"docker.io", "quay.io"}}
	if result := r.check(t); result.OK || !strings.Contains(result.Problem, "does not take images from ghcr.io; it allows docker.io, quay.io") || len(r.fake.calls) != 0 {
		t.Errorf("registries: %+v", result)
	}
	r.options.Config = &runner.Config{CustomBase: true, BaseRegistries: []string{"ghcr.io"}}
	if result := r.check(t); !result.OK {
		t.Errorf("an allowed registry: %+v\n%s", result, r.out)
	}
}

func TestABuildThatFailsIsSaidAndStillCleansUp(t *testing.T) {
	r := newRig(t, pythonFile)
	r.fake.buildErr = map[int]error{1: errors.New("exit status 100")}
	result := r.check(t)
	if result.OK || !strings.Contains(result.Problem, "the build of the Containerfile failed: exit status 100") || len(r.fake.called("run")) != 0 || len(r.fake.called("rmi")) != 1 {
		t.Errorf("%+v %v", result, r.fake.calls)
	}
	r = newRig(t, pythonFile)
	r.fake.buildErr = map[int]error{2: errors.New("exit status 1")}
	if result := r.check(t); result.OK || !strings.Contains(result.Problem, "could not finish the image (the base has to be a Linux image with /bin/sh)") {
		t.Errorf("the finishing: %+v", result)
	}
}

func TestAnImageThatDoesNotKeepTheContractIsNotAccepted(t *testing.T) {
	agentSum := runner.AgentSum([]byte("THE AGENT"))
	for name, test := range map[string]struct{ inspected, want string }{
		"a volume":      {finishedImage(agentSum, "1000:1000", "", []string{"/var/lib/redis"}), "declares the volume(s) /var/lib/redis"},
		"root":          {finishedImage(agentSum, "root", "", nil), `the user of the image is "root"`},
		"a workdir":     {finishedImage(agentSum, "1000:1000", "/srv", nil), `the working folder of the image is "/srv"`},
		"another agent": {finishedImage("deadbeef", "1000:1000", "", nil), "does not say which agent the image holds"},
	} {
		r := newRig(t, pythonFile)
		r.fake.inspected = test.inspected
		result := r.check(t)
		if result.OK || !strings.Contains(result.Problem, test.want) || len(r.fake.called("run")) != 0 {
			t.Errorf("%s: %+v", name, result)
		}
	}
}

func TestAFailedSelfTestSaysWhichChecks(t *testing.T) {
	r := newRig(t, pythonFile)
	r.fake.selftest = `{"version":"t","ok":false,"checks":[{"name":"user","ok":true},{"name":"no-sudo","ok":false,"detail":"sudo is in the image"}]}`
	result := r.check(t)
	if result.OK || !strings.Contains(result.Problem, "no-sudo: sudo is in the image") || len(result.Checks) != 2 || result.Checks[1].OK {
		t.Errorf("%+v", result)
	}
	if !strings.Contains(r.out.String(), "self test   FAIL  no-sudo: sudo is in the image") {
		t.Errorf("the report:\n%s", r.out)
	}
	// An agent that did not run at all (not the right architecture, no /proc).
	r = newRig(t, pythonFile)
	r.fake.selftest = "exec format error"
	r.fake.runErr = errors.New("exit status 126")
	if result := r.check(t); result.OK || !strings.Contains(result.Problem, "the agent in the image did not run") || !strings.Contains(result.Problem, "exit status 126") || !strings.Contains(result.Problem, "built for this machine's architecture") {
		t.Errorf("an agent that did not run: %+v", result)
	}
}

func TestWhatStopsTheCheckIsAnErrorNotAnImageThatFailed(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Agent = filepath.Join(r.dir, "no-agent")
	if _, err := Check(context.Background(), r.options); err == nil || !strings.Contains(err.Error(), "--agent names a static cbe-agent") {
		t.Errorf("no agent: %v", err)
	}
	r = newRig(t, pythonFile)
	r.fake.noPodman = true
	if _, err := Check(context.Background(), r.options); err == nil || !strings.Contains(err.Error(), "is podman installed") {
		t.Errorf("no podman: %v", err)
	}
	r = newRig(t, pythonFile)
	r.options.Dir = filepath.Join(r.dir, "nothing-here")
	if _, err := Check(context.Background(), r.options); err == nil {
		t.Error("a folder that is not there")
	}
}

func TestTheFolderMayBeThePackageTheImageFolderOrTheFile(t *testing.T) {
	r := newRig(t, pythonFile)
	for name, dir := range map[string]string{"package": r.dir, "image folder": filepath.Join(r.dir, "image"), "the file": filepath.Join(r.dir, "image", "Containerfile")} {
		r.options.Dir = dir
		r.fake.calls, r.fake.builds = nil, 0
		if result := r.check(t); !result.OK || r.fake.contexts["author"]["Containerfile"] != pythonFile {
			t.Errorf("%s: %+v", name, result)
		}
	}
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "Dockerfile"), []byte("FROM x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.options.Dir = empty
	if _, err := Check(context.Background(), r.options); err == nil || !strings.Contains(err.Error(), "is not called Dockerfile here") {
		t.Errorf("a Dockerfile: %v", err)
	}
}

func TestALinkInTheBuildContextIsRefused(t *testing.T) {
	r := newRig(t, pythonFile)
	if err := os.Symlink("/etc/passwd", filepath.Join(r.dir, "image", "data", "link")); err != nil {
		t.Skip("links cannot be made here:", err)
	}
	if _, err := Check(context.Background(), r.options); err == nil || !strings.Contains(err.Error(), "holds no links") {
		t.Errorf("%v", err)
	}
}
