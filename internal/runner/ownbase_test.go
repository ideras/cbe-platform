package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const pythonBase = "docker.io/library/python:3.12-slim@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

const pythonContainerfile = "FROM " + pythonBase + "\nRUN pip install --no-cache-dir numpy==2.1.3\n"

// ownBaseRig is a runner that has an agent to put in images, and a runtime that does what the real one does to the two builds of an image with a base of its
// own: the author's, which makes an image of its own, and the runner's, which makes a workspace image of it.
func ownBaseRig(t *testing.T, edit func(*Config)) (*rig, []byte) {
	t.Helper()
	agent := []byte("THE AGENT, A STATIC PROGRAM")
	path := filepath.Join(t.TempDir(), "cbe-agent")
	if err := os.WriteFile(path, agent, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRigWith(t, func(c *Config) {
		c.AgentBinary = path
		if edit != nil {
			edit(c)
		}
	})
	label := regexp.MustCompile(LabelAgent + `="([0-9a-f]{64})"`)
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, data []byte, log io.Writer) error {
		files := contextOf(t, data)
		if _, finishing := files["cbe-finalize.sh"]; !finishing {
			fmt.Fprintf(log, "STEP 1/2: FROM python:3.12-slim\nSTEP 2/2: RUN pip install numpy==2.1.3\nSuccessfully tagged %s\n", spec.Tag)
			r.rt.addImage(spec.Tag, ImageInfo{ID: "sha256:author", User: "root", Layers: []string{"sha256:python", "sha256:numpy"}, Labels: map[string]string{}})
			return nil
		}
		fmt.Fprintf(log, "STEP 1/9: FROM %s\ncbe-finalize: student 1000:1000 (/bin/bash), root locked, no sudo, no setuid files\nSuccessfully tagged %s\n", "the author's image", spec.Tag)
		labels := map[string]string{}
		for k, v := range spec.Labels {
			labels[k] = v
		}
		if match := label.FindStringSubmatch(files["Containerfile"]); match != nil {
			labels[LabelAgent] = match[1]
		}
		r.rt.addImage(spec.Tag, ImageInfo{ID: "sha256:final", User: "1000:1000", Entrypoint: []string{AgentPath}, WorkDir: "/work", Layers: []string{"sha256:python", "sha256:numpy", "sha256:finish"}, Labels: labels})
		return nil
	}
	return r, agent
}

func ownRequest() BuildRequest { return BuildRequest{Exam: "exam1"} }

func (r *rig) buildOwn(t *testing.T, containerfile string, extra map[string]string) (BuildResult, error) {
	t.Helper()
	files := map[string]string{"Containerfile": containerfile}
	for name, content := range extra {
		files[name] = content
	}
	return r.svc.BuildImage(context.Background(), ownRequest(), bytes.NewReader(archive(t, files)))
}

