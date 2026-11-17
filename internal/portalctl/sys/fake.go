package sys

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Fake is a small simulated machine: files and folders with owners and modes, accounts, mounts, and the few programs the configurator
// runs (useradd, mkfs.xfs, mount, systemctl ...), which change that state as the real ones do. It is what the tests of the configurator
// run against, so that "run it twice and the second run changes nothing" can be tested without changing a machine.
type Fake struct {
	Root bool
	Arc  string
	// Calls is every program run, as a line, in order.
	Calls []string
	// Failures makes a program fail: the key is the start of the line it is run with ("mkfs.xfs").
	Failures map[string]error
	// Containers is what `podman ps -q` prints: the containers that run.
	Containers string
	// Missing are programs that are not installed.
	Missing map[string]bool
	// FS is the type of filesystem of a folder (nearest ancestor), as stat -f says it; default ext2/ext3.
	FS map[string]string
	// Free is the room, in bytes, of a folder (nearest ancestor); default 1 TiB.
	Free map[string]int64

	nodes     map[string]*node
	users     map[string]User
	groups    map[string]int
	members   map[string]map[string]bool
	linger    map[string]bool
	enabled   map[string]bool
	active    map[string]bool
	formatted map[string]string
	mounts    []mount
	nextUID   int
	nextSys   int
	// busy are the accounts that have a process running (the user manager that lingering starts), which userdel refuses to delete.
	busy map[string]bool
}

type node struct {
	data []byte
	dir  bool
	link string
	mode fs.FileMode
	uid  int
	gid  int
	size int64
}

type mount struct{ source, target, fstype, options string }

// NewFake is a machine with root, an empty /, and nothing installed but what the system always has.
func NewFake() *Fake {
	f := &Fake{Root: true, Arc: "amd64", Failures: map[string]error{}, Missing: map[string]bool{}, FS: map[string]string{}, Free: map[string]int64{},
		nodes: map[string]*node{}, users: map[string]User{}, groups: map[string]int{"root": 0}, members: map[string]map[string]bool{},
		linger: map[string]bool{}, busy: map[string]bool{}, enabled: map[string]bool{}, active: map[string]bool{}, formatted: map[string]string{}, nextUID: 1000, nextSys: 900}
	f.users["root"] = User{Name: "root", Home: "/root"}
	for _, dir := range []string{"/", "/etc", "/etc/systemd", "/etc/systemd/system", "/home", "/opt", "/usr", "/usr/local", "/usr/local/bin", "/var", "/var/lib", "/run", "/run/systemd", "/run/systemd/system"} {
		f.nodes[dir] = &node{dir: true, mode: 0o755}
	}
	f.nodes["/etc/fstab"] = &node{data: []byte("UUID=1234 / ext4 defaults 0 1\n"), mode: 0o644}
	f.nodes["/etc/subuid"] = &node{mode: 0o644}
	f.nodes["/etc/subgid"] = &node{mode: 0o644}
	f.nodes["/proc/meminfo"] = &node{data: []byte("MemTotal:       65536000 kB\n"), mode: 0o444}
	f.nodes["/sys/fs/cgroup/cgroup.controllers"] = &node{data: []byte("cpuset cpu io memory hugetlb pids rdma\n"), mode: 0o444}
	return f
}

func (f *Fake) IsRoot() bool { return f.Root }
func (f *Fake) Arch() string { return f.Arc }

func (f *Fake) LookPath(name string) (string, bool) {
	if f.Missing[name] {
		return "", false
	}
	return "/usr/bin/" + name, true
}

// Install puts a file in place, with its folders, as a test's starting point.
func (f *Fake) Install(p string, data string, mode fs.FileMode) {
	f.ensureDirs(path.Dir(p))
	f.nodes[p] = &node{data: []byte(data), mode: mode}
}

// Text is a file's content, or "" when there is none.
func (f *Fake) Text(p string) string {
	if n, ok := f.nodes[p]; ok {
		return string(n.data)
	}
	return ""
}

// Active and Enabled say what the simulated systemd was told. A user unit is "user:unit".
func (f *Fake) Active(unit string) bool  { return f.active[unit] }
func (f *Fake) Enabled(unit string) bool { return f.enabled[unit] }

// Mounted says whether something is mounted on a folder.
func (f *Fake) Mounted(target string) bool {
	for _, m := range f.mounts {
		if m.target == target {
			return true
		}
	}
	return false
}

