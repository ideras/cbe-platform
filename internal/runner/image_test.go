package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var baseDigestOfTheFake = "sha256:" + strings.Repeat("b", 64)

const plainContainerfile = "FROM cbe/cpp20\nUSER root\nRUN apt-get install -y libfoo-dev\nUSER 1000:1000\n"

func (r *rig) buildRequest() BuildRequest {
	return BuildRequest{Exam: "exam1", Image: "cbe/cpp20"}
}

// build builds a context made of a Containerfile and other files.
func (r *rig) build(t *testing.T, containerfile string, extra map[string]string) (BuildResult, error) {
	t.Helper()
	files := map[string]string{"Containerfile": containerfile}
	for name, content := range extra {
		files[name] = content
	}
	return r.svc.BuildImage(context.Background(), r.buildRequest(), bytes.NewReader(archive(t, files)))
}

func lendCA(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := hostCABundle
	hostCABundle = path
	t.Cleanup(func() { hostCABundle = old })
}

// contextOf reads the files of the context a build was given.
func contextOf(t *testing.T, data []byte) map[string]string {
	t.Helper()
	files := map[string]string{}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err != nil {
			return files
		}
		content, _ := io.ReadAll(reader)
		files[header.Name] = string(content)
	}
}

func (r *rig) noContainersLeft(t *testing.T) {
	t.Helper()
	r.rt.mu.Lock()
	defer r.rt.mu.Unlock()
	if len(r.rt.containers) != 0 {
		t.Errorf("containers left: %v", r.rt.containers)
	}
}

// Phase 8: an exam that says image_warnings: off has the checker's warnings neither in the log nor in the result.
func TestTheWarningsOfAContainerfileCanBeLeftOutOfABuild(t *testing.T) {
	r := newRig(t)
	lendCA(t, "THE HOST'S CERTIFICATES")
	request := r.buildRequest()
	request.NoWarnings = true
	result, err := r.svc.BuildImage(context.Background(), request, bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})))
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(result.Warnings) != 0 || strings.Contains(result.Log, "warning:") || !strings.Contains(result.Log, "Verified: made from cbe/cpp20") {
		t.Errorf("warnings %q in\n%s", result.Warnings, result.Log)
	}
	// A Containerfile that is refused is still refused, and says why.
	request.Rebuild = true
	refused, err := r.svc.BuildImage(context.Background(), request, bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM ubuntu:24.04\n"})))
	if err != nil || refused.OK || refused.Failure != FailContainerfile {
		t.Errorf("a refused Containerfile: %+v %v", refused, err)
	}
}

func TestAnImageIsBuiltPinnedVerifiedAndTagged(t *testing.T) {
	r := newRig(t)
	lendCA(t, "THE HOST'S CERTIFICATES")
	result, err := r.build(t, plainContainerfile, map[string]string{"lib/foo.txt": "a library"})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(result.Hash) != 16 || result.Tag != "localhost/cbe-exam/exam1:"+result.Hash || result.ImageID != "sha256:built1" || result.Cached {
		t.Errorf("hash, tag and image: %+v", result)
	}
	if result.BaseName != "cbe/cpp20" || result.BaseDigest != baseDigestOfTheFake {
		t.Errorf("the base: %q %q", result.BaseName, result.BaseDigest)
	}
	// The log records the base's digest (the plan's acceptance), the warning about the unpinned install and the verification.
	for _, want := range []string{"Base image: cbe/cpp20 is localhost/cbe/cpp20:latest", "digest " + baseDigestOfTheFake, "pinned by this build",
		"warning: Containerfile line 3: apt-get installs libfoo-dev without a version", "STEP 2/2: RUN true", "Verified: made from cbe/cpp20", "Tagged " + result.Tag} {
		if !strings.Contains(result.Log, want) {
			t.Errorf("the log does not say %q:\n%s", want, result.Log)
		}
	}
	if len(result.Warnings) != 1 {
		t.Errorf("warnings: %q", result.Warnings)
	}
	// What the runtime was given: the Containerfile pinned to the base's digest, the author's files, and the host's certificates.
	if r.rt.builds != 1 {
		t.Fatalf("%d builds", r.rt.builds)
	}
	files := contextOf(t, r.rt.buildContexts[0])
	if !strings.HasPrefix(files["Containerfile"], "FROM localhost/cbe/cpp20@"+baseDigestOfTheFake+"\n") || !strings.Contains(files["Containerfile"], "USER 1000:1000") {
		t.Errorf("the Containerfile of the build:\n%s", files["Containerfile"])
	}
	if files["ca.crt"] != "THE HOST'S CERTIFICATES" || files["lib/foo.txt"] != "a library" {
		t.Errorf("the context: %v", files)
	}
	spec := r.rt.buildSpecs[0]
	if spec.Tag != "localhost/cbe-pending/exam1:"+result.Hash || spec.NoCache || spec.MemoryBytes != 2048<<20 ||
		spec.Labels[LabelBuildExam] != "exam1" || spec.Labels[LabelBuildHash] != result.Hash || spec.Labels[LabelBuildBase] != baseDigestOfTheFake || spec.Labels[LabelBuildImage] != "cbe/cpp20" {
		t.Errorf("the build spec: %+v", spec)
	}
	// What is left: the final name, and nothing else.
	if !r.rt.images[result.Tag] || r.rt.images[spec.Tag] {
		t.Errorf("images: %v", r.rt.images)
	}
	r.noContainersLeft(t)
	// The image answers image.inspect under its name, and only as the image of that exam.
	status, err := r.svc.ImageInspect(context.Background(), BuiltName("exam1", result.Hash))
	if err != nil || !status.Approved || !status.Present || status.ID != result.ImageID {
		t.Errorf("image.inspect: %+v %v", status, err)
	}
	// The checking container was made as a student's is: no network, a read-only root, no capabilities, no mounts, and it ran sha256sum.
	var verifier *ContainerSpec
	for i := range r.rt.specs {
		if r.rt.specs[i].Labels[LabelRole] == RoleVerify {
			verifier = &r.rt.specs[i]
		}
	}
	if verifier == nil {
		t.Fatal("the agent was not checked in a container")
	}
	if verifier.NetworkMode != "none" || !verifier.ReadonlyRootfs || len(verifier.CapDrop) != 1 || len(verifier.Binds) != 0 || verifier.Image != spec.Tag ||
		verifier.Entrypoint[0] != "/usr/bin/sha256sum" || verifier.Cmd[0] != AgentPath || verifier.User != "1000:1000" {
		t.Errorf("the checking container: %+v", *verifier)
	}
}

