package portalctl

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// ----- the volume for the students' folders

// mountOf finds a mount in /proc/self/mountinfo: its filesystem type and the options of the filesystem itself.
func mountOf(c *Ctx, dir string) (fstype, options string, found bool) {
	data, err := c.Sys.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		fields := strings.Fields(left)
		after := strings.Fields(right)
		if len(fields) < 5 || len(after) < 3 {
			continue
		}
		// The mount point can hold spaces written as \040; the ones this program writes do not.
		if fields[4] == dir {
			found, fstype, options = true, after[0], after[2]
		}
	}
	return fstype, options, found
}

// volumeReady says whether the folder is an XFS volume with project quotas, which is what the quota helper needs.
func volumeReady(c *Ctx, dir string) (bool, string) {
	fstype, options, found := mountOf(c, dir)
	switch {
	case !found:
		return false, dir + " is not a mount point"
	case fstype != "xfs":
		return false, fmt.Sprintf("%s is %s, not xfs", dir, fstype)
	case !hasOption(options, "prjquota") && !hasOption(options, "pquota"):
		return false, dir + " is xfs, but mounted without project quotas (prjquota)"
	}
	return true, ""
}

func hasOption(options, want string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == want {
			return true
		}
	}
	return false
}

// volumeStep makes the volume the students' folders are on: an XFS file system with project quotas, mounted on WORK_DIR and in
// /etc/fstab. It never formats what has something on it: a file that is not empty and not XFS, a device that is mounted, are problems.
func volumeStep() Step {
	return Step{
		Name: "the XFS volume for the students' folders",
		Check: func(c *Ctx) (bool, string, error) {
			if ok, why := volumeReady(c, c.L.WorkDir); ok {
				return true, c.L.WorkDir + " is xfs with project quotas", nil
			} else if !c.L.CreateVolume {
				return false, "", fmt.Errorf("%s; set CREATE_XFS_VOLUME=yes to make one, or mount an XFS volume there with  mount -o prjquota", why)
			}
			if c.L.VolumeDevice != "" {
				if fstype, _, mounted := mountOfDevice(c, c.L.VolumeDevice); mounted {
					return false, "", fmt.Errorf("%s is mounted (as %s) and will not be formatted", c.L.VolumeDevice, fstype)
				}
				return false, "format " + c.L.VolumeDevice + " as XFS and mount it on " + c.L.WorkDir, nil
			}
			if c.Sys.Stat(c.L.VolumeFile).Exists {
				t, _ := c.Sys.Run("blkid", "-o", "value", "-s", "TYPE", c.L.VolumeFile)
				switch strings.TrimSpace(t) {
				case "xfs":
					return false, "mount " + c.L.VolumeFile + " on " + c.L.WorkDir, nil
				case "":
					return false, "", fmt.Errorf("%s exists and has no file system (an earlier run was stopped half way?), and what cannot be told to be empty is not formatted: take the file away and it is made again, or format it yourself with  mkfs.xfs -L cbework %s", c.L.VolumeFile, c.L.VolumeFile)
				}
				return false, "", fmt.Errorf("%s is a %s file system, not XFS, and is not formatted over", c.L.VolumeFile, strings.TrimSpace(t))
			}
			return false, fmt.Sprintf("make %s (%s, XFS), add it to /etc/fstab and mount it on %s", c.L.VolumeFile, c.L.VolumeSize, c.L.WorkDir), nil
		},
		Apply: func(c *Ctx) error {
			if err := c.Sys.MkdirAll(c.L.WorkDir, 0o711); err != nil {
				return err
			}
			source, line, err := prepareVolume(c)
			if err != nil {
				return err
			}
			if err := addFstab(c, line); err != nil {
				return err
			}
			if !c.Sys.Stat(c.L.WorkDir).IsDir {
				return fmt.Errorf("%s is not a folder", c.L.WorkDir)
			}
			if _, _, mounted := mountOf(c, c.L.WorkDir); !mounted {
				if _, err := c.Sys.Run("mount", c.L.WorkDir); err != nil {
					return fmt.Errorf("mounting %s (%s): %w", source, c.L.WorkDir, err)
				}
			}
			return c.Sys.Chmod(c.L.WorkDir, 0o711)
		},
	}
}

