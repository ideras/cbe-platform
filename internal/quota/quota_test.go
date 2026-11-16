//go:build linux

package quota

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	fail  string // a command that contains this text fails
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.fail != "" && strings.Contains(line, f.fail) {
		return "boom", fmt.Errorf("exit status 1")
	}
	if name == "lsattr" {
		return "100123 ----------------- " + args[len(args)-1], nil
	}
	return "", nil
}

func newHelper(t *testing.T, runner *fakeRunner) (*Helper, string, string) {
	t.Helper()
	base := t.TempDir()
	volume := filepath.Join(base, "work")
	if err := os.Mkdir(volume, 0o755); err != nil {
		t.Fatal(err)
	}
	var chowned [][3]any
	h, err := New(Config{Volume: volume, State: base, OwnerUID: 232071, OwnerGID: 232071, Runner: runner,
		Chown: func(path string, uid, gid int) error { chowned = append(chowned, [3]any{path, uid, gid}); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	_ = chowned
	return h, volume, base
}

func TestCreateMakesAPrivateFolderWithItsQuota(t *testing.T) {
	runner := &fakeRunner{}
	h, volume, base := newHelper(t, runner)
	folder, err := h.Create("11111111-2222-3333-4444-555555555555", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(folder.Path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || folder.Path != filepath.Join(volume, "11111111-2222-3333-4444-555555555555") {
		t.Fatalf("folder = %+v, %v, %v", folder, info, err)
	}
	if folder.Project != FirstProject || folder.SizeMB != 100 || folder.Inodes != DefaultFiles {
		t.Errorf("folder = %+v", folder)
	}
	want := []string{
		fmt.Sprintf("xfs_quota -x -c project -s -p %s %d %s", folder.Path, FirstProject, volume),
		fmt.Sprintf("xfs_quota -x -c limit -p bhard=100m ihard=%d %d %s", DefaultFiles, FirstProject, volume),
	}
	if strings.Join(runner.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(runner.calls, "\n"), strings.Join(want, "\n"))
	}
	// The next folder gets the next project, from the counter that outlives the helper.
	second, err := h.Create("another-session", 50, 5000)
	if err != nil || second.Project != FirstProject+1 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if data, _ := os.ReadFile(filepath.Join(base, "projects.next")); strings.TrimSpace(string(data)) != fmt.Sprint(FirstProject+2) {
		t.Errorf("counter = %q", data)
	}
	// Asking again for a session that has its folder changes nothing.
	calls := len(runner.calls)
	again, err := h.Create("another-session", 50, 5000)
	if err != nil || again.Path != second.Path || len(runner.calls) != calls+1 { // one lsattr to read the project
		t.Errorf("again = %+v, %v, %d new commands", again, err, len(runner.calls)-calls)
	}
}

func TestCreateRefusesWhatItShould(t *testing.T) {
	runner := &fakeRunner{}
	h, volume, _ := newHelper(t, runner)
	for name, test := range map[string]struct {
		session        string
		sizeMB, inodes int
	}{
		"empty id": {"", 10, 0}, "slash": {"a/b", 10, 0}, "dots": {"..", 10, 0}, "dot-dot-slash": {"../x", 10, 0}, "space": {"a b", 10, 0}, "too long": {strings.Repeat("a", 65), 10, 0},
		"nul": {"a\x00b", 10, 0}, "no size": {"ok", 0, 0}, "huge size": {"ok", MaxSizeMB + 1, 0}, "few inodes": {"ok", 10, 5}, "many inodes": {"ok", 10, MaxInodes + 1},
	} {
		if _, err := h.Create(test.session, test.sizeMB, test.inodes); err == nil {
			t.Errorf("%s: created", name)
		}
	}
	entries, _ := os.ReadDir(volume)
	if len(entries) != 0 || len(runner.calls) != 0 {
		t.Errorf("a refused request left %v and ran %v", entries, runner.calls)
	}
	// A failed quota command leaves no folder behind.
	runner.fail = "limit -p"
	if _, err := h.Create("fails", 10, 0); err == nil {
		t.Fatal("created although xfs_quota failed")
	}
	if _, err := os.Stat(filepath.Join(volume, "fails")); !os.IsNotExist(err) {
		t.Errorf("the folder was left: %v", err)
	}
	// A file in the way is not replaced.
	runner.fail = ""
	if err := os.WriteFile(filepath.Join(volume, "inway"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Create("inway", 10, 0); err == nil {
		t.Error("created over a file")
	}
}

func TestCreateWillNotUseAnUnmountedVolume(t *testing.T) {
	h, err := New(Config{Volume: t.TempDir(), State: t.TempDir(), OwnerUID: 1, OwnerGID: 1, Runner: &fakeRunner{}, RequireMount: true,
		Chown: func(string, int, int) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Create("a", 10, 0); err == nil || !strings.Contains(err.Error(), "not mounted") {
		t.Errorf("err = %v", err)
	}
}

func TestRemoveDeletesTheTreeAndFollowsNothing(t *testing.T) {
	runner := &fakeRunner{}
	h, volume, _ := newHelper(t, runner)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder, err := h.Create("student", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"a/b/c", "locked"} {
		if err := os.MkdirAll(filepath.Join(folder.Path, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(folder.Path, "a/b/c/file"), []byte("x"), 0o644)
	_ = os.Symlink(outside, filepath.Join(folder.Path, "a/link-to-outside"))
	_ = os.Symlink(filepath.Join(outside, "keep.txt"), filepath.Join(folder.Path, "link-to-file"))
	_ = os.Symlink("/", filepath.Join(folder.Path, "root"))
	_ = os.WriteFile(filepath.Join(folder.Path, "locked", "x"), nil, 0o644)
	// The helper is root, so a folder a student made unwritable does not stop it; as an ordinary user this test cannot say that.
	if os.Geteuid() == 0 {
		if err := os.Chmod(filepath.Join(folder.Path, "locked"), 0o500); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Remove("student"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(folder.Path); !os.IsNotExist(err) {
		t.Fatalf("the folder is still there: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("a file outside was touched: %q, %v", data, err)
	}
	if last := runner.calls[len(runner.calls)-1]; !strings.Contains(last, "limit -p bhard=0 ihard=0 100123") {
		t.Errorf("the quota was not freed: %q", last)
	}
	if err := h.Remove("student"); err != nil {
		t.Errorf("removing what is gone: %v", err)
	}
	for _, bad := range []string{"", "..", "a/b", "../work"} {
		if err := h.Remove(bad); err == nil {
			t.Errorf("Remove(%q) worked", bad)
		}
	}
	if entries, _ := os.ReadDir(volume); len(entries) != 0 {
		t.Errorf("volume = %v", entries)
	}
}

func TestMappedID(t *testing.T) {
	file := filepath.Join(t.TempDir(), "subuid")
	if err := os.WriteFile(file, []byte("ideras:100000:65536\ncbe-runner:231072:65536\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if id, err := MappedID(file, "cbe-runner", 1000); err != nil || id != 232071 {
		t.Errorf("MappedID = %d, %v", id, err)
	}
	for _, bad := range []struct {
		user string
		n    int
	}{{"nobody", 1000}, {"cbe-runner", 0}, {"cbe-runner", 70000}} {
		if id, err := MappedID(file, bad.user, bad.n); err == nil {
			t.Errorf("MappedID(%s, %d) = %d", bad.user, bad.n, id)
		}
	}
}

func serve(t *testing.T, h *Helper, uid int) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "q")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "h.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = Serve(ctx, listener, h, uid); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return NewClient(socket)
}

func TestTheHelperServesOnlyItsRunner(t *testing.T) {
	h, _, _ := newHelper(t, &fakeRunner{})
	client := serve(t, h, os.Getuid())
	folder, err := client.Create("over-the-socket", 20, 0)
	if err != nil || folder.Session != "over-the-socket" {
		t.Fatalf("Create = %+v, %v", folder, err)
	}
	if sessions, err := client.Sessions(); err != nil || len(sessions) != 1 || sessions[0] != "over-the-socket" {
		t.Errorf("Sessions = %v, %v", sessions, err)
	}
	if err := client.Remove("over-the-socket"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create("../escape", 20, 0); err == nil || !strings.Contains(err.Error(), "bad session") {
		t.Errorf("a bad session: %v", err)
	}
	// A different user is hung up on: the kernel says who is calling.
	stranger := serve(t, h, os.Getuid()+1)
	if _, err := stranger.Create("nope", 20, 0); err == nil {
		t.Error("a stranger was served")
	}
}

func TestTheHelperTakesOnlyItsOwnJSON(t *testing.T) {
	h, volume, _ := newHelper(t, &fakeRunner{})
	client := serve(t, h, os.Getuid())
	for _, body := range []string{`{"session":"a","size_mb":10,"path":"/etc"}`, `{"session":"a","size_mb":10}{"session":"b","size_mb":10}`, `[]`, `nope`, ``} {
		request, _ := http.NewRequest("POST", "http://quota/v1/folders", strings.NewReader(body))
		response, err := client.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status %d", body, response.StatusCode)
		}
	}
	for _, path := range []string{"/v1/folders/../../etc", "/v1/anything", "/"} {
		request, _ := http.NewRequest("DELETE", "http://quota"+path, nil)
		response, err := client.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 300 && response.StatusCode != http.StatusNoContent {
			t.Errorf("%s: status %d", path, response.StatusCode)
		}
	}
	if entries, _ := os.ReadDir(volume); len(entries) != 0 {
		t.Errorf("volume = %v", entries)
	}
	time.Sleep(10 * time.Millisecond)
}