// Members are the accounts in a group.
func (f *Fake) Members(group string) []string {
	var names []string
	for name := range f.members[group] {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (f *Fake) ensureDirs(p string) {
	for dir := p; dir != "/" && dir != "."; dir = path.Dir(dir) {
		if _, ok := f.nodes[dir]; !ok {
			f.nodes[dir] = &node{dir: true, mode: 0o755}
		}
	}
}

func (f *Fake) ReadFile(p string) ([]byte, error) {
	if p == "/proc/self/mountinfo" {
		var lines []string
		for i, m := range f.mounts {
			lines = append(lines, fmt.Sprintf("%d 1 0:%d / %s rw - %s %s rw,%s", 30+i, 30+i, m.target, m.fstype, m.source, m.options))
		}
		return []byte(strings.Join(lines, "\n") + "\n"), nil
	}
	n, ok := f.nodes[p]
	if !ok || n.dir {
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), n.data...), nil
}

func (f *Fake) WriteFile(p string, data []byte, mode fs.FileMode) error {
	parent, ok := f.nodes[path.Dir(p)]
	if !ok || !parent.dir {
		return &fs.PathError{Op: "write", Path: p, Err: fs.ErrNotExist}
	}
	old := f.nodes[p]
	n := &node{data: append([]byte(nil), data...), mode: mode, size: int64(len(data))}
	if old != nil {
		n.uid, n.gid = old.uid, old.gid
	}
	f.nodes[p] = n
	return nil
}

func (f *Fake) Stat(p string) Info {
	n, ok := f.nodes[p]
	if !ok {
		return Info{}
	}
	info := Info{Exists: true, IsDir: n.dir, Mode: n.mode, UID: n.uid, GID: n.gid, Size: int64(len(n.data))}
	if n.size > 0 {
		info.Size = n.size
	}
	if n.link != "" {
		info.IsLink, info.Target = true, n.link
		if t, ok := f.nodes[n.link]; ok {
			info.IsDir = t.dir
		}
	}
	return info
}

func (f *Fake) ReadDir(p string) ([]string, error) {
	if n, ok := f.nodes[p]; !ok || !n.dir {
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: fs.ErrNotExist}
	}
	var names []string
	for name := range f.nodes {
		if name != p && path.Dir(name) == p {
			names = append(names, path.Base(name))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (f *Fake) MkdirAll(p string, mode fs.FileMode) error {
	for dir := p; dir != "/" && dir != "."; dir = path.Dir(dir) {
		if n, ok := f.nodes[dir]; ok {
			if !n.dir {
				return fmt.Errorf("%s is not a folder", dir)
			}
			continue
		}
		f.nodes[dir] = &node{dir: true, mode: mode}
	}
	return nil
}

func (f *Fake) Chown(p string, uid, gid int) error {
	n, ok := f.nodes[p]
	if !ok {
		return &fs.PathError{Op: "chown", Path: p, Err: fs.ErrNotExist}
	}
	n.uid, n.gid = uid, gid
	return nil
}

func (f *Fake) Chmod(p string, mode fs.FileMode) error {
	n, ok := f.nodes[p]
	if !ok {
		return &fs.PathError{Op: "chmod", Path: p, Err: fs.ErrNotExist}
	}
	n.mode = mode
	return nil
}

func (f *Fake) Symlink(target, link string) error {
	if _, ok := f.nodes[link]; ok {
		return &fs.PathError{Op: "symlink", Path: link, Err: fs.ErrExist}
	}
	if parent, ok := f.nodes[path.Dir(link)]; !ok || !parent.dir {
		return &fs.PathError{Op: "symlink", Path: link, Err: fs.ErrNotExist}
	}
	f.nodes[link] = &node{link: target, mode: 0o777}
	return nil
}

func (f *Fake) Remove(p string) error {
	if _, ok := f.nodes[p]; !ok {
		return &fs.PathError{Op: "remove", Path: p, Err: fs.ErrNotExist}
	}
	delete(f.nodes, p)
	return nil
}

func (f *Fake) RemoveAll(p string) error {
	for name := range f.nodes {
		if name == p || strings.HasPrefix(name, p+"/") {
			delete(f.nodes, name)
		}
	}
	return nil
}

func (f *Fake) Lookup(name string) (User, bool) { u, ok := f.users[name]; return u, ok }

func (f *Fake) LookupGroup(name string) (int, bool) { gid, ok := f.groups[name]; return gid, ok }

func (f *Fake) FreeBytes(p string) (int64, error) {
	for dir := p; ; dir = path.Dir(dir) {
		if n, ok := f.Free[dir]; ok {
			return n, nil
		}
		if dir == "/" || dir == "." {
			return 1 << 40, nil
		}
	}
}

func (f *Fake) fsType(p string) string {
	for dir := p; ; dir = path.Dir(dir) {
		if t, ok := f.FS[dir]; ok {
			return t
		}
		if dir == "/" || dir == "." {
			return "ext2/ext3"
		}
	}
}

func (f *Fake) Run(name string, args ...string) (string, error) { return f.run("", nil, name, args) }

func (f *Fake) RunAs(account string, env []string, name string, args ...string) (string, error) {
	return f.run(account, env, name, args)
}

func (f *Fake) run(account string, env []string, name string, args []string) (string, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if account != "" {
		line = "[" + account + "] " + line
	}
	f.Calls = append(f.Calls, line)
	for prefix, err := range f.Failures {
		if strings.HasPrefix(strings.TrimPrefix(line, "["+account+"] "), prefix) {
			return "", err
		}
	}
	if f.Missing[name] {
		return "", fmt.Errorf("%s: not found", name)
	}
	switch name {
	case "useradd":
		return "", f.useradd(args)
	case "groupadd":
		return "", f.groupadd(args)
	case "usermod":
		return "", f.usermod(args)
	case "loginctl":
		return f.loginctl(args)
	case "systemctl":
		return f.systemctl(account, args)
	case "mkfs.xfs":
		target := args[len(args)-1]
		if n, ok := f.nodes[target]; !ok || n.dir {
			return "", fmt.Errorf("mkfs.xfs: cannot open %s", target)
		}
		f.formatted[target] = "xfs"
		return "", nil
	case "blkid":
		target := args[len(args)-1]
		wanted := args[len(args)-2]
		if wanted == "UUID" {
			return "uuid-of-" + path.Base(target) + "\n", nil
		}
		if t := f.formatted[target]; t != "" {
			return t + "\n", nil
		}
		return "", errors.New("blkid: no signature")
	case "truncate":
		size, err := strconv.ParseInt(strings.TrimRight(args[1], "MGT"), 10, 64)
		if err != nil {
			return "", err
		}
		unit := map[byte]int64{'M': 1 << 20, 'G': 1 << 30, 'T': 1 << 40}[args[1][len(args[1])-1]]
		if n, ok := f.nodes[args[2]]; ok {
			n.size = size * unit
			return "", nil
		}
		return "", fmt.Errorf("truncate: %s: no such file", args[2])
	case "touch":
		f.ensureDirs(path.Dir(args[0]))
		if _, ok := f.nodes[args[0]]; !ok {
			f.nodes[args[0]] = &node{mode: 0o644}
		}
		return "", nil
	case "chattr":
		return "", nil
	case "stat":
		return f.fsType(args[len(args)-1]) + "\n", nil
	case "mount":
		return "", f.mount(args)
	case "umount":
		for i, m := range f.mounts {
			if m.target == args[len(args)-1] {
				f.mounts = append(f.mounts[:i], f.mounts[i+1:]...)
				return "", nil
			}
		}
		return "", errors.New("umount: not mounted")
	case "pkill":
		for name := range f.busy {
			if len(args) == 3 && args[2] == name {
				delete(f.busy, name)
				return "", nil
			}
		}
		return "", errors.New("pkill: exit status 1")
	case "userdel":
		name := args[len(args)-1]
		u, ok := f.users[name]
		if !ok {
			return "", fmt.Errorf("userdel: user '%s' does not exist", name)
		}
		if f.busy[name] {
			return "", fmt.Errorf("userdel: user %s is currently used by process 2708: exit status 8", name)
		}
		delete(f.users, name)
		delete(f.groups, name)
		for _, members := range f.members {
			delete(members, name)
		}
		if len(args) > 1 && args[0] == "--remove" {
			f.RemoveAll(u.Home)
		}
		return "", nil
	case "groupdel":
		if _, ok := f.groups[args[0]]; !ok {
			return "", fmt.Errorf("groupdel: group '%s' does not exist", args[0])
		}
		delete(f.groups, args[0])
		delete(f.members, args[0])
		return "", nil
	case "podman":
		if len(args) > 0 && (args[0] == "ps") {
			return f.Containers, nil
		}
		if len(args) > 0 && args[0] == "rm" {
			f.Containers = ""
			return "", nil
		}
		return "podman version 4.9.3\n", nil
	case "id":
		if len(args) == 2 && args[0] == "-nG" {
			if _, ok := f.users[args[1]]; !ok {
				return "", fmt.Errorf("id: '%s': no such user", args[1])
			}
			names := []string{args[1]}
			for group, members := range f.members {
				if members[args[1]] {
					names = append(names, group)
				}
			}
			sort.Strings(names[1:])
			return strings.Join(names, " ") + "\n", nil
		}
	}
	return "", fmt.Errorf("fake: %s is not simulated", name)
}

func (f *Fake) useradd(args []string) error {
	name := args[len(args)-1]
	if _, ok := f.users[name]; ok {
		return fmt.Errorf("useradd: user '%s' already exists", name)
	}
	system, createHome, home := false, false, ""
	for i, arg := range args[:len(args)-1] {
		switch arg {
		case "--system", "-r":
			system = true
		case "--create-home", "-m":
			createHome = true
		case "--home-dir", "-d":
			home = args[i+1]
		}
	}
	uid := f.nextUID
	if system {
		uid, f.nextSys = f.nextSys, f.nextSys+1
	} else {
		f.nextUID++
	}
	if home == "" {
		home = "/home/" + name
	}
	f.users[name] = User{Name: name, UID: uid, GID: uid, Home: home}
	f.groups[name] = uid
	if createHome {
		f.ensureDirs(home)
		f.nodes[home].uid, f.nodes[home].gid, f.nodes[home].mode = uid, uid, 0o700
	}
	if !system {
		for _, file := range []string{"/etc/subuid", "/etc/subgid"} {
			f.nodes[file].data = append(f.nodes[file].data, []byte(fmt.Sprintf("%s:%d:65536\n", name, 100000+65536*(uid-1000)))...)
		}
	}
	return nil
}

func (f *Fake) groupadd(args []string) error {
	name := args[len(args)-1]
	if _, ok := f.groups[name]; ok {
		return fmt.Errorf("groupadd: group '%s' already exists", name)
	}
	f.groups[name] = f.nextSys
	f.nextSys++
	return nil
}

func (f *Fake) usermod(args []string) error {
	if len(args) != 3 || args[0] != "-aG" {
		return fmt.Errorf("fake: usermod %v is not simulated", args)
	}
	group, account := args[1], args[2]
	if _, ok := f.users[account]; !ok {
		return fmt.Errorf("usermod: user '%s' does not exist", account)
	}
	if _, ok := f.groups[group]; !ok {
		return fmt.Errorf("usermod: group '%s' does not exist", group)
	}
	if f.members[group] == nil {
		f.members[group] = map[string]bool{}
	}
	f.members[group][account] = true
	return nil
}

func (f *Fake) loginctl(args []string) (string, error) {
	switch {
	case len(args) == 2 && args[0] == "disable-linger":
		delete(f.linger, args[1])
		delete(f.nodes, "/var/lib/systemd/linger/"+args[1])
		return "", nil
	case len(args) == 2 && args[0] == "enable-linger":
		f.linger[args[1]] = true
		f.Install("/var/lib/systemd/linger/"+args[1], "", 0o644)
		if u, ok := f.users[args[1]]; ok {
			f.active[fmt.Sprintf("user@%d.service", u.UID)] = true // lingering starts the user manager
			f.busy[args[1]] = true
			f.controllers(u.UID)
		}
		return "", nil
	case len(args) == 2 && args[0] == "terminate-user":
		delete(f.busy, args[1])
		return "", nil
	case len(args) >= 2 && args[0] == "show-user":
		if f.linger[args[1]] {
			return "Linger=yes\n", nil
		}
		return "Linger=no\n", nil
	}
	return "", fmt.Errorf("fake: loginctl %v is not simulated", args)
}

func (f *Fake) systemctl(account string, args []string) (string, error) {
	scope := ""
	if len(args) > 0 && args[0] == "--user" {
		scope, args = account+":", args[1:]
	}
	if len(args) == 0 {
		return "", errors.New("systemctl: no command")
	}
	unit := ""
	if len(args) > 1 {
		unit = args[len(args)-1]
	}
	switch args[0] {
	case "daemon-reload", "restart-nothing":
		return "", nil
	case "link":
		target := args[1]
		dir := "/etc/systemd/system"
		if scope != "" {
			dir = f.users[account].Home + "/.config/systemd/user"
			f.ensureDirs(dir)
		}
		link := dir + "/" + path.Base(target)
		if _, ok := f.nodes[link]; ok {
			return "", nil
		}
		f.nodes[link] = &node{link: target, mode: 0o777}
		return "", nil
	case "enable":
		f.enabled[scope+unit] = true
		for _, arg := range args[1 : len(args)-1] {
			if arg == "--now" {
				f.active[scope+unit] = true
				if unit == "podman.socket" && scope != "" {
					u := f.users[account]
					f.Install(fmt.Sprintf("/run/user/%d/podman/podman.sock", u.UID), "", 0o660)
				}
			}
		}
		return "", nil
	case "disable":
		delete(f.enabled, scope+unit)
		return "", nil
	case "start", "restart":
		f.active[scope+unit] = true
		if strings.HasPrefix(unit, "user@") {
			uid, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(unit, "user@"), ".service"))
			f.controllers(uid)
		}
		return "", nil
	case "stop":
		delete(f.active, scope+unit)
		if strings.HasPrefix(unit, "user@") {
			uid, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(unit, "user@"), ".service"))
			for name, u := range f.users {
				if u.UID == uid {
					delete(f.busy, name)
				}
			}
		}
		return "", nil
	case "is-active":
		if f.active[scope+unit] {
			return "active\n", nil
		}
		return "inactive\n", errors.New("inactive")
	case "is-enabled":
		if f.enabled[scope+unit] {
			return "enabled\n", nil
		}
		return "disabled\n", errors.New("disabled")
	}
	return "", fmt.Errorf("fake: systemctl %v is not simulated", args)
}

