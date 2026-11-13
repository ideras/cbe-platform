//go:build linux

// Package quota is the quota helper (workspace plan, section 4): the one small program that runs as root, because only root can set
// XFS project quotas, and whose only job is to make and remove the size-limited folders that become a student's /work. It accepts a
// session ID and sizes, nothing else: no paths, no owners, no commands. The runner (the cbe-runner user) is its only client.
//
// A folder is made on the XFS volume (decision P0-3) as <volume>/<session>, owned by the host user that the image's student user
// (uid and gid 1000) is mapped to by the runner's subordinate IDs, with mode 0700 so that neither the runner nor the exam server can
// read a student's files from the host (P0-5). It gets its own XFS project with a hard limit on blocks and on inodes.
package quota

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Limits of what the helper will make.
const (
	MaxSizeMB    = 4096
	MaxInodes    = 1_000_000
	DefaultFiles = 20_000
	// FirstProject is where project IDs start; the helper keeps the next free one in a file on the volume's parent.
	FirstProject = 100000
)

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidSession says whether id is a session name the helper accepts: letters, digits, _ and -, at most 64.
func ValidSession(id string) bool { return sessionPattern.MatchString(id) }

// Runner runs the XFS administration commands. The helper never builds a shell command line: it passes arguments.
type Runner interface {
	// Run runs name with args and returns its combined output.
	Run(name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Config is how a Helper is set up.
type Config struct {
	// Volume is the mount point of the XFS volume, such as /data/cbe-platform/work; State is a folder (outside the volume) for the
	// project ID counter, such as /data/cbe-platform.
	Volume string
	State  string
	// Owner is the host user and group a folder is given, which are those the student's uid and gid 1000 map to.
	OwnerUID, OwnerGID int
	// Mode is the folder's permission, 0700 by default.
	Mode fs.FileMode
	// Runner and Chown can be replaced by tests; the defaults run xfs_quota and chown for real.
	Runner Runner
	Chown  func(path string, uid, gid int) error
	// RequireMount makes Create refuse when Volume is not a mount point, so a container never gets the empty folder under an
	// unmounted volume (P0-4). Tests that use a plain folder turn it off.
	RequireMount bool
}

// Helper makes and removes the folders.
type Helper struct {
	cfg Config
	mu  sync.Mutex
}

// New checks the configuration and returns a Helper.
func New(cfg Config) (*Helper, error) {
	if cfg.Volume == "" || cfg.State == "" || cfg.OwnerUID <= 0 || cfg.OwnerGID <= 0 {
		return nil, errors.New("quota: volume, state and the owner's uid and gid are required")
	}
	if cfg.Mode == 0 {
		cfg.Mode = 0o700
	}
	if cfg.Runner == nil {
		cfg.Runner = execRunner{}
	}
	if cfg.Chown == nil {
		cfg.Chown = os.Chown
	}
	return &Helper{cfg: cfg}, nil
}

// MappedID reads the first ID of the user's range in /etc/subuid or /etc/subgid and returns the host ID that the container's ID n maps
// to (container ID 0 is the user itself, so ID n >= 1 is first + n - 1).
func MappedID(file, user string, n int) (int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) == 3 && fields[0] == user {
			first, err := strconv.Atoi(fields[1])
			count, err2 := strconv.Atoi(fields[2])
			if err != nil || err2 != nil || n < 1 || n > count {
				return 0, fmt.Errorf("quota: bad entry for %s in %s", user, file)
			}
			return first + n - 1, nil
		}
	}
	return 0, fmt.Errorf("quota: no entry for %s in %s", user, file)
}

// Folder is a folder the helper made.
type Folder struct {
	Session string `json:"session"`
	Path    string `json:"path"`
	Project int    `json:"project"`
	SizeMB  int    `json:"size_mb"`
	Inodes  int    `json:"inodes"`
}

