package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"exam-platform/internal/agentproto"
)

// Real builds of exams with a base of their own, through the container runtime of the container host (deploy/workspace/test-ownbase.sh runs this
// against a private runner): the author's Containerfile, the runner's finishing layer, the self test in a container made as a student's is, and then a
// real container made from the build and driven through its agent. The bases that are on the host are used, so no network is needed; with
// CBE_OWNBASE_NET=1 it also pulls Python and Redis (the latter declares a volume, which is refused).

func contextTar(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	// In a fixed order: the same files are the same bytes, which is what the image's name is made of (the compiler sorts them too).
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := files[name]
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buffer.Bytes())
}

func TestRealOwnBase(t *testing.T) {
	runner := realRunner(t)
	ctx := context.Background()
	exam := "ob" + strings.TrimPrefix(randomID("x"), "x-")
	build := func(t *testing.T, files map[string]string, rebuild bool) BuildResult {
		t.Helper()
		result, err := runner.BuildImage(ctx, BuildRequest{Exam: exam, Rebuild: rebuild}, contextTar(t, files), func(string) {})
		if err != nil {
			t.Fatalf("image.build: %v", err)
		}
		return result
	}
	// A real container of a build, an agent that answers, and what the student's terminal sees.
	session := func(t *testing.T, result BuildResult, seed ...[]byte) (*machine, *machineSession) {
		t.Helper()
		m := &machine{t: t, runner: runner, session: randomID("sob")}
		t.Cleanup(func() { _ = runner.Remove(ctx, m.session) })
		if _, err := runner.Create(ctx, CreateRequest{Session: m.session, Exam: exam, Image: "", Build: result.Hash, Resources: Resources{CPU: 1, MemoryMB: 256, DiskMB: 20}}); err != nil {
			t.Fatalf("create: %v", err)
		}
		starter := bytes.NewReader(nil)
		if len(seed) > 0 {
			starter = bytes.NewReader(seed[0])
		} else {
			starter = contextTar(t, map[string]string{"README.txt": "the starter\n"})
		}
		if _, err := runner.Seed(ctx, m.session, starter); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := runner.Start(ctx, m.session); err != nil {
			t.Fatalf("start: %v", err)
		}
		waitAgent(t, runner, m.session)
		return m, &machineSession{m: m, c: m.connect()}
	}

	t.Run("Alpine: a base with no bash, no user 1000, a busybox shell", func(t *testing.T) {
		result := build(t, map[string]string{
			"Containerfile": "FROM docker.io/library/alpine:3.20\nCOPY greeting.txt /srv/greeting.txt\nRUN echo 'made by the author' > /etc/author-was-here\n",
			"greeting.txt":  "hello from the exam's package\n",
		}, false)
		if !result.OK {
			t.Fatalf("%+v\n%s", result, result.Log)
		}
		for _, want := range []string{"cbe-finalize: student 1000:1000 (/bin/sh)", "self test  ok    user", "self test  ok    shell", "self test  ok    no-setuid-files", "Verified: student 1000:1000"} {
			if !strings.Contains(result.Log, want) {
				t.Errorf("the log lacks %q:\n%s", want, result.Log)
			}
		}
		_, s := session(t, result)
		for what, want := range map[string]string{
			"id -un; id -u; id -g":                                "student\n1000\n1000",
			"cat /srv/greeting.txt /etc/author-was-here":          "hello from the exam's package\nmade by the author",
			"echo $SHELL; echo $HOME":                             "/bin/sh\n/tmp/home",
			"grep '^student' /etc/passwd":                         "student:x:1000:1000:Student:/home/student:/bin/sh",
			"touch /usr/x 2>&1; echo status $?":                   "Read-only file system",
			"ls /work > /dev/null && echo work-ok":                "work-ok",
			"cat /proc/self/status | grep -E 'CapEff|NoNewPrivs'": "0000000000000000",
		} {
			if code, out := s.m.run(s.c, what); code != 0 || !strings.Contains(strings.ReplaceAll(out, "\r", ""), want) {
				t.Errorf("%q: status %d, output %q, want %q", what, code, out, want)
			}
		}
		if code, out := s.m.run(s.c, "ping -c 1 -W 1 1.1.1.1 2>&1; echo status $?"); !strings.Contains(out, "status 1") && !strings.Contains(out, "Network") {
			t.Errorf("the container has a network: %d %q", code, out)
		}
		// The same files, the same agent: the same image, built already.
		if again := build(t, map[string]string{
			"Containerfile": "FROM docker.io/library/alpine:3.20\nCOPY greeting.txt /srv/greeting.txt\nRUN echo 'made by the author' > /etc/author-was-here\n",
			"greeting.txt":  "hello from the exam's package\n",
		}, false); !again.OK || !again.Cached || again.Hash != result.Hash {
			t.Errorf("a second build: %+v", again)
		}
	})

	t.Run("Ubuntu: a base that has a user at 1000 already", func(t *testing.T) {
		result := build(t, map[string]string{"Containerfile": "FROM docker.io/library/ubuntu:24.04\nRUN echo 'ubuntu base' > /etc/base-was-here\n"}, false)
		if !result.OK {
			t.Fatalf("%+v\n%s", result, result.Log)
		}
		if !strings.Contains(result.Log, "took out the account(s) and group(s) that had 1000: ubuntu") {
			t.Errorf("the account that had 1000 is not said to be taken out:\n%s", result.Log)
		}
		_, s := session(t, result)
		for what, want := range map[string]string{
			"id -un":                                "student",
			"getent passwd 1000":                    "student:x:1000:1000",
			"getent passwd ubuntu; echo $?":         "2",
			"echo $SHELL":                           "/bin/bash",
			"cat /etc/base-was-here":                "ubuntu base",
			"test -e /usr/bin/sudo || echo no-sudo": "no-sudo",
			"find / -xdev -perm /6000 -type f 2>/dev/null | wc -l": "0",
		} {
			if code, out := s.m.run(s.c, what); code != 0 || !strings.Contains(strings.ReplaceAll(out, "\r", ""), want) {
				t.Errorf("%q: status %d, output %q, want %q", what, code, out, want)
			}
		}
	})

	t.Run("a Containerfile that breaks the rules, and a base the runner cannot finish", func(t *testing.T) {
		refused := build(t, map[string]string{"Containerfile": "FROM docker.io/library/alpine:3.20\nVOLUME /data\n"}, false)
		if refused.OK || refused.Failure != FailContainerfile || !strings.Contains(refused.Message, "VOLUME is not allowed") {
			t.Errorf("VOLUME: %+v", refused)
		}
		// FROM scratch has no /bin/sh for the runner's layer: it cannot be finished, and the message says why.
		scratch := build(t, map[string]string{"Containerfile": "FROM scratch\nCOPY x /x\n", "x": "x"}, false)
		if scratch.OK || scratch.Failure != FailBuild || !strings.Contains(scratch.Message, "the base has to be a Linux image with /bin/sh") {
			t.Errorf("FROM scratch: %+v\n%s", scratch, scratch.Log)
		}
		// A step that fails is the author's failure, with the step.
		broken := build(t, map[string]string{"Containerfile": "FROM docker.io/library/alpine:3.20\nRUN exit 3\n"}, false)
		if broken.OK || broken.Failure != FailBuild {
			t.Errorf("a failing step: %+v", broken)
		}
	})

	if os.Getenv("CBE_OWNBASE_NET") == "" {
		return
	}
	t.Run("Python with a library from the network", func(t *testing.T) {
		result := build(t, map[string]string{"Containerfile": "FROM docker.io/library/python:3.12-slim\nRUN pip install --no-cache-dir six==1.16.0\n"}, false)
		if !result.OK {
			t.Fatalf("%+v\n%s", result, result.Log)
		}
		if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "has no digest") {
			t.Errorf("a base with no digest is warned about: %q", result.Warnings)
		}
		_, s := session(t, result)
		if code, out := s.m.run(s.c, "python -c 'import six, sys; print(six.__version__, sys.version_info[:2])'"); code != 0 || !strings.Contains(out, "1.16.0 (3, 12)") {
			t.Errorf("python: %d %q", code, out)
		}
		if code, out := s.m.run(s.c, "python -c 'open(\"/work/out.txt\",\"w\").write(\"x\"); print(open(\"/work/out.txt\").read())'"); code != 0 || !strings.Contains(out, "x") {
			t.Errorf("python writes in /work: %d %q", code, out)
		}
	})
	t.Run("the C++ example package: a toolchain from the Ubuntu archive snapshot, and its starter compiles", func(t *testing.T) {
		containerfile, err := os.ReadFile("../../testdata/workspace/lab-listas-own-base/image/Containerfile")
		if err != nil {
			t.Fatal(err)
		}
		result := build(t, map[string]string{"Containerfile": string(containerfile)}, false)
		if !result.OK {
			t.Fatalf("%+v\n%s", result, result.Log)
		}
		bundle, _ := starterBundle(t)
		_, s := session(t, result, bundle)
		for what, want := range map[string]string{
			"g++ --version | head -n 1": "g++ (Ubuntu",
			"id -un":                    "student",
			"make 2>&1; echo status $?": "status 0",
		} {
			if code, out := s.m.run(s.c, what); code != 0 || !strings.Contains(strings.ReplaceAll(out, "\r", ""), want) {
				t.Errorf("%q: status %d, output %q, want %q", what, code, out, want)
			}
		}
	})
	t.Run("an image that declares a volume is refused, and says which", func(t *testing.T) {
		result := build(t, map[string]string{"Containerfile": "FROM docker.io/library/redis:7-alpine\nRUN echo hi > /etc/hi\n"}, false)
		if result.OK || result.Failure != FailVerify || !strings.Contains(result.Message, "declares the volume(s) /data") {
			t.Errorf("%+v\n%s", result, result.Log)
		}
	})
}

// machineSession is a machine with an agent connection.
type machineSession struct {
	m *machine
	c *agentproto.Client
}