func (f *Fake) mount(args []string) error {
	var source, target, options string
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			options = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	switch len(rest) {
	case 1: // mount TARGET: from /etc/fstab
		target = rest[0]
		for _, line := range strings.Split(string(f.nodes["/etc/fstab"].data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[1] == target {
				source, options = fields[0], fields[3]
			}
		}
		if source == "" {
			return fmt.Errorf("mount: %s is not in /etc/fstab", target)
		}
	case 2:
		source, target = rest[0], rest[1]
	default:
		return fmt.Errorf("fake: mount %v is not simulated", args)
	}
	if f.Mounted(target) {
		return fmt.Errorf("mount: %s is already mounted", target)
	}
	if n, ok := f.nodes[target]; !ok || !n.dir {
		return fmt.Errorf("mount: %s: mount point does not exist", target)
	}
	fstype := f.formatted[source]
	if fstype == "" && strings.HasPrefix(source, "UUID=uuid-of-") {
		fstype = "xfs"
	}
	if fstype == "" {
		return fmt.Errorf("mount: %s: wrong fs type", source)
	}
	f.mounts = append(f.mounts, mount{source: source, target: target, fstype: fstype, options: options})
	return nil
}

// AddMount makes a mount exist already, as on a machine that has been configured before.
func (f *Fake) AddMount(source, target, fstype, options string) {
	f.ensureDirs(target)
	f.mounts = append(f.mounts, mount{source: source, target: target, fstype: fstype, options: options})
}

// AddUser makes an account exist already.
func (f *Fake) AddUser(name string, uid int, home string) {
	f.users[name] = User{Name: name, UID: uid, GID: uid, Home: home}
	f.groups[name] = uid
	f.ensureDirs(home)
	if uid >= 1000 {
		for _, file := range []string{"/etc/subuid", "/etc/subgid"} {
			f.nodes[file].data = append(f.nodes[file].data, []byte(fmt.Sprintf("%s:%d:65536\n", name, 100000+65536*(uid-1000)))...)
		}
	}
}

// controllers writes the control groups a user manager has: those it is delegated when the drop-in was there as it started.
func (f *Fake) controllers(uid int) {
	list := "pids"
	if n, ok := f.nodes[fmt.Sprintf("/etc/systemd/system/user@%d.service.d/delegate.conf", uid)]; ok && strings.Contains(string(n.data), "Delegate=") {
		list = "cpuset cpu io memory pids"
	}
	f.Install(fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/cgroup.controllers", uid, uid), list+"\n", 0o444)
}