func TestTheSameFilesOnTheSameBaseAreACacheHit(t *testing.T) {
	r := newRig(t)
	first, err := r.build(t, plainContainerfile, nil)
	if err != nil || !first.OK {
		t.Fatalf("%+v %v", first, err)
	}
	again, err := r.build(t, plainContainerfile, nil)
	if err != nil || !again.OK || !again.Cached || again.Hash != first.Hash || again.ImageID != first.ImageID || r.rt.builds != 1 {
		t.Fatalf("a second build of the same files: %+v %v (%d builds)", again, err, r.rt.builds)
	}
	if !strings.Contains(again.Log, "nothing was built") || !strings.Contains(again.Log, "Base image: cbe/cpp20") {
		t.Errorf("the log of a cache hit:\n%s", again.Log)
	}
	// Rebuild builds again, without the cache, and the tag stays.
	req := r.buildRequest()
	req.Rebuild = true
	rebuilt, err := r.svc.BuildImage(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})))
	if err != nil || !rebuilt.OK || rebuilt.Cached || r.rt.builds != 2 || !r.rt.buildSpecs[1].NoCache || rebuilt.Hash != first.Hash || !r.rt.images[first.Tag] {
		t.Fatalf("a rebuild: %+v %v", rebuilt, err)
	}
	// Other files, or another base, are another image.
	other, _ := r.build(t, plainContainerfile, map[string]string{"extra.txt": "x"})
	if other.Hash == first.Hash || r.rt.builds != 3 {
		t.Errorf("other files, the same hash: %+v", other)
	}
	info := baseImage()
	info.RepoDigests = []string{"localhost/cbe/cpp20@sha256:" + strings.Repeat("c", 64)}
	r.rt.addImage("localhost/cbe/cpp20:latest", info)
	newBase, _ := r.build(t, plainContainerfile, nil)
	if newBase.Hash == first.Hash || newBase.BaseDigest != "sha256:"+strings.Repeat("c", 64) || newBase.Cached {
		t.Errorf("another base, the same hash: %+v", newBase)
	}
	// An image of that name that was not built for this exam and hash is not a hit.
	r.rt.addImage(BuiltReference("exam1", newBase.Hash), ImageInfo{ID: "sha256:forged", Labels: map[string]string{LabelBuildExam: "exam2", LabelBuildHash: newBase.Hash}})
	rebuiltForged, _ := r.build(t, plainContainerfile, nil)
	if rebuiltForged.Cached || r.rt.images[BuiltReference("exam1", newBase.Hash)] != true {
		t.Errorf("a forged image was taken for the build: %+v", rebuiltForged)
	}
}

func TestTheDigestOfTheBaseIsCheckedOrPinned(t *testing.T) {
	r := newRig(t)
	// A digest that is the server's is accepted, and the log says the Containerfile pinned it.
	pinned, err := r.build(t, "FROM cbe/cpp20@"+baseDigestOfTheFake+"\nRUN true\n", nil)
	if err != nil || !pinned.OK || !strings.Contains(pinned.Log, "pinned by the Containerfile") {
		t.Fatalf("%+v %v", pinned, err)
	}
	if files := contextOf(t, r.rt.buildContexts[0]); !strings.HasPrefix(files["Containerfile"], "FROM localhost/cbe/cpp20@"+baseDigestOfTheFake) {
		t.Errorf("the pinned Containerfile:\n%s", files["Containerfile"])
	}
	// Another one fails the build, saying both digests, and nothing is built.
	other := "sha256:" + strings.Repeat("1", 64)
	result, err := r.build(t, "FROM cbe/cpp20@"+other+"\n", nil)
	if err != nil || result.OK || result.Failure != FailBase || !strings.Contains(result.Message, other) || !strings.Contains(result.Message, baseDigestOfTheFake) || r.rt.builds != 1 {
		t.Fatalf("a digest that is not the server's: %+v %v", result, err)
	}
	// A server with no such course image says so.
	delete(r.rt.images, "localhost/cbe/cpp20:latest")
	missing, err := r.build(t, plainContainerfile, nil)
	if err != nil || missing.OK || missing.Failure != FailBase || !strings.Contains(missing.Message, "does not have the course image cbe/cpp20") {
		t.Errorf("no course image: %+v %v", missing, err)
	}
}