func mountOfDevice(c *Ctx, device string) (fstype string, target string, mounted bool) {
	data, _ := c.Sys.ReadFile("/proc/self/mountinfo")
	for _, line := range strings.Split(string(data), "\n") {
		left, right, ok := strings.Cut(line, " - ")
		after := strings.Fields(right)
		fields := strings.Fields(left)
		if ok && len(after) >= 2 && len(fields) >= 5 && after[1] == device {
			return after[0], fields[4], true
		}
	}
	return "", "", false
}

// prepareVolume makes what is to be mounted and says its /etc/fstab line.
func prepareVolume(c *Ctx) (source, fstab string, err error) {
	if c.L.VolumeDevice != "" {
		device := c.L.VolumeDevice
		if t, _ := c.Sys.Run("blkid", "-o", "value", "-s", "TYPE", device); strings.TrimSpace(t) != "xfs" {
			// It has been asked for, by name, and erase has been said: validation does not let this step be reached otherwise.
			if _, err := c.Sys.Run("mkfs.xfs", "-q", "-f", "-L", "cbework", device); err != nil {
				return "", "", err
			}
		}
		uuid, err := c.Sys.Run("blkid", "-o", "value", "-s", "UUID", device)
		if err != nil || strings.TrimSpace(uuid) == "" {
			return "", "", fmt.Errorf("the UUID of %s cannot be read: %v", device, err)
		}
		return device, fmt.Sprintf("UUID=%s %s xfs prjquota,nofail 0 0", strings.TrimSpace(uuid), c.L.WorkDir), nil
	}
	file := c.L.VolumeFile
	folder := path.Dir(file)
	size, err := sizeArg(c.L.VolumeSize)
	if err != nil {
		return "", "", err
	}
	if free, err := c.Sys.FreeBytes(folder); err == nil && free < 2<<30 {
		return "", "", fmt.Errorf("%s has only %d MiB free: the volume file grows as students use it, and needs room", folder, free>>20)
	}
	created := false
	if !c.Sys.Stat(file).Exists {
		created = true
		if err := c.Sys.MkdirAll(folder, 0o755); err != nil {
			return "", "", err
		}
		if _, err := c.Sys.Run("touch", file); err != nil {
			return "", "", err
		}
		// A copy-on-write file system (btrfs) would copy a loop image on every write: no copy-on-write, while the file is empty.
		if t, _ := c.Sys.Run("stat", "-f", "-c", "%T", folder); strings.TrimSpace(t) == "btrfs" {
			if _, err := c.Sys.Run("chattr", "+C", file); err != nil {
				return "", "", fmt.Errorf("the folder is on btrfs and %s cannot be made without copy-on-write: %w", file, err)
			}
		}
		if _, err := c.Sys.Run("truncate", "-s", size, file); err != nil {
			return "", "", err
		}
	}
	if err := c.Sys.Chmod(file, 0o600); err != nil { // the raw image holds every student's files: root only
		return "", "", err
	}
	t, _ := c.Sys.Run("blkid", "-o", "value", "-s", "TYPE", file)
	switch strings.TrimSpace(t) {
	case "xfs":
	case "":
		// Only a file this run has just made is formatted: one that is there already may hold something that has no signature to be seen.
		if !created {
			return "", "", fmt.Errorf("%s has no file system and was not made by this run, so it is not formatted", file)
		}
		if _, err := c.Sys.Run("mkfs.xfs", "-q", "-L", "cbework", file); err != nil {
			return "", "", err
		}
	default:
		return "", "", fmt.Errorf("%s is a %s file system, not XFS, and is not formatted over", file, strings.TrimSpace(t))
	}
	return file, fmt.Sprintf("%s %s xfs loop,prjquota,nofail,x-systemd.requires-mounts-for=%s 0 0", file, c.L.WorkDir, folder), nil
}

