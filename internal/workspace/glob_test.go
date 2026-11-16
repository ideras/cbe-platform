package workspace

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The compiler's matcher and this one are held to the same table.
func TestGlobMatchSharedTable(t *testing.T) {
	data, err := os.ReadFile("../../testdata/workspace/glob-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Pattern, Name string
			Want          bool
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil || len(table.Cases) < 30 {
		t.Fatalf("the table: %v, %d cases", err, len(table.Cases))
	}
	for _, test := range table.Cases {
		if got := GlobMatch(test.Pattern, test.Name); got != test.Want {
			t.Errorf("GlobMatch(%q, %q) = %v, want %v", test.Pattern, test.Name, got, test.Want)
		}
	}
}

func TestWhatASubmissionHolds(t *testing.T) {
	manifest := Manifest{Version: 1, Entries: []Entry{
		{Path: "Makefile", Type: "file", Size: 10, Hash: "a"},
		{Path: "build", Type: "dir"}, {Path: "build/app", Type: "file", Size: 1000, Hash: "b"}, {Path: "build/x.o", Type: "file", Size: 5, Hash: "c"},
		{Path: "docs", Type: "dir"},
		{Path: "src", Type: "dir"}, {Path: "src/a.cpp", Type: "file", Size: 20, Hash: "d"}, {Path: "src/a.o", Type: "file", Size: 7, Hash: "e"},
		{Path: "src/deep", Type: "dir"}, {Path: "src/deep/b.cpp", Type: "file", Size: 30, Hash: "f"}, {Path: "src/link", Type: "link", Link: "a.cpp"},
		{Path: "notes.txt", Type: "file", Size: 3, Hash: "g"},
	}}
	paths := func(m Manifest) []string {
		var out []string
		for _, e := range m.Entries {
			out = append(out, e.Path)
		}
		return out
	}
	// Everything but what is ignored, when the exam names no submit list; folders only when something is under them.
	filtered, files, size := FilterManifest(manifest, nil, []string{"build/**", "**/*.o"})
	if want := []string{"Makefile", "notes.txt", "src", "src/a.cpp", "src/deep", "src/deep/b.cpp", "src/link"}; !reflect.DeepEqual(paths(filtered), want) || files != 5 || size != 63 {
		t.Errorf("filtered = %v, %d files, %d bytes", paths(filtered), files, size)
	}
	// A submit list narrows it, and ignore still takes away.
	filtered, files, _ = FilterManifest(manifest, []string{"src/**", "Makefile"}, []string{"**/*.o"})
	if want := []string{"Makefile", "src", "src/a.cpp", "src/deep", "src/deep/b.cpp", "src/link"}; !reflect.DeepEqual(paths(filtered), want) || files != 4 {
		t.Errorf("with a submit list = %v, %d files", paths(filtered), files)
	}
	// Ignore wins over submit.
	filtered, _, _ = FilterManifest(manifest, []string{"build/**"}, []string{"build/**"})
	if len(filtered.Entries) != 0 {
		t.Errorf("an ignored file was submitted: %v", paths(filtered))
	}
	// Nothing is changed in what it was given.
	if len(manifest.Entries) != 12 {
		t.Error("the manifest was changed")
	}
}