// An exam with a base of its own: the author's Containerfile is built as written, the runner's layer is built on top of it, the result is checked by the agent's
// self test in a container made as a student's is, and what is kept is the image of the exam alone.
func TestAnExamWithABaseOfItsOwnIsBuiltFinishedCheckedAndTagged(t *testing.T) {
	r, agent := ownBaseRig(t, nil)
	lendCA(t, "THE HOST'S CERTIFICATES")
	result, err := r.buildOwn(t, pythonContainerfile, map[string]string{"data/train.csv": "1,2,3"})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(result.Hash) != 16 || result.Tag != "localhost/cbe-exam/exam1:"+result.Hash || result.ImageID != "sha256:final" || result.Cached {
		t.Errorf("hash, tag and image: %+v", result)
	}
	if result.BaseName != pythonBase {
		t.Errorf("the base: %q", result.BaseName)
	}
	agentSum := sha256.Sum256(agent)
	agentHex := hex.EncodeToString(agentSum[:])
	for _, want := range []string{"Base image: " + pythonBase, "the exam's own: the runner adds the agent", "Agent: sha256:" + agentHex, "Building the exam's image from its Containerfile",
		"Finishing the image", "cbe-finalize: student 1000:1000", "self test  ok    user", "self test  ok    no-setuid-files", "Verified: student 1000:1000, the agent", "Tagged " + result.Tag} {
		if !strings.Contains(result.Log, want) {
			t.Errorf("the log does not say %q:\n%s", want, result.Log)
		}
	}
	if len(result.Warnings) != 0 {
		t.Errorf("a pinned base has no warning: %q", result.Warnings)
	}
	if r.rt.builds != 2 {
		t.Fatalf("%d builds, want the author's and the runner's", r.rt.builds)
	}
	// The author's build: the Containerfile as it is written (no pinning, no rewriting), the author's files and the host's certificates.
	first := contextOf(t, r.rt.buildContexts[0])
	if first["Containerfile"] != pythonContainerfile || first["data/train.csv"] != "1,2,3" || first["ca.crt"] != "THE HOST'S CERTIFICATES" {
		t.Errorf("the author's context: %v", first)
	}
	if spec := r.rt.buildSpecs[0]; spec.Tag != "localhost/cbe-pending/exam1:"+result.Hash+"-author" || spec.NoCache || spec.MemoryBytes != 2048<<20 || len(spec.Labels) != 0 {
		t.Errorf("the author's build spec: %+v", spec)
	}
	// The runner's: its own Containerfile on the author's image, its script, the agent, and the labels that make the image this exam's.
	second := contextOf(t, r.rt.buildContexts[1])
	containerfile := second["Containerfile"]
	for _, want := range []string{"FROM localhost/cbe-pending/exam1:" + result.Hash + "-author\n", "USER root\n", "RUN /bin/sh /cbe-finalize.sh && rm -f /cbe-finalize.sh\n",
		"COPY --chmod=0755 cbe-agent " + AgentPath + "\n", LabelAgent + "=\"" + agentHex + "\"", "HEALTHCHECK NONE\n", "USER 1000:1000\n", "WORKDIR /work\n", "ENTRYPOINT [\"" + AgentPath + "\"]\n", "CMD []\n"} {
		if !strings.Contains(containerfile, want) {
			t.Errorf("the runner's Containerfile lacks %q:\n%s", want, containerfile)
		}
	}
	if second["cbe-finalize.sh"] != string(finalizeScript) || second["cbe-agent"] != string(agent) || len(second) != 3 {
		t.Errorf("the runner's context holds %d files", len(second))
	}
	if spec := r.rt.buildSpecs[1]; spec.Tag != "localhost/cbe-pending/exam1:"+result.Hash || spec.Labels[LabelBuildExam] != "exam1" || spec.Labels[LabelBuildHash] != result.Hash || spec.Labels[LabelBuildImage] != pythonBase {
		t.Errorf("the runner's build spec: %+v", spec)
	}
	// What is left is the final image: not the author's, not the pending one.
	if !r.rt.images[result.Tag] || r.rt.images["localhost/cbe-pending/exam1:"+result.Hash+"-author"] || r.rt.images["localhost/cbe-pending/exam1:"+result.Hash] {
		t.Errorf("images: %v", r.rt.images)
	}
	// The self test ran in a container made as a student's is made: no network, a read-only root, no capabilities, in place of the entrypoint.
	var checker *ContainerSpec
	for i := range r.rt.specs {
		if len(r.rt.specs[i].Cmd) == 1 && r.rt.specs[i].Cmd[0] == "-selftest" {
			checker = &r.rt.specs[i]
		}
	}
	if checker == nil {
		t.Fatal("the self test was not run")
	}
	if checker.NetworkMode != "none" || !checker.ReadonlyRootfs || len(checker.CapDrop) != 1 || checker.User != "1000:1000" || checker.Entrypoint[0] != AgentPath || checker.Labels[LabelRole] != RoleVerify {
		t.Errorf("the checking container: %+v", *checker)
	}
	r.noContainersLeft(t)
	// The image answers image.inspect as the image of that exam only.
	if status, err := r.svc.ImageInspect(context.Background(), BuiltName("exam1", result.Hash)); err != nil || !status.Approved || !status.Present {
		t.Errorf("image.inspect: %+v %v", status, err)
	}
}