// Create makes the folder for a session with the given limits. A folder that already exists for the session is returned as it is
// (it keeps its limits), so a restarted runner can ask again.
func (h *Helper) Create(session string, sizeMB, inodes int) (Folder, error) {
	switch {
	case !ValidSession(session):
		return Folder{}, errors.New("bad session ID")
	case sizeMB < 1 || sizeMB > MaxSizeMB:
		return Folder{}, fmt.Errorf("size must be 1 to %d MiB", MaxSizeMB)
	case inodes == 0:
		inodes = DefaultFiles
	case inodes < 100 || inodes > MaxInodes:
		return Folder{}, fmt.Errorf("inodes must be 100 to %d", MaxInodes)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg.RequireMount && !isMount(h.cfg.Volume) {
		return Folder{}, fmt.Errorf("%s is not mounted", h.cfg.Volume)
	}
	dir := filepath.Join(h.cfg.Volume, session)
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() {
			return Folder{}, errors.New("something that is not a folder is in the way")
		}
		project, _ := h.projectOf(dir)
		return Folder{Session: session, Path: dir, Project: project, SizeMB: sizeMB, Inodes: inodes}, nil
	}
	project, err := h.nextProject()
	if err != nil {
		return Folder{}, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Folder{}, err
	}
	fail := func(err error) (Folder, error) {
		_ = os.RemoveAll(dir)
		return Folder{}, err
	}
	if out, err := h.cfg.Runner.Run("xfs_quota", "-x", "-c", fmt.Sprintf("project -s -p %s %d", dir, project), h.cfg.Volume); err != nil {
		return fail(fmt.Errorf("xfs_quota project: %v: %s", err, out))
	}
	if out, err := h.cfg.Runner.Run("xfs_quota", "-x", "-c", fmt.Sprintf("limit -p bhard=%dm ihard=%d %d", sizeMB, inodes, project), h.cfg.Volume); err != nil {
		return fail(fmt.Errorf("xfs_quota limit: %v: %s", err, out))
	}
	if err := h.cfg.Chown(dir, h.cfg.OwnerUID, h.cfg.OwnerGID); err != nil {
		return fail(err)
	}
	if err := os.Chmod(dir, h.cfg.Mode); err != nil {
		return fail(err)
	}
	return Folder{Session: session, Path: dir, Project: project, SizeMB: sizeMB, Inodes: inodes}, nil
}

// Remove deletes a session's folder, with everything in it, and frees its quota. The container must be gone first. A folder that is
// not there is not an error: removing is what the caller wants, and it is done.
func (h *Helper) Remove(session string) error {
	if !ValidSession(session) {
		return errors.New("bad session ID")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	dir := filepath.Join(h.cfg.Volume, session)
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("something that is not a folder is in the way")
	}
	project, _ := h.projectOf(dir)
	// The folder is the student's: removal never follows a link out of it, and never leaves the volume.
	if err := removeTree(dir); err != nil {
		return err
	}
	if project != 0 {
		if out, err := h.cfg.Runner.Run("xfs_quota", "-x", "-c", fmt.Sprintf("limit -p bhard=0 ihard=0 %d", project), h.cfg.Volume); err != nil {
			return fmt.Errorf("xfs_quota limit: %v: %s", err, out)
		}
	}
	return nil
}

// Sessions lists the folders that exist.
func (h *Helper) Sessions() ([]string, error) {
	entries, err := os.ReadDir(h.cfg.Volume)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() && ValidSession(entry.Name()) {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}

// nextProject takes the next project ID from the counter file in the state folder.
func (h *Helper) nextProject() (int, error) {
	path := filepath.Join(h.cfg.State, "projects.next")
	next := FirstProject
	if data, err := os.ReadFile(path); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n >= FirstProject {
			next = n
		}
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(next+1)+"\n"), 0o644); err != nil {
		return 0, err
	}
	return next, nil
}

// projectOf reads the XFS project ID of a folder (0 when it has none, or on a file system without projects).
func (h *Helper) projectOf(dir string) (int, error) {
	out, err := h.cfg.Runner.Run("lsattr", "-dp", dir)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, errors.New("no project")
	}
	return strconv.Atoi(fields[0])
}

func isMount(path string) bool {
	var st, parent unix.Stat_t
	if unix.Stat(path, &st) != nil || unix.Stat(filepath.Dir(path), &parent) != nil {
		return false
	}
	return st.Dev != parent.Dev
}

// removeTree deletes a folder without following links and without crossing onto another file system. It works from open
// descriptors, never from path strings, so nothing a student leaves in the folder (or does to it while it is being deleted) can make
// the helper, which is root, delete or follow anything outside it.
func removeTree(dir string) error {
	parent, err := unix.Open(filepath.Dir(dir), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	var st unix.Stat_t
	if err := unix.Fstatat(parent, filepath.Base(dir), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	return removeAt(parent, filepath.Base(dir), uint64(st.Dev), 0)
}

func removeAt(parent int, name string, dev uint64, depth int) error {
	if depth > 4096 {
		return errors.New("folder is too deep")
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	folder := os.NewFile(uintptr(fd), name)
	defer folder.Close()
	var self unix.Stat_t
	if err := unix.Fstat(fd, &self); err != nil || uint64(self.Dev) != dev {
		return fmt.Errorf("%s is on another file system", name)
	}
	names, err := folder.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, child := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(fd, child, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			if err := removeAt(fd, child, dev, depth+1); err != nil {
				return err
			}
			continue
		}
		if err := unix.Unlinkat(fd, child, 0); err != nil {
			return err
		}
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
}