func TestAContainerfileThatBreaksTheRulesIsRefusedBeforeAnythingIsBuilt(t *testing.T) {
	r := newRig(t)
	for name, c := range map[string]struct{ text, says string }{
		"another image":   {"FROM ubuntu:24.04\n", "the only image a workspace exam's Containerfile may start from"},
		"host network":    {"FROM cbe/cpp20\nRUN --network=host curl x\n", "host would let the build reach"},
		"a volume":        {"FROM cbe/cpp20\nVOLUME /etc\n", "VOLUME is not allowed"},
		"copy from image": {"FROM cbe/cpp20\nCOPY --from=golang /go /go\n", "only name an earlier stage"},
		"no FROM":         {"RUN true\n", "comes before the first FROM"},
		"the image's tag": {"FROM cbe/cpp20:latest\n", "without a tag"},
	} {
		result, err := r.build(t, c.text, nil)
		if err != nil || result.OK || result.Failure != FailContainerfile || !strings.Contains(result.Message, c.says) || !strings.Contains(result.Log, "Containerfile line") {
			t.Errorf("%s: %+v %v", name, result, err)
		}
	}
	if r.rt.builds != 0 || len(r.rt.images) != 1 {
		t.Errorf("something was built: %d builds, images %v", r.rt.builds, r.rt.images)
	}
}

