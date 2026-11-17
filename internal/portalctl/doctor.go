package portalctl

import (
	"fmt"
	"strconv"
	"strings"
)

// Level is how bad a finding of the pre-flight check is.
type Level int

const (
	OK Level = iota
	Warn
	Fail
)

// Finding is one thing the pre-flight check found out about the machine.
type Finding struct {
	Level Level
	Name  string
	Note  string
}

func (f Finding) String() string {
	label := map[Level]string{OK: "ok  ", Warn: "warn", Fail: "FAIL"}[f.Level]
	if f.Note == "" {
		return fmt.Sprintf("  %s  %s", label, f.Name)
	}
	return fmt.Sprintf("  %s  %s: %s", label, f.Name, f.Note)
}

// Doctor looks at the machine and says whether the configuration can be done on it and what is missing, changing nothing. A configuration
// is not started when a finding fails.
func Doctor(c *Ctx) []Finding {
	var out []Finding
	add := func(level Level, name, note string) { out = append(out, Finding{level, name, note}) }
	l := c.L

	if _, found := c.Sys.LookPath("systemctl"); found && c.Sys.Stat("/run/systemd/system").IsDir {
		add(OK, "systemd", "")
	} else {
		add(Fail, "systemd", "this machine does not run systemd: the services are systemd units")
	}
	for _, name := range l.Programs() {
		if c.Sys.Stat(l.Bin(name)).Exists {
			add(OK, "program "+name, "")
		} else {
			add(Fail, "program "+name, l.Bin(name)+" is missing: the installer puts it there (reinstall), and WORKSPACE_ENABLED=no does not need the runner")
		}
	}
	for _, tool := range []string{"useradd", "groupadd", "usermod", "id"} {
		if _, found := c.Sys.LookPath(tool); !found {
			add(Fail, "program "+tool, "it is needed to make the accounts (package passwd or shadow-utils)")
		}
	}
	if free, err := c.Sys.FreeBytes(l.Data); err == nil {
		switch {
		case free < 1<<30:
			add(Fail, "room for the data", fmt.Sprintf("%s has %d MiB free", l.Data, free>>20))
		default:
			add(OK, "room for the data", fmt.Sprintf("%d GiB free where %s is", free>>30, l.Data))
		}
	}
	if !l.Workspace {
		return out
	}

	if out, err := c.Sys.Run("podman", "--version"); err != nil {
		add(Fail, "podman", "it is not installed: the container runtime of the workspaces (apt install podman uidmap)")
	} else if major := podmanMajor(out); major != 0 && major < 4 {
		add(Fail, "podman", "version "+strings.TrimSpace(out)+" is too old: 4 or later")
	} else {
		add(OK, "podman", strings.TrimSpace(out))
	}
	for _, tool := range []string{"newuidmap", "newgidmap"} {
		if _, found := c.Sys.LookPath(tool); !found {
			add(Fail, "program "+tool, "rootless containers need it (package uidmap)")
		}
	}
	if data, err := c.Sys.ReadFile("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		add(Fail, "control groups v2", "/sys/fs/cgroup/cgroup.controllers is not there: the containers' limits need cgroup v2 (the default on current systems)")
	} else {
		for _, need := range []string{"cpu", "memory", "pids"} {
			if !containsWord(string(data), need) {
				add(Fail, "control groups v2", "the "+need+" controller is not available")
			}
		}
	}
	if _, found := c.Sys.LookPath("loginctl"); !found {
		add(Fail, "program loginctl", "it is needed to let the runner's services outlive a login")
	}
	switch {
	case ready(c):
		add(OK, "XFS volume", l.WorkDir+" is an XFS mount with project quotas")
	case !l.CreateVolume:
		add(Fail, "XFS volume", l.WorkDir+" is not an XFS mount with project quotas, and CREATE_XFS_VOLUME is no")
	default:
		if _, found := c.Sys.LookPath("mkfs.xfs"); !found {
			add(Fail, "program mkfs.xfs", "it makes the XFS volume (package xfsprogs)")
		}
		if l.VolumeDevice == "" {
			for _, tool := range []string{"mount", "truncate", "touch"} {
				if _, found := c.Sys.LookPath(tool); !found {
					add(Fail, "program "+tool, "it is needed to make and mount the volume file")
				}
			}
			size, _ := parseGiB(l.VolumeSize)
			if free, err := c.Sys.FreeBytes(l.VolumeFile); err == nil && free < 2<<30 {
				add(Fail, "room for the volume file", fmt.Sprintf("%s has %d MiB free", l.VolumeFile, free>>20))
			} else if err == nil && size > 0 && free < size<<30 {
				add(Warn, "room for the volume file", fmt.Sprintf("the file is %s and grows as it is used, but there are %d GiB free where it is: the volume fills the disk before it is full", l.VolumeSize, free>>30))
			} else {
				add(OK, "room for the volume file", l.VolumeSize+" (sparse: it takes disk as it is used)")
			}
		} else if _, mountedAs, mounted := mountOfDevice(c, l.VolumeDevice); mounted {
			add(Fail, "XFS device", l.VolumeDevice+" is mounted on "+mountedAs+" and is not formatted")
		} else if !c.Sys.Stat(l.VolumeDevice).Exists {
			add(Fail, "XFS device", l.VolumeDevice+" does not exist")
		}
	}
	if l.StorageDir != "" {
		if free, err := c.Sys.FreeBytes(l.StorageDir); err == nil && free < 10<<30 {
			add(Warn, "room for the images", fmt.Sprintf("%d GiB free where %s is: an image with Python's libraries is several GB", free>>30, l.StorageDir))
		}
	}
	return out
}

func ready(c *Ctx) bool { ok, _ := volumeReady(c, c.L.WorkDir); return ok }

func parseGiB(size string) (int64, error) {
	if len(size) < 2 {
		return 0, fmt.Errorf("bad size")
	}
	n, err := strconv.ParseInt(size[:len(size)-1], 10, 64)
	if err != nil {
		return 0, err
	}
	switch size[len(size)-1] {
	case 'T':
		return n << 10, nil
	case 'G':
		return n, nil
	case 'M':
		return n >> 10, nil
	}
	return 0, fmt.Errorf("bad size")
}

func podmanMajor(version string) int {
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return 0
	}
	major, _, _ := strings.Cut(fields[len(fields)-1], ".")
	n, _ := strconv.Atoi(major)
	return n
}

// Failed says whether any finding stops the configuration.
func Failed(findings []Finding) bool {
	for _, f := range findings {
		if f.Level == Fail {
			return true
		}
	}
	return false
}
