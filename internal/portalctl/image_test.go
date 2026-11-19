package portalctl

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"exam-platform/internal/portalctl/sys"
	"exam-platform/internal/runner"
)

// imageCommand runs `cbe-portalctl image check` with real files, and the settings and agent named, so that nothing of the machine the test runs on is read.
func imageCommand(t *testing.T, containerfile, runnerJSON string, extra ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "image"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"image/Containerfile": containerfile, "agent": "AGENT", "runner.json": runnerJSON} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	args := append([]string{"image", "check", dir, "--agent", filepath.Join(dir, "agent"), "--runner-config", filepath.Join(dir, "runner.json")}, extra...)
	code := Main(args, Env{Sys: sys.NewFake(), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errs, Exe: "/opt/cbe-portal/bin/cbe-portalctl", Version: "test"})
	return code, out.String(), errs.String()
}

func TestImageCheckSaysWhatTheContainerfileBreaksWithoutNeedingPodman(t *testing.T) {
	code, out, _ := imageCommand(t, "FROM python:3.12\nVOLUME /data\n", "{}", "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(out, "NOT ACCEPTED: the Containerfile is not accepted: line 2: VOLUME is not allowed") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestImageCheckAppliesTheServersSettings(t *testing.T) {
	code, out, _ := imageCommand(t, "FROM ghcr.io/acme/lab:1\n", `{"base_registries": ["docker.io"]}`, "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(out, "does not take images from ghcr.io; it allows docker.io") {
		t.Errorf("registries: exit %d\n%s", code, out)
	}
	code, out, _ = imageCommand(t, "FROM python:3.12\n", `{"custom_base": false}`, "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(out, "custom_base is off") {
		t.Errorf("custom_base: exit %d\n%s", code, out)
	}
	code, _, errs := imageCommand(t, "FROM python:3.12\n", `{"custom_basee": true}`)
	if code != 1 || !strings.Contains(errs, "the runner's settings") {
		t.Errorf("a settings file that is wrong: exit %d\n%s", code, errs)
	}
}

func TestImageCheckWithNoContainerRuntimeIsAnErrorThatSaysSo(t *testing.T) {
	code, _, errs := imageCommand(t, "FROM python:3.12\n", "{}", "--podman", "no-such-container-runtime")
	if code != 1 || !strings.Contains(errs, "the container runtime does not answer (is podman installed") {
		t.Errorf("exit %d\n%s", code, errs)
	}
}

func TestImageCheckUsage(t *testing.T) {
	// A wrong call says what is wrong and how that command is called; "image" alone, or a word that is not one of its commands, lists them.
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"image"}, "image install   Build a class image"},
		{[]string{"image", "bogus"}, `"image bogus" is not a command`},
		{[]string{"image", "chek"}, "(did you mean image check?)"},
		{[]string{"image", "check"}, "usage: cbe-portalctl image check [--agent FILE]"},
		{[]string{"image", "check", "a", "b"}, "it takes one DIR"},
		{[]string{"image", "install"}, "usage: cbe-portalctl image install NAME DIR"},
		{[]string{"image", "install", "nn-2026"}, "it takes a NAME and a DIR"},
		{[]string{"image", "list", "extra"}, "usage: cbe-portalctl image list\n"},
	} {
		var out, errs bytes.Buffer
		if code := Main(c.args, Env{Sys: sys.NewFake(), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errs, Exe: "/opt/cbe-portal/bin/cbe-portalctl"}); code != 2 || !strings.Contains(errs.String(), c.want) {
			t.Errorf("%v: exit %d, and it lacks %q\n%s", c.args, code, c.want, errs.String())
		}
	}
	var out bytes.Buffer
	if code := Main([]string{"image", "--help"}, Env{Sys: sys.NewFake(), Stdout: &out, Stderr: &out}); code != 0 || !strings.Contains(out.String(), "image check     Check an exam's own base image") {
		t.Errorf("image --help: exit %d\n%s", code, out.String())
	}
}

// ----- image install and image list

const pythonFile = "FROM docker.io/library/python:3.12-slim@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\nRUN pip install numpy==2.1.3\n"

// fakeRuntime is the runner account's container runtime: it records what it is asked and answers as one that builds a good image.
type fakeRuntime struct {
	calls     [][]string
	selftest  string
	installed map[string]string // image reference -> "id label" for `image inspect --format`
	workModes []os.FileMode     // the mode of the folder that holds each build's context, when the build is made
}

