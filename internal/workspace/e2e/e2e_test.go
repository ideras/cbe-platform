//go:build linux

// Package e2e tests workspace exams with nothing replaced: the exam server's binary, running as its own account, the installed
// cbe-runner and quota helper, rootless Podman, the real course image and the example package. It is skipped unless
// CBE_E2E_SERVER_BIN is set; deploy/workspace/test-server.sh sets everything up and runs it in two phases, restarting the runner
// between them.
//
//	CBE_E2E_SERVER_BIN        the server binary             CBE_E2E_PHASE   "A" (everything, leaves sessions), "B" (after the runner restarted) or "cleanup"
//	CBE_E2E_STATE             a folder both phases share    CBE_RUNNER_SOCKET / CBE_RUNNER_SECRET_FILE   the runner's
//	CBE_E2E_RUNTIME_SOCKET    the runtime's socket, which this account must not be able to reach
//	CBE_E2E_PACKAGE           a folder with what examc made of the example package: document.json, starter.tar, grading.tar
package e2e

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"exam-platform/internal/runner"
	"exam-platform/internal/workspace/workspacetest"
)

const (
	pin       = "e2e-pin-1234"
	classCSV  = "canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,ada@example.invalid,s1\n2,0002,Grace,grace@example.invalid,s2\n3,0003,Joan,joan@example.invalid,s3\n"
	adminTokn = "e2e-admin-token-0123456789abcdef0123456789abcdef"
)

// state is what phase A leaves for phase B.
type state struct {
	Dir      string                       `json:"dir"`
	ExamID   string                       `json:"exam_id"`
	Sessions map[string]sessionState      `json:"sessions"`
	Started  map[string]time.Time         `json:"started"`
	Files    map[string]map[string]string `json:"files"`
}

type sessionState struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type rig struct {
	t         *testing.T
	dir       string
	bin       string
	cmd       *exec.Cmd
	base      string
	runner    *runner.Client
	pkgDir    string
	state     state
	statePath string
}

func env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is not set", name)
	}
	return v
}

func newRig(t *testing.T) *rig {
	t.Helper()
	bin := os.Getenv("CBE_E2E_SERVER_BIN")
	if bin == "" {
		t.Skip("CBE_E2E_SERVER_BIN is not set: these tests need the container host (deploy/workspace/test-server.sh)")
	}
	secret, err := os.ReadFile(env(t, "CBE_RUNNER_SECRET_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, bin: bin, pkgDir: env(t, "CBE_E2E_PACKAGE"),
		runner:    runner.NewClient(env(t, "CBE_RUNNER_SOCKET"), strings.TrimSpace(string(secret))),
		statePath: filepath.Join(env(t, "CBE_E2E_STATE"), "state.json")}
	return r
}

func (r *rig) save() {
	data, _ := json.Marshal(r.state)
	if err := os.WriteFile(r.statePath, data, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) load() {
	data, err := os.ReadFile(r.statePath)
	if err != nil {
		r.t.Fatalf("phase B needs phase A's state: %v", err)
	}
	if err := json.Unmarshal(data, &r.state); err != nil {
		r.t.Fatal(err)
	}
	r.dir = r.state.Dir
}

// start runs the server on this rig's folder and waits for it to answer.
func (r *rig) start() {
	r.t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		r.t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	logFile, err := os.OpenFile(filepath.Join(r.dir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		r.t.Fatal(err)
	}
	r.cmd = exec.Command(r.bin, "-bind", addr, "-database", filepath.Join(r.dir, "exams.db"), "-screenshots", filepath.Join(r.dir, "shots"),
		"-admin-token-file", filepath.Join(r.dir, "admin-token"), "-runner-socket", env(r.t, "CBE_RUNNER_SOCKET"), "-runner-secret-file", env(r.t, "CBE_RUNNER_SECRET_FILE"),
		"-workspace-dir", filepath.Join(r.dir, "workspace"), "-workspace-snapshot-every", "10s", "-worker-interval", "500ms")
	r.cmd.Stdout, r.cmd.Stderr = logFile, logFile
	if err := r.cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	logFile.Close()
	r.base = "http://" + addr
	workspacetest.Eventually(r.t, 20*time.Second, "the server to answer", func() bool {
		resp, err := http.Get(r.base + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	})
	r.t.Cleanup(r.stop)
}

// stop ends the server the way a service manager does.
func (r *rig) stop() {
	if r.cmd == nil || r.cmd.Process == nil {
		return
	}
	_ = r.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = r.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = r.cmd.Process.Kill()
		<-done
		r.t.Error("the server did not stop on SIGTERM")
	}
	r.cmd = nil
}

func (r *rig) do(method, path, contentType, token string, body io.Reader) (int, []byte) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.base+path, body)
	if err != nil {
		r.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		r.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func (r *rig) admin(method, path, contentType string, body io.Reader, want int) []byte {
	r.t.Helper()
	status, data := r.do(method, path, contentType, adminTokn, body)
	if status != want {
		r.t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, data)
	}
	return data
}

// importExam compiles the example package, gives it a title, a PIN and (if set) another length, imports it and returns its ID.
func (r *rig) importExam(classID, title string, duration, grace int64) string {
	r.t.Helper()
	var exam map[string]any
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(r.pkgDir, name))
		if err != nil {
			r.t.Fatal(err)
		}
		return data
	}
	if err := json.Unmarshal(read("document.json"), &exam); err != nil {
		r.t.Fatal(err)
	}
	exam["title"], exam["pin"] = title, pin
	if duration > 0 {
		exam["duration_seconds"], exam["grace_seconds"] = duration, grace
	}
	document, _ := json.Marshal(exam)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, data := range map[string][]byte{"document": document, "starter": read("starter.tar"), "grading": read("grading.tar")} {
		part, _ := form.CreateFormField(name)
		_, _ = part.Write(data)
	}
	_ = form.Close()
	var created struct {
		ID string `json:"exam_id"`
	}
	if err := json.Unmarshal(r.admin("POST", "/admin/class/"+classID+"/workspace-exam", form.FormDataContentType(), &body, 201), &created); err != nil || created.ID == "" {
		r.t.Fatalf("import answer: %v", err)
	}
	return created.ID
}