// sizeArg is a size for truncate.
func sizeArg(size string) (string, error) {
	if len(size) < 2 {
		return "", fmt.Errorf("%q is not a size", size)
	}
	if _, err := strconv.Atoi(size[:len(size)-1]); err != nil {
		return "", fmt.Errorf("%q is not a size", size)
	}
	return size, nil
}

// addFstab adds the line for the mount, once, keeping the file as it was (once) in /etc/fstab.bak-cbe.
func addFstab(c *Ctx, line string) error {
	data, err := c.Sys.ReadFile("/etc/fstab")
	if err != nil {
		return err
	}
	target := strings.Fields(line)[1]
	for _, existing := range lines(string(data)) {
		if fields := strings.Fields(existing); len(fields) >= 2 && fields[1] == target {
			return nil // the machine already says what is mounted there
		}
	}
	if !c.Sys.Stat("/etc/fstab.bak-cbe").Exists {
		if err := c.Sys.WriteFile("/etc/fstab.bak-cbe", data, 0o644); err != nil {
			return err
		}
	}
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return c.Sys.WriteFile("/etc/fstab", []byte(text+line+"\n"), 0o644)
}

// ----- the container runtime's account

func lingerStep() Step {
	return Step{
		Name: "lingering for the runner (its services outlive any login)",
		Check: func(c *Ctx) (bool, string, error) {
			if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
				return false, "the account is not made yet", nil
			}
			return c.Sys.Stat("/var/lib/systemd/linger/" + c.L.RunnerUser).Exists, "", nil
		},
		Apply: func(c *Ctx) error { _, err := c.Sys.Run("loginctl", "enable-linger", c.L.RunnerUser); return err },
	}
}

// delegationFiles are the two drop-ins of the runner's slice.
func delegationContent(c *Ctx) (string, error) {
	return "# Written by cbe-portalctl. Rootless Podman needs these controllers to hold the limits of the containers, and the user manager waits for the\n" +
		"# volume: without it a container would bind-mount the empty folder under it, and a student would see an empty /work after a reboot.\n" +
		"[Unit]\nRequiresMountsFor=" + c.L.WorkDir + "\n\n[Service]\nDelegate=cpu cpuset io memory pids\n", nil
}

func delegationStep() Step {
	dropin := func(c *Ctx) string {
		u, _ := c.Sys.Lookup(c.L.RunnerUser)
		return fmt.Sprintf("/etc/systemd/system/user@%d.service.d/delegate.conf", u.UID)
	}
	step := fileStep("delegation of the control groups to the runner", "", 0o644, delegationContent, "user-manager")
	step.Check = func(c *Ctx) (bool, string, error) {
		if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
			return false, "the account is not made yet", nil
		}
		want, _ := delegationContent(c)
		if have, err := c.Sys.ReadFile(dropin(c)); err != nil || string(have) != want {
			return false, "write " + dropin(c), nil
		}
		return true, dropin(c), nil
	}
	step.Apply = func(c *Ctx) error {
		file := dropin(c)
		want, _ := delegationContent(c)
		if err := c.Sys.MkdirAll(path.Dir(file), 0o755); err != nil {
			return err
		}
		if err := c.Sys.WriteFile(file, []byte(want), 0o644); err != nil {
			return err
		}
		c.Changed["user-manager"] = true
		_, err := c.systemctl("daemon-reload")
		return err
	}
	return step
}