func (f *fakeRuntime) run(_ context.Context, args []string, stdout, _ io.Writer) error {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "version":
		io.WriteString(stdout, "4.9.3")
	case "build":
		if info, err := os.Stat(filepath.Dir(args[len(args)-1])); err == nil {
			f.workModes = append(f.workModes, info.Mode().Perm())
		}
	case "image":
		if len(args) > 2 && args[2] == "--format" { // image list: image inspect --format F REFERENCE
			if answer, ok := f.installed[args[4]]; ok {
				io.WriteString(stdout, answer)
				return nil
			}
			return os.ErrNotExist
		}
		data, _ := json.Marshal([]map[string]any{{"Id": "abc123def456789", "Config": map[string]any{"User": "1000:1000", "Entrypoint": []string{runner.AgentPath}, "WorkingDir": "/work",
			"Labels": map[string]string{runner.LabelAgent: runner.AgentSum([]byte("AGENT"))}}}})
		stdout.Write(data)
	case "run":
		if f.selftest != "" {
			io.WriteString(stdout, f.selftest)
		} else {
			io.WriteString(stdout, `{"version":"t","ok":true,"checks":[{"name":"user","ok":true}]}`)
		}
	}
	return nil
}

func (f *fakeRuntime) called(name string) [][]string {
	var found [][]string
	for _, c := range f.calls {
		if c[0] == name {
			found = append(found, c)
		}
	}
	return found
}

// classImage is a configured server, a package folder and an agent, and the runner's runtime.
type classImage struct {
	f       *sys.Fake
	runtime *fakeRuntime
	dir     string
	agent   string
}

