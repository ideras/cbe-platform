package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/agentproto"
)

// These tests run against the real runner, the real quota helper and rootless Podman on the container host, as the exam server's
// account (cbe-server), so that "the exam server cannot reach the runtime" is true of the process that runs them. They are skipped
// unless CBE_RUNNER_SOCKET is set; deploy/workspace/test-runner.sh sets everything up.
//
//	CBE_RUNNER_SOCKET   the runner's socket          CBE_RUNNER_SECRET_FILE   the shared secret's file
//	CBE_IT_PHASE        "A" (everything, then leave a session behind) or "B" (after the runner was restarted)

func realRunner(t *testing.T) *Client {
	t.Helper()
	socket := os.Getenv("CBE_RUNNER_SOCKET")
	if socket == "" {
		t.Skip("CBE_RUNNER_SOCKET is not set: these tests need the container host")
	}
	secret, err := os.ReadFile(os.Getenv("CBE_RUNNER_SECRET_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(socket, strings.TrimSpace(string(secret)))
}

func randomID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}

// starterBundle packs the example project the way the compiler does (owner 0, sorted), so the runner has to rewrite it.
func starterBundle(t *testing.T) ([]byte, map[string]string) {
	t.Helper()
	root := "../../testdata/workspace/lab-listas/project"
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	contents := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		name, _ := filepath.Rel(root, path)
		if d.IsDir() {
			return writer.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		contents[name] = string(data)
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	return buffer.Bytes(), contents
}

func readTarFiles(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	out := map[string]string{}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			data, _ := io.ReadAll(reader)
			out[strings.TrimPrefix(header.Name, "./")] = string(data)
		}
	}
}

type machine struct {
	t       *testing.T
	runner  *Client
	session string
	agent   *agentproto.Client
}

func (m *machine) connect() *agentproto.Client {
	m.t.Helper()
	conn, err := m.runner.ConnectAgent(context.Background(), m.session)
	if err != nil {
		m.t.Fatalf("agent.connect: %v", err)
	}
	c := agentproto.NewClient(conn)
	m.t.Cleanup(func() { c.Close() })
	return c
}

func (m *machine) run(c *agentproto.Client, cmd string) (int, string) {
	m.t.Helper()
	reply, err := c.Do(&agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 120, Rows: 30, Cmd: cmd}, 20*time.Second)
	if err != nil {
		m.t.Fatalf("pty.open %q: %v", cmd, err)
	}
	var output strings.Builder
	deadline := time.After(90 * time.Second)
	for {
		select {
		case out, open := <-c.Terminal:
			if !open {
				m.t.Fatalf("connection closed during %q", cmd)
			}
			if out.Term == reply.Term {
				output.Write(out.Data)
			}
		case event, open := <-c.Events:
			if !open {
				m.t.Fatalf("connection closed during %q", cmd)
			}
			if event.Type == agentproto.TypePTYExit && event.Term == reply.Term {
				for {
					select {
					case out := <-c.Terminal:
						if out.Term == reply.Term {
							output.Write(out.Data)
						}
						continue
					default:
					}
					break
				}
				_, _ = c.Do(&agentproto.PTYClose{Header: agentproto.Header{Type: agentproto.TypePTYClose}, Term: reply.Term}, 10*time.Second)
				return *event.ExitCode, output.String()
			}
		case <-deadline:
			m.t.Fatalf("%q did not end; output %q", cmd, output.String())
		}
	}
}

