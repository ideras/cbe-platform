package workspace

import (
	"path"
	"sort"
	"strings"
)

// GlobMatch reports whether the file at name, a slash-separated path under /work, matches a `submit` or `ignore` pattern of an exam
// (the dialect of docs/workspace-format.md, decision P1-12). It is the same matcher as the compiler's (shared/examdoc, a module the
// server does not import); testdata/workspace/glob-cases.json is the table both are tested against. The pattern has passed the
// importer's check. A ** segment matches any number of whole folders, none included, except as the last segment, where it matches
// the one or more names below a folder: so build/** matches build/a.o and build/x/y, but not a file called build, and **/*.o matches
// a.o and src/a.o.
func GlobMatch(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			rest := pattern[1:]
			if len(rest) == 0 {
				return len(name) > 0
			}
			for skip := 0; skip <= len(name); skip++ {
				if matchSegments(rest, name[skip:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// Submitted says whether a file of a snapshot is part of a submission: it matches one of the exam's `submit` patterns (every file
// does when the exam names none) and none of its `ignore` patterns.
func Submitted(name string, submit, ignore []string) bool {
	for _, pattern := range ignore {
		if GlobMatch(pattern, name) {
			return false
		}
	}
	if len(submit) == 0 {
		return true
	}
	for _, pattern := range submit {
		if GlobMatch(pattern, name) {
			return true
		}
	}
	return false
}

// FilterManifest is the part of a snapshot that is a submission: the files and links the exam submits, and the folders that hold at
// least one of them. The blobs are the snapshot's own (they are named by their content), so a submission costs a manifest.
func FilterManifest(manifest Manifest, submit, ignore []string) (filtered Manifest, files int, bytes int64) {
	filtered = Manifest{Version: manifest.Version}
	kept := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.Type == "dir" || !Submitted(entry.Path, submit, ignore) {
			continue
		}
		filtered.Entries = append(filtered.Entries, entry)
		files++
		bytes += entry.Size
		for dir := parentOf(entry.Path); dir != ""; dir = parentOf(dir) {
			kept[dir] = true
		}
	}
	for _, entry := range manifest.Entries {
		if entry.Type == "dir" && kept[entry.Path] {
			filtered.Entries = append(filtered.Entries, entry)
		}
	}
	sortEntries(filtered.Entries)
	return filtered, files, bytes
}

func parentOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
}
