//go:build linux

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/runner"
	"exam-platform/internal/workspace/workspacetest"
)

// Phase I is Phase 7's real test: an exam's own image with nothing replaced. deploy/workspace/test-image.sh starts a private cbe-runner (its
// own socket and folders, the same runtime and quota helper) so that nobody's containers or images are touched, and runs this against it, with
//
//	CBE_E2E_PACKAGE        what examc made of testdata/workspace/lab-image (document.json, the three bundles)
//	CBE_E2E_PLAIN_PACKAGE  the same for testdata/workspace/lab-listas: an exam with no image folder, in the same class
//	CBE_E2E_SUBMISSIONS    testdata/workspace/lab-image-submissions
//	CBE_E2E_BASE_DIGEST    the digest of the course image on this host, which the build log must record
//	CBE_E2E_APT            1 to also build a library from the Ubuntu archive snapshot over HTTPS (needs the network; a few minutes)
//
// First, through the exam server and the proctor's routes: an exam whose package has image/ is refused a publication until it is built, is
// built in a real container build, is published, and its student compiles against a library that the course image does not have, with no
// network; an exam with no image/ in the same class has no such library. Then, straight at the runner: what the plan's acceptance asks of
// a build that breaks the rules, and the limits of a build.

type imageView struct {
	State   string `json:"state"`
	Ready   bool   `json:"ready"`
	Present *bool  `json:"present"`
	Build   *struct {
		ID         int64    `json:"build_id"`
		State      string   `json:"state"`
		Failure    string   `json:"failure"`
		Message    string   `json:"message"`
		Cached     bool     `json:"cached"`
		Hash       string   `json:"hash"`
		BaseName   string   `json:"base_name"`
		BaseDigest string   `json:"base_digest"`
		Warnings   []string `json:"warnings"`
		DurationMS int64    `json:"duration_ms"`
		Log        string   `json:"log"`
	} `json:"build"`
}

func (r *rig) imageOf(exam string, log bool) imageView {
	r.t.Helper()
	path := "/admin/workspace/exam/" + exam + "/image"
	if log {
		path += "?log=1"
	}
	var view imageView
	if err := json.Unmarshal(r.admin("GET", path, "", nil, 200), &view); err != nil {
		r.t.Fatal(err)
	}
	return view
}

// build builds an exam's image through the proctor's route and waits for the build to end.
func (r *rig) build(t *testing.T, exam, body string) imageView {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
		r.admin("POST", "/admin/workspace/exam/"+exam+"/image/build", "application/json", reader, 202)
	} else {
		r.admin("POST", "/admin/workspace/exam/"+exam+"/image/build", "", nil, 202)
	}
	var view imageView
	workspacetest.Eventually(t, 10*time.Minute, "the build to end", func() bool {
		view = r.imageOf(exam, false)
		return view.State != "queued" && view.State != "building"
	})
	return view
}

// publish publishes an exam, replacing the one that is published (students may be working in it).
func (r *rig) publish(exam, current string, want int) []byte {
	r.t.Helper()
	body := fmt.Sprintf(`{"expected_current":%q,"allow_active":true}`, current)
	return r.admin("POST", "/admin/exam/"+exam+"/publish", "application/json", strings.NewReader(body), want)
}