func waitAgent(t *testing.T, runner *Client, session string) Status {
	t.Helper()
	var status Status
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		if status, err = runner.Status(context.Background(), session); err == nil && status.State == StateRunning && status.Agent.Connected && !status.Agent.LastSeen.IsZero() {
			return status
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the agent of %s did not come up: %+v", session, status)
	return status
}

func TestRealRunner(t *testing.T) {
	runner := realRunner(t)
	ctx := context.Background()
	if os.Getenv("CBE_IT_PHASE") == "B" {
		realRunnerAfterRestart(t, runner)
		return
	}
	if err := runner.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	exam := randomID("exam")
	a := &machine{t: t, runner: runner, session: randomID("sa")}
	b := &machine{t: t, runner: runner, session: randomID("sb")}
	bundle, files := starterBundle(t)
	cleanup := func() {
		for _, m := range []*machine{a, b} {
			_ = runner.Remove(ctx, m.session)
		}
	}
	t.Cleanup(func() {
		if os.Getenv("CBE_IT_KEEP") == "" {
			cleanup()
		}
	})

	t.Run("images", func(t *testing.T) {
		if image, err := runner.ImageInspect(ctx, "cbe/cpp20"); err != nil || !image.Approved || !image.Present {
			t.Fatalf("image = %+v, %v", image, err)
		}
		if image, _ := runner.ImageInspect(ctx, "docker.io/library/debian"); image.Approved {
			t.Error("an unapproved image is approved")
		}
		if _, err := runner.Create(ctx, CreateRequest{Session: randomID("sx"), Exam: exam, Image: "docker.io/library/debian"}); !errors.Is(err, ErrNotApproved) {
			t.Errorf("an unapproved image: %v", err)
		}
	})

	t.Run("create, seed and start", func(t *testing.T) {
		for _, m := range []*machine{a, b} {
			status, err := runner.Create(ctx, CreateRequest{Session: m.session, Exam: exam, Image: "cbe/cpp20", Resources: Resources{CPU: 1, MemoryMB: 256, DiskMB: 20}})
			if err != nil || status.State != StateCreated {
				t.Fatalf("create = %+v, %v", status, err)
			}
			if _, err := runner.Seed(ctx, m.session, bytes.NewReader(bundle)); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if status, err := runner.Start(ctx, m.session); err != nil || status.State != StateRunning {
				t.Fatalf("start = %+v, %v", status, err)
			}
			waitAgent(t, runner, m.session)
		}
		if _, err := runner.Seed(ctx, a.session, bytes.NewReader(bundle)); !errors.Is(err, ErrConflict) {
			t.Errorf("a second seed: %v", err)
		}
	})

	t.Run("the files round-trip through the agent, owned by the student", func(t *testing.T) {
		c := a.connect()
		list, err := c.Do(&agentproto.FSList{Header: agentproto.Header{Type: agentproto.TypeFSList}}, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, entry := range list.Entries {
			names[entry.Name] = true
		}
		if !names["Makefile"] || !names["src"] || !names["include"] {
			t.Fatalf("the starter is not in /work: %+v", list.Entries)
		}
		read, err := c.Do(&agentproto.FSRead{Header: agentproto.Header{Type: agentproto.TypeFSRead}, Path: "src/list.cpp"}, 10*time.Second)
		if err != nil || *read.Content != files["src/list.cpp"] {
			t.Fatalf("read = %v", err)
		}
		if code, out := a.run(c, "stat -c '%u %g %a' Makefile src/list.cpp; id -u"); code != 0 || !strings.Contains(out, "1000 1000 644") {
			t.Errorf("owner and mode: %d %q", code, out)
		}
		edited := strings.Replace(files["src/list.cpp"], "(void)value;\n}\n\nvoid List::insert_sorted", "head_ = new Node{value, head_};\n}\n\nvoid List::insert_sorted", 1)
		if _, err := c.Do(&agentproto.FSWrite{Header: agentproto.Header{Type: agentproto.TypeFSWrite}, Path: "src/list.cpp", Content: edited, BaseVersion: read.Version}, 10*time.Second); err != nil {
			t.Fatal(err)
		}
		if code, out := a.run(c, "make check"); code != 0 || !strings.Contains(out, "push_front: ok") {
			t.Errorf("make check: %d %q", code, out)
		}
		files["src/list.cpp"] = edited
	})

	t.Run("snapshots read /work through the archive, running and paused, byte for byte", func(t *testing.T) {
		running, err := runner.Archive(ctx, a.session, "/work/.")
		if err != nil {
			t.Fatal(err)
		}
		fromRunning := readTarFiles(t, running)
		running.Close()
		for name, want := range files {
			if fromRunning[name] != want {
				t.Errorf("%s differs in the archive of the running container", name)
			}
		}
		if _, err := runner.Pause(ctx, a.session); err != nil {
			t.Fatal(err)
		}
		if status, _ := runner.Status(ctx, a.session); status.State != StatePaused {
			t.Fatalf("status = %+v", status)
		}
		paused, err := runner.Archive(ctx, a.session, "/work/.")
		if err != nil {
			t.Fatal(err)
		}
		fromPaused := readTarFiles(t, paused)
		paused.Close()
		for name, want := range files {
			if fromPaused[name] != want {
				t.Errorf("%s differs in the archive of the paused container", name)
			}
		}
		for name := range fromRunning {
			if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
				t.Errorf("unsafe name %q", name)
			}
		}
		if _, err := runner.ConnectAgent(ctx, a.session); !errors.Is(err, ErrFrozen) {
			t.Errorf("agent.connect to a paused container: %v", err)
		}
		if stats, err := runner.Stats(ctx, b.session); err != nil || stats.MemoryLimit == 0 || stats.Pids == 0 {
			t.Errorf("stats = %+v, %v", stats, err)
		}
		if all, err := runner.ExamStats(ctx, exam); err != nil || len(all) < 1 {
			t.Errorf("exam stats = %+v, %v", all, err)
		}
		if _, err := runner.Unpause(ctx, a.session); err != nil {
			t.Fatal(err)
		}
		waitAgent(t, runner, a.session)
	})

	t.Run("the abuse of one student does not reach the other", func(t *testing.T) {
		ca, cb := a.connect(), b.connect()
		// A fork bomb hits the limit of 256 processes, and B's agent still answers while it runs.
		reply, err := ca.Do(&agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cmd: "bash -c 'while :; do sleep 60 & done'"}, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		if _, err := cb.Do(&agentproto.Ping{Header: agentproto.Header{Type: agentproto.TypePing}}, 10*time.Second); err != nil {
			t.Fatalf("the other student's agent did not answer during a fork bomb: %v", err)
		}
		stats, err := runner.Stats(ctx, a.session)
		if err != nil || stats.Pids < 100 || stats.Pids > 260 {
			t.Errorf("the fork bomb's container has %d processes (%v)", stats.Pids, err)
		}
		_, _ = ca.Do(&agentproto.PTYClose{Header: agentproto.Header{Type: agentproto.TypePTYClose}, Term: reply.Term}, 20*time.Second)
		time.Sleep(time.Second)
		// A full disk is QUOTA.
		if _, out := a.run(ca, "dd if=/dev/zero of=big bs=1M count=100 2>&1; echo done"); !strings.Contains(out, "No space left") {
			t.Errorf("the disk did not fill: %q", out)
		}
		_, err = ca.Do(&agentproto.FSCreate{Header: agentproto.Header{Type: agentproto.TypeFSCreate}, Path: "after-full.txt", Content: "x"}, 10*time.Second)
		if agentproto.CodeOf(err) != agentproto.CodeQuota {
			t.Errorf("a create on a full disk: %v", err)
		}
		if _, err := cb.Do(&agentproto.FSCreate{Header: agentproto.Header{Type: agentproto.TypeFSCreate}, Path: "b-still-writes.txt", Content: "x"}, 10*time.Second); err != nil {
			t.Errorf("the other student's disk is full too: %v", err)
		}
		a.run(ca, "rm -f big")
		// There is no network.
		if code, _ := a.run(ca, "exec 3<>/dev/tcp/1.1.1.1/80"); code == 0 {
			t.Error("a connection to the Internet worked")
		}
		if code, out := a.run(ca, "getent hosts example.com; echo rc=$?"); strings.Contains(out, "rc=0") {
			t.Errorf("a name was resolved: %d %q", code, out)
		}
		// The other student's files are not there, and the host's are not reachable from either side.
		if code, out := a.run(ca, "ls /work/../ /run/cbe; cat /work/b-still-writes.txt"); code == 0 || strings.Contains(out, "b-still-writes") && !strings.Contains(out, "No such file") {
			t.Errorf("one student can see the other: %d %q", code, out)
		}
	})

	t.Run("the exam server's account cannot reach the runtime or a student's files", func(t *testing.T) {
		if _, err := os.Stat("/run/user/1001/podman/podman.sock"); err == nil {
			t.Error("the runtime's socket can be looked at by the exam server's account")
		}
		if conn, err := net.Dial("unix", "/run/user/1001/podman/podman.sock"); err == nil {
			conn.Close()
			t.Error("the runtime's socket can be connected to")
		}
		for _, session := range []string{a.session, b.session} {
			if _, err := os.ReadDir("/data/cbe-platform/work/" + session); err == nil {
				t.Errorf("the exam server's account can list the files of %s", session)
			}
			if _, err := os.Stat("/data/cbe-platform/run/" + session); err == nil {
				t.Errorf("the exam server's account can enter the runner's folders for %s", session)
			}
		}
		if _, err := os.ReadFile("/data/cbe-platform/runner/" + a.session + ".json"); err == nil {
			t.Error("the runner's notes can be read")
		}
	})

	t.Run("losing the agent: the container comes back on the same volume with its files", func(t *testing.T) {
		c := a.connect()
		// A student can end the agent with the one signal PID 1 hears: the container ends with it.
		reply, err := c.Do(&agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cmd: "kill -TERM 1; sleep 5"}, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = reply
		deadline := time.Now().Add(20 * time.Second)
		var status Status
		for time.Now().Before(deadline) {
			if status, _ = runner.Status(ctx, a.session); status.State == StateExited {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if status.State != StateExited {
			t.Fatalf("the container did not end: %+v", status)
		}
		if _, err := runner.Start(ctx, a.session); err != nil {
			t.Fatal(err)
		}
		waitAgent(t, runner, a.session)
		again := a.connect()
		read, err := again.Do(&agentproto.FSRead{Header: agentproto.Header{Type: agentproto.TypeFSRead}, Path: "src/list.cpp"}, 10*time.Second)
		if err != nil || *read.Content != files["src/list.cpp"] {
			t.Errorf("the student's work is not there after the restart: %v", err)
		}
	})

	t.Run("a session is left for the restart test", func(t *testing.T) {
		if os.Getenv("CBE_IT_KEEP") == "" {
			t.Skip("CBE_IT_KEEP is not set")
		}
		if err := os.WriteFile(os.Getenv("CBE_IT_STATE"), []byte(a.session+"\n"+b.session+"\n"+exam+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
}

// After the runner was restarted: it finds its containers again, its links to the agents come back, and the files are there.
func realRunnerAfterRestart(t *testing.T, runner *Client) {
	ctx := context.Background()
	data, err := os.ReadFile(os.Getenv("CBE_IT_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	ids := strings.Fields(string(data))
	for _, session := range ids[:2] {
		status := waitAgent(t, runner, session)
		if status.Exam != ids[2] || !status.Seeded {
			t.Errorf("status after the restart = %+v", status)
		}
		m := &machine{t: t, runner: runner, session: session}
		c := m.connect()
		if _, err := c.Do(&agentproto.FSRead{Header: agentproto.Header{Type: agentproto.TypeFSRead}, Path: "Makefile"}, 10*time.Second); err != nil {
			t.Errorf("%s: %v", session, err)
		}
		if err := runner.Remove(ctx, session); err != nil {
			t.Errorf("remove %s: %v", session, err)
		}
		if status, _ := runner.Status(ctx, session); status.State != StateMissing {
			t.Errorf("after remove: %+v", status)
		}
	}
	_ = os.Remove(os.Getenv("CBE_IT_STATE"))
}