// The same files with the same agent are the same image: a second build builds nothing, and a new agent is a new image.
func TestTheSameFilesAndTheSameAgentAreTheSameImage(t *testing.T) {
	r, _ := ownBaseRig(t, nil)
	first, err := r.buildOwn(t, pythonContainerfile, nil)
	if err != nil || !first.OK || first.Cached {
		t.Fatalf("%+v %v", first, err)
	}
	again, err := r.buildOwn(t, pythonContainerfile, nil)
	if err != nil || !again.OK || !again.Cached || again.Hash != first.Hash || r.rt.builds != 2 {
		t.Fatalf("a second build: %+v %v (%d builds)", again, err, r.rt.builds)
	}
	if !strings.Contains(again.Log, "is built already from these files with this agent: nothing was built") {
		t.Errorf("the log:\n%s", again.Log)
	}
	// Rebuild: built again, without the cache.
	request := ownRequest()
	request.Rebuild = true
	rebuilt, err := r.svc.BuildImage(context.Background(), request, bytes.NewReader(archive(t, map[string]string{"Containerfile": pythonContainerfile})))
	if err != nil || !rebuilt.OK || rebuilt.Cached || r.rt.builds != 4 || !r.rt.buildSpecs[2].NoCache {
		t.Errorf("a rebuild: %+v %v (%d builds)", rebuilt, err, r.rt.builds)
	}
	// Another agent: another image.
	if err := os.WriteFile(r.svc.cfg.AgentBinary, []byte("A NEWER AGENT"), 0o755); err != nil {
		t.Fatal(err)
	}
	newer, err := r.buildOwn(t, pythonContainerfile, nil)
	if err != nil || !newer.OK || newer.Cached || newer.Hash == first.Hash {
		t.Errorf("a new agent: %+v %v", newer, err)
	}
}

// What the administrator of the server says: no bases of their own, only these registries, and an agent to put in the image.
func TestTheAdministratorsPolicyForBasesOfTheirOwn(t *testing.T) {
	// custom_base off.
	r, _ := ownBaseRig(t, func(c *Config) { c.CustomBase = false })
	result, err := r.buildOwn(t, pythonContainerfile, nil)
	if err != nil || result.OK || result.Failure != FailPolicy || !strings.Contains(result.Message, "custom_base is off") || r.rt.builds != 0 {
		t.Errorf("custom_base off: %+v %v", result, err)
	}
	// Registries: docker.io is allowed, ghcr.io is not; every FROM is looked at, and a stage of the file is not a registry.
	r, _ = ownBaseRig(t, func(c *Config) { c.BaseRegistries = []string{"docker.io", "quay.io"} })
	if result, err := r.buildOwn(t, pythonContainerfile, nil); err != nil || !result.OK {
		t.Fatalf("an allowed registry: %+v %v", result, err)
	}
	for _, text := range []string{
		"FROM ghcr.io/acme/db:1@sha256:" + strings.Repeat("a", 64) + "\n",
		"FROM " + pythonBase + " AS build\nFROM ghcr.io/acme/db:1@sha256:" + strings.Repeat("a", 64) + "\nCOPY --from=build /a /a\n",
	} {
		refused, err := r.buildOwn(t, text, nil)
		if err != nil || refused.OK || refused.Failure != FailPolicy || !strings.Contains(refused.Message, "does not take images from ghcr.io; it allows docker.io, quay.io") {
			t.Errorf("a registry that is not allowed: %+v %v", refused, err)
		}
	}
	if len(r.rt.buildSpecs) != 2 {
		t.Errorf("%d builds: a refused file is not built", len(r.rt.buildSpecs))
	}
	// Any registry when none are listed (the default).
	r, _ = ownBaseRig(t, nil)
	if result, err := r.buildOwn(t, "FROM ghcr.io/acme/db:1@sha256:"+strings.Repeat("a", 64)+"\n", nil); err != nil || !result.OK {
		t.Errorf("any registry: %+v %v", result, err)
	}
	// No agent to put in the image.
	r, _ = ownBaseRig(t, func(c *Config) { c.AgentBinary = "" })
	if result, err := r.buildOwn(t, pythonContainerfile, nil); err != nil || result.OK || result.Failure != FailPolicy || !strings.Contains(result.Message, "agent_binary is not set") {
		t.Errorf("no agent: %+v %v", result, err)
	}
	r, _ = ownBaseRig(t, func(c *Config) { c.AgentBinary = "/nonexistent/cbe-agent" })
	if result, err := r.buildOwn(t, pythonContainerfile, nil); err != nil || result.OK || result.Failure != FailPolicy || !strings.Contains(result.Message, "cannot read the agent") {
		t.Errorf("an agent that is not there: %+v %v", result, err)
	}
}

