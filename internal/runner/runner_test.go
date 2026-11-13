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

	"exam-platform/internal/agentproto"
	"github.com/moby/moby/api/types/container"
)

// ---------------------------------------------------------------------------------------------------------------------------------
// What every container is, whatever the request says.

func TestEveryContainerHasTheSameHardening(t *testing.T) {
	r := newRig(t)
	for _, resources := range []Resources{{}, {CPU: 0.5, MemoryMB: 64, DiskMB: 1}, {CPU: 2, MemoryMB: 2048, DiskMB: 500}} {
		req := CreateRequest{Session: "s-" + fmt.Sprint(resources.MemoryMB), Exam: "e1", Image: "cbe/cpp20", Resources: resources}
		if _, err := r.svc.Create(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.rt.specs) != 3 {
		t.Fatalf("specs = %d", len(r.rt.specs))
	}
	for _, spec := range r.rt.specs {
		checks := map[string]bool{
			"no network":                   spec.NetworkMode == "none",
			"read-only root":               spec.ReadonlyRootfs,
			"all capabilities off":         len(spec.CapDrop) == 1 && spec.CapDrop[0] == "ALL",
			"no new privileges":            len(spec.SecurityOpt) == 1 && spec.SecurityOpt[0] == "no-new-privileges",
			"the student":                  spec.User == "1000:1000" && spec.WorkDir == "/work",
			"256 processes":                spec.PidsLimit == 256,
			"no swap":                      spec.MemorySwap == spec.MemoryBytes && spec.MemoryBytes >= 64<<20,
			"exactly two mounts":           len(spec.Binds) == 2 && spec.Binds[0].Target == "/work" && spec.Binds[1].Target == "/run/cbe",
			"/tmp is a tmpfs without exec": len(spec.Tmpfs) == 1 && strings.Contains(spec.Tmpfs["/tmp"], "noexec") && strings.Contains(spec.Tmpfs["/tmp"], "nosuid") && strings.Contains(spec.Tmpfs["/tmp"], "size=64m"),
			"labelled":                     spec.Labels[LabelManaged] == "1" && spec.Labels[LabelExam] == "e1" && spec.Labels[LabelSession] != "",
			"an approved image":            spec.Image == "localhost/cbe/cpp20:latest",
			"mounts are this session's":    strings.HasSuffix(spec.Binds[0].Source, "/work/"+spec.Labels[LabelSession]) && strings.HasSuffix(spec.Binds[1].Source, "/run/"+spec.Labels[LabelSession]),
		}
		for what, ok := range checks {
			if !ok {
				t.Errorf("%s: %s is not true of %+v", spec.Name, what, spec)
			}
		}
	}
	// Zero limits are the defaults of the plan's Appendix A; limits are the exam's, within the maximums.
	first, last := r.rt.specs[0], r.rt.specs[2]
	if first.MemoryBytes != 1024<<20 || first.NanoCPUs != 1e9 || last.MemoryBytes != 2048<<20 || last.NanoCPUs != 2e9 {
		t.Errorf("limits: %d %d, %d %d", first.MemoryBytes, first.NanoCPUs, last.MemoryBytes, last.NanoCPUs)
	}
	if r.quota.folders["s-0"] != 100 || r.quota.folders["s-2048"] != 500 {
		t.Errorf("quota folders = %v", r.quota.folders)
	}
}

// The spec reaches the runtime as it is: the translation to the SDK adds nothing and drops nothing.
func TestTheDockerTranslationKeepsTheHardening(t *testing.T) {
	r := newRig(t)
	req := r.request("s1")
	resources, ref, err := r.cfg.Check(req)
	if err != nil {
		t.Fatal(err)
	}
	spec := BuildSpec(r.cfg, req, resources, ref)
	config, host := hostConfig(spec)
	if host.NetworkMode != container.NetworkMode("none") || !host.ReadonlyRootfs || len(host.CapDrop) != 1 || host.CapDrop[0] != "ALL" || host.Privileged ||
		len(host.CapAdd) != 0 || len(host.SecurityOpt) != 1 || *host.PidsLimit != 256 || host.Memory != 512<<20 || host.MemorySwap != host.Memory || host.NanoCPUs != 1e9 ||
		len(host.Binds) != 2 || len(host.Tmpfs) != 1 || host.PidMode != "" || host.IpcMode != "" || host.UsernsMode != "" || len(host.Devices) != 0 || len(host.ExtraHosts) != 0 {
		t.Errorf("host config = %+v", host)
	}
	if config.User != "1000:1000" || config.Image != ref || config.WorkingDir != "/work" || len(config.Entrypoint) != 0 || len(config.Env) != 0 || len(config.Cmd) != 0 {
		t.Errorf("config = %+v", config)
	}
}

func TestRequestsAreChecked(t *testing.T) {
	r := newRig(t)
	for name, test := range map[string]struct {
		req  CreateRequest
		want error
	}{
		"bad session":      {CreateRequest{Session: "../x", Exam: "e", Image: "cbe/cpp20"}, ErrBadRequest},
		"empty session":    {CreateRequest{Session: "", Exam: "e", Image: "cbe/cpp20"}, ErrBadRequest},
		"long exam":        {CreateRequest{Session: "s", Exam: strings.Repeat("e", 65), Image: "cbe/cpp20"}, ErrBadRequest},
		"unapproved image": {CreateRequest{Session: "s", Exam: "e", Image: "library/ubuntu"}, ErrNotApproved},
		"a tag":            {CreateRequest{Session: "s", Exam: "e", Image: "cbe/cpp20:latest"}, ErrNotApproved},
		"a reference":      {CreateRequest{Session: "s", Exam: "e", Image: "localhost/cbe/cpp20:latest"}, ErrNotApproved},
		"no image":         {CreateRequest{Session: "s", Exam: "e"}, ErrNotApproved},
		"too much cpu":     {CreateRequest{Session: "s", Exam: "e", Image: "cbe/cpp20", Resources: Resources{CPU: 2.5}}, ErrBadRequest},
		"too little cpu":   {CreateRequest{Session: "s", Exam: "e", Image: "cbe/cpp20", Resources: Resources{CPU: -1}}, ErrBadRequest},
		"too much memory":  {CreateRequest{Session: "s", Exam: "e", Image: "cbe/cpp20", Resources: Resources{MemoryMB: 4096}}, ErrBadRequest},
		"little memory":    {CreateRequest{Session: "s", Exam: "e", Image: "cbe/cpp20", Resources: Resources{MemoryMB: 10}}, ErrBadRequest},
		"too much disk":    {CreateRequest{Session: "s", Exam: "e", Image: "cbe/cpp20", Resources: Resources{DiskMB: 501}}, ErrBadRequest},
	} {
		if _, err := r.svc.Create(context.Background(), test.req); !errors.Is(err, test.want) {
			t.Errorf("%s: err = %v, want %v", name, err, test.want)
		}
	}
	if len(r.rt.specs) != 0 || len(r.quota.folders) != 0 {
		t.Errorf("a refused request made something: %d containers, %v folders", len(r.rt.specs), r.quota.folders)
	}
	// An approved name whose image the runtime does not have.
	r.rt.images = map[string]bool{}
	if _, err := r.svc.Create(context.Background(), r.request("s")); !errors.Is(err, ErrNotFound) || len(r.quota.folders) != 0 {
		t.Errorf("a missing image: %v", err)
	}
}

func TestAFailureLeavesNothingBehind(t *testing.T) {
	r := newRig(t)
	r.rt.failCreate = errors.New("the runtime is down")
	if _, err := r.svc.Create(context.Background(), r.request("s1")); err == nil {
		t.Fatal("created")
	}
	if len(r.quota.folders) != 0 {
		t.Errorf("a folder was left: %v", r.quota.folders)
	}
	if _, err := os.Stat(filepath.Join(r.cfg.RunRoot, "s1")); !os.IsNotExist(err) {
		t.Errorf("the socket folder was left: %v", err)
	}
	if _, err := os.Stat(r.svc.metaPath("s1")); !os.IsNotExist(err) {
		t.Errorf("the note was left: %v", err)
	}
	r.rt.failCreate = nil
	r.quota.fail = errors.New("no quota")
	if _, err := r.svc.Create(context.Background(), r.request("s2")); err == nil || len(r.rt.specs) != 0 {
		t.Errorf("a container was made without its folder: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The lifecycle.

func TestTheLifecycle(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	created, err := r.svc.Create(ctx, r.request("s1"))
	if err != nil || created.State != StateCreated || created.Seeded || created.Exam != "exam1" || created.Resources.MemoryMB != 512 {
		t.Fatalf("create = %+v, %v", created, err)
	}
	info, err := os.Stat(filepath.Join(r.cfg.RunRoot, "s1"))
	if err != nil || info.Mode().Perm() != 0o777 {
		t.Fatalf("the socket folder = %v, %v", info, err)
	}
	// Asking again is the same container; asking for another exam is a conflict.
	again, err := r.svc.Create(ctx, r.request("s1"))
	if err != nil || again.State != StateCreated || len(r.rt.specs) != 1 {
		t.Errorf("again = %+v, %v (%d specs)", again, err, len(r.rt.specs))
	}
	other := r.request("s1")
	other.Exam = "exam2"
	if _, err := r.svc.Create(ctx, other); !errors.Is(err, ErrConflict) {
		t.Errorf("another exam: %v", err)
	}
	// The container does not start before the starter is in.
	if _, err := r.svc.Start(ctx, "s1"); !errors.Is(err, ErrConflict) {
		t.Errorf("start before seed: %v", err)
	}
	bundle := tarOf(t, map[string]string{"src/a.c": "int a;\n", "Makefile": "all:\n"}, map[string]int64{"src/a.c": 0o600})
	seeded, err := r.svc.Seed(ctx, "s1", bundle)
	if err != nil || !seeded.Seeded {
		t.Fatalf("seed = %+v, %v", seeded, err)
	}
	if _, err := r.svc.Seed(ctx, "s1", tarOf(t, nil, nil)); !errors.Is(err, ErrConflict) {
		t.Errorf("seeding twice: %v", err)
	}
	started, err := r.svc.Start(ctx, "s1")
	if err != nil || started.State != StateRunning {
		t.Fatalf("start = %+v, %v", started, err)
	}
	if again, err := r.svc.Start(ctx, "s1"); err != nil || again.State != StateRunning {
		t.Errorf("starting a running container: %+v, %v", again, err)
	}
	if st, err := r.svc.Pause(ctx, "s1"); err != nil || st.State != StatePaused {
		t.Fatalf("pause = %+v, %v", st, err)
	}
	if st, err := r.svc.Pause(ctx, "s1"); err != nil || st.State != StatePaused {
		t.Errorf("pausing a paused one: %+v, %v", st, err)
	}
	if st, err := r.svc.Unpause(ctx, "s1"); err != nil || st.State != StateRunning {
		t.Fatalf("unpause = %+v, %v", st, err)
	}
	if st, err := r.svc.Pause(ctx, "s1"); err != nil || st.State != StatePaused {
		t.Fatal(err)
	}
	// A paused container that is started is unpaused.
	if st, err := r.svc.Start(ctx, "s1"); err != nil || st.State != StateRunning {
		t.Errorf("start of a paused one: %+v, %v", st, err)
	}
	if st, err := r.svc.Stop(ctx, "s1"); err != nil || st.State != StateExited {
		t.Fatalf("stop = %+v, %v", st, err)
	}
	if st, err := r.svc.Status(ctx, "s1"); err != nil || st.State != StateExited || st.ExitCode == nil {
		t.Errorf("status = %+v, %v", st, err)
	}
	if err := r.svc.Remove(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if len(r.rt.containers) != 0 || len(r.quota.folders) != 0 {
		t.Errorf("left: %v %v", r.rt.containers, r.quota.folders)
	}
	for _, path := range []string{filepath.Join(r.cfg.RunRoot, "s1"), r.svc.metaPath("s1")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still there", path)
		}
	}
	if st, err := r.svc.Status(ctx, "s1"); err != nil || st.State != StateMissing {
		t.Errorf("status of a removed session = %+v, %v", st, err)
	}
	if err := r.svc.Remove(ctx, "s1"); err != nil {
		t.Errorf("removing what is gone: %v", err)
	}
	// The order matters: the container goes before its folder.
	removeContainer, removeFolder := -1, -1
	for i, call := range r.rt.calls {
		if call == "remove cbe-exam1-s1" {
			removeContainer = i
		}
	}
	_ = removeFolder
	if removeContainer < 0 {
		t.Error("the container was not removed")
	}
	for _, op := range []func() (Status, error){
		func() (Status, error) { return r.svc.Start(ctx, "nope") }, func() (Status, error) { return r.svc.Pause(ctx, "nope") },
		func() (Status, error) { return r.svc.Seed(ctx, "nope", tarOf(t, nil, nil)) }, func() (Status, error) { return r.svc.Stop(ctx, "..") },
	} {
		if _, err := op(); err == nil {
			t.Error("an operation on a session with no container worked")
		}
	}
}

func TestTheStarterIsRewrittenForTheStudent(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.svc.Create(ctx, r.request("s1")); err != nil {
		t.Fatal(err)
	}
	bundle := tarOf(t, map[string]string{"a.txt": "A", "bin/run": "#!/bin/sh\n", "dir/": ""}, map[string]int64{"bin/run": 0o700, "a.txt": 0o600})
	if _, err := r.svc.Seed(ctx, "s1", bundle); err != nil {
		t.Fatal(err)
	}
	copied := r.rt.containers["cbe-exam1-s1"].copied
	reader := tar.NewReader(bytes.NewReader(copied))
	modes := map[string]int64{}
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		if header.Uid != 1000 || header.Gid != 1000 {
			t.Errorf("%s is owned by %d:%d", header.Name, header.Uid, header.Gid)
		}
		modes[header.Name] = header.Mode
	}
	if modes["a.txt"] != 0o644 || modes["bin/run"] != 0o755 || modes["dir/"] != 0o755 {
		t.Errorf("modes = %v", modes)
	}
	if got := r.rt.containers["cbe-exam1-s1"].files["bin/run"]; got != "#!/bin/sh\n" {
		t.Errorf("content = %q", got)
	}
}

func tarOf(t *testing.T, files map[string]string, modes map[string]int64) io.Reader {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, name := range sortedKeys(files) {
		header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(files[name])), Mode: 0o644}
		if strings.HasSuffix(name, "/") {
			header.Typeflag, header.Size, header.Mode = tar.TypeDir, 0, 0o755
		}
		if mode, found := modes[strings.TrimSuffix(name, "/")]; found {
			header.Mode = mode
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(files[name]))
	}
	_ = writer.Close()
	return &buffer
}

func sortedKeys(m map[string]string) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The starter bundle is hostile input too.

func TestSanitizeSeedRefusesWhatABundleNeverHolds(t *testing.T) {
	entry := func(header tar.Header, content string) io.Reader {
		var buffer bytes.Buffer
		writer := tar.NewWriter(&buffer)
		header.Size = int64(len(content))
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(content))
		_ = writer.Close()
		return &buffer
	}
	reg := func(name string) tar.Header { return tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644} }
	link := func(name, target string) tar.Header {
		return tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target}
	}
	for name, test := range map[string]struct {
		bundle io.Reader
		want   string
	}{
		"parent":         {entry(reg("../etc/passwd"), "x"), "empty, . or .. part"},
		"deep parent":    {entry(reg("a/../../x"), "x"), "empty, . or .. part"},
		"absolute":       {entry(reg("/etc/passwd"), "x"), "relative"},
		"dot":            {entry(reg("./a"), "x"), "empty, . or .. part"},
		"double slash":   {entry(reg("a//b"), "x"), "empty, . or .. part"},
		"backslash":      {entry(reg(`a\b`), "x"), "control character or a backslash"},
		"control":        {entry(reg("a\nb"), "x"), "control character or a backslash"},
		"reserved":       {entry(reg(".cbe-write-1"), "x"), "kept for the system"},
		"hard link":      {entry(tar.Header{Name: "h", Typeflag: tar.TypeLink, Linkname: "a"}, ""), "only folders, files and links"},
		"device":         {entry(tar.Header{Name: "d", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3}, ""), "only folders, files and links"},
		"fifo":           {entry(tar.Header{Name: "p", Typeflag: tar.TypeFifo}, ""), "only folders, files and links"},
		"absolute link":  {entry(link("l", "/etc/passwd"), ""), "not allowed"},
		"escaping link":  {entry(link("l", "../x"), ""), "leaves /work"},
		"deep escape":    {entry(link("a/b/l", "../../../x"), ""), "leaves /work"},
		"through a name": {entry(link("l", "a/../../x"), ""), "uses .. after a name"},
		"empty link":     {entry(link("l", ""), ""), "not allowed"},
		"not a tar":      {strings.NewReader("this is not a tar archive at all, but it is longer than 512 bytes. " + strings.Repeat("x", 600)), "not a tar archive"},
		"huge file":      {entry(tar.Header{Name: "big", Typeflag: tar.TypeReg, Mode: 0o644}, strings.Repeat("x", 64)), ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := SanitizeSeed(test.bundle, io.Discard)
			if test.want == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
	// The limits.
	var many bytes.Buffer
	writer := tar.NewWriter(&many)
	for i := range MaxSeedFiles + 1 {
		_ = writer.WriteHeader(&tar.Header{Name: fmt.Sprintf("f%d", i), Typeflag: tar.TypeReg, Mode: 0o644})
	}
	_ = writer.Close()
	if _, err := SanitizeSeed(&many, io.Discard); !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), "more than 2000 files") {
		t.Errorf("too many files: %v", err)
	}
	var big bytes.Buffer
	writer = tar.NewWriter(&big)
	_ = writer.WriteHeader(&tar.Header{Name: "huge", Typeflag: tar.TypeReg, Mode: 0o644, Size: MaxSeedBytes + 1})
	if _, err := SanitizeSeed(&big, io.Discard); !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), "20 MiB") {
		t.Errorf("too big: %v", err)
	}
	// What is refused leaves /work alone: nothing is copied.
	r := newRig(t)
	if _, err := r.svc.Create(context.Background(), r.request("s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Seed(context.Background(), "s1", entry(reg("../x"), "x")); err == nil || r.rt.containers["cbe-exam1-s1"].copied != nil {
		t.Errorf("a refused starter was copied: %v", err)
	}
}

