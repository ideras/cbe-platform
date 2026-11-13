//go:build linux

// Package agent is the program that runs inside a student's container as its first process (cmd/cbe-agent): it does the file
// operations, watches the files, runs the terminals and answers on one Unix socket in the language of internal/agentproto.
//
// The agent runs as the student, so the student can kill it, delete its socket or listen in its place. Everything the exam server
// learns from it is student-controlled input, and the server treats it so. What the agent must guarantee is the other direction:
// no request, however it is written, reaches a file outside /work.
package agent

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"syscall"
	"unicode/utf8"

	"exam-platform/internal/agentproto"
	"golang.org/x/sys/unix"
)

// FS is the files under /work. Every path it takes has passed agentproto.CheckPath, and it checks again. It never builds a path
// string to hand to the kernel: it holds a descriptor for /work and resolves each path from it with openat2 and RESOLVE_BENEATH,
// which the kernel refuses to take above that descriptor, whatever symbolic links the student has made. Where the kernel has no
// openat2 (or a sandbox forbids it) it walks the path one name at a time and follows no symbolic link at all.
type FS struct {
	root    int
	path    string
	beneath bool
}

// OpenFS opens dir as the root of the files the agent may touch.
func OpenFS(dir string) (*FS, error) {
	return openFS(dir, true)
}

func openFS(dir string, tryBeneath bool) (*FS, error) {
	fd, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	f := &FS{root: fd, path: dir}
	if tryBeneath {
		probe, err := unix.Openat2(fd, ".", &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH})
		if err == nil {
			_ = unix.Close(probe)
			f.beneath = true
		}
	}
	return f, nil
}

// SlowPaths says the kernel has no openat2 here, so no symbolic link is followed.
func (f *FS) SlowPaths() bool { return !f.beneath }

// Close releases the descriptor of /work.
func (f *FS) Close() error { return unix.Close(f.root) }

// open opens rel ("" is /work) with flags, from /work, and never above it.
func (f *FS) open(rel string, flags int, mode uint32) (int, error) {
	if err := agentproto.CheckPath(rel); err != nil {
		return -1, err
	}
	if rel == "" {
		rel = "."
	}
	if f.beneath {
		how := &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC), Mode: uint64(mode), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS}
		for {
			fd, err := unix.Openat2(f.root, rel, how)
			if err != unix.EINTR {
				return fd, err
			}
		}
	}
	return f.openSlow(rel, flags, mode)
}

