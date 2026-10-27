//go:build !bindings

package student

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedFrontend(t *testing.T) {
	root, err := fs.Sub(Assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "<title>CBE Portal</title>") {
		t.Fatal("frontend must be built before testing")
	}
	if err := fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		// Svelte's inert error URLs and XML namespace identifiers are not remote assets.
		text := regexp.MustCompile(`https://svelte\.dev/e/[a-z_]+|http://www\.w3\.org/(1999/xhtml|2000/svg|1998/Math/MathML)`).ReplaceAllString(string(content), "")
		for _, forbidden := range []string{"https://", "http://", "/admin/", "AdminToken", "CBE Portal Proctor", "session_token", "__studentHarness", "fake-api"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden external/admin reference %q", path, forbidden)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
