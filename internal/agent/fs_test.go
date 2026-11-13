//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"exam-platform/internal/agentproto"
	"golang.org/x/sys/unix"
)

// newFS makes a folder to be /work, and next to it a folder that is not, with a file in it that no request may touch.
func newFS(t *testing.T, beneath bool) (files *FS, work, outside string) {
	t.Helper()
	base := t.TempDir()
	work, outside = filepath.Join(base, "work"), filepath.Join(base, "outside")
	for _, dir := range []string{work, outside} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("the secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := openFS(work, beneath)
	if err != nil {
		t.Fatal(err)
	}
	if files.beneath != beneath {
		t.Skip("this kernel has no openat2")
	}
	t.Cleanup(func() { files.Close() })
	return files, work, outside
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func code(t *testing.T, err error, want string) {
	t.Helper()
	if got := agentproto.CodeOf(err); got != want {
		t.Fatalf("error = %v (code %q), want %s", err, got, want)
	}
}

func intact(t *testing.T, outside string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil || string(data) != "the secret" {
		t.Fatalf("the file outside /work was touched: %q, %v", data, err)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 {
		t.Fatalf("something was made outside /work: %v", entries)
	}
}

func TestList(t *testing.T) {
	files, work, _ := newFS(t, true)
	write(t, filepath.Join(work, "b.txt"), "bb")
	write(t, filepath.Join(work, "a.txt"), "a")
	write(t, filepath.Join(work, "src", "main.c"), "int main;")
	write(t, filepath.Join(work, "src", "nested", "x"), "")
	_ = os.Symlink("b.txt", filepath.Join(work, "link"))
	_ = syscall.Mkfifo(filepath.Join(work, "pipe"), 0o644)
	// Names the protocol cannot carry, and the agent's own, are not listed.
	write(t, filepath.Join(work, ".cbe-write-123"), "temp")
	write(t, filepath.Join(work, "bad\nname"), "x")
	write(t, filepath.Join(work, ".hidden"), "ok")

	entries, err := files.List("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]agentproto.Entry{}
	var names []string
	for _, entry := range entries {
		got[entry.Name] = entry
		names = append(names, entry.Name)
	}
	if want := ".hidden a.txt b.txt link pipe src"; strings.Join(names, " ") != want {
		t.Fatalf("names = %v, want %s", names, want)
	}
	for name, kind := range map[string]string{"a.txt": "file", "src": "dir", "link": "link", "pipe": "other", ".hidden": "file"} {
		if got[name].Kind != kind {
			t.Errorf("%s is %q, want %q", name, got[name].Kind, kind)
		}
	}
	if got["b.txt"].Size != 2 || got["b.txt"].MTime == 0 {
		t.Errorf("b.txt = %+v", got["b.txt"])
	}
	if sub, err := files.List("src"); err != nil || len(sub) != 2 || sub[0].Name != "main.c" || sub[1].Kind != "dir" {
		t.Errorf("src = %v, %v", sub, err)
	}

	_, err = files.List("missing")
	code(t, err, agentproto.CodeNotFound)
	_, err = files.List("a.txt")
	code(t, err, agentproto.CodeBadPath)
	_, err = files.List("../x")
	code(t, err, agentproto.CodeBadPath)

	for i := range agentproto.MaxListEntries + 1 {
		if err := os.WriteFile(filepath.Join(work, "src", "nested", "n"+strings.Repeat("x", 3)+string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('a'+i/676%26))), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err = files.List("src/nested")
	code(t, err, agentproto.CodeLimit)
}

func TestRead(t *testing.T) {
	files, work, _ := newFS(t, true)
	write(t, filepath.Join(work, "hello.txt"), "héllo\r\nwörld")
	write(t, filepath.Join(work, "empty"), "")
	write(t, filepath.Join(work, "nul"), "a\x00b")
	write(t, filepath.Join(work, "latin1"), "caf\xe9")
	write(t, filepath.Join(work, "exact"), strings.Repeat("x", agentproto.MaxFileBytes))
	write(t, filepath.Join(work, "big"), strings.Repeat("x", agentproto.MaxFileBytes+1))
	write(t, filepath.Join(work, "dir", "inner.txt"), "inner")
	_ = syscall.Mkfifo(filepath.Join(work, "pipe"), 0o644)

	content, version, err := files.Read("hello.txt")
	if err != nil || content != "héllo\r\nwörld" || version != version2("héllo\r\nwörld") {
		t.Fatalf("hello.txt = %q, %q, %v", content, version, err)
	}
	if content, _, err := files.Read("empty"); err != nil || content != "" {
		t.Fatalf("empty = %q, %v", content, err)
	}
	if _, _, err := files.Read("exact"); err != nil {
		t.Fatalf("a file of exactly the limit: %v", err)
	}
	for path, want := range map[string]string{"nul": agentproto.CodeBinary, "latin1": agentproto.CodeBinary, "big": agentproto.CodeTooLarge, "missing": agentproto.CodeNotFound,
		"dir": agentproto.CodeBadPath, "dir/inner.txt/x": agentproto.CodeBadPath, "../outside/secret.txt": agentproto.CodeBadPath, "/etc/passwd": agentproto.CodeBadPath} {
		_, _, err := files.Read(path)
		code(t, err, want)
	}
	// A named pipe would block for ever if it were opened for reading the usual way.
	done := make(chan error, 1)
	go func() { _, _, err := files.Read("pipe"); done <- err }()
	select {
	case err := <-done:
		code(t, err, agentproto.CodeBadPath)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a named pipe blocked")
	}
}

func version2(content string) string { return version([]byte(content)) }

// Symbolic links a student makes: inside /work they work, and none leads out.
func TestSymbolicLinks(t *testing.T) {
	files, work, outside := newFS(t, true)
	write(t, filepath.Join(work, "real.txt"), "real")
	write(t, filepath.Join(work, "sub", "deep.txt"), "deep")
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(work, name)); err != nil {
			t.Fatal(err)
		}
	}
	link("real.txt", "inside")
	link("sub", "dirlink")
	link("..", "sub/up")
	link("/etc", "evil")
	link("/etc/passwd", "evilfile")
	link(outside, "evilout")
	link("../outside/secret.txt", "relative")
	link("../outside", "reldir")
	link("sub/../../outside", "dotdot")
	link("loop2", "loop1")
	link("loop1", "loop2")
	link("selfloop", "selfloop")
	link(".", "sub/self")

	// What stays inside works, through a link to a file, to a folder, and up and back down.
	for path, want := range map[string]string{"inside": "real", "dirlink/deep.txt": "deep", "sub/up/real.txt": "real", "sub/self/deep.txt": "deep", "sub/self/self/self/deep.txt": "deep"} {
		if content, _, err := files.Read(path); err != nil || content != want {
			t.Errorf("Read(%s) = %q, %v", path, content, err)
		}
	}
	if entries, err := files.List("dirlink"); err != nil || len(entries) != 3 {
		t.Errorf("List(dirlink) = %v, %v", entries, err)
	}

	// What leads out is refused by every operation.
	for _, path := range []string{"evil/passwd", "evilfile", "evilout/secret.txt", "relative", "reldir/secret.txt", "dotdot/secret.txt", "evil", "evilout", "loop1", "selfloop", "loop1/x", "evil/hostname"} {
		if _, _, err := files.Read(path); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Read(%s) = %v, want BAD_PATH", path, err)
		}
		if _, err := files.List(path); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("List(%s) = %v, want BAD_PATH", path, err)
		}
	}
	for _, path := range []string{"evil/new", "evilout/new", "reldir/new"} {
		if _, err := files.Create(path, "x"); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Create(%s) = %v, want BAD_PATH", path, err)
		}
		if err := files.Mkdir(path); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Mkdir(%s) = %v, want BAD_PATH", path, err)
		}
	}
	for _, path := range []string{"evilout/secret.txt", "reldir/secret.txt", "dotdot/secret.txt"} {
		if _, _, err := files.Write(path, "overwritten", version2("the secret")); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Write(%s) = %v, want BAD_PATH", path, err)
		}
		if err := files.Delete(path); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Delete(%s) = %v, want BAD_PATH", path, err)
		}
		if err := files.Rename(path, "stolen"); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Rename(%s) = %v, want BAD_PATH", path, err)
		}
		if err := files.Rename("real.txt", strings.TrimSuffix(path, "secret.txt")+"planted"); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Rename(real.txt, %s) = %v, want BAD_PATH", path, err)
		}
	}
	intact(t, outside)
	if _, err := os.Stat("/etc/new"); err == nil {
		t.Fatal("a file was created in /etc")
	}

	// A hundred hops through links is a loop: the kernel gives up, and the answer is BAD_PATH, not a hang.
	link("sub/self/"+strings.Repeat("self/", 45), "toodeep")
	_, err := files.List("sub/" + strings.Repeat("self/", 45) + "self")
	code(t, err, agentproto.CodeBadPath)
}

