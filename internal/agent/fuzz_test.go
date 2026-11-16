//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"exam-platform/internal/agentproto"
)

// FuzzFilePaths throws every file operation at paths and content the fuzzer makes up, in a /work that has symbolic links to a folder
// outside it, and checks that the folder outside is never read, changed or added to. It runs its seeds in a plain `go test`; the
// acceptance runs it for a minute:
//
//	go test ./internal/agent -run '^$' -fuzz FuzzFilePaths -fuzztime 60s
func FuzzFilePaths(f *testing.F) {
	base, err := os.MkdirTemp("", "cbefuzz")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { os.RemoveAll(base) })
	work, outside := filepath.Join(base, "work"), filepath.Join(base, "outside")
	for _, dir := range []string{work, filepath.Join(work, "sub"), outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			f.Fatal(err)
		}
	}
	canary := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(canary, []byte("the secret"), 0o644); err != nil {
		f.Fatal(err)
	}
	files, err := OpenFS(work)
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { files.Close() })
	links := map[string]string{"out": outside, "etc": "/etc", "rel": "../outside", "relfile": "../outside/secret.txt", "sub/up": "../..", "sub/self": ".", "loop": "loop", "abs": canary}
	plant := func() {
		for name, target := range links {
			_ = os.Symlink(target, filepath.Join(work, name))
		}
		_ = os.WriteFile(filepath.Join(work, "sub", "file.txt"), []byte("x"), 0o644)
	}

	for op, path := range []string{"out/secret.txt", "etc/passwd", "rel/secret.txt", "relfile", "sub/up/outside/secret.txt", "sub/self/self/file.txt", "loop", "abs", "../outside/secret.txt", "/etc/passwd"} {
		f.Add(uint8(op), path, "sub/file.txt", "content")
		f.Add(uint8(op+3), path, path, "")
	}
	f.Fuzz(func(t *testing.T, op uint8, path, other, content string) {
		plant()
		switch op % 8 {
		case 0:
			_, _, _ = files.Read(path)
		case 1:
			_, _ = files.List(path)
		case 2:
			_, _, _ = files.Write(path, content, version([]byte("the secret")))
		case 3:
			_, _ = files.Create(path, content)
		case 4:
			_ = files.Mkdir(path)
		case 5:
			_ = files.Rename(path, other)
		case 6:
			_ = files.Delete(path)
		case 7:
			_ = files.IsDir(path)
		}
		if data, err := os.ReadFile(canary); err != nil || string(data) != "the secret" {
			t.Fatalf("the file outside /work is now %q, %v (op %d, %q, %q)", data, err, op%8, path, other)
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 1 {
			t.Fatalf("something was made outside /work: %v (op %d, %q, %q)", entries, op%8, path, other)
		}
		// A path that the protocol refuses is refused by the agent too, whoever checked first.
		if agentproto.CheckPath(path) != nil && op%8 == 0 {
			if _, _, err := files.Read(path); agentproto.CodeOf(err) != agentproto.CodeBadPath {
				t.Fatalf("Read(%q) = %v", path, err)
			}
		}
	})
}