func newClassImage(t *testing.T, containerfile string) *classImage {
	t.Helper()
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "image"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(dir, "agent")
	for name, text := range map[string]string{"image/Containerfile": containerfile, "agent": "AGENT"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &classImage{f: f, runtime: &fakeRuntime{}, dir: dir, agent: agent}
}

func (c *classImage) run(args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := Main(args, Env{Sys: c.f, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errs, Exe: "/opt/cbe-portal/bin/cbe-portalctl", Version: "test", Podman: c.runtime.run})
	return code, out.String(), errs.String()
}

func (c *classImage) install(args ...string) (int, string, string) {
	return c.run(append([]string{"image", "install"}, append(args, "--agent", c.agent)...)...)
}

func (c *classImage) settings(t *testing.T) map[string]any {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal([]byte(c.f.Text("/opt/cbe-portal/etc/runner.json")), &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestInstallingAClassImageBuildsItOnceAndApprovesItsName(t *testing.T) {
	c := newClassImage(t, pythonFile)
	// What an administrator changed in the settings stays.
	before := c.settings(t)
	before["max"] = map[string]any{"cpu": 4, "memory_mb": 4096, "disk_mb": 800}
	data, _ := json.Marshal(before)
	c.f.Install("/opt/cbe-portal/etc/runner.json", string(data), 0o644)

	code, out, errs := c.install("nn-2026", c.dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}
	settings := c.settings(t)
	images := settings["images"].(map[string]any)
	if images["nn-2026"] != "localhost/cbe-course/nn-2026:latest" || images["cbe/cpp20"] == nil {
		t.Errorf("the approved images: %v", images)
	}
	if max := settings["max"].(map[string]any); max["cpu"] != float64(4) || settings["agent_binary"] != "/opt/cbe-portal/bin/cbe-agent" {
		t.Errorf("the rest of the settings was not kept: %v", settings)
	}
	// It was built by the runner's runtime, named, and labelled with the name; the check's names were taken off.
	tagged := c.runtime.called("tag")
	if len(tagged) != 1 || strings.Join(tagged[0][2:], " ") != "localhost/cbe-course/nn-2026:abc123def456 localhost/cbe-course/nn-2026:latest" {
		t.Errorf("tag: %v", tagged)
	}
	if builds := c.runtime.called("build"); len(builds) != 2 || !strings.Contains(strings.Join(builds[1], " "), "--label io.cbe.course=nn-2026") {
		t.Errorf("the builds: %v", builds)
	}
	// The runtime is the runner account's, not the one that made the folders of the build: they must be readable by it.
	if runtime.GOOS != "windows" {
		if len(c.runtime.workModes) != 2 || c.runtime.workModes[0]&0o055 != 0o055 || c.runtime.workModes[1]&0o055 != 0o055 {
			t.Errorf("the folders of the build, as the runner account finds them: %v", c.runtime.workModes)
		}
	}
	for _, want := range []string{"Installed nn-2026: localhost/cbe-course/nn-2026:latest", "run `cbe-portalctl restart` when no class is working", "`image: nn-2026`"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

func TestInstallingTheSameNameAgainNeedsReplaceAndNoRestart(t *testing.T) {
	c := newClassImage(t, pythonFile)
	if code, out, errs := c.install("nn-2026", c.dir); code != 0 {
		t.Fatalf("%d\n%s\n%s", code, out, errs)
	}
	written := c.f.Text("/opt/cbe-portal/etc/runner.json")
	builds := len(c.runtime.called("build"))
	code, _, errs := c.install("nn-2026", c.dir)
	if code != 1 || !strings.Contains(errs, "nn-2026 is approved already (localhost/cbe-course/nn-2026:latest): --replace") || len(c.runtime.called("build")) != builds {
		t.Errorf("without --replace: exit %d\n%s", code, errs)
	}
	code, out, errs := c.install("nn-2026", c.dir, "--replace")
	if code != 0 || len(c.runtime.called("build")) != builds+2 {
		t.Fatalf("with --replace: exit %d\n%s\n%s", code, out, errs)
	}
	if c.f.Text("/opt/cbe-portal/etc/runner.json") != written || !strings.Contains(out, "no restart is needed") || strings.Contains(out, "run `cbe-portalctl restart`") {
		t.Errorf("a name that is approved already is not written again, and needs no restart:\n%s", out)
	}
	// An approved image of another kind (a course image built by hand) is replaced only when it is asked for, and the output says what it was.
	code, out, _ = c.install("cbe/cpp20", c.dir, "--replace")
	if code != 0 || !strings.Contains(out, "It was localhost/cbe/cpp20:latest before.") || c.settings(t)["images"].(map[string]any)["cbe/cpp20"] != "localhost/cbe-course/cbe/cpp20:latest" {
		t.Errorf("replacing a course image: exit %d\n%s", code, out)
	}
}

func TestInstallRestartsTheRunnerOnlyWhenItIsAskedAndNoOneIsWorking(t *testing.T) {
	c := newClassImage(t, pythonFile)
	c.f.Containers = "a\nb\n"
	before := len(c.f.Calls)
	code, out, errs := c.install("nn-2026", c.dir, "--restart")
	if code != 1 || !strings.Contains(errs, "the image is installed, and the runner was not restarted") || !strings.Contains(errs, "2 student workspace(s) are running") {
		t.Fatalf("with students working: exit %d\n%s\n%s", code, out, errs)
	}
	if c.settings(t)["images"].(map[string]any)["nn-2026"] == nil {
		t.Error("the image was not approved")
	}
	c.f.Containers = ""
	code, out, errs = c.install("nn-3000", c.dir, "--restart")
	restarted := strings.Join(c.f.Calls[before:], "\n")
	if code != 0 || !strings.Contains(restarted, "[cbe-runner] systemctl --user restart cbe-runner.service") || !strings.Contains(out, "The runner was restarted: exams can say `image: nn-3000`") {
		t.Errorf("exit %d\n%s\n%s\n%s", code, out, errs, restarted)
	}
}

func TestInstallWhatIsNotAcceptedChangesNothing(t *testing.T) {
	c := newClassImage(t, pythonFile)
	written := c.f.Text("/opt/cbe-portal/etc/runner.json")
	c.runtime.selftest = `{"version":"t","ok":false,"checks":[{"name":"no-sudo","ok":false,"detail":"sudo is in the image"}]}`
	code, out, _ := c.install("nn-2026", c.dir)
	if code != 1 || !strings.Contains(out, "NOT INSTALLED: the image does not keep the contract of a workspace image: no-sudo: sudo is in the image") || c.f.Text("/opt/cbe-portal/etc/runner.json") != written {
		t.Errorf("exit %d\n%s", code, out)
	}
	if len(c.runtime.called("tag")) != 0 {
		t.Error("an image that failed was named")
	}
	// The settings of the server apply: the registries do, custom_base (what an exam brings) does not.
	c = newClassImage(t, "FROM ghcr.io/acme/lab@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n")
	settings := c.settings(t)
	settings["base_registries"] = []string{"docker.io"}
	settings["custom_base"] = false
	data, _ := json.Marshal(settings)
	c.f.Install("/opt/cbe-portal/etc/runner.json", string(data), 0o644)
	if code, out, _ := c.install("nn-2026", c.dir); code != 1 || !strings.Contains(out, "does not take images from ghcr.io; it allows docker.io") {
		t.Errorf("registries: exit %d\n%s", code, out)
	}
	c = newClassImage(t, pythonFile)
	settings = c.settings(t)
	settings["custom_base"] = false
	data, _ = json.Marshal(settings)
	c.f.Install("/opt/cbe-portal/etc/runner.json", string(data), 0o644)
	if code, out, errs := c.install("nn-2026", c.dir); code != 0 {
		t.Errorf("custom_base off is about exams: an administrator may still install a class image: exit %d\n%s\n%s", code, out, errs)
	}
}

func TestInstallAndListNeedAServerThatCanKeepImages(t *testing.T) {
	c := newClassImage(t, pythonFile)
	for name, args := range map[string][]string{"a bad name": {"install", "NN 2026", c.dir, "--agent", c.agent}, "a tag in the name": {"install", "nn:1", c.dir, "--agent", c.agent}} {
		if code, _, errs := c.run(append([]string{"image"}, args...)...); code != 2 || !strings.Contains(errs, "is not the name of a course image") {
			t.Errorf("%s: exit %d\n%s", name, code, errs)
		}
	}
	c.f.Root = false
	for _, args := range [][]string{{"image", "install", "nn-2026", c.dir, "--agent", c.agent}, {"image", "list"}} {
		if code, _, errs := c.run(args...); code != 1 || !strings.Contains(errs, "run it as root") {
			t.Errorf("%v as someone else: exit %d\n%s", args, code, errs)
		}
	}
	c.f.Root = true
	if code, _, errs := c.run("image", "install", "nn-2026", filepath.Join(c.dir, "nothing"), "--agent", c.agent); code != 1 || errs == "" {
		t.Errorf("a folder that is not there: exit %d\n%s", code, errs)
	}
	// A machine that serves question exams only has no runner to keep images for, and one that was never configured has no saved answers.
	q := machine()
	q.Install("/root/q.conf", "", 0o644)
	if code, s := command(q, "", false, "configure", "--config", "/root/q.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	if code, s := command(q, "", false, "image", "list"); code != 1 || !strings.Contains(s.err.String(), "does not serve workspace exams") {
		t.Errorf("question exams only: exit %d\n%s", code, s.err.String())
	}
	if code, s := command(machine(), "", false, "image", "list"); code != 1 || !strings.Contains(s.err.String(), "has not been configured") {
		t.Errorf("never configured: exit %d\n%s", code, s.err.String())
	}
}

func TestImageListSaysWhatTheRunnerApprovesAndWhatTheRuntimeHas(t *testing.T) {
	c := newClassImage(t, pythonFile)
	if code, out, errs := c.install("nn-2026", c.dir); code != 0 {
		t.Fatalf("%d\n%s\n%s", code, out, errs)
	}
	c.runtime.installed = map[string]string{"localhost/cbe-course/nn-2026:latest": "sha256:abc123def456789 nn-2026\n", "localhost/cbe/cpp20:latest": "sha256:feed00112233 \n"}
	code, out, errs := c.run("image", "list")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}
	for _, want := range []string{"cbe/cpp20  localhost/cbe/cpp20:latest  present (feed00112233)", "nn-2026    localhost/cbe-course/nn-2026:latest  present, installed by image install (abc123def456)"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list lacks %q:\n%s", want, out)
		}
	}
	// An approved name whose image the runtime lost is said, and is a failure: an exam of it would not start.
	delete(c.runtime.installed, "localhost/cbe-course/nn-2026:latest")
	if code, out, _ := c.run("image", "list"); code != 1 || !strings.Contains(out, "nn-2026") || !strings.Contains(out, "MISSING: the container runtime does not have it") {
		t.Errorf("a missing image: exit %d\n%s", code, out)
	}
}