func (r *rig) login(account string) sessionState {
	r.t.Helper()
	body, _ := json.Marshal(map[string]string{"account_number": account, "exam_pin": pin})
	status, data := r.do("POST", "/login", "application/json", "", bytes.NewReader(body))
	if status != 200 {
		r.t.Fatalf("login %s = %d: %s", account, status, data)
	}
	var out struct {
		SessionID    string `json:"session_id"`
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		r.t.Fatal(err)
	}
	return sessionState{ID: out.SessionID, Token: out.SessionToken}
}

type view struct {
	Terminal bool            `json:"terminal"`
	Done     map[string]bool `json:"done"`
	Status   struct {
		State string `json:"state"`
	} `json:"status"`
}

func (r *rig) view(s sessionState) (int, view) {
	status, data := r.do("GET", "/workspace", "", s.Token, nil)
	var v view
	_ = json.Unmarshal(data, &v)
	return status, v
}

// waitRunning waits for a student's workspace to be there.
func (r *rig) waitRunning(s sessionState) {
	r.t.Helper()
	workspacetest.Eventually(r.t, 90*time.Second, "the workspace of "+s.ID+" to run", func() bool {
		status, v := r.view(s)
		return status == 200 && v.Status.State == "running"
	})
}

type adminSession struct {
	State         string `json:"state"`
	AgentRestarts int    `json:"agent_restarts"`
	Snapshots     []struct {
		ID     int64  `json:"id"`
		Reason string `json:"reason"`
	} `json:"snapshots"`
}