// limitsContent is the limits of the control group every container runs in: the same numbers deploy/workspace/limits.sh gave (decision P8-3).
func limitsContent(c *Ctx) (string, error) {
	data, err := c.Sys.ReadFile("/proc/meminfo")
	if err != nil {
		return "", err
	}
	totalMB := 0
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, _ := strconv.Atoi(fields[1])
			totalMB = kb / 1024
		}
	}
	if totalMB == 0 {
		return "", fmt.Errorf("the machine's memory cannot be read from /proc/meminfo")
	}
	budget := c.L.HostMemoryMB
	if budget == 0 {
		budget = totalMB * 7 / 10
	}
	// Room for what is not a student's container: two grading runs of the largest size, a build, the runtime and the runner; but the
	// machine keeps 8 GiB for everything else, whatever the budget says.
	maxMB := budget + 2*2048 + 2048 + 3072
	if maxMB > totalMB-8192 {
		maxMB = totalMB - 8192
	}
	if maxMB < budget+2048 {
		maxMB = budget + 2048
	}
	return fmt.Sprintf("# Written by cbe-portalctl. The runner keeps the containers' memory limits under host_memory_mb (%d MiB).\n"+
		"# CPUWeight=50: under contention the exam server and the machine's owner win two to one; an idle machine still gives the students every core.\n"+
		"[Slice]\nCPUWeight=50\nMemoryMax=%dM\nMemorySwapMax=0\nTasksMax=12000\n", budget, maxMB), nil
}

func limitsStep() Step {
	dropinDir := func(c *Ctx) string {
		u, _ := c.Sys.Lookup(c.L.RunnerUser)
		return fmt.Sprintf("/etc/systemd/system/user-%d.slice.d", u.UID)
	}
	return Step{
		Name: "the limits of the containers' control group",
		Check: func(c *Ctx) (bool, string, error) {
			if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
				return false, "the account is not made yet", nil
			}
			want, err := limitsContent(c)
			if err != nil {
				return false, "", err
			}
			dir := dropinDir(c)
			if c.Sys.Stat(path.Join(dir, "spike-backstop.conf")).Exists {
				return false, "the spike's temporary cap is taken away", nil
			}
			if have, err := c.Sys.ReadFile(path.Join(dir, "cbe-limits.conf")); err != nil || string(have) != want {
				return false, "write " + path.Join(dir, "cbe-limits.conf"), nil
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			dir := dropinDir(c)
			want, err := limitsContent(c)
			if err != nil {
				return err
			}
			if err := c.Sys.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if old := path.Join(dir, "spike-backstop.conf"); c.Sys.Stat(old).Exists {
				if data, err := c.Sys.ReadFile(old); err == nil {
					_ = c.Sys.WriteFile(old+".removed", data, 0o644) // not read by systemd (it reads *.conf)
				}
				if err := c.Sys.Remove(old); err != nil {
					return err
				}
			}
			if err := c.Sys.WriteFile(path.Join(dir, "cbe-limits.conf"), []byte(want), 0o644); err != nil {
				return err
			}
			_, err = c.systemctl("daemon-reload")
			return err
		},
	}
}

