package examdoc

import (
	"archive/tar"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyPackage copies the example package into a new folder, as an author would have it on disk.
func copyPackage(t *testing.T) string {
	t.Helper()
	source := "../../testdata/workspace/lab-listas"
	dest := t.TempDir()
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(source, path)
		target := filepath.Join(dest, relative)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dest
}

func dirProblems(t *testing.T, dir string) string {
	t.Helper()
	_, _, err := CompileWorkspaceDir(dir)
	var problems Problems
	if !errors.As(err, &problems) {
		t.Fatalf("err = %v, want problems", err)
	}
	return problems.Error()
}

func symlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("this system cannot make symbolic links here: %v", err)
	}
}

// A package copied somewhere else, with other times and no executable bits, compiles to the same bundles.
func TestACopyOfThePackageCompilesToTheSameBundles(t *testing.T) {
	original, originalBundles, err := CompileWorkspaceDir("../../testdata/workspace/lab-listas")
	if err != nil {
		t.Fatal(err)
	}
	copied, copiedBundles, err := CompileWorkspaceDir(copyPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	if original.StarterBundle != copied.StarterBundle || original.GradingBundle != copied.GradingBundle ||
		string(originalBundles.Starter.Data) != string(copiedBundles.Starter.Data) || string(originalBundles.Grading.Data) != string(copiedBundles.Grading.Data) {
		t.Fatalf("the bundles differ: %s %s / %s %s", original.StarterBundle, original.GradingBundle, copied.StarterBundle, copied.GradingBundle)
	}
}

func TestRealSymbolicLinks(t *testing.T) {
	dir := copyPackage(t)
	project := filepath.Join(dir, "project")
	// Links that stay inside the folder are kept as links, and a link to a folder is not followed, even a link to its own folder.
	symlink(t, "src/list.cpp", filepath.Join(project, "list"))
	symlink(t, ".", filepath.Join(project, "src", "self"))
	symlink(t, "../include", filepath.Join(project, "src", "headers"))
	_, bundles, err := CompileWorkspaceDir(dir)
	if err != nil {
		t.Fatalf("links inside the project were refused: %v", err)
	}
	links := map[string]string{}
	for _, header := range readTar(t, bundles.Starter.Data) {
		if header.Typeflag == tar.TypeSymlink {
			links[header.Name] = header.Linkname
		}
	}
	if len(links) != 3 || links["list"] != "src/list.cpp" || links["src/self"] != "." || links["src/headers"] != "../include" {
		t.Fatalf("links = %v", links)
	}

	// The link below is project/src/escape: one folder up is project (inside), two folders up is the package (outside). A path that
	// climbs out of the project and comes back in, as ../../project/x does, is refused too: the check never has to resolve it.
	for target, escapes := range map[string]bool{"/etc/passwd": true, "../../workspace.md": true, "../../../etc": true, "../../project/x": true, "../x": false, "x/y": false} {
		t.Run(target, func(t *testing.T) {
			dir := copyPackage(t)
			symlink(t, target, filepath.Join(dir, "project", "src", "escape"))
			_, _, err := CompileWorkspaceDir(dir)
			var problems Problems
			switch {
			case !escapes && err != nil:
				t.Fatalf("a link that stays inside the project was refused: %v", err)
			case escapes && !errors.As(err, &problems):
				t.Fatalf("err = %v, want problems", err)
			case escapes && (!strings.Contains(problems.Error(), "project/src/escape: the symbolic link") || !strings.Contains(problems.Error(), "outside the folder")):
				t.Fatalf("problems:\n%s", problems.Error())
			}
		})
	}
}

func TestAProjectThatIsALinkIsRefused(t *testing.T) {
	dir := copyPackage(t)
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "project")); err != nil {
		t.Fatal(err)
	}
	symlink(t, elsewhere, filepath.Join(dir, "project"))
	if problems := dirProblems(t, dir); !strings.Contains(problems, "project must be a folder") {
		t.Fatalf("problems:\n%s", problems)
	}
}

func TestAWorkspaceMdThatIsALinkOutOfThePackageCannotBeRead(t *testing.T) {
	dir := copyPackage(t)
	outside := filepath.Join(t.TempDir(), "workspace.md")
	if err := os.WriteFile(outside, []byte("---\ntitle: x\nduration: 1h\nimage: a\n---\n[ ] x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "workspace.md")); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(dir, "workspace.md"))
	if problems := dirProblems(t, dir); !strings.Contains(problems, "workspace.md must be a regular file") {
		t.Fatalf("problems:\n%s", problems)
	}
}

func TestAFolderThatCannotBeOpened(t *testing.T) {
	if problems := dirProblems(t, filepath.Join(t.TempDir(), "missing")); !strings.Contains(problems, "the package folder cannot be opened") {
		t.Fatalf("problems:\n%s", problems)
	}
	if problems := dirProblems(t, t.TempDir()); !strings.Contains(problems, "workspace.md is missing") || !strings.Contains(problems, "project/ is missing") {
		t.Fatalf("problems:\n%s", problems)
	}
}