func (r *rig) adminSession(s sessionState) adminSession {
	r.t.Helper()
	var out adminSession
	if err := json.Unmarshal(r.admin("GET", "/admin/workspace/session/"+s.ID, "", nil, 200), &out); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// snapshotFiles downloads a snapshot as the proctor would and returns its regular files.
func (r *rig) snapshotFiles(id int64) map[string]string {
	r.t.Helper()
	files := map[string]string{}
	reader := tar.NewReader(bytes.NewReader(r.admin("GET", fmt.Sprintf("/admin/workspace/snapshot/%d/tar", id), "", nil, 200)))
	for {
		header, err := reader.Next()
		if err != nil {
			return files
		}
		if header.Typeflag == tar.TypeReg {
			data, _ := io.ReadAll(reader)
			files[strings.TrimSuffix(header.Name, "/")] = string(data)
		}
	}
}

var counter int

// sh runs a command line in a student's terminal and returns its exit code and its output.
func sh(t *testing.T, c *workspacetest.Client, term uint32, line string) (int, string) {
	t.Helper()
	counter++
	tag := fmt.Sprintf("T%d", counter)
	before := len(c.OutputOf(term))
	c.Keys(term, line+"; printf 'R:%s:"+tag+"\\n' \"$?\"\n")
	pattern := regexp.MustCompile(`R:(\d+):` + tag + `\r?\n`)
	var out string
	workspacetest.Eventually(t, 60*time.Second, "the end of "+line, func() bool {
		out = c.OutputOf(term)[before:]
		return pattern.MatchString(out)
	})
	var code int
	fmt.Sscanf(pattern.FindStringSubmatch(out)[1], "%d", &code)
	return code, out
}

func openTerminal(t *testing.T, c *workspacetest.Client) uint32 {
	t.Helper()
	reply := c.Call(map[string]any{"type": "pty.open", "cols": 120, "rows": 30})
	if !workspacetest.OK(reply) {
		t.Fatalf("pty.open = %v", reply)
	}
	return uint32(workspacetest.Num(reply["term"]))
}

func TestRealServer(t *testing.T) {
	r := newRig(t)
	switch phase := os.Getenv("CBE_E2E_PHASE"); phase {
	case "A":
		phaseA(t, r)
	case "B":
		phaseB(t, r)
	case "cleanup":
		// Whatever the other phases made, whether or not they got to the end: the containers and their folders go.
		r.load()
		for name, s := range r.state.Sessions {
			if err := r.runner.Remove(context.Background(), s.ID); err != nil {
				t.Errorf("removing %s: %v", name, err)
			}
		}
	default:
		t.Fatalf("CBE_E2E_PHASE must be A, B or cleanup, not %q", phase)
	}
}

// The exam server's account has no road to the runtime: not the socket, not through the files of the process.
func checkNoRuntimeAccess(t *testing.T, r *rig) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Fatal("the test must run as the exam server's account, not root")
	}
	if _, err := net.Dial("unix", env(t, "CBE_E2E_RUNTIME_SOCKET")); !errors.Is(err, os.ErrPermission) && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("this account reached the runtime's socket: %v", err)
	} else if err == nil {
		t.Error("dial succeeded")
	}
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", r.cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		target, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", r.cmd.Process.Pid, fd.Name()))
		if strings.Contains(target, "podman") || strings.Contains(target, "docker") || strings.Contains(target, "containers") {
			t.Errorf("the server holds %s", target)
		}
	}
	status, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", r.cmd.Process.Pid))
	if !regexp.MustCompile(fmt.Sprintf(`Uid:\s+%d\s`, os.Getuid())).Match(status) {
		t.Errorf("the server's status: %s", status)
	}
}