// openSlow is open without openat2: each name is opened from the one before, with O_NOFOLLOW, so a symbolic link anywhere in the
// path is an error and none can lead out of /work.
func (f *FS) openSlow(rel string, flags int, mode uint32) (int, error) {
	dir, err := unix.Dup(f.root)
	if err != nil {
		return -1, err
	}
	if rel == "." {
		_ = unix.Close(dir)
		return unix.Openat(f.root, ".", flags|unix.O_CLOEXEC, mode)
	}
	names := splitNames(rel)
	for i, name := range names {
		last := i == len(names)-1
		var fd int
		if last {
			fd, err = unix.Openat(dir, name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
		} else {
			fd, err = unix.Openat(dir, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		_ = unix.Close(dir)
		if err != nil {
			return -1, err
		}
		dir = fd
	}
	return dir, nil
}

func splitNames(rel string) []string {
	var names []string
	start := 0
	for i := 0; i <= len(rel); i++ {
		if i == len(rel) || rel[i] == '/' {
			names = append(names, rel[start:i])
			start = i + 1
		}
	}
	return names
}

// parent opens the folder that holds rel and returns it with rel's last name.
func (f *FS) parent(rel string) (dir int, name string, err error) {
	if rel == "" {
		return -1, "", agentproto.Errorf(agentproto.CodeBadPath, "this needs a path inside /work")
	}
	if err := agentproto.CheckPath(rel); err != nil {
		return -1, "", err
	}
	folder, name := agentproto.SplitPath(rel)
	dir, err = f.open(folder, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, "", mapErrno(err)
	}
	return dir, name, nil
}

// mapErrno turns what the kernel said into the protocol's error. A full disk is QUOTA whether it says ENOSPC (XFS project quotas)
// or EDQUOT (the other file systems).
func mapErrno(err error) error {
	var protocol *agentproto.Error
	if errors.As(err, &protocol) {
		return protocol
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return agentproto.Errorf(agentproto.CodeInternal, "%v", err)
	}
	switch errno {
	case unix.ENOENT:
		return agentproto.Errorf(agentproto.CodeNotFound, "there is no such file or folder")
	case unix.EEXIST, unix.ENOTEMPTY:
		return agentproto.Errorf(agentproto.CodeExists, "there is already something at that path")
	case unix.ENOSPC, unix.EDQUOT:
		return agentproto.Errorf(agentproto.CodeQuota, "the disk space of /work is used up")
	case unix.EXDEV:
		return agentproto.Errorf(agentproto.CodeBadPath, "the path leads outside /work")
	case unix.ELOOP:
		return agentproto.Errorf(agentproto.CodeBadPath, "the path has a symbolic link that is a loop, or one that cannot be followed here")
	case unix.ENOTDIR:
		return agentproto.Errorf(agentproto.CodeBadPath, "part of the path is not a folder")
	case unix.EISDIR:
		return agentproto.Errorf(agentproto.CodeBadPath, "that is a folder")
	case unix.ENAMETOOLONG, unix.EINVAL:
		return agentproto.Errorf(agentproto.CodeBadPath, "the path cannot be used")
	case unix.EACCES, unix.EPERM, unix.EROFS:
		return agentproto.Errorf(agentproto.CodeDenied, "the permissions of the file or folder do not allow that")
	case unix.EMFILE, unix.ENFILE, unix.ENOMEM:
		return agentproto.Errorf(agentproto.CodeLimit, "the container has run out of a resource (%v)", errno)
	}
	return agentproto.Errorf(agentproto.CodeInternal, "%v", errno)
}

func version(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// List lists one folder: names that fail the protocol's path rules, or start with the agent's own prefix, are left out because no
// request could name them.
func (f *FS) List(rel string) ([]agentproto.Entry, error) {
	fd, err := f.open(rel, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, mapErrno(err)
	}
	dir := os.NewFile(uintptr(fd), "folder")
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, mapErrno(err)
	}
	if len(names) > agentproto.MaxListEntries {
		return nil, agentproto.Errorf(agentproto.CodeLimit, "the folder has more than %d names", agentproto.MaxListEntries)
	}
	entries := make([]agentproto.Entry, 0, len(names))
	for _, name := range names {
		if agentproto.CheckName(name) != nil {
			continue
		}
		var st unix.Stat_t
		if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			continue // it went away since the listing
		}
		entry := agentproto.Entry{Name: name, MTime: int64(st.Mtim.Sec)*1000 + int64(st.Mtim.Nsec)/1_000_000}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFREG:
			entry.Kind, entry.Size = "file", st.Size
		case unix.S_IFDIR:
			entry.Kind = "dir"
		case unix.S_IFLNK:
			entry.Kind = "link"
		default:
			entry.Kind = "other"
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Read reads a text file of at most MaxFileBytes through any symbolic link that stays inside /work. A file that is too large, or is
// not UTF-8 text, is TOO_LARGE or BINARY, and the editor shows it as not editable.
func (f *FS) Read(rel string) (content string, ver string, err error) {
	fd, err := f.open(rel, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY, 0)
	if err != nil {
		return "", "", mapErrno(err)
	}
	defer unix.Close(fd)
	data, err := readText(fd)
	if err != nil {
		return "", "", err
	}
	return string(data), version(data), nil
}

// readText reads an open file, which must be a regular file holding UTF-8 text without NUL bytes, of at most MaxFileBytes.
func readText(fd int) ([]byte, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, mapErrno(err)
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFREG:
	case unix.S_IFDIR:
		return nil, agentproto.Errorf(agentproto.CodeBadPath, "that is a folder, not a file")
	default:
		return nil, agentproto.Errorf(agentproto.CodeBadPath, "that is not a regular file")
	}
	if st.Size > agentproto.MaxFileBytes {
		return nil, agentproto.Errorf(agentproto.CodeTooLarge, "the file is larger than %d bytes, which the editor does not open", agentproto.MaxFileBytes)
	}
	data := make([]byte, 0, st.Size+1)
	buffer := make([]byte, 32<<10)
	for {
		n, err := unix.Read(fd, buffer)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, mapErrno(err)
		}
		if n == 0 {
			break
		}
		if len(data)+n > agentproto.MaxFileBytes {
			return nil, agentproto.Errorf(agentproto.CodeTooLarge, "the file is larger than %d bytes, which the editor does not open", agentproto.MaxFileBytes)
		}
		data = append(data, buffer[:n]...)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, agentproto.Errorf(agentproto.CodeBinary, "the file is not UTF-8 text, so the editor cannot open it")
	}
	return data, nil
}

// Write replaces the content of an existing regular file, if its version is base. The content goes to a temporary file in the same
// folder and is renamed over the file, so a reader never sees half a write, and a full disk leaves the old content in place. The
// file keeps its permissions. When the version is not base, the error is CONFLICT and current is the version the file has now.
func (f *FS) Write(rel, content, base string) (newVersion, current string, err error) {
	if len(content) > agentproto.MaxFileBytes {
		return "", "", agentproto.Errorf(agentproto.CodeTooLarge, "the content is larger than %d bytes", agentproto.MaxFileBytes)
	}
	dir, name, err := f.parent(rel)
	if err != nil {
		return "", "", err
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if err := unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", "", mapErrno(err)
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFREG:
	case unix.S_IFLNK:
		return "", "", agentproto.Errorf(agentproto.CodeBadPath, "that is a symbolic link; edit the file it points to")
	default:
		return "", "", agentproto.Errorf(agentproto.CodeBadPath, "that is not a regular file")
	}
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", "", mapErrno(err)
	}
	old, readErr := readText(fd)
	_ = unix.Close(fd)
	switch {
	case agentproto.CodeOf(readErr) == agentproto.CodeBinary:
		// The file was changed from the terminal into something the editor cannot hold: whatever it had open is out of date.
		return "", "", agentproto.Errorf(agentproto.CodeConflict, "the file changed since you opened it, and is no longer text")
	case readErr != nil:
		return "", "", readErr
	}
	current = version(old)
	if current != base {
		return "", current, agentproto.Errorf(agentproto.CodeConflict, "the file changed since you opened it")
	}
	tmp := agentproto.ReservedPrefix + "write-" + randomName()
	tfd, err := unix.Openat(dir, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return "", "", mapErrno(err)
	}
	if err := writeAll(tfd, []byte(content)); err != nil {
		_ = unix.Close(tfd)
		_ = unix.Unlinkat(dir, tmp, 0)
		return "", "", mapErrno(err)
	}
	if err := unix.Fchmod(tfd, st.Mode&0o777); err != nil {
		_ = unix.Close(tfd)
		_ = unix.Unlinkat(dir, tmp, 0)
		return "", "", mapErrno(err)
	}
	if err := unix.Close(tfd); err != nil {
		_ = unix.Unlinkat(dir, tmp, 0)
		return "", "", mapErrno(err)
	}
	if err := unix.Renameat(dir, tmp, dir, name); err != nil {
		_ = unix.Unlinkat(dir, tmp, 0)
		return "", "", mapErrno(err)
	}
	return version([]byte(content)), "", nil
}

// Create makes a new regular file, with content in it when given.
func (f *FS) Create(rel, content string) (string, error) {
	if len(content) > agentproto.MaxFileBytes {
		return "", agentproto.Errorf(agentproto.CodeTooLarge, "the content is larger than %d bytes", agentproto.MaxFileBytes)
	}
	dir, name, err := f.parent(rel)
	if err != nil {
		return "", err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return "", mapErrno(err)
	}
	if err := writeAll(fd, []byte(content)); err != nil {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(dir, name, 0)
		return "", mapErrno(err)
	}
	if err := unix.Close(fd); err != nil {
		_ = unix.Unlinkat(dir, name, 0)
		return "", mapErrno(err)
	}
	return version([]byte(content)), nil
}

func writeAll(fd int, data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(fd, data)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

// Mkdir makes one folder in an existing one.
func (f *FS) Mkdir(rel string) error {
	dir, name, err := f.parent(rel)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	return mapErrnoOrNil(unix.Mkdirat(dir, name, 0o755))
}

// Rename moves a file, link or folder, never over something that is there. A symbolic link is moved itself, not what it points to.
func (f *FS) Rename(from, to string) error {
	oldDir, oldName, err := f.parent(from)
	if err != nil {
		return err
	}
	defer unix.Close(oldDir)
	newDir, newName, err := f.parent(to)
	if err != nil {
		return err
	}
	defer unix.Close(newDir)
	err = unix.Renameat2(oldDir, oldName, newDir, newName, unix.RENAME_NOREPLACE)
	if err == unix.EINVAL {
		return agentproto.Errorf(agentproto.CodeBadPath, "a folder cannot be moved into itself")
	}
	return mapErrnoOrNil(err)
}

// maxDeleteDepth is how deep a folder may be and still be deleted by one request; a student can make one deeper from the terminal,
// and rm -r deletes it.
const maxDeleteDepth = 128

// Delete deletes a file or link, or a folder with everything in it. It never follows a symbolic link: a link in a deleted folder is
// deleted, and what it points to is left alone.
func (f *FS) Delete(rel string) error {
	dir, name, err := f.parent(rel)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if err := unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return mapErrno(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return mapErrnoOrNil(unix.Unlinkat(dir, name, 0))
	}
	return removeTree(dir, name, 0)
}

func removeTree(parent int, name string, depth int) error {
	if depth > maxDeleteDepth {
		return agentproto.Errorf(agentproto.CodeLimit, "the folder is more than %d levels deep; delete it from the terminal", maxDeleteDepth)
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return mapErrno(err)
	}
	folder := os.NewFile(uintptr(fd), "folder")
	names, err := folder.Readdirnames(-1)
	if err != nil {
		folder.Close()
		return mapErrno(err)
	}
	for _, child := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(fd, child, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			continue
		}
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			err = removeTree(fd, child, depth+1)
		} else {
			err = unix.Unlinkat(fd, child, 0)
		}
		if err != nil {
			folder.Close()
			return mapErrno(err)
		}
	}
	folder.Close()
	return mapErrnoOrNil(unix.Unlinkat(parent, name, unix.AT_REMOVEDIR))
}

func mapErrnoOrNil(err error) error {
	if err == nil {
		return nil
	}
	return mapErrno(err)
}

// IsDir says whether rel names a folder inside /work (through links that stay inside it).
func (f *FS) IsDir(rel string) error {
	fd, err := f.open(rel, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return mapErrno(err)
	}
	return unix.Close(fd)
}

var randomMu sync.Mutex

func randomName() string {
	randomMu.Lock()
	defer randomMu.Unlock()
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