func TestARequestTheRunnerCannotTakeIsAnError(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	good := bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n"}))
	for name, c := range map[string]struct {
		req     BuildRequest
		context []byte
		want    error
	}{
		"an image that is not approved": {BuildRequest{Exam: "exam1", Image: "docker.io/library/alpine"}, archive(t, map[string]string{"Containerfile": "FROM x\n"}), ErrNotApproved},
		"an exam in capitals":           {BuildRequest{Exam: "Exam1", Image: "cbe/cpp20"}, archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n"}), ErrBadRequest},
		"no exam":                       {BuildRequest{Image: "cbe/cpp20"}, archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n"}), ErrBadRequest},
		"a time that is too short":      {BuildRequest{Exam: "exam1", Image: "cbe/cpp20", TimeoutSeconds: 3}, archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n"}), ErrBadRequest},
		"a time over the runner's":      {BuildRequest{Exam: "exam1", Image: "cbe/cpp20", TimeoutSeconds: 99999}, archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n"}), ErrBadRequest},
		"no Containerfile":              {BuildRequest{Exam: "exam1", Image: "cbe/cpp20"}, archive(t, map[string]string{"setup.sh": "x"}), ErrBadRequest},
		"a Dockerfile":                  {BuildRequest{Exam: "exam1", Image: "cbe/cpp20"}, archive(t, map[string]string{"Dockerfile": "FROM cbe/cpp20\n"}), ErrBadRequest},
		"not a tar":                     {BuildRequest{Exam: "exam1", Image: "cbe/cpp20"}, []byte("FROM cbe/cpp20\n"), ErrBadRequest},
		"a link out of the context":     {BuildRequest{Exam: "exam1", Image: "cbe/cpp20"}, archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n", "x": "->../../etc/passwd"}), ErrBadRequest},
		"a context over the limit":      {BuildRequest{Exam: "exam1", Image: "cbe/cpp20"}, archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20\n", "big": strings.Repeat("x", maxBuildContextBytes+1)}), ErrBadRequest},
	} {
		if _, err := r.svc.BuildImage(ctx, c.req, bytes.NewReader(c.context)); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_ = good
	if r.rt.builds != 0 {
		t.Errorf("a refused request built something")
	}
}

func TestABuildThatFailsOrDoesNotEndIsAResultWithItsLog(t *testing.T) {
	r := newRig(t)
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, log io.Writer) error {
		fmt.Fprint(log, "STEP 1/3: FROM x\nSTEP 2/3: RUN make\ncc: error: no such file\n")
		r.rt.addImage(spec.Tag, r.rt.builtOn(baseImage(), spec)) // the half-made image is left under the temporary name
		return fmt.Errorf("%w: building at STEP \"RUN make\": while running runtime: exit status 2", ErrBuildFailed)
	}
	result, err := r.build(t, plainContainerfile, nil)
	if err != nil || result.OK || result.Failure != FailBuild || !strings.Contains(result.Message, "exit status 2") {
		t.Fatalf("%+v %v", result, err)
	}
	for _, want := range []string{"Base image:", "cc: error: no such file", "exit status 2"} {
		if !strings.Contains(result.Log, want) {
			t.Errorf("the log does not say %q:\n%s", want, result.Log)
		}
	}
	if result.Tag != "" && r.rt.images[result.Tag] || len(r.rt.images) != 1 {
		t.Errorf("a failed build left an image: %v", r.rt.images)
	}
	r.noContainersLeft(t)

	// A build that does not end is stopped at its time, with what it had printed.
	buildSecond = time.Millisecond
	t.Cleanup(func() { buildSecond = time.Second })
	r.rt.build = func(ctx context.Context, _ ImageBuildSpec, _ []byte, log io.Writer) error {
		fmt.Fprint(log, "STEP 2/3: RUN sleep 99999\n")
		<-ctx.Done()
		return ctx.Err()
	}
	req := r.buildRequest()
	req.TimeoutSeconds = 10
	slow, err := r.svc.BuildImage(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})))
	if err != nil || slow.OK || slow.Failure != FailTimeout || !strings.Contains(slow.Message, "did not end in 10 seconds") || !strings.Contains(slow.Log, "sleep 99999") {
		t.Errorf("a build that does not end: %+v %v", slow, err)
	}

	// A build that ends without an image says so; a runtime that fails is an error, not a result; a request that is cancelled is its error.
	r.rt.build = func(context.Context, ImageBuildSpec, []byte, io.Writer) error { return nil }
	if empty, err := r.build(t, plainContainerfile, nil); err != nil || empty.OK || empty.Failure != FailBuild || !strings.Contains(empty.Message, "without making an image") {
		t.Errorf("no image: %+v %v", empty, err)
	}
	r.rt.build = func(context.Context, ImageBuildSpec, []byte, io.Writer) error {
		return errors.New("the runtime is gone")
	}
	if _, err := r.build(t, plainContainerfile, nil); err == nil || !strings.Contains(err.Error(), "the runtime is gone") {
		t.Errorf("a runtime that fails: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.rt.build = func(ctx context.Context, _ ImageBuildSpec, _ []byte, _ io.Writer) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := r.svc.BuildImage(ctx, r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile}))); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled request: %v", err)
	}
	// Through all of that the temporary name never stayed.
	for ref := range r.rt.images {
		if strings.HasPrefix(ref, pendingRepo) {
			t.Errorf("the temporary image %s was left", ref)
		}
	}
}

// What the build makes is checked before it is kept: each thing the course image's contract needs.
func TestAnImageThatBreaksTheContractFailsVerification(t *testing.T) {
	cases := map[string]struct {
		change func(info *ImageInfo)
		sum    string
		says   []string
	}{
		"the user is root":           {func(i *ImageInfo) { i.User = "root" }, "", []string{"changed the user of the image", "USER 1000:1000"}},
		"no user":                    {func(i *ImageInfo) { i.User = "" }, "", []string{"changed the user"}},
		"another uid":                {func(i *ImageInfo) { i.User = "1001:1001" }, "", []string{"changed the user"}},
		"a different entrypoint":     {func(i *ImageInfo) { i.Entrypoint = []string{"/bin/sh"} }, "", []string{"changed the entrypoint", "the agent is what runs"}},
		"no entrypoint":              {func(i *ImageInfo) { i.Entrypoint = nil }, "", []string{"changed the entrypoint"}},
		"a command":                  {func(i *ImageInfo) { i.Cmd = []string{"sleep", "1"} }, "", []string{"changed the command"}},
		"another working folder":     {func(i *ImageInfo) { i.WorkDir = "/" }, "", []string{"changed the working folder"}},
		"the agent's label changed":  {func(i *ImageInfo) { i.Labels[LabelAgent] = strings.Repeat("d", 64) }, "", []string{"changed the label " + LabelAgent}},
		"the agent was replaced":     {func(*ImageInfo) {}, strings.Repeat("c", 64), []string{"is not the course image's agent", "must not replace it"}},
		"not made from the course":   {func(i *ImageInfo) { i.Layers = []string{"sha256:other1", "sha256:other2", "sha256:other3"} }, "", []string{"not made from the course image cbe/cpp20"}},
		"fewer layers than the base": {func(i *ImageInfo) { i.Layers = i.Layers[:1] }, "", []string{"not made from the course image"}},
	}
	for name, c := range cases {
		r := newRig(t)
		r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, log io.Writer) error {
			fmt.Fprint(log, "STEP 1/1: RUN true\n")
			info := r.rt.builtOn(baseImage(), spec)
			c.change(&info)
			r.rt.addImage(spec.Tag, info)
			if c.sum != "" {
				r.rt.agentSums[spec.Tag] = c.sum
			}
			return nil
		}
		result, err := r.build(t, plainContainerfile, nil)
		if err != nil || result.OK || result.Failure != FailVerify {
			t.Errorf("%s: %+v %v", name, result, err)
			continue
		}
		for _, say := range c.says {
			if !strings.Contains(result.Message, say) {
				t.Errorf("%s: the message does not say %q: %s", name, say, result.Message)
			}
		}
		if !strings.Contains(result.Log, result.Message) {
			t.Errorf("%s: the log does not hold the message", name)
		}
		if len(r.rt.images) != 1 || result.Tag != "" && r.rt.images[result.Tag] {
			t.Errorf("%s: an image that failed verification was kept: %v", name, r.rt.images)
		}
		r.noContainersLeft(t)
	}

	// A change that is allowed: more layers, other labels, files, even a different ENV. The agent in the image is still the course's.
	r := newRig(t)
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, _ io.Writer) error {
		info := r.rt.builtOn(baseImage(), spec)
		info.Layers = append(info.Layers, "sha256:more1", "sha256:more2")
		info.Labels["io.cbe.course"] = "cpp20-extra"
		r.rt.addImage(spec.Tag, info)
		return nil
	}
	if result, err := r.build(t, plainContainerfile, nil); err != nil || !result.OK {
		t.Errorf("an image that keeps the contract: %+v %v", result, err)
	}
	// A checking container that fails or prints nonsense fails the build too.
	r = newRig(t)
	r.rt.agentSums["localhost/cbe-pending/exam1:"] = ""
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, _ io.Writer) error {
		r.rt.addImage(spec.Tag, r.rt.builtOn(baseImage(), spec))
		r.rt.agentSums[spec.Tag] = "not a hash"
		return nil
	}
	if result, _ := r.build(t, plainContainerfile, nil); result.OK || result.Failure != FailVerify || !strings.Contains(result.Message, "could not be checked") {
		t.Errorf("a checking container that prints nonsense: %+v", result)
	}
}