func phaseImage(t *testing.T, r *rig) {
	dir, err := os.MkdirTemp(env(t, "CBE_E2E_STATE"), "image-")
	if err != nil {
		t.Fatal(err)
	}
	r.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(adminTokn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.start()
	var class struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	if err := json.Unmarshal(r.admin("POST", "/admin/classes?name=Images", "text/csv", strings.NewReader(classCSV), 201), &class); err != nil || class.Class.ID == "" {
		t.Fatalf("class: %v", err)
	}
	imageExam := r.importExam(class.Class.ID, "Its own image", 0, 0)
	plainExam := r.importExamFrom(env(t, "CBE_E2E_PLAIN_PACKAGE"), class.Class.ID, "The course image", 0, 0)
	defer func() {
		r.removeWorkspaces(imageExam)
		r.removeWorkspaces(plainExam)
	}()
	var built imageView

	t.Run("an exam whose image is not built cannot be published", func(t *testing.T) {
		view := r.imageOf(imageExam, false)
		if view.State != "not_built" || view.Ready {
			t.Fatalf("before the build: %+v", view)
		}
		body := r.publish(imageExam, "", 409)
		if !strings.Contains(string(body), "has not been built") {
			t.Errorf("the refusal: %s", body)
		}
	})
	t.Run("the build, its log and the base's digest", func(t *testing.T) {
		start := time.Now()
		built = r.build(t, imageExam, "")
		t.Logf("built in %v", time.Since(start).Round(time.Millisecond))
		if built.State != "ready" || !built.Ready || built.Build == nil || built.Build.State != "ok" || built.Build.Cached || built.Present == nil || !*built.Present {
			t.Fatalf("after the build: %+v %+v", built, built.Build)
		}
		if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(built.Build.Hash) || built.Build.BaseName != "cbe/cpp20" || len(built.Build.Warnings) != 0 {
			t.Errorf("the build: %+v", built.Build)
		}
		// The plan's acceptance: the log records the base image's digest, and it is the course image's on this host.
		want := env(t, "CBE_E2E_BASE_DIGEST")
		log := r.imageOf(imageExam, true).Build.Log
		if built.Build.BaseDigest != want || !strings.Contains(log, "digest "+want) || !strings.Contains(log, "pinned by this build") {
			t.Errorf("the base digest is %q (the course image's is %q); log:\n%s", built.Build.BaseDigest, want, log)
		}
		for _, say := range []string{"STEP 1/", "FROM localhost/cbe/cpp20@" + want, "COPY --from=build", "Verified: made from cbe/cpp20", "Tagged localhost/cbe-exam/" + imageExam + ":" + built.Build.Hash} {
			if !strings.Contains(log, say) {
				t.Errorf("the log does not say %q", say)
			}
		}
		// The same files on the same base are a cache hit, quickly; a rebuild builds again.
		start = time.Now()
		again := r.build(t, imageExam, "")
		if !again.Build.Cached || again.Build.Hash != built.Build.Hash || time.Since(start) > 30*time.Second {
			t.Errorf("a second build: %+v after %v", again.Build, time.Since(start))
		}
		rebuilt := r.build(t, imageExam, `{"rebuild": true}`)
		if rebuilt.State != "ready" || rebuilt.Build.Cached || rebuilt.Build.Hash != built.Build.Hash {
			t.Errorf("a rebuild: %+v", rebuilt.Build)
		}
	})
	t.Run("published, its student compiles against the library, with no network and the contract of any container", func(t *testing.T) {
		r.publish(imageExam, "", 200)
		session := r.login("0001")
		r.waitRunning(session)
		c := workspacetest.Dial(t, r.base, session.Token)
		defer c.Close()
		overlay(t, c, filepath.Join(env(t, "CBE_E2E_SUBMISSIONS"), "good"))
		term := openTerminal(t, c)
		if code, out := sh(t, c, term, "ls /usr/local/include/greet.hpp /usr/local/lib/libgreet.a"); code != 0 {
			t.Fatalf("the library is not in the image: %d %s", code, out)
		}
		if code, out := sh(t, c, term, "cd /work && make run"); code != 0 || !strings.Contains(out, "hello, Ada") {
			t.Errorf("make run: %d %s", code, out)
		}
		// The acceptance: a container from a customized image has no network, and is hardened as every container is.
		for name, check := range map[string]struct {
			line string
			ok   func(code int, out string) bool
		}{
			"no name resolves":  {"getent hosts archive.ubuntu.com", func(code int, _ string) bool { return code != 0 }},
			"no connection":     {"timeout 5 bash -c 'exec 3<>/dev/tcp/1.1.1.1/53'", func(code int, _ string) bool { return code != 0 }},
			"only the loopback": {"echo ifaces=$(ls /sys/class/net | tr '\\n' ' ')", func(_ int, out string) bool { return regexp.MustCompile(`ifaces=lo\s*\r?\n`).MatchString(out) }},
			"the student":       {"echo uid=$(id -u)", func(_ int, out string) bool { return strings.Contains(out, "uid=1000") }},
			"the agent runs":    {"tr '\\0' ' ' < /proc/1/cmdline; echo", func(_ int, out string) bool { return strings.Contains(out, "/usr/local/bin/cbe-agent") }},
			"a read-only root":  {"touch /usr/local/x", func(code int, _ string) bool { return code != 0 }},
			"no capabilities": {"grep -E 'CapEff|NoNewPrivs' /proc/self/status | tr '\\t\\n' '  '", func(_ int, out string) bool {
				return strings.Contains(out, "CapEff: 0000000000000000") && strings.Contains(out, "NoNewPrivs: 1")
			}},
			"/work is writable":               {"touch /work/mine", func(code int, _ string) bool { return code == 0 }},
			"the certificates were only lent": {"ls /run/host-ca.crt /ca.crt", func(code int, _ string) bool { return code != 0 }},
		} {
			if code, out := sh(t, c, term, check.line); !check.ok(code, out) {
				t.Errorf("%s: %d %s", name, code, out)
			}
		}
		// The hidden test needs the library, so it can only pass in this exam's image: it is graded there.
		if reply := c.Call(map[string]any{"type": "submit"}); !workspacetest.OK(reply) {
			t.Fatalf("submit: %v", reply)
		}
		r.admin("POST", "/admin/workspace/session/"+session.ID+"/grade", "", nil, 202)
		var grading gradingView
		workspacetest.Eventually(t, 3*time.Minute, "the grading", func() bool {
			list := r.gradings(session.ID)
			if len(list) == 0 || (list[0].State != "done" && list[0].State != "failed") {
				return false
			}
			grading = list[0]
			return true
		})
		if grading.State != "done" || grading.Points == nil || *grading.Points != 10 {
			t.Errorf("the grading in the exam's image: %+v (%s)", grading, grading.Stderr)
		}
	})
	t.Run("the library is in that exam only", func(t *testing.T) {
		r.publish(plainExam, imageExam, 200)
		session := r.login("0002")
		r.waitRunning(session)
		c := workspacetest.Dial(t, r.base, session.Token)
		defer c.Close()
		term := openTerminal(t, c)
		if code, _ := sh(t, c, term, "ls /usr/local/include/greet.hpp"); code == 0 {
			t.Error("another exam's container has the library")
		}
		if code, out := sh(t, c, term, "cd /tmp && printf '#include <greet.hpp>\\nint main(){}\\n' > t.cpp && g++ t.cpp -o t 2>&1 | head -2; test -x /tmp/t"); code == 0 || !strings.Contains(out, "greet.hpp") {
			t.Errorf("a program that needs the library compiles in another exam: %d %s", code, out)
		}
		// Straight at the runner: another exam cannot be given this exam's image, whether it names the build or forges one.
		req := runner.CreateRequest{Session: "probe-other", Exam: plainExam, Image: "cbe/cpp20", Build: built.Build.Hash, Resources: runner.Resources{CPU: 1, MemoryMB: 256, DiskMB: 20}}
		if _, err := r.runner.Create(context.Background(), req); !errors.Is(err, runner.ErrNotFound) {
			t.Errorf("a container of another exam from this exam's build: %v", err)
		}
		status, err := r.runner.ImageInspect(context.Background(), runner.BuiltName(plainExam, built.Build.Hash))
		if err != nil || status.Present || status.Approved {
			t.Errorf("another exam's name for this build: %+v %v", status, err)
		}
		own, err := r.runner.ImageInspect(context.Background(), runner.BuiltName(imageExam, built.Build.Hash))
		if err != nil || !own.Present || !own.Approved {
			t.Errorf("this exam's own name: %+v %v", own, err)
		}
	})
	t.Run("a FROM that is not the exam's course image fails with a clear message", func(t *testing.T) { badFrom(t, r) })
	t.Run("a Containerfile that changes the user, the entrypoint or the agent fails verification", func(t *testing.T) { breaksTheContract(t, r) })
	t.Run("the limits of a build: its time, its memory, and one at a time", func(t *testing.T) { buildLimits(t, r) })
	if os.Getenv("CBE_E2E_APT") == "1" {
		t.Run("a library from the Ubuntu archive over HTTPS, with the lent certificates", func(t *testing.T) { aptLibrary(t, r) })
	}
}

// buildNow builds a Containerfile (and files) straight at the runner, as an exam of its own.
func (r *rig) buildNow(t *testing.T, exam, containerfile string, files map[string]string, seconds int) (runner.BuildResult, error) {
	t.Helper()
	all := map[string]string{"Containerfile": containerfile}
	for name, content := range files {
		all[name] = content
	}
	return r.runner.BuildImage(context.Background(), runner.BuildRequest{Exam: exam, Image: "cbe/cpp20", TimeoutSeconds: seconds}, bytes.NewReader(tarBundle(t, all)))
}

func badFrom(t *testing.T, r *rig) {
	digest := env(t, "CBE_E2E_BASE_DIGEST")
	for name, c := range map[string]struct{ text, failure, says string }{
		"another image":      {"FROM ubuntu:24.04\n", "containerfile", "FROM ubuntu:24.04: the only image a workspace exam's Containerfile may start from is the exam's own course image, cbe/cpp20"},
		"another course":     {"FROM cbe/python-nn\n", "containerfile", "the only image"},
		"the image's tag":    {"FROM cbe/cpp20:latest\n", "containerfile", "write the course image without a tag"},
		"a second stage":     {"FROM cbe/cpp20 AS a\nFROM docker.io/library/alpine:3.20\n", "containerfile", "the only image"},
		"host networking":    {"FROM cbe/cpp20\nRUN --network=host true\n", "containerfile", "host would let the build reach the services of the machine"},
		"a volume":           {"FROM cbe/cpp20\nVOLUME /etc\n", "containerfile", "VOLUME is not allowed"},
		"a digest not ours":  {"FROM cbe/cpp20@sha256:" + strings.Repeat("1", 64) + "\n", "base", digest},
		"copy from an image": {"FROM cbe/cpp20\nCOPY --from=docker.io/library/alpine:3.20 /bin/sh /x\n", "containerfile", "it can only name an earlier stage"},
	} {
		result, err := r.buildNow(t, "e2e-badfrom", c.text, nil, 0)
		if err != nil || result.OK || result.Failure != c.failure || !strings.Contains(result.Message, c.says) {
			t.Errorf("%s: %+v %v", name, result, err)
			continue
		}
		if !strings.Contains(result.Log, "Base image: cbe/cpp20") {
			t.Errorf("%s: the log has no base: %s", name, result.Log)
		}
	}
	// A digest that is ours is accepted, and the log says the Containerfile pinned it.
	if result, err := r.buildNow(t, "e2e-badfrom", "FROM cbe/cpp20@"+digest+"\nRUN true\n", nil, 0); err != nil || !result.OK || !strings.Contains(result.Log, "pinned by the Containerfile") {
		t.Errorf("a digest that is the server's: %+v %v", result, err)
	}
	// What the runner cannot take at all is an error, not a result.
	if _, err := r.runner.BuildImage(context.Background(), runner.BuildRequest{Exam: "e2e-badfrom", Image: "docker.io/library/alpine"}, bytes.NewReader(tarBundle(t, map[string]string{"Containerfile": "FROM alpine\n"}))); !errors.Is(err, runner.ErrNotApproved) {
		t.Errorf("an image that is not approved: %v", err)
	}
	if _, err := r.runner.BuildImage(context.Background(), runner.BuildRequest{Exam: "e2e-badfrom", Image: "cbe/cpp20"}, bytes.NewReader(tarBundle(t, map[string]string{"setup.sh": "x"}))); !errors.Is(err, runner.ErrBadRequest) {
		t.Errorf("a context with no Containerfile: %v", err)
	}
}

func breaksTheContract(t *testing.T, r *rig) {
	cases := map[string]struct {
		containerfile string
		files         map[string]string
		says          string
	}{
		"the user is left as root": {"FROM cbe/cpp20\nUSER root\nRUN echo x > /opt/x\n", nil, "changed the user of the image"},
		"another uid":              {"FROM cbe/cpp20\nUSER 0:0\n", nil, "changed the user of the image"},
		"another entrypoint":       {"FROM cbe/cpp20\nENTRYPOINT [\"/bin/sh\"]\n", nil, "changed the entrypoint of the image"},
		"a command":                {"FROM cbe/cpp20\nCMD [\"sleep\", \"1\"]\n", nil, "changed the command of the image"},
		"another working folder":   {"FROM cbe/cpp20\nWORKDIR /tmp\n", nil, "changed the working folder of the image"},
		"the agent replaced":       {"FROM cbe/cpp20\nUSER root\nCOPY --chmod=0755 agent /usr/local/bin/cbe-agent\nUSER 1000:1000\n", map[string]string{"agent": "#!/bin/sh\necho evil\n"}, "is not the course image's agent"},
		"the agent's label":        {"FROM cbe/cpp20\nLABEL io.cbe.agent.sha256=" + strings.Repeat("0", 64) + "\n", nil, "changed the label io.cbe.agent.sha256"},
	}
	for name, c := range cases {
		result, err := r.buildNow(t, "e2e-contract", c.containerfile, c.files, 0)
		if err != nil || result.OK || result.Failure != "verify" || !strings.Contains(result.Message, c.says) {
			t.Errorf("%s: %+v %v", name, result, err)
			continue
		}
		// A build that failed verification leaves no image under the exam's name: not now, and not for a student's container.
		if result.Hash != "" {
			if status, err := r.runner.ImageInspect(context.Background(), runner.BuiltName("e2e-contract", result.Hash)); err != nil || status.Present {
				t.Errorf("%s: the image of a build that failed verification is there: %+v %v", name, status, err)
			}
		}
	}
	// What a build may do: add files and layers, take root to do it, and give the student back.
	ok, err := r.buildNow(t, "e2e-contract", "FROM cbe/cpp20\nUSER root\nRUN echo extra > /opt/extra.txt\nENV EXTRA=1\nUSER 1000:1000\n", nil, 0)
	if err != nil || !ok.OK || ok.Failure != "" || !strings.Contains(ok.Log, "Verified: made from cbe/cpp20, user 1000:1000, the agent is the course image's") {
		t.Errorf("a build that keeps the contract: %+v %v", ok, err)
	}
}

func buildLimits(t *testing.T, r *rig) {
	// A build that does not end is stopped at its time, with what it printed, and leaves nothing running.
	start := time.Now()
	slow, err := r.buildNow(t, "e2e-limits", "FROM cbe/cpp20\nRUN echo begins && sleep 1000\n", nil, 10)
	if err != nil || slow.OK || slow.Failure != "timeout" || !strings.Contains(slow.Message, "did not end in 10 seconds") || !strings.Contains(slow.Log, "begins") {
		t.Errorf("a build that does not end: %+v %v", slow, err)
	}
	if took := time.Since(start); took < 9*time.Second || took > 60*time.Second {
		t.Errorf("the build was stopped after %v, want about 10 s", took)
	}
	// A step that uses more memory than the build's limit (the private runner's build_memory_mb is 256) is killed, and the build fails.
	greedy, err := r.buildNow(t, "e2e-limits", "FROM cbe/cpp20\nRUN sh -c 'x=$(head -c 600000000 /dev/zero | tr \"\\0\" a); echo ${#x}'\n", nil, 0)
	if err != nil || greedy.OK || greedy.Failure != "build" {
		t.Errorf("a step that uses more memory than the build's limit: %+v %v", greedy, err)
	} else {
		t.Logf("the memory limit stopped the build: %s", greedy.Message)
	}
	// One build at a time: two builds that each take five seconds take ten.
	var wg sync.WaitGroup
	results := make([]runner.BuildResult, 2)
	errs := make([]error, 2)
	begin := time.Now()
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = r.buildNow(t, fmt.Sprintf("e2e-serial%d", i), fmt.Sprintf("FROM cbe/cpp20\nRUN sleep 5 && echo %d\n", i), nil, 0)
		}()
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || !results[i].OK {
			t.Errorf("build %d: %+v %v", i, results[i], errs[i])
		}
	}
	if total := time.Since(begin); total < 9500*time.Millisecond {
		t.Errorf("two builds of five seconds each took %v together: they ran at once", total.Round(time.Millisecond))
	} else {
		t.Logf("two builds of five seconds each took %v: one at a time", total.Round(time.Millisecond))
	}
}

