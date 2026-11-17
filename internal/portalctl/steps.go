package portalctl

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"exam-platform/internal/runner"
)

const (
	rootUser = "root"
	nologin  = "/usr/sbin/nologin"
)

// ids are the numbers of an account and a group, and whether both exist.
func (c *Ctx) ids(user, group string) (uid, gid int, ok bool) {
	u, hasUser := c.Sys.Lookup(user)
	g, hasGroup := c.Sys.LookupGroup(group)
	return u.UID, g, hasUser && hasGroup
}

func (c *Ctx) systemctl(args ...string) (string, error) { return c.Sys.Run("systemctl", args...) }

// userEnv is what the runner's user manager needs to be spoken to (the same words as the scripts of the first deployment).
func (c *Ctx) userEnv(uid int, home string) []string {
	return []string{fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid), fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid), "HOME=" + home}
}

// userctl runs systemctl --user in the runner's user manager.
func (c *Ctx) userctl(args ...string) (string, error) {
	u, ok := c.Sys.Lookup(c.L.RunnerUser)
	if !ok {
		return "", fmt.Errorf("the account %s does not exist", c.L.RunnerUser)
	}
	return c.Sys.RunAs(c.L.RunnerUser, c.userEnv(u.UID, u.Home), "systemctl", append([]string{"--user"}, args...)...)
}

// ----- folders

// dirStep makes a folder, owned by an account, with a mode. A folder that exists and belongs to someone else is not this
// configuration's to change: it is a problem to be told, never a chown of what may be a system folder.
func dirStep(name, dir, user, group string, mode fs.FileMode) Step {
	return Step{
		Name: name,
		Check: func(c *Ctx) (bool, string, error) {
			info := c.Sys.Stat(dir)
			if !info.Exists {
				return false, "make " + dir, nil
			}
			if !info.IsDir {
				return false, "", fmt.Errorf("%s exists and is not a folder", dir)
			}
			uid, gid, known := c.ids(user, group)
			if !known {
				return false, dir + " exists; its owner is not made yet", nil
			}
			if info.UID != uid || info.GID != gid {
				return false, "", fmt.Errorf("%s belongs to %d:%d, not to %s:%s, and it was not made by this configuration, so it is left alone: give it to them (chown) or choose another folder", dir, info.UID, info.GID, user, group)
			}
			if info.Mode != mode {
				return false, fmt.Sprintf("%s: mode %v to %v", dir, info.Mode, mode), nil
			}
			return true, dir, nil
		},
		Apply: func(c *Ctx) error {
			if !c.Sys.Stat(dir).Exists {
				if err := c.Sys.MkdirAll(dir, mode); err != nil {
					return err
				}
			}
			if uid, gid, ok := c.ids(user, group); ok {
				if err := c.Sys.Chown(dir, uid, gid); err != nil {
					return err
				}
			}
			return c.Sys.Chmod(dir, mode)
		},
	}
}

// ----- accounts

func groupStep(group string) Step {
	return Step{
		Name: "group " + group,
		Check: func(c *Ctx) (bool, string, error) {
			_, ok := c.Sys.LookupGroup(group)
			return ok, "", nil
		},
		Apply: func(c *Ctx) error { _, err := c.Sys.Run("groupadd", "--system", group); return err },
	}
}

func userStep(name, home, comment string, system bool) Step {
	return Step{
		Name: "account " + name,
		Check: func(c *Ctx) (bool, string, error) {
			u, ok := c.Sys.Lookup(name)
			if ok {
				return true, fmt.Sprintf("uid %d", u.UID), nil
			}
			return false, "make it", nil
		},
		Apply: func(c *Ctx) error {
			args := []string{"--shell", nologin, "--comment", comment}
			if system {
				args = append(args, "--system", "--no-create-home", "--home-dir", home)
			} else {
				args = append(args, "--create-home")
			}
			_, err := c.Sys.Run("useradd", append(args, name)...)
			return err
		},
	}
}

func memberStep(user, group string) Step {
	return Step{
		Name: fmt.Sprintf("%s in group %s", user, group),
		Check: func(c *Ctx) (bool, string, error) {
			if _, ok := c.Sys.Lookup(user); !ok {
				return false, "the account is not made yet", nil
			}
			if _, ok := c.Sys.LookupGroup(group); !ok {
				return false, "the group is not made yet", nil
			}
			out, err := c.Sys.Run("id", "-nG", user)
			if err != nil {
				return false, "", err
			}
			for _, name := range strings.Fields(out) {
				if name == group {
					return true, "", nil
				}
			}
			return false, "add it", nil
		},
		Apply: func(c *Ctx) error { _, err := c.Sys.Run("usermod", "-aG", group, user); return err },
	}
}