func TestTheLogKeepsTheStartAndTheEndOfAHugeBuild(t *testing.T) {
	r := newRig(t)
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, log io.Writer) error {
		fmt.Fprint(log, "THE START OF THE BUILD\n")
		line := strings.Repeat("x", 99) + "\n"
		for range 30000 { // 3 MB
			fmt.Fprint(log, line)
		}
		fmt.Fprint(log, "THE END OF THE BUILD\n")
		r.rt.addImage(spec.Tag, r.rt.builtOn(baseImage(), spec))
		return nil
	}
	result, err := r.build(t, plainContainerfile, nil)
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(result.Log) > maxLogBytes+1024 || !strings.Contains(result.Log, "Base image:") || !strings.Contains(result.Log, "THE START OF THE BUILD") ||
		!strings.Contains(result.Log, "THE END OF THE BUILD") || !strings.Contains(result.Log, "bytes of the log are not kept") || !strings.Contains(result.Log, "Verified:") {
		t.Errorf("the log (%d bytes) keeps the wrong parts", len(result.Log))
	}
}

func TestOnlyOneBuildRunsAtATime(t *testing.T) {
	r := newRig(t)
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, _ io.Writer) error {
		time.Sleep(30 * time.Millisecond)
		r.rt.addImage(spec.Tag, r.rt.builtOn(baseImage(), spec))
		return nil
	}
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := BuildRequest{Exam: fmt.Sprintf("exam%d", i), Image: "cbe/cpp20"}
			if result, err := r.svc.BuildImage(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile}))); err != nil || !result.OK {
				t.Errorf("exam%d: %+v %v", i, result, err)
			}
		}()
	}
	wg.Wait()
	if r.rt.builds != 4 || r.rt.buildPeak != 1 {
		t.Errorf("%d builds, %d at once at most", r.rt.builds, r.rt.buildPeak)
	}
}

// The image of an exam is used by that exam only (decision P7-9), and a container made from it is made as every container is.
func TestAContainerAndAGradingRunUseTheExamsOwnImage(t *testing.T) {
	r := newRig(t)
	built, err := r.build(t, plainContainerfile, nil)
	if err != nil || !built.OK {
		t.Fatalf("%+v %v", built, err)
	}
	ctx := context.Background()
	req := r.request("s1")
	req.Build = built.Hash
	status, err := r.svc.Create(ctx, req)
	if err != nil || status.State != StateCreated {
		t.Fatalf("%+v %v", status, err)
	}
	var spec ContainerSpec
	for _, s := range r.rt.specs {
		if s.Labels[LabelSession] == "s1" {
			spec = s
		}
	}
	if spec.Image != built.Tag || spec.Labels[LabelBuild] != built.Hash || spec.Labels[LabelImage] != "cbe/cpp20" {
		t.Errorf("the container: image %q labels %v", spec.Image, spec.Labels)
	}
	// No network, whatever the build had: the spec is the one of any student's container.
	plain, _ := r.svc.Create(ctx, r.request("s2"))
	_ = plain
	var plainSpec ContainerSpec
	for _, s := range r.rt.specs {
		if s.Labels[LabelSession] == "s2" {
			plainSpec = s
		}
	}
	if spec.NetworkMode != "none" || !spec.ReadonlyRootfs || spec.User != "1000:1000" || len(spec.Binds) != 2 || len(spec.CapDrop) != 1 || spec.NetworkMode != plainSpec.NetworkMode ||
		spec.PidsLimit != plainSpec.PidsLimit || spec.SecurityOpt[0] != plainSpec.SecurityOpt[0] || len(spec.Tmpfs) != len(plainSpec.Tmpfs) {
		t.Errorf("a container of a built image is not made like any other: %+v against %+v", spec, plainSpec)
	}
	// Asking again for a session that has its container returns it, whatever build the new request names.
	again := r.request("s1")
	again.Build = "0123456789abcdef"
	if status, err := r.svc.Create(ctx, again); err != nil || status.Session != "s1" {
		t.Errorf("asking again: %+v %v", status, err)
	}
	// Another exam cannot have it: the name has the exam in it, so its image is not there, and a forged one is not taken.
	other := r.request("s3")
	other.Exam = "exam2"
	other.Build = built.Hash
	if _, err := r.svc.Create(ctx, other); !errors.Is(err, ErrNotFound) {
		t.Errorf("another exam's image that is not there: %v", err)
	}
	r.rt.addImage(BuiltReference("exam2", built.Hash), ImageInfo{ID: "sha256:forged", Labels: map[string]string{LabelBuildExam: "exam1", LabelBuildHash: built.Hash}})
	if _, err := r.svc.Create(ctx, other); !errors.Is(err, ErrNotApproved) {
		t.Errorf("an image built for another exam: %v", err)
	}
	for name, bad := range map[string]CreateRequest{
		"a short build":         {Session: "s4", Exam: "exam1", Image: "cbe/cpp20", Build: "abc"},
		"a build in capitals":   {Session: "s4", Exam: "exam1", Image: "cbe/cpp20", Build: "0123456789ABCDEF"},
		"a path as a build":     {Session: "s4", Exam: "exam1", Image: "cbe/cpp20", Build: "../../etc/passwd"},
		"an exam in capitals":   {Session: "s4", Exam: "Exam1", Image: "cbe/cpp20", Build: built.Hash},
		"an image not approved": {Session: "s4", Exam: "exam1", Image: "docker.io/library/alpine", Build: built.Hash},
	} {
		if _, err := r.svc.Create(ctx, bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	// A grading run of the exam is run in the exam's image, with the hardening of any grading run.
	greq := r.gradeRequest("g1")
	greq.Build = built.Hash
	if _, err := r.svc.Grade(ctx, greq, bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(archive(t, theBundle))); err != nil {
		t.Fatal(err)
	}
	var gspec ContainerSpec
	for _, s := range r.rt.specs {
		if s.Labels[LabelRole] == RoleGrading {
			gspec = s
		}
	}
	if gspec.Image != built.Tag || gspec.NetworkMode != "none" || gspec.Entrypoint[0] != GradingPath+"/grade" {
		t.Errorf("the grading container: %+v", gspec)
	}
	greq.Grading, greq.Exam = "g2", "exam2"
	if _, err := r.svc.Grade(ctx, greq, bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(archive(t, theBundle))); !errors.Is(err, ErrNotApproved) {
		t.Errorf("a grading run in another exam's image: %v", err)
	}
}

func TestImageInspectKnowsAnExamsBuiltImageOnlyAsItsOwn(t *testing.T) {
	r := newRig(t)
	built, _ := r.build(t, plainContainerfile, nil)
	ctx := context.Background()
	hash := built.Hash
	r.rt.addImage(BuiltReference("exam2", hash), ImageInfo{ID: "sha256:forged", Labels: map[string]string{LabelBuildExam: "exam1", LabelBuildHash: hash}})
	for name, c := range map[string]struct {
		image             string
		approved, present bool
	}{
		"its own":                      {BuiltName("exam1", hash), true, true},
		"one that is not there":        {BuiltName("exam3", hash), false, false},
		"a forged one":                 {BuiltName("exam2", hash), false, true},
		"a course image":               {"cbe/cpp20", true, true},
		"an image not approved":        {"docker.io/library/alpine", false, false},
		"a name that is not a build's": {"cbe-exam/exam1:latest", false, false},
	} {
		status, err := r.svc.ImageInspect(ctx, c.image)
		if err != nil || status.Approved != c.approved || status.Present != c.present {
			t.Errorf("%s: %+v %v", name, status, err)
		}
	}
}

func TestImageBuildOverTheAPI(t *testing.T) {
	r := newRig(t)
	client := r.api(t)
	ctx := context.Background()
	result, err := client.BuildImage(ctx, r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile, "x.txt": "x"})), nil)
	if err != nil || !result.OK || result.Hash == "" || !strings.Contains(result.Log, "Base image: cbe/cpp20") {
		t.Fatalf("%+v %v", result, err)
	}
	// A build that fails is a result with a 200, as a refused Containerfile is.
	refused, err := client.BuildImage(ctx, r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM ubuntu\n"})), nil)
	if err != nil || refused.OK || refused.Failure != FailContainerfile {
		t.Errorf("a Containerfile that is refused: %+v %v", refused, err)
	}
	// What the runner cannot take is the right status.
	if _, err := client.BuildImage(ctx, BuildRequest{Exam: "exam1", Image: "docker.io/library/alpine"}, bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM x\n"})), nil); !errors.Is(err, ErrNotApproved) {
		t.Errorf("an image that is not approved: %v", err)
	}
	if _, err := client.BuildImage(ctx, r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"x": "y"})), nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("no Containerfile: %v", err)
	}
	if _, err := NewClient(client.socket, "not the secret").BuildImage(ctx, r.buildRequest(), strings.NewReader(""), nil); err == nil {
		t.Error("a build without the secret")
	}
	if r.rt.builds != 1 {
		t.Errorf("%d builds, want 1", r.rt.builds)
	}
}