// aptLibrary builds nlohmann-json3-dev from the dated snapshot of the Ubuntu archive over HTTPS, using the certificates the build lends, and
// uses it in a grading run of that exam with no network; the same program does not compile in the course image.
func aptLibrary(t *testing.T, r *rig) {
	containerfile := `FROM cbe/cpp20
USER root
RUN --mount=type=bind,source=ca.crt,target=/run/host-ca.crt,ro \
    set -eux; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/20261105T000000Z\nSuites: noble noble-updates noble-security\nComponents: main universe\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' > /etc/apt/sources.list.d/snapshot.sources; \
    apt_options='-o Acquire::https::CaInfo=/run/host-ca.crt -o Acquire::Check-Valid-Until=false'; \
    apt-get ${apt_options} update; \
    apt-get ${apt_options} install -y --no-install-recommends nlohmann-json3-dev; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/* /etc/apt/sources.list.d/snapshot.sources
USER 1000:1000
`
	start := time.Now()
	result, err := r.buildNow(t, "e2e-apt", containerfile, nil, 0)
	if err != nil || !result.OK || len(result.Warnings) != 0 {
		t.Fatalf("the build: %+v %v", result, err)
	}
	t.Logf("built in %v: %s", time.Since(start).Round(time.Second), result.Tag)
	grade := `#!/bin/sh
printf '#include <nlohmann/json.hpp>\n#include <iostream>\nint main(){ std::cout << nlohmann::json::parse("[1,2,3]").size() << "\\n"; }\n' > /work/t.cpp
if g++ -std=c++20 /work/t.cpp -o /work/t 2>/work/err; then echo "library: $(/work/t)" >&2; else echo "library: missing" >&2; fi
if timeout 5 bash -c 'exec 3<>/dev/tcp/1.1.1.1/53' 2>/dev/null; then echo "network: yes" >&2; else echo "network: no" >&2; fi
if ls /run/host-ca.crt >/dev/null 2>&1; then echo "ca: left in the image" >&2; else echo "ca: not in the image" >&2; fi
echo '{}'
`
	run := func(build string) runner.GradeResult {
		got, err := r.runner.Grade(context.Background(), runner.GradeRequest{Grading: "apt" + build, Exam: "e2e-apt", Image: "cbe/cpp20", Build: build, TimeoutSeconds: 60,
			Resources: runner.Resources{CPU: 1, MemoryMB: 512, DiskMB: 50}}, bytes.NewReader(tarBundle(t, map[string]string{"x": "x"})), bytes.NewReader(tarBundle(t, map[string]string{"grade": grade})))
		if err != nil {
			t.Fatalf("grading with build %q: %v", build, err)
		}
		return got
	}
	if out := run(result.Hash).Stderr; !strings.Contains(out, "library: 3") || !strings.Contains(out, "network: no") || !strings.Contains(out, "ca: not in the image") {
		t.Errorf("in the exam's image: %s", out)
	}
	if out := run("").Stderr; !strings.Contains(out, "library: missing") {
		t.Errorf("in the course image the library is there: %s", out)
	}
}
