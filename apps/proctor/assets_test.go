//go:build !bindings

package proctor

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
	if !strings.Contains(string(index), "CBE Portal Proctor") {
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
		// Svelte's inert error URLs, XML namespace identifiers and the example server address shown in the connection dialog
		// are not remote assets.
		text := regexp.MustCompile(`https://svelte\.dev/e/[a-z_]+|http://www\.w3\.org/1999/xhtml|http://www\.w3\.org/2000/svg|http://www\.w3\.org/1998/Math/MathML|http://127\.0\.0\.1:8080|http://192\.168\.1\.10:8080|http:// address`).ReplaceAllString(string(content), "")
		for _, forbidden := range []string{"ProctorHarness", "PROCTOR_UI_FIXTURE", "demo-admin-secret"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains a test fixture", path)
			}
		}
		// Attribution links in the bundled license are plain text, not remote assets.
		if path != "lucide-LICENSE.txt" && (strings.Contains(text, "https://") || strings.Contains(text, "http://")) {
			t.Errorf("%s contains an external resource", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