func TestTheBuildSettingsHaveDefaultsAndLimits(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.BuildMaxSeconds != 900 || cfg.BuildMemoryMB != 2048 {
		t.Errorf("defaults: %+v", cfg)
	}
	write := func(body string) Config {
		path := filepath.Join(t.TempDir(), "runner.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return cfg
	}
	if got := write(`{"build_max_seconds": 120, "build_memory_mb": 512}`); got.BuildMaxSeconds != 120 || got.BuildMemoryMB != 512 || got.GradeParallel != 2 {
		t.Errorf("overlay: %+v", got)
	}
	if got := write(`{}`); got.BuildMaxSeconds != 900 {
		t.Errorf("a file that does not set them: %+v", got)
	}
	for _, bad := range []Config{{}, {BuildMaxSeconds: 5, BuildMemoryMB: 2048}, {BuildMaxSeconds: 900, BuildMemoryMB: 64}} {
		c := DefaultConfig()
		if bad.BuildMaxSeconds != 0 || bad.BuildMemoryMB != 0 {
			c.BuildMaxSeconds, c.BuildMemoryMB = bad.BuildMaxSeconds, bad.BuildMemoryMB
		} else {
			c.BuildMaxSeconds = 0
		}
		if c.Validate() == nil {
			t.Errorf("accepted: %+v", c)
		}
	}
}

func TestARestartedRunnerRemovesTheCheckingContainersAStoppedOneLeft(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.rt.Create(ctx, ContainerSpec{Name: "cbe-verify-exam1-abc", Labels: map[string]string{LabelRole: RoleVerify, LabelExam: "exam1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Create(ctx, r.request("s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.rt.Inspect(ctx, "cbe-verify-exam1-abc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the checking container was left: %v", err)
	}
	if _, err := r.rt.Inspect(ctx, ContainerName("exam1", "s1")); err != nil {
		t.Errorf("a student's container was taken: %v", err)
	}
}

func TestTheLogBufferKeepsTheHeadAndTheRingInOrder(t *testing.T) {
	// Small writes of every size, past the head and around the ring more than once, must read back in order.
	l := newLogBuffer()
	var all []byte
	for i := 0; i < 200000; i++ {
		piece := []byte(fmt.Sprintf("line %d\n", i))
		all = append(all, piece...)
		l.Write(piece)
	}
	got := l.String()
	if !strings.HasPrefix(got, string(all[:logHeadBytes])) || !strings.HasSuffix(got, string(all[len(all)-(maxLogBytes-logHeadBytes):])) {
		t.Fatal("the head or the end of the log is not what was written")
	}
	if !strings.Contains(got, fmt.Sprintf("[… %d bytes of the log are not kept", len(all)-maxLogBytes)) {
		t.Errorf("the note of what is not kept is wrong")
	}
	// A single write larger than the ring, and one exactly the size of the room.
	big := newLogBuffer()
	big.Write([]byte(strings.Repeat("h", logHeadBytes)))
	big.Write([]byte(strings.Repeat("a", 2*maxLogBytes)))
	big.Write([]byte("END"))
	if text := big.String(); !strings.HasSuffix(text, "aaaEND") || !strings.HasPrefix(text, "hhh") || len(text) > maxLogBytes+200 {
		t.Errorf("a write larger than the ring: %d bytes", len(text))
	}
	// A log that fits is kept whole, with no note.
	small := newLogBuffer()
	small.Write([]byte("only this\n"))
	if small.String() != "only this\n" {
		t.Errorf("a short log: %q", small.String())
	}
	// Bytes that are not UTF-8 do not break the result.
	small.Write([]byte{0xff, 'x'})
	if text := small.String(); !strings.Contains(text, "x") || strings.ContainsRune(text, 0xff) && false {
		t.Errorf("bad UTF-8: %q", text)
	}
}

// A build that is cancelled in the middle of a step leaves the builder's working container, which the runtime does not list and can only
// remove by its name; the runner removes it. A build that ends any other way leaves nothing, so nothing is removed (a name that is not a
// student's, or another exam's, is never guessed at).
func TestACancelledBuildHasItsWorkingContainersRemoved(t *testing.T) {
	removed := func(r *rig) []string {
		var out []string
		for _, call := range r.rt.calls {
			if strings.HasPrefix(call, "remove ") && strings.Contains(call, "-working-container") {
				out = append(out, strings.TrimPrefix(call, "remove "))
			}
		}
		return out
	}
	buildSecond = time.Millisecond
	t.Cleanup(func() { buildSecond = time.Second })

	// A build that does not end in time: the first stage's container and the next one's are tried, and the stage after is not.
	r := newRig(t)
	r.rt.build = func(ctx context.Context, _ ImageBuildSpec, _ []byte, _ io.Writer) error {
		<-ctx.Done()
		return ctx.Err()
	}
	req := r.buildRequest()
	req.TimeoutSeconds = 10
	result, err := r.svc.BuildImage(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM cbe/cpp20 AS a\nRUN true\nFROM cbe/cpp20\nRUN true\n"})))
	if err != nil || result.Failure != FailTimeout {
		t.Fatalf("%+v %v", result, err)
	}
	if got := removed(r); strings.Join(got, ",") != "cpp20-working-container,cpp20-working-container-1,cpp20-working-container-2" {
		t.Errorf("after a build that timed out: %v", got)
	}

	// A request that is cancelled.
	r = newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.rt.build = func(ctx context.Context, _ ImageBuildSpec, _ []byte, _ io.Writer) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := r.svc.BuildImage(ctx, r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile}))); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if got := removed(r); len(got) == 0 || got[0] != "cpp20-working-container" {
		t.Errorf("after a cancelled request: %v", got)
	}

	// A build that fails, or succeeds, or is refused, removes nothing.
	for name, hook := range map[string]func(context.Context, ImageBuildSpec, []byte, io.Writer) error{
		"a step that fails": func(context.Context, ImageBuildSpec, []byte, io.Writer) error {
			return fmt.Errorf("%w: exit status 3", ErrBuildFailed)
		},
		"a good build": nil,
	} {
		r = newRig(t)
		r.rt.build = hook
		if _, err := r.build(t, plainContainerfile, nil); err != nil {
			t.Fatal(err)
		}
		if got := removed(r); len(got) != 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
	r = newRig(t)
	if result, _ := r.build(t, "FROM ubuntu\n", nil); result.Failure != FailContainerfile || len(removed(r)) != 0 {
		t.Errorf("a Containerfile that is refused: %+v %v", result, removed(r))
	}

	// A runner that starts finds what a runner that was stopped in the middle of a build left, and removes it.
	r = newRig(t)
	if _, err := r.rt.Create(context.Background(), ContainerSpec{Name: "cpp20-working-container-1", Labels: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.rt.Inspect(context.Background(), "cpp20-working-container-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the working container of a build that was stopped is still there: %v", err)
	}
}

// The answer of image.build is a stream (decision P7-10): what the build prints reaches the client while the build is still going, and the stream
// ends with the result, which holds the same log.
func TestTheLogOfABuildReachesTheClientWhileTheBuildGoes(t *testing.T) {
	r := newRig(t)
	release := make(chan struct{})
	r.rt.build = func(ctx context.Context, spec ImageBuildSpec, _ []byte, log io.Writer) error {
		fmt.Fprint(log, "STEP 1/2: FROM cbe/cpp20\nSTEP 2/2: RUN make\n")
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		fmt.Fprint(log, "make: done\n")
		r.rt.addImage(spec.Tag, r.rt.builtOn(baseImage(), spec))
		return nil
	}
	client := r.api(t)
	chunks := make(chan string, 100)
	type outcome struct {
		result BuildResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.BuildImage(context.Background(), r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})), func(text string) { chunks <- text })
		done <- outcome{result, err}
	}()
	// The base image and the steps are there while the build has not ended.
	var seen strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(seen.String(), "STEP 2/2: RUN make") {
		select {
		case text := <-chunks:
			seen.WriteString(text)
		case <-deadline:
			t.Fatalf("the log did not arrive while the build went: %q", seen.String())
		case got := <-done:
			t.Fatalf("the build ended first: %+v %v", got.result, got.err)
		}
	}
	if !strings.Contains(seen.String(), "Base image: cbe/cpp20") {
		t.Errorf("the start of the log: %q", seen.String())
	}
	select {
	case got := <-done:
		t.Fatalf("the build ended before it was released: %+v %v", got.result, got.err)
	default:
	}
	close(release)
	got := <-done
	if got.err != nil || !got.result.OK {
		t.Fatalf("%+v %v", got.result, got.err)
	}
	for len(chunks) > 0 {
		seen.WriteString(<-chunks)
	}
	// What was streamed is what the result's log says.
	if seen.String() != got.result.Log || !strings.Contains(got.result.Log, "make: done") || !strings.Contains(got.result.Log, "Verified:") {
		t.Errorf("the streamed text and the result's log differ:\nstreamed: %q\nresult:   %q", seen.String(), got.result.Log)
	}
}