func TestTheRulesOfTheContainerfileHoldForABaseOfItsOwn(t *testing.T) {
	r, _ := ownBaseRig(t, nil)
	result, err := r.buildOwn(t, "FROM python:3.12\nVOLUME /data\nRUN --network=host true\n", nil)
	if err != nil || result.OK || result.Failure != FailContainerfile || !strings.Contains(result.Message, "VOLUME is not allowed") || !strings.Contains(result.Message, "--network=host") || r.rt.builds != 0 {
		t.Errorf("%+v %v", result, err)
	}
	// An image with no digest builds, with a warning; and the warning can be left out.
	warned, err := r.buildOwn(t, "FROM python:3.12-slim\n", nil)
	if err != nil || !warned.OK || len(warned.Warnings) != 1 || !strings.Contains(warned.Log, "warning: Containerfile line 1: FROM python:3.12-slim has no digest") {
		t.Errorf("an image with no digest: %+v %v", warned, err)
	}
	request := ownRequest()
	request.NoWarnings, request.Rebuild = true, true
	quiet, err := r.svc.BuildImage(context.Background(), request, bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM python:3.12-slim\n"})))
	if err != nil || !quiet.OK || len(quiet.Warnings) != 0 || strings.Contains(quiet.Log, "warning:") {
		t.Errorf("warnings left out: %+v %v", quiet, err)
	}
}

// A step that fails, in the author's file or in the runner's layer, is a failure the proctor can read, and nothing of the build is left.
func TestAFailedStepOfEitherBuildLeavesNothingAndSaysWhich(t *testing.T) {
	r, _ := ownBaseRig(t, nil)
	good := r.rt.build
	r.rt.build = func(ctx context.Context, spec ImageBuildSpec, data []byte, log io.Writer) error {
		if _, finishing := contextOf(t, data)["cbe-finalize.sh"]; !finishing {
			fmt.Fprint(log, "STEP 2/2: RUN pip install numpy==9.9\nERROR: No matching distribution found for numpy==9.9\n")
			return fmt.Errorf("%w: building at STEP \"RUN pip install numpy==9.9\": exit status 1", ErrBuildFailed)
		}
		return good(ctx, spec, data, log)
	}
	result, err := r.buildOwn(t, pythonContainerfile, nil)
	if err != nil || result.OK || result.Failure != FailBuild || !strings.Contains(result.Message, "RUN pip install numpy==9.9") || r.rt.builds != 1 {
		t.Fatalf("the author's step: %+v %v", result, err)
	}
	if len(r.rt.images) != 1 {
		t.Errorf("images left: %v", r.rt.images)
	}
	// The runner's layer cannot be built (the author's image has no /bin/sh).
	r.rt.build = func(ctx context.Context, spec ImageBuildSpec, data []byte, log io.Writer) error {
		if _, finishing := contextOf(t, data)["cbe-finalize.sh"]; finishing {
			return fmt.Errorf("%w: building at STEP \"RUN /bin/sh /cbe-finalize.sh\": exec: \"/bin/sh\": no such file", ErrBuildFailed)
		}
		return good(ctx, spec, data, log)
	}
	result, err = r.buildOwn(t, "FROM scratch\nCOPY a /a\n", map[string]string{"a": "x"})
	if err != nil || result.OK || result.Failure != FailBuild || !strings.Contains(result.Message, "could not finish the image (the base has to be a Linux image with /bin/sh)") {
		t.Errorf("the runner's layer: %+v %v", result, err)
	}
	if len(r.rt.images) != 1 {
		t.Errorf("images left: %v", r.rt.images)
	}
	r.noContainersLeft(t)
}

// What the image must be, as the runner checks it: its configuration, its volumes, and the agent's own self test.
func TestAnImageThatDoesNotKeepTheContractIsNotKept(t *testing.T) {
	for name, test := range map[string]struct {
		change func(*ImageInfo)
		report string
		want   string
	}{
		"another user":              {func(i *ImageInfo) { i.User = "root" }, "", `the user of the image is "root", and it must be 1000:1000`},
		"another entrypoint":        {func(i *ImageInfo) { i.Entrypoint = []string{"/bin/sh"} }, "", "the entrypoint of the image is"},
		"another folder":            {func(i *ImageInfo) { i.WorkDir = "/" }, "", "the working folder of the image is"},
		"no agent label":            {func(i *ImageInfo) { i.Labels[LabelAgent] = "" }, "", "does not say which agent"},
		"volumes":                   {func(i *ImageInfo) { i.Volumes = []string{"/var/lib/postgresql/data"} }, "", "declares the volume(s) /var/lib/postgresql/data"},
		"a failed check":            {nil, `{"ok":false,"checks":[{"name":"shell","ok":false,"detail":"there is no shell at /bin/bash"},{"name":"user","ok":true}]}`, "does not keep the contract of a workspace image: shell: there is no shell at /bin/bash"},
		"an agent that did not run": {nil, "exec /usr/local/bin/cbe-agent: exec format error", "the agent in the image did not run"},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := ownBaseRig(t, nil)
			good := r.rt.build
			r.rt.build = func(ctx context.Context, spec ImageBuildSpec, data []byte, log io.Writer) error {
				if err := good(ctx, spec, data, log); err != nil {
					return err
				}
				if test.change != nil && strings.Contains(spec.Tag, "-author") == false {
					info := r.rt.infos[spec.Tag]
					test.change(&info)
					r.rt.infos[spec.Tag] = info
				}
				return nil
			}
			if test.report != "" {
				r.rt.selfTests["*"] = test.report // whatever image the checking container is made from, it prints this
			}
			result, err := r.buildOwn(t, pythonContainerfile, nil)
			if err != nil || result.OK || result.Failure != FailVerify || !strings.Contains(result.Message, test.want) {
				t.Fatalf("%+v %v", result, err)
			}
			for ref := range r.rt.images {
				if strings.HasPrefix(ref, "localhost/cbe-exam/") {
					t.Errorf("an image that fails is kept: %s", ref)
				}
			}
			r.noContainersLeft(t)
		})
	}
}

