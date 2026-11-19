package imagetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAChecksFileIsChecksOfCommands(t *testing.T) {
	checks, err := ParseChecks("# The file's own comments.\n\n# check: the compiler\ng++ --version\n\n# check:   make, twice  \n# a comment of the check\nmake\nmake check\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Name != "the compiler" || checks[0].Line != 3 || checks[0].Script != "g++ --version\n" ||
		checks[1].Name != "make, twice" || checks[1].Line != 6 || checks[1].Script != "# a comment of the check\nmake\nmake check\n" {
		t.Errorf("%+v", checks)
	}
	for name, test := range map[string]struct{ text, want string }{
		"no check":           {"# only comments\nls\n", "there is no check in it"},
		"a check unnamed":    {"# check:\nls\n", "line 1: `# check:` is followed by what the check checks"},
		"a check of nothing": {"# check: one\nls\n# check: two\n# just a comment\n\n", `line 3: the check "two" has no command`},
	} {
		if _, err := ParseChecks(test.text); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheChecksFileIsReadFromTheImagesFolder(t *testing.T) {
	dir := t.TempDir()
	if checks, found, err := ReadChecks(dir); err != nil || found || checks != nil {
		t.Errorf("a folder without one: %v %v %v", checks, found, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ChecksFile), []byte("ls\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadChecks(dir); !found || err == nil || !strings.Contains(err.Error(), filepath.Join(dir, ChecksFile)+": there is no check") {
		t.Errorf("a file that is wrong: %v %v", found, err)
	}
}

// The checks of the course image in this repository are checks: a mistake in the file is found here, not on the server.
func TestTheChecksOfCpp20AreReadable(t *testing.T) {
	checks, found, err := ReadChecks(filepath.Join("..", "..", "images", "cpp20"))
	if err != nil || !found || len(checks) < 5 {
		t.Fatalf("%d checks, found %v: %v", len(checks), found, err)
	}
}