// A great deal of output is sent in few messages, and none of it is lost; the result's own log is cut to its limit as before.
func TestAChattyBuildIsStreamedWholeInFewMessages(t *testing.T) {
	r := newRig(t)
	line := strings.Repeat("x", 99) + "\n"
	const writes = 30000
	r.rt.build = func(_ context.Context, spec ImageBuildSpec, _ []byte, log io.Writer) error {
		for range writes {
			fmt.Fprint(log, line)
		}
		r.rt.addImage(spec.Tag, r.rt.builtOn(baseImage(), spec))
		return nil
	}
	client := r.api(t)
	messages, total := 0, 0
	result, err := client.BuildImage(context.Background(), r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})), func(text string) { messages++; total += len(text) })
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if total < writes*len(line) || messages > writes/20 {
		t.Errorf("%d bytes in %d messages for %d writes of %d bytes", total, messages, writes, len(line))
	}
	if len(result.Log) > maxLogBytes+1024 || !strings.Contains(result.Log, "bytes of the log are not kept") {
		t.Errorf("the result's log is %d bytes", len(result.Log))
	}
}

// What the runner cannot take is refused with a status, as before, because the stream has not begun; a failure of its own after it began is the
// last message of the stream, and the client turns it into the error it is.
func TestAFailureBeforeAndAfterTheStreamBeginsIsTheRightError(t *testing.T) {
	r := newRig(t)
	client := r.api(t)
	if _, err := client.BuildImage(context.Background(), BuildRequest{Exam: "exam1", Image: "docker.io/library/alpine"}, bytes.NewReader(archive(t, map[string]string{"Containerfile": "FROM x\n"})), func(string) {}); !errors.Is(err, ErrNotApproved) {
		t.Errorf("before: %v", err)
	}
	r.rt.build = func(_ context.Context, _ ImageBuildSpec, _ []byte, log io.Writer) error {
		fmt.Fprint(log, "STEP 1/1: RUN make\n")
		return errors.New("the runtime is gone")
	}
	var seen strings.Builder
	_, err := client.BuildImage(context.Background(), r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})), func(text string) { seen.WriteString(text) })
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusInternalServerError || strings.Contains(api.Message, "gone") {
		t.Errorf("after: %v", err)
	}
	if !strings.Contains(seen.String(), "STEP 1/1: RUN make") {
		t.Errorf("what was printed before the failure was lost: %q", seen.String())
	}
}