// An exam with a base of its own is run from the image the runner built for it, and from no other: a request for it needs the build, and the image must
// be this exam's.
func TestAContainerOfAnExamWithABaseOfItsOwnIsMadeFromItsBuild(t *testing.T) {
	r, _ := ownBaseRig(t, nil)
	built, err := r.buildOwn(t, pythonContainerfile, nil)
	if err != nil || !built.OK {
		t.Fatalf("%+v %v", built, err)
	}
	request := r.request("s1")
	request.Image, request.Build = "", built.Hash
	if _, err := r.svc.Create(context.Background(), request); err != nil {
		t.Fatalf("a container of the exam's build: %v", err)
	}
	spec := r.rt.specs[len(r.rt.specs)-1]
	if spec.Image != BuiltReference("exam1", built.Hash) || spec.Labels[LabelBuild] != built.Hash || spec.NetworkMode != "none" || !spec.ReadonlyRootfs {
		t.Errorf("the container: %+v", spec)
	}
	// No build: nothing names the image, and no image is approved under the empty name.
	request = r.request("s2")
	request.Image = ""
	if _, err := r.svc.Create(context.Background(), request); !errors.Is(err, ErrNotApproved) {
		t.Errorf("no build: %v", err)
	}
	// Another exam's build is not this exam's image.
	request = r.request("s3")
	request.Image, request.Build, request.Exam = "", built.Hash, "exam2"
	if _, err := r.svc.Create(context.Background(), request); err == nil {
		t.Error("a container of another exam's image was made")
	}
}

func TestTheRunnerSettingsForBasesOfTheirOwn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runner.json")
	if err := os.WriteFile(path, []byte(`{"custom_base": false, "base_registries": ["docker.io", "localhost:5000"], "agent_binary": "/opt/cbe-portal/bin/cbe-agent"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || cfg.CustomBase || len(cfg.BaseRegistries) != 2 || cfg.AgentBinary != "/opt/cbe-portal/bin/cbe-agent" {
		t.Fatalf("%+v %v", cfg, err)
	}
	if def := DefaultConfig(); !def.CustomBase || len(def.BaseRegistries) != 0 {
		t.Errorf("the defaults: any base, any registry: %+v", def)
	}
	for name, text := range map[string]string{
		"a registry that is not": `{"base_registries": ["Docker.io/x"]}`,
		"an agent not by path":   `{"agent_binary": "cbe-agent"}`,
		"no images and no bases": `{"images": {}, "custom_base": false}`,
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	// No approved images is fine when the exams bring their own.
	if err := os.WriteFile(path, []byte(`{"images": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Errorf("no approved images, custom bases on: %v", err)
	}
}
