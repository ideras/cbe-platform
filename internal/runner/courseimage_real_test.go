package runner

import (
	"context"
	"os"
	"strings"
	"testing"
)

// A real container made from a course image that `cbe-portalctl image install` installed (deploy/workspace/test-imageinstall.sh sets it up against a
// private runner that approves it, and names it in CBE_COURSE_IMAGE): the course-image path (a name the runner approves, no build of the exam's own)
// runs an image that went through the runner's finishing and verification, as a student's container, and the agent in it answers.
func TestRealInstalledCourseImage(t *testing.T) {
	name := os.Getenv("CBE_COURSE_IMAGE")
	if name == "" {
		t.Skip("CBE_COURSE_IMAGE names an installed class image")
	}
	runner := realRunner(t)
	ctx := context.Background()
	status, err := runner.ImageInspect(ctx, name)
	if err != nil || !status.Approved || !status.Present {
		t.Fatalf("image.inspect %s: %+v %v (the runner must approve it, and the runtime have it)", name, status, err)
	}
	exam := "ci" + strings.TrimPrefix(randomID("x"), "x-")
	m := &machine{t: t, runner: runner, session: randomID("sci")}
	t.Cleanup(func() { _ = runner.Remove(ctx, m.session) })
	if _, err := runner.Create(ctx, CreateRequest{Session: m.session, Exam: exam, Image: name, Resources: Resources{CPU: 1, MemoryMB: 256, DiskMB: 20}}); err != nil {
		t.Fatalf("create from the installed image: %v", err)
	}
	if _, err := runner.Seed(ctx, m.session, contextTar(t, map[string]string{"README.txt": "the starter\n"})); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := runner.Start(ctx, m.session); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitAgent(t, runner, m.session)
	c := m.connect()
	for what, want := range map[string]string{
		"id -un; id -u; id -g":                                "student\n1000\n1000",
		"cat /work/README.txt":                                "the starter",
		"cat /srv/from-the-class-image.txt":                   "in the class image",
		"touch /usr/x 2>&1; echo status $?":                   "Read-only file system",
		"cat /proc/self/status | grep -E 'CapEff|NoNewPrivs'": "0000000000000000",
	} {
		if code, out := m.run(c, what); code != 0 || !strings.Contains(strings.ReplaceAll(out, "\r", ""), want) {
			t.Errorf("%q: status %d, output %q, want %q", what, code, out, want)
		}
	}
}