// subIDStep checks that the runner has the ranges of user and group numbers that rootless containers map their users into. useradd gives
// them to a regular account; when there are none the host's login.defs has none to give, and that is the administrator's to set.
func subIDStep(user string) Step {
	return Step{
		Name: "subordinate ids of " + user,
		Check: func(c *Ctx) (bool, string, error) {
			if _, ok := c.Sys.Lookup(user); !ok {
				return false, "the account is not made yet", nil
			}
			for _, file := range []string{"/etc/subuid", "/etc/subgid"} {
				data, _ := c.Sys.ReadFile(file)
				found := false
				for _, line := range lines(string(data)) {
					if strings.HasPrefix(line, user+":") {
						found = true
					}
				}
				if !found {
					return false, "", fmt.Errorf("%s has no range for %s, which rootless containers need: run  usermod --add-subuids 200000-265535 --add-subgids 200000-265535 %s  with a range that is free, and run this again", file, user, user)
				}
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error { return fmt.Errorf("nothing to do by itself") },
	}
}

// ----- the admin token

func tokenStep() Step {
	return Step{
		Name: "the proctor's token",
		Check: func(c *Ctx) (bool, string, error) {
			info := c.Sys.Stat(c.L.Token)
			uid, gid, known := c.ids(c.L.ServerUser, c.L.ServerUser)
			switch {
			case !info.Exists:
				return false, "make " + c.L.Token + " with a random token", nil
			case info.IsDir:
				return false, "", fmt.Errorf("%s is a folder", c.L.Token)
			case info.Size == 0:
				return false, "", fmt.Errorf("%s is empty: take it away and a token is made, or put one in it", c.L.Token)
			case !known:
				return false, "its owner is not made yet", nil
			case info.Mode != 0o600 || info.UID != uid || info.GID != gid:
				return false, "its owner and mode are put right", nil
			}
			return true, c.L.Token, nil
		},
		Apply: func(c *Ctx) error {
			if !c.Sys.Stat(c.L.Token).Exists {
				raw := make([]byte, 40)
				if _, err := rand.Read(raw); err != nil {
					return err
				}
				token := base64.RawURLEncoding.EncodeToString(raw)
				if err := c.Sys.WriteFile(c.L.Token, []byte(token+"\n"), 0o600); err != nil {
					return err
				}
				c.Changed["token"] = true
			}
			uid, gid, _ := c.ids(c.L.ServerUser, c.L.ServerUser)
			if err := c.Sys.Chown(c.L.Token, uid, gid); err != nil {
				return err
			}
			return c.Sys.Chmod(c.L.Token, 0o600)
		},
	}
}

// ----- files of the configuration

// fileStep writes a file whole: the units (kept in INSTALL_DIR) and the drop-ins. content is made when the step is checked.
func fileStep(name, file string, mode fs.FileMode, content func(c *Ctx) (string, error), flag string) Step {
	return Step{
		Name: name,
		Check: func(c *Ctx) (bool, string, error) {
			want, err := content(c)
			if err != nil {
				return false, "", err
			}
			have, readErr := c.Sys.ReadFile(file)
			if readErr != nil {
				return false, "write " + file, nil
			}
			if string(have) != want {
				return false, "change " + file, nil
			}
			return true, file, nil
		},
		Apply: func(c *Ctx) error {
			want, err := content(c)
			if err != nil {
				return err
			}
			if err := c.Sys.MkdirAll(path.Dir(file), 0o755); err != nil {
				return err
			}
			if err := c.Sys.WriteFile(file, []byte(want), mode); err != nil {
				return err
			}
			if flag != "" {
				c.Changed[flag] = true
			}
			return nil
		},
	}
}

// ----- units

const systemUnitDir = "/etc/systemd/system"

// systemUnitStep puts a unit in INSTALL_DIR, links it into /etc/systemd/system and, when asked, enables it. A file that is already
// where the link goes (the scripts of the first deployment put units there) is kept as name.before-cbe-portalctl.
func systemUnitStep(unit string, enable bool) Step {
	return Step{
		Name: "unit " + unit,
		Check: func(c *Ctx) (bool, string, error) {
			want, err := c.L.Unit(unit)
			if err != nil {
				return false, "", err
			}
			file := path.Join(c.L.UnitDir(), unit)
			if have, err := c.Sys.ReadFile(file); err != nil || string(have) != want {
				return false, "write " + file, nil
			}
			link := c.Sys.Stat(path.Join(systemUnitDir, unit))
			if !link.IsLink || link.Target != file {
				return false, "link it into " + systemUnitDir, nil
			}
			if enable {
				if out, _ := c.systemctl("is-enabled", unit); strings.TrimSpace(out) != "enabled" {
					return false, "enable it at boot", nil
				}
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			want, err := c.L.Unit(unit)
			if err != nil {
				return err
			}
			file := path.Join(c.L.UnitDir(), unit)
			if have, err := c.Sys.ReadFile(file); err != nil || string(have) != want {
				if err := c.Sys.MkdirAll(c.L.UnitDir(), 0o755); err != nil {
					return err
				}
				if err := c.Sys.WriteFile(file, []byte(want), 0o644); err != nil {
					return err
				}
				if err == nil {
					c.Changed["restart:"+unit] = true // a unit that was there is now another: what runs is not what is written
				}
			}
			if link := c.Sys.Stat(path.Join(systemUnitDir, unit)); link.Exists && (!link.IsLink || link.Target != file) {
				if err := keepAside(c, path.Join(systemUnitDir, unit)); err != nil {
					return err
				}
			}
			if _, err := c.systemctl("link", file); err != nil {
				return err
			}
			if _, err := c.systemctl("daemon-reload"); err != nil {
				return err
			}
			if enable {
				if _, err := c.systemctl("enable", unit); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// keepAside moves what is where a link must go out of the way, keeping it.
func keepAside(c *Ctx, file string) error {
	data, err := c.Sys.ReadFile(file)
	if err == nil {
		if err := c.Sys.WriteFile(file+".before-cbe-portalctl", data, 0o644); err != nil {
			return err
		}
	}
	return c.Sys.Remove(file)
}

// userUnitStep is systemUnitStep for the runner's user manager: the link is in the runner's home, and systemctl is spoken to as the runner.
func userUnitStep(unit string, enable bool) Step {
	userUnitDir := func(c *Ctx) string {
		u, _ := c.Sys.Lookup(c.L.RunnerUser)
		return path.Join(u.Home, ".config", "systemd", "user")
	}
	return Step{
		Name: "user unit " + unit,
		Check: func(c *Ctx) (bool, string, error) {
			if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
				return false, "the account is not made yet", nil
			}
			want, err := c.L.Unit(unit)
			if err != nil {
				return false, "", err
			}
			file := path.Join(c.L.UnitDir(), unit)
			if have, err := c.Sys.ReadFile(file); err != nil || string(have) != want {
				return false, "write " + file, nil
			}
			link := c.Sys.Stat(path.Join(userUnitDir(c), unit))
			if !link.IsLink || link.Target != file {
				return false, "link it into the runner's user units", nil
			}
			if enable {
				if out, _ := c.userctl("is-enabled", unit); strings.TrimSpace(out) != "enabled" {
					return false, "enable it", nil
				}
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			u, _ := c.Sys.Lookup(c.L.RunnerUser)
			want, err := c.L.Unit(unit)
			if err != nil {
				return err
			}
			file := path.Join(c.L.UnitDir(), unit)
			if have, err := c.Sys.ReadFile(file); err != nil || string(have) != want {
				if err := c.Sys.MkdirAll(c.L.UnitDir(), 0o755); err != nil {
					return err
				}
				if err := c.Sys.WriteFile(file, []byte(want), 0o644); err != nil {
					return err
				}
				if err == nil {
					c.Changed["restart:"+unit] = true // a unit that was there is now another: what runs is not what is written
				}
			}
			dir := userUnitDir(c)
			for _, folder := range []string{path.Join(u.Home, ".config"), path.Join(u.Home, ".config", "systemd"), dir} {
				if !c.Sys.Stat(folder).Exists {
					if err := c.Sys.MkdirAll(folder, 0o755); err != nil {
						return err
					}
					_ = c.Sys.Chown(folder, u.UID, u.GID)
				}
			}
			link := path.Join(dir, unit)
			if info := c.Sys.Stat(link); info.Exists && (!info.IsLink || info.Target != file) {
				if err := keepAside(c, link); err != nil {
					return err
				}
			}
			if !c.Sys.Stat(link).Exists {
				if err := c.Sys.Symlink(file, link); err != nil {
					return err
				}
				_ = c.Sys.Chown(link, u.UID, u.GID)
			}
			if _, err := c.userctl("daemon-reload"); err != nil {
				return err
			}
			if enable {
				if _, err := c.userctl("enable", unit); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// startSystemStep starts a service that is not running. One that runs is not restarted: a restart is a decision about the classes
// that are in progress, and `cbe-portalctl restart` is where it is made.
func startSystemStep(unit string) Step {
	return Step{
		Name: "start " + unit,
		Check: func(c *Ctx) (bool, string, error) {
			if out, _ := c.systemctl("is-active", unit); strings.TrimSpace(out) == "active" {
				if c.Changed["restart:"+unit] {
					return true, "running; its unit changed, and `cbe-portalctl restart` applies it", nil
				}
				return true, "running", nil
			}
			return false, "", nil
		},
		Apply: func(c *Ctx) error { _, err := c.systemctl("start", unit); return err },
	}
}

func startUserStep(unit string) Step {
	return Step{
		Name: "start " + unit + " (runner)",
		Check: func(c *Ctx) (bool, string, error) {
			if out, _ := c.userctl("is-active", unit); strings.TrimSpace(out) == "active" {
				if c.Changed["restart:"+unit] {
					return true, "running; its unit changed, and `cbe-portalctl restart` applies it", nil
				}
				return true, "running", nil
			}
			return false, "", nil
		},
		Apply: func(c *Ctx) error { _, err := c.userctl("start", unit); return err },
	}
}

// ----- the runner's configuration

// runnerConfigStep writes the runner's settings. What the configuration decides (the folders, the sockets, the memory budget) is set; what
// an administrator may have changed in the file (the limits, the images) is kept.
func runnerConfigStep() Step {
	desired := func(c *Ctx) ([]byte, error) {
		u, ok := c.Sys.Lookup(c.L.RunnerUser)
		if !ok {
			return nil, fmt.Errorf("the account %s is not made yet", c.L.RunnerUser)
		}
		base, err := json.Marshal(runner.DefaultConfig())
		if err != nil {
			return nil, err
		}
		settings := map[string]any{}
		if err := json.Unmarshal(base, &settings); err != nil {
			return nil, err
		}
		if have, err := c.Sys.ReadFile(c.L.RunnerConfig()); err == nil {
			var existing map[string]any
			if err := json.Unmarshal(have, &existing); err != nil {
				return nil, fmt.Errorf("%s is not JSON (%v): put it right or take it away", c.L.RunnerConfig(), err)
			}
			for key, value := range existing {
				settings[key] = value
			}
		}
		for key, value := range map[string]any{
			"socket": path.Join(c.L.IPC(), "runner.sock"), "socket_group": c.L.IPCGroup, "secret_file": path.Join(c.L.IPC(), "runner.secret"),
			"runtime_socket": fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", u.UID), "quota_socket": "/run/cbe-quota/helper.sock",
			"work_root": c.L.WorkDir, "run_root": path.Join(c.L.Data, "run"), "state_root": path.Join(c.L.Data, "runner"), "grade_root": path.Join(c.L.Data, "grading"),
			"host_memory_mb": c.L.HostMemoryMB,
		} {
			settings[key] = value
		}
		out, err := json.MarshalIndent(settings, "", "  ")
		return append(out, '\n'), err
	}
	return Step{
		Name: "the runner's settings",
		Check: func(c *Ctx) (bool, string, error) {
			want, err := desired(c)
			if err != nil {
				if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
					return false, err.Error(), nil
				}
				return false, "", err
			}
			if have, err := c.Sys.ReadFile(c.L.RunnerConfig()); err != nil || !bytes.Equal(have, want) {
				return false, "write " + c.L.RunnerConfig(), nil
			}
			return true, c.L.RunnerConfig(), nil
		},
		Apply: func(c *Ctx) error {
			want, err := desired(c)
			if err != nil {
				return err
			}
			if err := c.Sys.MkdirAll(path.Dir(c.L.RunnerConfig()), 0o755); err != nil {
				return err
			}
			if c.Sys.Stat(c.L.RunnerConfig()).Exists {
				c.Changed["restart:cbe-runner.service"] = true
			}
			return c.Sys.WriteFile(c.L.RunnerConfig(), want, 0o644)
		},
	}
}