// managerStep makes the runner's user manager run with the delegation. A manager that was started before the delegation was written has
// to be restarted, and a restart stops every container that is running: it is done only when none is.
func managerStep() Step {
	controllers := func(c *Ctx, uid int) string {
		data, _ := c.Sys.ReadFile(fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/cgroup.controllers", uid, uid))
		return string(data)
	}
	return Step{
		Name: "the runner's user manager, with its control groups",
		Check: func(c *Ctx) (bool, string, error) {
			u, ok := c.Sys.Lookup(c.L.RunnerUser)
			if !ok {
				return false, "the account is not made yet", nil
			}
			unit := fmt.Sprintf("user@%d.service", u.UID)
			if out, _ := c.systemctl("is-active", unit); strings.TrimSpace(out) != "active" {
				return false, "start " + unit, nil
			}
			have := controllers(c, u.UID)
			for _, need := range []string{"cpu", "memory", "pids"} {
				if !containsWord(have, need) {
					return false, unit + " runs without " + need + ": restart it (when no container is running)", nil
				}
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			u, _ := c.Sys.Lookup(c.L.RunnerUser)
			unit := fmt.Sprintf("user@%d.service", u.UID)
			if out, _ := c.systemctl("is-active", unit); strings.TrimSpace(out) == "active" {
				if running, _ := c.Sys.RunAs(c.L.RunnerUser, c.userEnv(u.UID, u.Home), "podman", "ps", "-q"); strings.TrimSpace(running) != "" {
					return fmt.Errorf("the user manager has to be restarted to hold the limits, and that stops the %d container(s) that are running: do it when no class is working (reboot, or systemctl restart %s), then run this again", len(strings.Fields(running)), unit)
				}
				_, err := c.systemctl("restart", unit)
				return err
			}
			_, err := c.systemctl("start", unit)
			return err
		},
	}
}

func containsWord(text, word string) bool {
	for _, field := range strings.Fields(text) {
		if field == word {
			return true
		}
	}
	return false
}

// podmanStep turns on the runtime's API socket for the runner, and the restart of the containers that were running at a boot.
func podmanStep() Step {
	return Step{
		Name: "the container runtime's socket for the runner",
		Check: func(c *Ctx) (bool, string, error) {
			u, ok := c.Sys.Lookup(c.L.RunnerUser)
			if !ok {
				return false, "the account is not made yet", nil
			}
			if out, err := c.userctl("is-enabled", "podman.socket"); err != nil || strings.TrimSpace(out) != "enabled" {
				return false, "enable podman.socket", nil
			}
			if out, err := c.userctl("is-enabled", "podman-restart.service"); err != nil || strings.TrimSpace(out) != "enabled" {
				return false, "enable podman-restart.service", nil
			}
			if !c.Sys.Stat(fmt.Sprintf("/run/user/%d/podman/podman.sock", u.UID)).Exists {
				return false, "start podman.socket", nil
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			if _, err := c.userctl("enable", "--now", "podman.socket"); err != nil {
				return err
			}
			_, err := c.userctl("enable", "podman-restart.service")
			return err
		},
	}
}

// storageStep says where the runtime keeps its images, when that is not under the runner's home.
func storageStep() Step {
	content := func(c *Ctx) string {
		return "# Written by cbe-portalctl (CONTAINER_STORAGE_DIR). Images already in the old place are not moved.\n[storage]\ndriver = \"overlay\"\ngraphroot = \"" + c.L.StorageDir + "\"\n"
	}
	file := func(c *Ctx) string {
		u, _ := c.Sys.Lookup(c.L.RunnerUser)
		return path.Join(u.Home, ".config", "containers", "storage.conf")
	}
	return Step{
		Name: "where the runtime keeps its images",
		Check: func(c *Ctx) (bool, string, error) {
			u, ok := c.Sys.Lookup(c.L.RunnerUser)
			if !ok {
				return false, "the account is not made yet", nil
			}
			info := c.Sys.Stat(c.L.StorageDir)
			if !info.Exists {
				return false, "make " + c.L.StorageDir, nil
			}
			if info.UID != u.UID {
				return false, "", fmt.Errorf("%s belongs to uid %d, not to %s", c.L.StorageDir, info.UID, c.L.RunnerUser)
			}
			if have, err := c.Sys.ReadFile(file(c)); err != nil || string(have) != content(c) {
				return false, "write " + file(c), nil
			}
			return true, c.L.StorageDir, nil
		},
		Apply: func(c *Ctx) error {
			u, _ := c.Sys.Lookup(c.L.RunnerUser)
			if !c.Sys.Stat(c.L.StorageDir).Exists {
				if err := c.Sys.MkdirAll(c.L.StorageDir, fs.FileMode(0o700)); err != nil {
					return err
				}
				if err := c.Sys.Chown(c.L.StorageDir, u.UID, u.GID); err != nil {
					return err
				}
			}
			conf := file(c)
			for _, folder := range []string{path.Join(u.Home, ".config"), path.Dir(conf)} {
				if !c.Sys.Stat(folder).Exists {
					if err := c.Sys.MkdirAll(folder, 0o755); err != nil {
						return err
					}
					_ = c.Sys.Chown(folder, u.UID, u.GID)
				}
			}
			if err := c.Sys.WriteFile(conf, []byte(content(c)), 0o644); err != nil {
				return err
			}
			return c.Sys.Chown(conf, u.UID, u.GID)
		},
	}
}