func TestLinksAreRemovedNotFollowed(t *testing.T) {
	files, work, outside := newFS(t, true)
	write(t, filepath.Join(work, "dir", "kept.txt"), "x")
	_ = os.Symlink(outside, filepath.Join(work, "dir", "toout"))
	_ = os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(work, "filelink"))
	if err := files.Delete("filelink"); err != nil {
		t.Fatal(err)
	}
	if err := files.Delete("dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(work, "dir")); !os.IsNotExist(err) {
		t.Fatalf("dir is still there: %v", err)
	}
	intact(t, outside)

	// Renaming a link moves the link.
	write(t, filepath.Join(work, "real.txt"), "real")
	_ = os.Symlink("real.txt", filepath.Join(work, "ln"))
	if err := files.Rename("ln", "ln2"); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(work, "ln2")); err != nil || target != "real.txt" {
		t.Fatalf("ln2 -> %q, %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(work, "real.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestWrite(t *testing.T) {
	files, work, outside := newFS(t, true)
	path := filepath.Join(work, "a.txt")
	write(t, path, "first")
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatal(err)
	}

	newVersion, _, err := files.Write("a.txt", "second\r\nline", version2("first"))
	if err != nil || newVersion != version2("second\r\nline") {
		t.Fatalf("Write = %q, %v", newVersion, err)
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "second\r\nline" || info.Mode().Perm() != 0o750 {
		t.Fatalf("file = %q, mode %v", data, info.Mode())
	}
	entries, _ := os.ReadDir(work)
	if len(entries) != 1 {
		t.Fatalf("a temporary file was left: %v", entries)
	}

	// The next write needs the new version.
	_, current, err := files.Write("a.txt", "third", version2("first"))
	code(t, err, agentproto.CodeConflict)
	if current != newVersion {
		t.Fatalf("the conflict says the version is %q, want %q", current, newVersion)
	}
	if data, _ := os.ReadFile(path); string(data) != "second\r\nline" {
		t.Fatalf("a refused write changed the file: %q", data)
	}
	// A change from the terminal is a conflict too, including into something that is no longer text.
	write(t, path, "changed in the terminal")
	_, current, err = files.Write("a.txt", "mine", newVersion)
	code(t, err, agentproto.CodeConflict)
	if current != version2("changed in the terminal") {
		t.Fatalf("current = %q", current)
	}
	write(t, path, "now\x00binary")
	_, _, err = files.Write("a.txt", "mine", version2("changed in the terminal"))
	code(t, err, agentproto.CodeConflict)
	write(t, path, strings.Repeat("x", agentproto.MaxFileBytes+1))
	_, _, err = files.Write("a.txt", "mine", "v")
	code(t, err, agentproto.CodeTooLarge)

	// Empty content is a write; the file becomes empty.
	write(t, path, "x")
	if _, _, err := files.Write("a.txt", "", version2("x")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); len(data) != 0 {
		t.Fatalf("file = %q", data)
	}

	for path, want := range map[string]string{"missing.txt": agentproto.CodeNotFound, "dir": agentproto.CodeBadPath, "link": agentproto.CodeBadPath, "nodir/x.txt": agentproto.CodeNotFound, "../outside/secret.txt": agentproto.CodeBadPath, "a.txt/x": agentproto.CodeBadPath} {
		if path == "dir" {
			_ = os.Mkdir(filepath.Join(work, "dir"), 0o755)
		}
		if path == "link" {
			_ = os.Symlink("a.txt", filepath.Join(work, "link"))
		}
		_, _, err := files.Write(path, "x", "v")
		code(t, err, want)
	}
	_, _, err = files.Write("a.txt", strings.Repeat("x", agentproto.MaxFileBytes+1), "v")
	code(t, err, agentproto.CodeTooLarge)
	intact(t, outside)
}

func TestCreateMkdirRenameDelete(t *testing.T) {
	files, work, _ := newFS(t, true)

	v, err := files.Create("new.txt", "")
	if err != nil || v != version2("") {
		t.Fatalf("Create = %q, %v", v, err)
	}
	if v, err := files.Create("with.txt", "text"); err != nil || v != version2("text") {
		t.Fatalf("Create with content = %q, %v", v, err)
	}
	_, err = files.Create("new.txt", "again")
	code(t, err, agentproto.CodeExists)
	_, err = files.Create("nodir/x", "")
	code(t, err, agentproto.CodeNotFound)
	_, err = files.Create("new.txt/x", "")
	code(t, err, agentproto.CodeBadPath)
	_, err = files.Create("big", strings.Repeat("x", agentproto.MaxFileBytes+1))
	code(t, err, agentproto.CodeTooLarge)
	if _, err := os.Stat(filepath.Join(work, "big")); err == nil {
		t.Fatal("a refused create left a file")
	}

	if err := files.Mkdir("d"); err != nil {
		t.Fatal(err)
	}
	if err := files.Mkdir("d/e"); err != nil {
		t.Fatal(err)
	}
	code(t, files.Mkdir("d"), agentproto.CodeExists)
	code(t, files.Mkdir("new.txt"), agentproto.CodeExists)
	code(t, files.Mkdir("x/y"), agentproto.CodeNotFound)

	// Rename: a file, into a folder, over nothing; a folder; never over something.
	if err := files.Rename("new.txt", "d/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if err := files.Rename("d", "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "renamed", "moved.txt")); err != nil {
		t.Fatal(err)
	}
	code(t, files.Rename("with.txt", "renamed"), agentproto.CodeExists)
	code(t, files.Rename("with.txt", "renamed/moved.txt"), agentproto.CodeExists)
	code(t, files.Rename("missing", "other"), agentproto.CodeNotFound)
	code(t, files.Rename("with.txt", "nodir/x"), agentproto.CodeNotFound)
	code(t, files.Rename("renamed", "renamed/e/inside"), agentproto.CodeBadPath)
	if data, _ := os.ReadFile(filepath.Join(work, "with.txt")); string(data) != "text" {
		t.Fatalf("a refused rename changed the file: %q", data)
	}

	// Delete: a file, an empty folder, a tree.
	write(t, filepath.Join(work, "tree", "a", "b", "c.txt"), "c")
	write(t, filepath.Join(work, "tree", "top.txt"), "t")
	for _, path := range []string{"with.txt", "renamed/e", "tree"} {
		if err := files.Delete(path); err != nil {
			t.Fatalf("Delete(%s): %v", path, err)
		}
		if _, err := os.Lstat(filepath.Join(work, path)); !os.IsNotExist(err) {
			t.Fatalf("%s is still there: %v", path, err)
		}
	}
	code(t, files.Delete("tree"), agentproto.CodeNotFound)
	code(t, files.Delete(""), agentproto.CodeBadPath)
	code(t, files.Delete("renamed/moved.txt/x"), agentproto.CodeBadPath)

	// A folder deeper than a request deletes is refused with a message, and is still there.
	deep := strings.Repeat("d/", maxDeleteDepth+2)
	if err := os.MkdirAll(filepath.Join(work, "deep", deep), 0o755); err != nil {
		t.Fatal(err)
	}
	code(t, files.Delete("deep"), agentproto.CodeLimit)
	if err := os.RemoveAll(filepath.Join(work, "deep")); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionsAndNames(t *testing.T) {
	files, work, _ := newFS(t, true)
	write(t, filepath.Join(work, "locked", "x.txt"), "x")
	if err := os.Chmod(filepath.Join(work, "locked"), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(work, "locked"), 0o755)
	if os.Geteuid() != 0 {
		_, err := files.Create("locked/new.txt", "x")
		code(t, err, agentproto.CodeDenied)
		code(t, files.Delete("locked/x.txt"), agentproto.CodeDenied)
	}
	// Reserved and malformed names never reach the kernel.
	for _, path := range []string{".cbe-write-1", "a/.cbe-x", "a\nb", "a\\b", "a//b", "a/", "/a", "..", "a/..", "."} {
		_, err := files.Create(path, "x")
		code(t, err, agentproto.CodeBadPath)
		code(t, files.Mkdir(path), agentproto.CodeBadPath)
		code(t, files.Delete(path), agentproto.CodeBadPath)
		_, _, err = files.Read(path)
		code(t, err, agentproto.CodeBadPath)
	}
}

func TestErrnoMapping(t *testing.T) {
	for errno, want := range map[syscall.Errno]string{
		unix.ENOENT: agentproto.CodeNotFound, unix.EEXIST: agentproto.CodeExists, unix.ENOTEMPTY: agentproto.CodeExists,
		unix.ENOSPC: agentproto.CodeQuota, unix.EDQUOT: agentproto.CodeQuota, // a full XFS project quota says ENOSPC, other file systems EDQUOT (P0-7)
		unix.EXDEV: agentproto.CodeBadPath, unix.ELOOP: agentproto.CodeBadPath, unix.ENOTDIR: agentproto.CodeBadPath, unix.EISDIR: agentproto.CodeBadPath,
		unix.ENAMETOOLONG: agentproto.CodeBadPath, unix.EACCES: agentproto.CodeDenied, unix.EPERM: agentproto.CodeDenied, unix.EROFS: agentproto.CodeDenied,
		unix.EMFILE: agentproto.CodeLimit, unix.EIO: agentproto.CodeInternal,
	} {
		if got := agentproto.CodeOf(mapErrno(errno)); got != want {
			t.Errorf("%v -> %s, want %s", errno, got, want)
		}
	}
	if got := agentproto.CodeOf(mapErrno(os.ErrDeadlineExceeded)); got != agentproto.CodeInternal {
		t.Errorf("a Go error -> %s", got)
	}
	protocol := agentproto.Errorf(agentproto.CodeConflict, "x")
	if mapErrno(protocol) != error(protocol) {
		t.Error("a protocol error was changed")
	}
}

// Without openat2 the agent follows no link at all, which is stricter and still keeps everything in /work.
func TestWithoutOpenat2(t *testing.T) {
	files, work, outside := newFS(t, false)
	write(t, filepath.Join(work, "real.txt"), "real")
	write(t, filepath.Join(work, "sub", "deep.txt"), "deep")
	_ = os.Symlink("real.txt", filepath.Join(work, "inside"))
	_ = os.Symlink("sub", filepath.Join(work, "dirlink"))
	_ = os.Symlink(outside, filepath.Join(work, "evilout"))
	_ = os.Symlink("/etc", filepath.Join(work, "evil"))
	if !files.SlowPaths() {
		t.Fatal("the slow path is not on")
	}
	if content, _, err := files.Read("sub/deep.txt"); err != nil || content != "deep" {
		t.Fatalf("a plain path: %q, %v", content, err)
	}
	if entries, err := files.List(""); err != nil || len(entries) != 6 {
		t.Fatalf("List = %v, %v", entries, err)
	}
	for _, path := range []string{"inside", "dirlink/deep.txt", "evil/passwd", "evilout/secret.txt", "evilout"} {
		if _, _, err := files.Read(path); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Read(%s) = %v, want BAD_PATH", path, err)
		}
	}
	for _, path := range []string{"evilout/new", "evil/new"} {
		if _, err := files.Create(path, "x"); agentproto.CodeOf(err) != agentproto.CodeBadPath {
			t.Errorf("Create(%s) = %v", path, err)
		}
	}
	if _, _, err := files.Write("evilout/secret.txt", "x", version2("the secret")); agentproto.CodeOf(err) != agentproto.CodeBadPath {
		t.Errorf("Write = %v", err)
	}
	if err := files.Delete("evilout/secret.txt"); agentproto.CodeOf(err) != agentproto.CodeBadPath {
		t.Errorf("Delete = %v", err)
	}
	if _, _, err := files.Write("real.txt", "new", version2("real")); err != nil {
		t.Fatal(err)
	}
	if err := files.Mkdir("made"); err != nil {
		t.Fatal(err)
	}
	if err := files.Rename("made", "moved"); err != nil {
		t.Fatal(err)
	}
	intact(t, outside)
}

// Many requests at once, some of them racing a student who keeps replacing a folder with a link: nothing leaves /work.
func TestRacingWithLinkSwaps(t *testing.T) {
	files, work, outside := newFS(t, true)
	write(t, filepath.Join(work, "dir", "file.txt"), "x")
	stop := make(chan struct{})
	var swapper sync.WaitGroup
	swapper.Add(1)
	go func() {
		defer swapper.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Rename(filepath.Join(work, "dir"), filepath.Join(work, "dir.real"))
			_ = os.Symlink(outside, filepath.Join(work, "dir"))
			_ = os.Remove(filepath.Join(work, "dir"))
			_ = os.Rename(filepath.Join(work, "dir.real"), filepath.Join(work, "dir"))
		}
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, _, _ = files.Read("dir/file.txt")
		_, _ = files.List("dir")
		_, _ = files.Create("dir/made.txt", "x")
		_, _, _ = files.Write("dir/file.txt", "y", "v")
		_ = files.Delete("dir/made.txt")
		_ = files.Mkdir("dir/m")
		_ = files.Delete("dir/m")
	}
	close(stop)
	swapper.Wait()
	intact(t, outside)
}