func phaseA(t *testing.T, r *rig) {
	dir, err := os.MkdirTemp(env(t, "CBE_E2E_STATE"), "server-")
	if err != nil {
		t.Fatal(err)
	}
	r.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(adminTokn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.state = state{Dir: dir, Sessions: map[string]sessionState{}, Started: map[string]time.Time{}, Files: map[string]map[string]string{}}
	r.start()
	checkNoRuntimeAccess(t, r)

	// The class, the exam, the students.
	var class struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	if err := json.Unmarshal(r.admin("POST", "/admin/classes?name=E2E", "text/csv", strings.NewReader(classCSV), 201), &class); err != nil || class.Class.ID == "" {
		t.Fatalf("class: %v", err)
	}
	r.state.ExamID = r.importExam(class.Class.ID, "E2E lab", 0, 0)
	r.admin("POST", "/admin/exam/"+r.state.ExamID+"/publish", "application/json", strings.NewReader(`{"expected_current":""}`), 200)
	ada, grace := r.login("0001"), r.login("0002")
	r.state.Sessions["ada"], r.state.Sessions["grace"] = ada, grace
	r.waitRunning(ada)
	r.waitRunning(grace)
	r.save()

	// What the student's side of the exam holds, and what it does not.
	_, data := r.do("GET", "/workspace", "", ada.Token, nil)
	for _, secret := range []string{"hidden", "cbe/cpp20", `"run":`, `"check":`, "grading", pin, "starter_bundle", "memory_mb"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the student's view has %q: %s", secret, data)
		}
	}
	if status, _ := r.do("GET", "/admin/workspace/session/"+ada.ID, "", ada.Token, nil); status != 401 {
		t.Errorf("a student read the admin view: %d", status)
	}

	// Both students' containers exist, are limited as the plan says, and are two different things.
	for name, s := range r.state.Sessions {
		status, err := r.runner.Status(context.Background(), s.ID)
		if err != nil || status.State != "running" || status.Resources.CPU != 1 || status.Resources.MemoryMB != 1024 || status.Resources.DiskMB != 100 || !status.Agent.Connected {
			t.Errorf("%s: container %+v, %v", name, status, err)
		}
		r.state.Started[name] = status.StartedAt
	}
	r.save()

	t.Run("the student works in their workspace", func(t *testing.T) {
		c := workspacetest.Dial(t, r.base, ada.Token)
		c.Wait("ws.status", c.OfType("ws.status"))
		list := c.Call(map[string]any{"type": "fs.list", "path": ""})
		var names []string
		for _, e := range list["entries"].([]any) {
			names = append(names, e.(map[string]any)["name"].(string))
		}
		if !workspacetest.OK(list) || strings.Join(names, ",") != "Makefile,include,src,tests" {
			t.Fatalf("list = %v", list)
		}
		read := c.Call(map[string]any{"type": "fs.read", "path": "src/list.cpp"})
		if !workspacetest.OK(read) {
			t.Fatalf("read = %v", read)
		}
		edited := read["content"].(string) + "\n// ada was here: ñ 世界\n"
		if reply := c.Call(map[string]any{"type": "fs.write", "path": "src/list.cpp", "content": edited, "base_version": read["version"]}); !workspacetest.OK(reply) {
			t.Fatalf("write = %v", reply)
		}
		r.state.Files["ada"] = map[string]string{"src/list.cpp": edited}
		// The buttons build and run the exam's own commands.
		run := c.Call(map[string]any{"type": "run.start", "which": "run", "cols": 100, "rows": 30})
		if !workspacetest.OK(run) {
			t.Fatalf("run.start = %v", run)
		}
		term := uint32(workspacetest.Num(run["term"]))
		c.WaitExit(term)
		if out := c.OutputOf(term); !strings.Contains(out, "g++") {
			t.Errorf("Run did not build: %q", out)
		}
		check := c.Call(map[string]any{"type": "run.start", "which": "check"})
		c.WaitExit(uint32(workspacetest.Num(check["term"])))
		if out := c.OutputOf(uint32(workspacetest.Num(check["term"]))); !strings.Contains(out, "sample_test") {
			t.Errorf("Check did not build the tests: %q", out)
		}
		if reply := c.Call(map[string]any{"type": "todo.set", "item": "t1", "done": true}); !workspacetest.OK(reply) {
			t.Fatalf("todo.set = %v", reply)
		}
		// A terminal of their own: what it is allowed and what it is not.
		shell := openTerminal(t, c)
		for name, test := range map[string]struct {
			line string
			ok   bool
			has  string
		}{
			"the student's account":     {"id -u", true, "1000"},
			"no network":                {"bash -c 'echo > /dev/tcp/1.1.1.1/53'", false, ""},
			"no interfaces but lo":      {"ls /sys/class/net", true, "lo"},
			"a read-only root":          {"touch /etc/cbe-test", false, "Read-only"},
			"no execution from /tmp":    {"cp /bin/true /tmp/t && /tmp/t", false, "denied"},
			"no runtime socket":         {"ls /run/podman /var/run/docker.sock /run/user", false, ""},
			"the work folder is theirs": {"touch /work/mine && rm /work/mine", true, ""},
			"no way to be root":         {"sudo -n true", false, ""},
			"no capabilities":           {"grep CapEff /proc/self/status", true, "0000000000000000"},
		} {
			code, out := sh(t, c, shell, test.line)
			if (code == 0) != test.ok || !strings.Contains(out, test.has) {
				t.Errorf("%s: %q exited %d with %q", name, test.line, code, out)
			}
		}
		// And Submit takes a snapshot of exactly what is there.
		if reply := c.Call(map[string]any{"type": "submit"}); !workspacetest.OK(reply) {
			t.Fatalf("submit = %v", reply)
		}
		session := r.adminSession(ada)
		var submit int64
		for _, s := range session.Snapshots {
			if s.Reason == "submit" {
				submit = s.ID
			}
		}
		if submit == 0 {
			t.Fatalf("no submit snapshot: %+v", session)
		}
		files := r.snapshotFiles(submit)
		if files["src/list.cpp"] != edited || files["Makefile"] == "" || files["include/list.hpp"] == "" {
			t.Errorf("the snapshot's files: %d, list.cpp equal %v", len(files), files["src/list.cpp"] == edited)
		}
		// A snapshot is a copy of everything in /work, for recovery: build products too. What a submission leaves out (the exam's ignore
		// and submit lists) is applied when the submission is made (Phase 5), not here.
		if files["build/list_demo"] == "" {
			t.Error("the snapshot lacks what the Run button built")
		}
		for _, hidden := range []string{"grade", "tests/hidden"} {
			if _, found := files[hidden]; found {
				t.Errorf("the snapshot has %s", hidden)
			}
		}
	})

	t.Run("a connection that idles and one that is dropped", func(t *testing.T) {
		c := workspacetest.Dial(t, r.base, ada.Token)
		c.Wait("ws.status", c.OfType("ws.status"))
		term := openTerminal(t, c)
		sh(t, c, term, "echo scrollback-$((20+22)); echo changed-during-the-wait > /work/wait.txt")
		time.Sleep(40 * time.Second) // longer than the server's read and write timeouts
		if reply := c.Call(map[string]any{"type": "fs.list", "path": "src"}); !workspacetest.OK(reply) {
			t.Errorf("after 40 s of silence: %v", reply)
		}
		c.Close()
		again := workspacetest.Dial(t, r.base, ada.Token)
		again.Wait("ws.status", again.OfType("ws.status"))
		list := again.Call(map[string]any{"type": "pty.list"})
		found := false
		for _, e := range list["terminals"].([]any) {
			found = found || uint32(workspacetest.Num(e.(map[string]any)["term"])) == term
		}
		if !found {
			t.Fatalf("the terminal is gone: %v", list)
		}
		if reply := again.Call(map[string]any{"type": "pty.attach", "term": term}); !workspacetest.OK(reply) {
			t.Fatal(reply)
		}
		again.WaitOutput(term, "scrollback-42")
		// A periodic snapshot appeared meanwhile, with nothing in it that is not the student's.
		session := r.adminSession(ada)
		var periodic int64
		for _, s := range session.Snapshots {
			if s.Reason == "periodic" {
				periodic = s.ID
			}
		}
		if periodic == 0 {
			t.Fatalf("no periodic snapshot after 40 s with an interval of 10 s: %+v", session)
		}
		if got := r.snapshotFiles(periodic)["wait.txt"]; got != "changed-during-the-wait\n" {
			t.Errorf("the periodic snapshot lacks the file written before it: %q", got)
		}
	})

	t.Run("a student who abuses their container hurts nobody else", func(t *testing.T) {
		g := workspacetest.Dial(t, r.base, grace.Token)
		g.Wait("ws.status", g.OfType("ws.status"))
		a := workspacetest.Dial(t, r.base, ada.Token)
		a.Wait("ws.status", a.OfType("ws.status"))
		bad, good := openTerminal(t, g), openTerminal(t, a)
		// Processes: as many as the limit allows, then the next is refused; the other student's shell answers all the while.
		g.Keys(bad, "for i in $(seq 1 400); do sleep 25 & done 2>/dev/null; echo forks-done; wait\n")
		g.WaitOutput(bad, "forks-done")
		started := time.Now()
		if code, out := sh(t, a, good, "echo alive-$((1+1))"); code != 0 || !strings.Contains(out, "alive-2") || time.Since(started) > 5*time.Second {
			t.Errorf("the other student's terminal under a fork storm: %d %q after %v", code, out, time.Since(started))
		}
		stats, err := r.runner.Stats(context.Background(), grace.ID)
		if err != nil || stats.Pids > 256 {
			t.Errorf("stats = %+v, %v", stats, err)
		}
		g.Keys(bad, "\x03")
		time.Sleep(time.Second)
		// CPU: four spinning processes get one core between them.
		fresh := openTerminal(t, g)
		g.Keys(fresh, "for i in 1 2 3 4; do (timeout 12 sh -c 'while :; do :; done' &) ; done; echo spinning\n")
		g.WaitOutput(fresh, "spinning")
		time.Sleep(6 * time.Second)
		stats, err = r.runner.Stats(context.Background(), grace.ID)
		if err != nil || stats.CPUPercent > 130 {
			t.Errorf("cpu = %+v, %v (one core is allowed)", stats, err)
		}
		if code, out := sh(t, a, good, "echo still-$((3*3))"); code != 0 || !strings.Contains(out, "still-9") {
			t.Errorf("the other student's terminal under a busy neighbour: %d %q", code, out)
		}
		time.Sleep(8 * time.Second)
		// Disk: 100 MiB is the limit; going over is refused, and only for them.
		term := openTerminal(t, g)
		code, out := sh(t, g, term, "dd if=/dev/zero of=/work/big bs=1M count=300")
		if code == 0 || !(strings.Contains(out, "No space") || strings.Contains(out, "quota")) {
			t.Errorf("filling the disk: %d %q", code, out)
		}
		if code, out := sh(t, a, good, "dd if=/dev/zero of=/work/ok bs=1M count=20 && rm /work/ok"); code != 0 {
			t.Errorf("the other student's disk: %d %q", code, out)
		}
		sh(t, g, term, "rm -f /work/big; echo tidy > /work/after-the-abuse.txt")
		r.state.Files["grace"] = map[string]string{"after-the-abuse.txt": "tidy\n"}
	})

	t.Run("a student who ends their container finds the files when it is back", func(t *testing.T) {
		g := workspacetest.Dial(t, r.base, grace.Token)
		g.Wait("ws.status", g.OfType("ws.status"))
		term := openTerminal(t, g)
		g.Keys(term, "kill -TERM 1\n") // PID 1 is the agent: this ends the container
		g.Wait("ws.agent_restarted", g.OfType("ws.agent_restarted"))
		r.waitRunning(grace)
		if session := r.adminSession(grace); session.AgentRestarts != 1 {
			t.Errorf("agent_restarts = %d", session.AgentRestarts)
		} else {
			lost := false
			for _, s := range session.Snapshots {
				lost = lost || s.Reason == "agent_loss"
			}
			if !lost {
				t.Errorf("no agent_loss snapshot: %+v", session)
			}
		}
		workspacetest.Eventually(t, 30*time.Second, "the relay to reach the new agent", func() bool {
			reply := g.Call(map[string]any{"type": "fs.read", "path": "after-the-abuse.txt"})
			return workspacetest.OK(reply) && reply["content"] == "tidy\n"
		})
		status, err := r.runner.Status(context.Background(), grace.ID)
		if err != nil {
			t.Fatal(err)
		}
		r.state.Started["grace"] = status.StartedAt
		r.save()
	})

	t.Run("a restarted exam server takes over where it was", func(t *testing.T) {
		before := map[string]runner.Status{}
		for name, s := range r.state.Sessions {
			status, err := r.runner.Status(context.Background(), s.ID)
			if err != nil {
				t.Fatal(err)
			}
			before[name] = status
		}
		r.stop()
		r.start()
		checkNoRuntimeAccess(t, r)
		for name, s := range r.state.Sessions {
			r.waitRunning(s)
			status, err := r.runner.Status(context.Background(), s.ID)
			if err != nil || !status.StartedAt.Equal(before[name].StartedAt) {
				t.Errorf("%s: the container was restarted with the server: %v -> %v (%v)", name, before[name].StartedAt, status.StartedAt, err)
			}
		}
		c := workspacetest.Dial(t, r.base, ada.Token)
		c.Wait("ws.status", c.OfType("ws.status"))
		read := c.Call(map[string]any{"type": "fs.read", "path": "src/list.cpp"})
		if read["content"] != r.state.Files["ada"]["src/list.cpp"] {
			t.Errorf("ada's file after the restart: %v", read)
		}
		if _, v := r.view(ada); !v.Done["t1"] {
			t.Errorf("the TODO tick was lost: %+v", v)
		}
		if session := r.adminSession(grace); session.AgentRestarts != 1 {
			t.Errorf("the count of agent restarts after a server restart: %d", session.AgentRestarts)
		}
	})
	r.save()
	r.stop() // phase B starts its own, after the runner has been restarted
}