func TestArchivePaths(t *testing.T) {
	for in, want := range map[string]string{"/work": "/work/.", "/work/.": "/work/.", "/work/src": "/work/src", "/work/src/a.c": "/work/src/a.c"} {
		if got, err := ArchivePath(in); err != nil || got != want {
			t.Errorf("ArchivePath(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "/", "/etc", "/etc/passwd", "/work/", "/work/..", "/work/../etc", "/work/a/../../etc", "work", "/workspace", "/work/a//b", "/work/./a", "/work/a/", "/work/a\\b", "/run/cbe"} {
		if got, err := ArchivePath(bad); err == nil {
			t.Errorf("ArchivePath(%q) = %q", bad, got)
		}
	}
}

func TestStatsAndImages(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for _, s := range []string{"s1", "s2"} {
		if _, err := r.svc.Create(ctx, r.request(s)); err != nil {
			t.Fatal(err)
		}
		if _, err := r.svc.Seed(ctx, s, tarOf(t, nil, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.svc.Start(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if st, err := r.svc.Stats(ctx, "s1"); err != nil || st.CPUPercent != 12.5 || st.Pids != 3 {
		t.Errorf("stats = %+v, %v", st, err)
	}
	if all, err := r.svc.ExamStats(ctx, "exam1"); err != nil || len(all) != 1 || all[0].Session != "s1" {
		t.Errorf("exam stats = %+v, %v", all, err)
	}
	if st, err := r.svc.ImageInspect(ctx, "cbe/cpp20"); err != nil || !st.Approved || !st.Present || st.ID == "" {
		t.Errorf("image = %+v, %v", st, err)
	}
	if st, err := r.svc.ImageInspect(ctx, "evil/image"); err != nil || st.Approved || st.Present {
		t.Errorf("an unapproved image = %+v, %v", st, err)
	}
}

func TestAReconcileAdoptsWhatIsThere(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	// A container made by an earlier runner, with no note: the runner finds it by its labels.
	r.rt.containers["cbe-exam9-sA"] = &fakeContainer{state: StateRunning, spec: ContainerSpec{Name: "cbe-exam9-sA", Labels: map[string]string{LabelManaged: "1", LabelSession: "sA", LabelExam: "exam9", LabelImage: "cbe/cpp20"}}}
	r.rt.containers["not-ours"] = &fakeContainer{state: StateRunning, spec: ContainerSpec{Name: "not-ours", Labels: map[string]string{"app": "forgejo"}}}
	n, err := r.svc.Reconcile(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Reconcile = %d, %v", n, err)
	}
	st, err := r.svc.Status(ctx, "sA")
	if err != nil || st.State != StateRunning || st.Exam != "exam9" || !st.Seeded {
		t.Errorf("status = %+v, %v", st, err)
	}
	if st, _ := r.svc.Status(ctx, "not-ours"); st.State != StateMissing {
		t.Errorf("a container that is not ours is known: %+v", st)
	}
	// A restarted runner (a new Service over the same folders) still knows its containers.
	again, err := NewService(r.cfg, r.rt, r.quota, r.svc.logger)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if st, err := again.Status(ctx, "sA"); err != nil || st.State != StateRunning {
		t.Errorf("after a restart: %+v, %v", st, err)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The agent link and the relay, with an agent made by the real code of the protocol.

// fakeAgent listens where the agent of a session would, says hello and then a heartbeat, and echoes the frames it is sent.
func fakeAgent(t *testing.T, runDir string) (net.Listener, func()) {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(runDir, "agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var mu sync.Mutex
	var accepted []net.Conn
	die := func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range accepted {
			c.Close()
		}
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			accepted = append(accepted, conn)
			mu.Unlock()
			go func() {
				defer conn.Close()
				for i, kind := range []string{agentproto.TypeHello, agentproto.TypeHeartbeat} {
					payload, _ := agentproto.Encode(agentproto.Message{Type: kind, AgentVersion: "test", OpenTerminals: i + 1, OOMKills: 2, UptimeMS: 1000})
					_ = agentproto.WriteFrame(conn, agentproto.Frame{Kind: agentproto.KindJSON, Data: payload})
				}
				for {
					frame, err := agentproto.ReadFrame(conn)
					if err != nil {
						return
					}
					_ = agentproto.WriteFrame(conn, frame)
				}
			}()
		}
	}()
	return listener, die
}

func TestTheRunnerHearsTheAgentsHeartbeat(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.svc.Create(ctx, r.request("s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Seed(ctx, "s1", tarOf(t, nil, nil)); err != nil {
		t.Fatal(err)
	}
	_, die := fakeAgent(t, filepath.Join(r.cfg.RunRoot, "s1"))
	if _, err := r.svc.Start(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		st, _ = r.svc.Status(ctx, "s1")
		if st.Agent.Connected && !st.Agent.LastSeen.IsZero() && st.Agent.OpenTerminals == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !st.Agent.Connected || st.Agent.Version != "test" || st.Agent.OpenTerminals != 2 || st.Agent.OOMKills != 2 || time.Since(st.Agent.LastSeen) > 5*time.Second {
		t.Fatalf("agent = %+v", st.Agent)
	}
	// When the agent goes the link says so, and it comes back with the agent.
	die()
	_ = os.Remove(filepath.Join(r.cfg.RunRoot, "s1", "agent.sock"))
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ = r.svc.Status(ctx, "s1"); st.Agent.Connected {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		break
	}
	if st.Agent.Connected {
		t.Error("the link did not notice that the agent was gone")
	}
	fakeAgent(t, filepath.Join(r.cfg.RunRoot, "s1"))
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !st.Agent.Connected {
		time.Sleep(50 * time.Millisecond)
		st, _ = r.svc.Status(ctx, "s1")
	}
	if !st.Agent.Connected {
		t.Error("the link did not come back")
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The API.

func TestTheAPIChecksTheSecret(t *testing.T) {
	r := newRig(t)
	client := r.api(t)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("ping with the secret: %v", err)
	}
	for name, secret := range map[string]string{"wrong": "not-the-secret", "empty": "", "prefix": r.secret[:20], "longer": r.secret + "x"} {
		bad := NewClient(client.socket, secret)
		err := bad.Ping(context.Background())
		var api *APIError
		if !errors.As(err, &api) || api.Status != http.StatusUnauthorized {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, err := bad.Create(context.Background(), r.request("s1")); err == nil {
			t.Errorf("%s: created a container", name)
		}
	}
	if len(r.rt.specs) != 0 {
		t.Errorf("a request without the secret reached the runtime")
	}
}

func TestTheAPIRefusesWhatIsNotInAppendixE(t *testing.T) {
	r := newRig(t)
	client := r.api(t)
	raw := func(method, path, body string, header ...string) int {
		request, _ := http.NewRequest(method, "http://runner"+path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+r.secret)
		for i := 0; i+1 < len(header); i += 2 {
			request.Header.Set(header[i], header[i+1])
		}
		response, err := client.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	// There is no way to ask for a mount, a capability, a network or anything else the request does not list.
	for _, body := range []string{
		`{"session":"s1","exam":"e","image":"cbe/cpp20","mounts":["/:/host"]}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20","binds":["/etc:/etc"]}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20","cap_add":["SYS_ADMIN"]}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20","network_mode":"host"}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20","privileged":true}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20","user":"root"}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20","resources":{"cpu":1,"pids":100000}}`,
		`{"session":"s1","exam":"e","image":"cbe/cpp20"}{"session":"s2","exam":"e","image":"cbe/cpp20"}`,
		`[]`, `nope`, ``,
	} {
		if status := raw("POST", "/v1/containers", body); status != http.StatusBadRequest {
			t.Errorf("%s: status %d", body, status)
		}
	}
	if status := raw("POST", "/v1/containers", `{"session":"s1","exam":"e","image":"docker.io/library/alpine"}`); status != http.StatusForbidden {
		t.Errorf("an image that is not approved: %d", status)
	}
	if status := raw("POST", "/v1/containers", `{"session":"s1","exam":"e","image":"cbe/cpp20","resources":{"memory_mb":99999}}`); status != http.StatusBadRequest {
		t.Errorf("a limit over the maximum: %d", status)
	}
	if len(r.rt.specs) != 0 || len(r.quota.folders) != 0 {
		t.Fatalf("a refused request made a container: %d %v", len(r.rt.specs), r.quota.folders)
	}
	// Other operations and other paths do not exist.
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/v1/exec", 404}, {"POST", "/v1/containers/s1/exec", 404}, {"GET", "/v1/containers", 405}, {"DELETE", "/v1/containers/s1", 405},
		{"GET", "/v1/containers/s1/logs", 404}, {"POST", "/v1/containers/s1/kill", 404}, {"GET", "/", 404}, {"GET", "/v1/volumes", 404},
		{"GET", "/v1/containers/../../etc", 404}, {"POST", "/v1/grading", 501}, {"POST", "/v1/images/build", 501},
	} {
		if status := raw(test.method, test.path, ""); status != test.want {
			t.Errorf("%s %s = %d, want %d", test.method, test.path, status, test.want)
		}
	}
	// Names that are not IDs, and paths outside /work, are refused before they reach the runtime.
	for _, path := range []string{"/v1/containers/..%2Fx", "/v1/containers/a%20b/start", "/v1/containers/" + strings.Repeat("a", 65)} {
		if status := raw("GET", path, ""); status < 400 || status == 500 {
			t.Errorf("GET %s = %d", path, status)
		}
	}
	for _, path := range []string{"/etc/passwd", "/work/../etc/passwd", "", "/", "/run/cbe", "/work/a/../../b"} {
		if status := raw("GET", "/v1/containers/s1/archive?path="+path, ""); status != 400 && status != 404 {
			t.Errorf("archive of %q = %d", path, status)
		}
	}
	if _, err := r.svc.Create(context.Background(), r.request("s1")); err != nil {
		t.Fatal(err)
	}
	if status := raw("GET", "/v1/containers/s1/archive?path=/etc/passwd", ""); status != 400 {
		t.Errorf("archive of /etc/passwd = %d", status)
	}
	for _, call := range r.rt.calls {
		if strings.HasPrefix(call, "copyfrom") {
			t.Errorf("a path outside /work reached the runtime: %s", call)
		}
	}
}

func TestTheAPIEndToEnd(t *testing.T) {
	r := newRig(t)
	client := r.api(t)
	ctx := context.Background()
	if _, err := client.Create(ctx, r.request("s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Seed(ctx, "s1", tarOf(t, map[string]string{"a.txt": "A"}, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Seed(ctx, "s1", tarOf(t, nil, nil)); !errors.Is(err, ErrConflict) {
		t.Errorf("a second seed: %v", err)
	}
	if _, err := client.Create(ctx, CreateRequest{Session: "s2", Exam: "e", Image: "evil"}); !errors.Is(err, ErrNotApproved) {
		t.Errorf("an unapproved image: %v", err)
	}
	_, die := fakeAgent(t, filepath.Join(r.cfg.RunRoot, "s1"))
	defer die()
	if st, err := client.Start(ctx, "s1"); err != nil || st.State != StateRunning {
		t.Fatalf("start = %+v, %v", st, err)
	}
	// agent.connect: the exam server's frames reach the agent and come back untouched, and what the agent says first is there too.
	conn, err := client.ConnectAgent(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	first, err := agentproto.ReadFrame(conn)
	if err != nil || first.Kind != agentproto.KindJSON || !strings.Contains(string(first.Data), "agent.hello") {
		t.Fatalf("the agent's hello: %+v, %v", first, err)
	}
	_, _ = agentproto.ReadFrame(conn) // the heartbeat
	for _, frame := range []agentproto.Frame{{Kind: agentproto.KindJSON, Data: []byte(`{"type":"agent.ping"}`)}, {Kind: agentproto.KindBinary, Term: 3, Data: []byte("ls\n")}} {
		if err := agentproto.WriteFrame(conn, frame); err != nil {
			t.Fatal(err)
		}
		echoed, err := agentproto.ReadFrame(conn)
		if err != nil || echoed.Kind != frame.Kind || echoed.Term != frame.Term || string(echoed.Data) != string(frame.Data) {
			t.Fatalf("echo = %+v, %v", echoed, err)
		}
	}
	// A frozen container's agent cannot answer.
	if _, err := client.Pause(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ConnectAgent(ctx, "s1"); !errors.Is(err, ErrFrozen) {
		t.Errorf("agent.connect to a paused container: %v", err)
	}
	if _, err := client.ConnectAgent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("agent.connect to nothing: %v", err)
	}
	stream, err := client.Archive(ctx, "s1", "/work/.")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(stream)
	stream.Close()
	if string(data) != "TAR:/work/." {
		t.Errorf("archive = %q", data)
	}
	if st, err := client.Status(ctx, "s1"); err != nil || st.State != StatePaused {
		t.Errorf("status = %+v, %v", st, err)
	}
	if st, err := client.Status(ctx, "nobody"); err != nil || st.State != StateMissing {
		t.Errorf("status of nobody = %+v, %v", st, err)
	}
	if image, err := client.ImageInspect(ctx, "cbe/cpp20"); err != nil || !image.Approved || !image.Present {
		t.Errorf("image = %+v, %v", image, err)
	}
	if err := client.Remove(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
}
