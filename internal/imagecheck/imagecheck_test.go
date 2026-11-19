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
	"runtime"
	"strings"
	"testing"

	"exam-platform/internal/agenttest"
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
	again     string // what the second `image inspect` prints, when it is not what the first did
	inspects  int
	selftest  string // what the agent prints
	runErr    error
	noPodman  bool
	agentSum  string
	modes     map[string]os.FileMode // the mode of each build context's folder and of its files, by "context/file" ("context" for the folder, "work" for its parent)
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
		if f.modes == nil {
			f.modes = map[string]os.FileMode{}
		}
		name := filepath.Base(dir)
		if info, err := os.Stat(filepath.Dir(dir)); err == nil {
			f.modes["work"] = info.Mode().Perm()
		}
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				f.modes[name+"/"+e.Name()] = info.Mode().Perm()
			}
		}
		fmt.Fprintf(stdout, "STEP 1/1: build %d\n", f.builds)
		return f.buildErr[f.builds]
	case "image":
		f.inspects++
		if f.inspects > 1 && f.again != "" {
			fmt.Fprint(stdout, f.again)
			return nil
		}
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
	case "rmi", "tag", "untag":
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
	agentData  []byte
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
	agentData := agenttest.Agent(t, runtime.GOARCH)
	agent := filepath.Join(t.TempDir(), "cbe-agent")
	if err := os.WriteFile(agent, agentData, 0o755); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(ca, []byte("CERTS"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakePodman{agentSum: runner.AgentSum(agentData)}
	out := &bytes.Buffer{}
	return &rig{dir: dir, agent: agent, agentData: agentData, fake: fake, out: out, options: Options{Dir: dir, Agent: agent, Podman: fake.run, CABundle: ca, Out: out}}
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
		finish["cbe-agent"] != string(r.agentData) || finish["cbe-finalize.sh"] == "" || !strings.Contains(finish["Containerfile"], "USER 1000:1000") {
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
	agentSum := newRig(t, pythonFile).fake.agentSum
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
	if result := r.check(t); result.OK || !strings.Contains(result.Problem, "the agent in the image did not run") || !strings.Contains(result.Problem, "exit status 126") || !strings.Contains(result.Problem, "it needs /proc") {
		t.Errorf("an agent that did not run: %+v", result)
	}
}

func TestWhatStopsTheCheckIsAnErrorNotAnImageThatFailed(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Agent = filepath.Join(r.dir, "no-agent")
	if _, err := Check(context.Background(), r.options); err == nil || !strings.Contains(err.Error(), "the agent to put in the image") || !strings.Contains(err.Error(), "no-agent") {
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

func TestAnAcceptedImageCanBeKeptUnderItsNamesWithItsLabels(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Name = "localhost/cbe-course/nn-2026"
	r.options.Labels = map[string]string{"io.cbe.course": "nn-2026", "io.cbe.other": "x"}
	result := r.check(t)
	if !result.OK || result.Image != "localhost/cbe-course/nn-2026:latest" || result.ImageID != "abc123" ||
		strings.Join(result.Tags, " ") != "localhost/cbe-course/nn-2026:abc123 localhost/cbe-course/nn-2026:latest" {
		t.Fatalf("%+v\n%s", result, r.out)
	}
	// The labels are put on the finishing build, in a fixed order, and the image is named after it passed.
	built := r.fake.called("build")
	if line := strings.Join(built[1], " "); !strings.Contains(line, "--label io.cbe.course=nn-2026 --label io.cbe.other=x --tag") || strings.Contains(strings.Join(built[0], " "), "--label") {
		t.Errorf("the labels: %v", built)
	}
	tagged := r.fake.called("tag")
	if len(tagged) != 1 || !strings.Contains(tagged[0][1], ":finished") || strings.Join(tagged[0][2:], " ") != "localhost/cbe-course/nn-2026:abc123 localhost/cbe-course/nn-2026:latest" {
		t.Errorf("tag: %v", tagged)
	}
	// Only the check's own names are taken off, and never with rmi --force (which would take the image with them), and never with untag IMAGE alone (which takes every name off).
	if len(r.fake.called("rmi")) != 0 {
		t.Errorf("the image that is kept was removed: %v", r.fake.called("rmi"))
	}
	for _, u := range r.fake.called("untag") {
		if len(u) != 3 {
			t.Errorf("untag without the name to take off would take every name off: %v", u)
		}
	}
	untagged := r.fake.called("untag")
	if len(untagged) != 2 || untagged[0][1] != "localhost/cbe-course/nn-2026:abc123" || !strings.HasSuffix(untagged[0][2], ":finished") || untagged[1][1] != untagged[1][2] || !strings.HasSuffix(untagged[1][2], ":author") {
		t.Errorf("untag: %v", untagged)
	}
}

func TestAnImageThatFailsIsNotNamedAndIsRemoved(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Name = "localhost/cbe-course/nn-2026"
	r.fake.selftest = `{"version":"t","ok":false,"checks":[{"name":"no-sudo","ok":false,"detail":"sudo is in the image"}]}`
	result := r.check(t)
	if result.OK || result.Image != "" || len(result.Tags) != 0 || len(r.fake.called("tag")) != 0 || len(r.fake.called("untag")) != 0 || len(r.fake.called("rmi")) != 1 {
		t.Errorf("%+v %v", result, r.fake.calls)
	}
}

func TestTheFoldersOfTheBuildCanBeReadByAnotherAccountWhenAskedFor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not what they are elsewhere")
	}
	r := newRig(t, pythonFile)
	for _, name := range []string{"Containerfile", "data/train.csv"} {
		if err := os.Chmod(filepath.Join(r.dir, "image", name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.options.Readable = true
	if result := r.check(t); !result.OK {
		t.Fatalf("%+v", result)
	}
	if r.fake.modes["work"]&0o055 != 0o055 {
		t.Errorf("the folder of the build is %v: another account cannot enter it", r.fake.modes["work"])
	}
	if mode := r.fake.modes["author/Containerfile"]; mode&0o044 != 0o044 {
		t.Errorf("the Containerfile is %v: another account cannot read it", mode)
	}
	if mode := r.fake.modes["finish/cbe-agent"]; mode&0o055 != 0o055 {
		t.Errorf("the agent is %v", mode)
	}
	// Without it the author's files keep their modes.
	r = newRig(t, pythonFile)
	if err := os.Chmod(filepath.Join(r.dir, "image", "Containerfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := r.check(t); !result.OK || r.fake.modes["author/Containerfile"] != 0o600 {
		t.Errorf("%+v %v", result, r.fake.modes["author/Containerfile"])
	}
}

// ----- the agent, the dates of the images, and a build twice from nothing (decision D-CB-9)

func otherArch() string {
	if runtime.GOARCH == "arm64" {
		return "amd64"
	}
	return "arm64"
}

func TestTheAgentIsLookedAtBeforeAnythingIsBuilt(t *testing.T) {
	cases := map[string]struct {
		data []byte
		want string
	}{
		"not a program":   {[]byte("THE AGENT"), "it is not a Linux program"},
		"another machine": {agenttest.Agent(t, otherArch()), "it is a program for " + otherArch() + ", and the images of this machine are " + runtime.GOARCH},
		"another program": {agenttest.Program(t, "cbe-agent-cli", runtime.GOARCH), "it is exam-platform/cmd/cbe-agent-cli, not cbe-agent"},
	}
	if sh, err := os.ReadFile("/bin/sh"); err == nil && runtime.GOOS == "linux" {
		cases["a program that is not static"] = struct {
			data []byte
			want string
		}{sh, "it is not static"}
	}
	for name, c := range cases {
		r := newRig(t, pythonFile)
		if err := os.WriteFile(r.agent, c.data, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Check(context.Background(), r.options)
		if err == nil || !strings.Contains(err.Error(), "cannot go in an image: "+c.want) || len(r.fake.calls) != 0 {
			t.Errorf("%s: %v, podman was called %v", name, err, r.fake.calls)
		}
	}
	// The agent that is right says so, with where it was found, what it is and its hash.
	r := newRig(t, pythonFile)
	r.options.AgentFrom = "named with --agent"
	if result := r.check(t); !result.OK {
		t.Fatalf("%+v\n%s", result, r.out)
	}
	want := "agent       ok: " + r.agent + " (named with --agent), "
	if !strings.Contains(r.out.String(), want) || !strings.Contains(r.out.String(), "linux/"+runtime.GOARCH+", static, sha256 "+r.fake.agentSum[:12]) {
		t.Errorf("the report lacks %q:\n%s", want, r.out)
	}
}

// The version is asked of the agent itself. Only Linux runs it; elsewhere the check says it could not ask, and goes on.
func TestTheImageIsLabelledWithTheVersionTheAgentSays(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Release, r.options.ReleaseOf = "v9.9.9", "the server installed here"
	result := r.check(t)
	finishing := strings.Join(r.fake.called("build")[1], " ")
	if runtime.GOOS != "linux" {
		if !result.OK || !strings.Contains(r.out.String(), "warning: the agent's version could not be asked") || strings.Contains(finishing, LabelAgentVersion) {
			t.Errorf("%+v\n%s", result, r.out)
		}
		return
	}
	if !result.OK || !strings.Contains(r.out.String(), ", "+agenttest.Version+", linux/") || !strings.Contains(finishing, "--label "+LabelAgentVersion+"="+agenttest.Version) {
		t.Errorf("the version: %+v\n%s\n%s", result, finishing, r.out)
	}
	// An agent of another release than the server's is said, and is not a failure.
	if want := "warning: the agent is " + agenttest.Version + " and the server installed here is v9.9.9"; !strings.Contains(r.out.String(), want) || len(result.Warnings) != 1 {
		t.Errorf("the report lacks %q: %v\n%s", want, result.Warnings, r.out)
	}
	r = newRig(t, pythonFile)
	r.options.Release = agenttest.Version
	if result := r.check(t); !result.OK || len(result.Warnings) != 0 {
		t.Errorf("the same release: %+v", result)
	}
}

func TestAContainerfileThatCopiesTheAgentIsToldTheRunnerAddsIt(t *testing.T) {
	for name, line := range map[string]string{
		"as the course image did": "COPY --chmod=0755 cbe-agent /usr/local/bin/cbe-agent",
		"from a folder":           "COPY ./bin/cbe-agent /usr/local/bin/",
		"as a list":               `ADD ["cbe-agent", "/opt/agent"]`,
	} {
		r := newRig(t, pythonFile+line+"\n")
		result := r.check(t)
		if result.OK || !strings.Contains(result.Problem, "line 3: ") || !strings.Contains(result.Problem, "there is no cbe-agent in the build's folder: the runner adds the agent to every image") || len(r.fake.calls) != 0 {
			t.Errorf("%s: %+v, podman was called %v", name, result, r.fake.calls)
		}
	}
	// A file of that name made in an earlier stage is the author's own business.
	r := newRig(t, "FROM docker.io/library/golang:1.26 AS build\nRUN touch /cbe-agent\n"+pythonFile+"COPY --from=build /cbe-agent /opt/mine\n")
	if result := r.check(t); !result.OK {
		t.Errorf("from a stage: %+v\n%s", result, r.out)
	}
}

func TestEveryImageIsStampedWithTheSameDate(t *testing.T) {
	r := newRig(t, pythonFile)
	r.check(t)
	built := r.fake.called("build")
	for _, b := range built {
		if line := strings.Join(b, " "); !strings.Contains(line, "--timestamp 946684800") || strings.Contains(line, "SOURCE_DATE_EPOCH") {
			t.Errorf("a build: %s", line)
		}
	}
	// A Containerfile that declares SOURCE_DATE_EPOCH is given the date too (its own build only), and the date can be another.
	r = newRig(t, "ARG SOURCE_DATE_EPOCH=0\n"+pythonFile)
	r.options.Epoch = 1700000000
	r.check(t)
	built = r.fake.called("build")
	if author := strings.Join(built[0], " "); !strings.Contains(author, "--timestamp 1700000000 --build-arg SOURCE_DATE_EPOCH=1700000000") {
		t.Errorf("the author's build: %s", author)
	}
	if finishing := strings.Join(built[1], " "); !strings.Contains(finishing, "--timestamp 1700000000") || strings.Contains(finishing, "SOURCE_DATE_EPOCH") {
		t.Errorf("the finishing: %s", finishing)
	}
}

// inspectedLayers is `podman image inspect` of a finished image with its layers and the history that made them.
func inspectedLayers(agentSum, id string, layers ...string) string {
	history := []map[string]any{{"created_by": "/bin/sh -c #(nop) ARG V", "empty_layer": true}}
	for i := range layers {
		history = append(history, map[string]any{"created_by": fmt.Sprintf("/bin/sh -c step %d   with   spaces", i+1)})
	}
	data, _ := json.Marshal([]map[string]any{{"Id": id, "Config": map[string]any{"User": "1000:1000", "Entrypoint": []string{runner.AgentPath}, "WorkingDir": "/work",
		"Labels": map[string]string{runner.LabelAgent: agentSum}}, "RootFS": map[string]any{"Type": "layers", "Layers": layers}, "History": history}})
	return string(data)
}

func TestReproducibleBuildsEverythingTwiceFromNothingAndComparesTheImages(t *testing.T) {
	r := newRig(t, pythonFile)
	r.options.Reproducible = true
	r.fake.inspected = inspectedLayers(r.fake.agentSum, "sha256:aaaaaaaaaaaa1", "l1", "l2", "l3")
	result := r.check(t)
	if !result.OK || result.Irreproducible != "" || !strings.Contains(r.out.String(), "reproduced  ok: two builds from nothing gave the same image (aaaaaaaaaaaa)") {
		t.Fatalf("%+v\n%s", result, r.out)
	}
	built := r.fake.called("build")
	if len(built) != 4 {
		t.Fatalf("builds: %v", built)
	}
	for _, b := range built {
		if !strings.Contains(strings.Join(b, " "), "--no-cache") {
			t.Errorf("a build that is not from nothing: %v", b)
		}
	}
	// The first images are removed before the second build, which gets the same names; the self test runs once.
	var order []string
	for _, c := range r.fake.calls {
		order = append(order, c[0])
	}
	if got := strings.Join(order, " "); got != "version build build image run rmi build build image rmi" {
		t.Errorf("the calls: %s", got)
	}

	// Two images that differ: the layers that are not the same, and the steps that made them.
	r = newRig(t, pythonFile)
	r.options.Reproducible = true
	r.fake.inspected = inspectedLayers(r.fake.agentSum, "sha256:aaaaaaaaaaaa1", "l1", "l2", "l3")
	r.fake.again = inspectedLayers(r.fake.agentSum, "sha256:bbbbbbbbbbbb2", "l1", "l2-other", "l3")
	result = r.check(t)
	want := "the two builds from nothing differ in layer 2 of 3 (step 2 with spaces)"
	if !result.OK || !strings.HasPrefix(result.Irreproducible, want) || !strings.Contains(r.out.String(), "reproduced  NO: "+want) {
		t.Errorf("%+v\n%s", result, r.out)
	}
	if got := differences([]byte(inspectedLayers("", "a", "l1")), []byte(inspectedLayers("", "b", "l1", "l2"))); got != "the two builds from nothing gave images of 1 and 2 layers" {
		t.Errorf("layers that do not match: %s", got)
	}
	if got := differences([]byte(inspectedLayers("", "a", "l1")), []byte(inspectedLayers("", "b", "l1"))); !strings.Contains(got, "the same layers and different configurations") {
		t.Errorf("the same layers: %s", got)
	}
}