func phaseB(t *testing.T, r *rig) {
	r.load()
	r.start()
	checkNoRuntimeAccess(t, r)
	ada, grace := r.state.Sessions["ada"], r.state.Sessions["grace"]

	t.Run("after the runner and the server were restarted the containers are the same", func(t *testing.T) {
		for name, s := range r.state.Sessions {
			r.waitRunning(s)
			status, err := r.runner.Status(context.Background(), s.ID)
			if err != nil || !status.StartedAt.Equal(r.state.Started[name]) || !status.Agent.Connected {
				t.Errorf("%s: %+v (started before at %v), %v", name, status, r.state.Started[name], err)
			}
		}
		for name, files := range r.state.Files {
			c := workspacetest.Dial(t, r.base, r.state.Sessions[name].Token)
			c.Wait("ws.status", c.OfType("ws.status"))
			for path, want := range files {
				reply := c.Call(map[string]any{"type": "fs.read", "path": path})
				if !workspacetest.OK(reply) || reply["content"] != want {
					t.Errorf("%s: %s = %v", name, path, reply)
				}
			}
			term := openTerminal(t, c)
			if code, out := sh(t, c, term, "echo after-restarts-$((7*6))"); code != 0 || !strings.Contains(out, "after-restarts-42") {
				t.Errorf("%s's terminal: %d %q", name, code, out)
			}
		}
		if len(r.adminSession(ada).Snapshots) == 0 {
			t.Error("ada's snapshots are gone")
		}
		_ = grace
	})

	t.Run("at the deadline the work is snapshotted and the container frozen", func(t *testing.T) {
		var classes struct {
			Classes []struct {
				ID string `json:"class_id"`
			} `json:"classes"`
		}
		if err := json.Unmarshal(r.admin("GET", "/admin/classes", "", nil, 200), &classes); err != nil || len(classes.Classes) == 0 {
			t.Fatalf("classes: %v", err)
		}
		short := r.importExam(classes.Classes[0].ID, "E2E short", 25, 5)
		r.admin("POST", "/admin/exam/"+short+"/publish", "application/json", strings.NewReader(fmt.Sprintf(`{"expected_current":%q,"allow_active":true}`, r.state.ExamID)), 200)
		joan := r.login("0003")
		r.state.Sessions["joan"] = joan
		r.save()
		r.waitRunning(joan)
		c := workspacetest.Dial(t, r.base, joan.Token)
		c.Wait("ws.status", c.OfType("ws.status"))
		if reply := c.Call(map[string]any{"type": "fs.create", "path": "last-minute.txt", "content": "in time\n"}); !workspacetest.OK(reply) {
			t.Fatalf("create = %v", reply)
		}
		select {
		case <-c.Closed:
		case <-time.After(90 * time.Second):
			t.Fatal("the connection was not closed at the deadline")
		}
		workspacetest.Eventually(t, 60*time.Second, "the container to be frozen", func() bool {
			status, err := r.runner.Status(context.Background(), joan.ID)
			return err == nil && status.State == "paused"
		})
		session := r.adminSession(joan)
		last := int64(0)
		for _, s := range session.Snapshots {
			if s.Reason == "deadline" {
				last = s.ID
			}
		}
		if last == 0 || r.snapshotFiles(last)["last-minute.txt"] != "in time\n" {
			t.Errorf("the deadline snapshot: %+v", session)
		}
		// Nobody gets back in.
		if status, _ := r.do("GET", "/workspace/ws", "", joan.Token, nil); status == 101 || status == 200 {
			t.Errorf("a frozen workspace took a connection: %d", status)
		}
	})
}