// A client that goes away ends the build, which is what cancels it in the runtime; one whose runner goes away says the stream ended.
func TestAClientThatCancelsEndsTheBuildAndABrokenStreamIsAnError(t *testing.T) {
	r := newRig(t)
	cancelled := make(chan struct{})
	r.rt.build = func(ctx context.Context, _ ImageBuildSpec, _ []byte, log io.Writer) error {
		fmt.Fprint(log, "STEP 1/1: RUN sleep 1000\n")
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}
	client := r.api(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := client.BuildImage(ctx, r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})), func(string) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a client that cancels: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Error("the build was not ended when the client went away")
	}

	// A stream that ends with no result: a runner that died.
	// A short folder: the path of a Unix socket is at most 104 bytes on macOS, and t.TempDir has the name of the test in it.
	dir, err := os.MkdirTemp("", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "x.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{\"log\":\"the first line\\n\"}\n")
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	var got strings.Builder
	_, err = NewClient(socket, "secret").BuildImage(context.Background(), r.buildRequest(), bytes.NewReader(archive(t, map[string]string{"Containerfile": plainContainerfile})), func(text string) { got.WriteString(text) })
	if err == nil || !strings.Contains(err.Error(), "ended before the build's result") || got.String() != "the first line\n" {
		t.Errorf("a stream that ends with no result: %v (%q)", err, got.String())
	}
}
