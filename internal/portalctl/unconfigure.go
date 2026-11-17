package portalctl

import (
	"fmt"
	"path"
	"strings"
)

// The services, in the order they are started: the quota helper and the runner before the server that uses them. They are stopped in
// the opposite order.
const (
	quotaUnit  = "cbe-quota-helper.service"
	runnerUnit = "cbe-runner.service"
	serverUnit = "cbe-server.service"
)

// UnconfigurePlan takes the services away: they are stopped and disabled, their units unlinked, and the drop-ins of the runner's slice
// removed. The data (the database, the screenshots, the students' folders, the volume, the accounts that own them) is left, unless
// removeData says it is to go too: then the containers are removed, the volume unmounted and out of fstab, the volume file, the data
// folder and the accounts deleted.
func UnconfigurePlan(l Layout, removeData bool) []Step {
	var steps []Step
	add := func(more ...Step) { steps = append(steps, more...) }

	add(stopStep(serverUnit, false))
	if l.Workspace {
		add(stopStep(runnerUnit, true), stopStep(quotaUnit, false))
	}
	add(unlinkStep(serverUnit, false))
	if l.Workspace {
		add(unlinkStep(runnerUnit, true), unlinkStep(quotaUnit, false), dropinsStep())
	}
	if !removeData {
		// The answers outlive the program folder: with them a new installation finds its data again.
		add(keepAnswersStep())
		return steps
	}
	if l.Workspace {
		add(containersStep(), unmountStep(), fstabStep())
		if l.VolumeDevice == "" && l.CreateVolume {
			add(removeFileStep("the volume file", func(c *Ctx) string { return c.L.VolumeFile }))
		}
	}
	add(removeTreeStep())
	add(accountsStep())
	return steps
}

// KeptAnswers is where the answers are kept for a reinstallation, with the data, when the services are taken away and the data stays.
func (l Layout) KeptAnswers() string { return path.Join(l.Data, "cbe-portal.conf.last") }

func keepAnswersStep() Step {
	return Step{
		Name: "keep the answers with the data",
		Check: func(c *Ctx) (bool, string, error) {
			saved, err := c.Sys.ReadFile(c.L.SavedConfig())
			if err != nil || !c.Sys.Stat(c.L.Data).Exists {
				return true, "there is nothing to keep", nil
			}
			if kept, err := c.Sys.ReadFile(c.L.KeptAnswers()); err == nil && string(kept) == string(saved) {
				return true, c.L.KeptAnswers(), nil
			}
			return false, "copy them to " + c.L.KeptAnswers() + ": a new installation is configured with  --config  of that file, and finds its data again", nil
		},
		Apply: func(c *Ctx) error {
			saved, err := c.Sys.ReadFile(c.L.SavedConfig())
			if err != nil {
				return err
			}
			return c.Sys.WriteFile(c.L.KeptAnswers(), saved, 0o600)
		},
	}
}

func stopStep(unit string, user bool) Step {
	ctl := func(c *Ctx, args ...string) (string, error) {
		if user {
			return c.userctl(args...)
		}
		return c.systemctl(args...)
	}
	return Step{
		Name: "stop " + unit,
		Check: func(c *Ctx) (bool, string, error) {
			if user {
				if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
					return true, "no such account", nil
				}
			}
			active, _ := ctl(c, "is-active", unit)
			enabled, _ := ctl(c, "is-enabled", unit)
			if strings.TrimSpace(active) != "active" && strings.TrimSpace(enabled) != "enabled" {
				return true, "", nil
			}
			return false, "", nil
		},
		Apply: func(c *Ctx) error {
			_, _ = ctl(c, "stop", unit)
			_, err := ctl(c, "disable", unit)
			return err
		},
	}
}

func unlinkStep(unit string, user bool) Step {
	link := func(c *Ctx) string {
		if user {
			u, _ := c.Sys.Lookup(c.L.RunnerUser)
			return path.Join(u.Home, ".config", "systemd", "user", unit)
		}
		return path.Join(systemUnitDir, unit)
	}
	return Step{
		Name: "unlink " + unit,
		Check: func(c *Ctx) (bool, string, error) {
			if user {
				if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
					return true, "no such account", nil
				}
			}
			info := c.Sys.Stat(link(c))
			if !info.Exists {
				return true, "", nil
			}
			if !info.IsLink || info.Target != path.Join(c.L.UnitDir(), unit) {
				return true, link(c) + " is not this installation's: left alone", nil
			}
			return false, "", nil
		},
		Apply: func(c *Ctx) error {
			if err := c.Sys.Remove(link(c)); err != nil {
				return err
			}
			if user {
				_, err := c.userctl("daemon-reload")
				return err
			}
			_, err := c.systemctl("daemon-reload")
			return err
		},
	}
}

func dropinsStep() Step {
	files := func(c *Ctx) []string {
		u, _ := c.Sys.Lookup(c.L.RunnerUser)
		return []string{fmt.Sprintf("/etc/systemd/system/user@%d.service.d/delegate.conf", u.UID), fmt.Sprintf("/etc/systemd/system/user-%d.slice.d/cbe-limits.conf", u.UID)}
	}
	return Step{
		Name: "the runner's slice: delegation and limits",
		Check: func(c *Ctx) (bool, string, error) {
			if _, ok := c.Sys.Lookup(c.L.RunnerUser); !ok {
				return true, "no such account", nil
			}
			for _, file := range files(c) {
				if c.Sys.Stat(file).Exists {
					return false, "remove " + file, nil
				}
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			for _, file := range files(c) {
				if c.Sys.Stat(file).Exists {
					if err := c.Sys.Remove(file); err != nil {
						return err
					}
				}
			}
			_, err := c.systemctl("daemon-reload")
			return err
		},
	}
}

func containersStep() Step {
	return Step{
		Name: "the students' containers",
		Check: func(c *Ctx) (bool, string, error) {
			u, ok := c.Sys.Lookup(c.L.RunnerUser)
			if !ok {
				return true, "no such account", nil
			}
			out, err := c.Sys.RunAs(c.L.RunnerUser, c.userEnv(u.UID, u.Home), "podman", "ps", "-aq")
			if err != nil || strings.TrimSpace(out) == "" {
				return true, "", nil
			}
			return false, fmt.Sprintf("remove %d container(s) and what they hold", len(strings.Fields(out))), nil
		},
		Apply: func(c *Ctx) error {
			u, _ := c.Sys.Lookup(c.L.RunnerUser)
			_, err := c.Sys.RunAs(c.L.RunnerUser, c.userEnv(u.UID, u.Home), "podman", "rm", "-af")
			return err
		},
	}
}

func unmountStep() Step {
	return Step{
		Name: "unmount the students' volume",
		Check: func(c *Ctx) (bool, string, error) {
			if _, _, mounted := mountOf(c, c.L.WorkDir); !mounted {
				return true, "", nil
			}
			return false, "unmount " + c.L.WorkDir, nil
		},
		Apply: func(c *Ctx) error { _, err := c.Sys.Run("umount", c.L.WorkDir); return err },
	}
}

// fstabStep takes this installation's line out of /etc/fstab, and nothing else; the file is kept as it was in /etc/fstab.bak-cbe-removed.
func fstabStep() Step {
	ours := func(c *Ctx, line string) bool {
		fields := strings.Fields(line)
		return len(fields) >= 2 && fields[1] == c.L.WorkDir
	}
	return Step{
		Name: "the volume's line in /etc/fstab",
		Check: func(c *Ctx) (bool, string, error) {
			data, err := c.Sys.ReadFile("/etc/fstab")
			if err != nil {
				return true, "", nil
			}
			for _, line := range lines(string(data)) {
				if ours(c, line) {
					return false, "remove the line of " + c.L.WorkDir, nil
				}
			}
			return true, "", nil
		},
		Apply: func(c *Ctx) error {
			data, err := c.Sys.ReadFile("/etc/fstab")
			if err != nil {
				return err
			}
			if err := c.Sys.WriteFile("/etc/fstab.bak-cbe-removed", data, 0o644); err != nil {
				return err
			}
			var kept []string
			for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
				if !ours(c, line) {
					kept = append(kept, line)
				}
			}
			return c.Sys.WriteFile("/etc/fstab", []byte(strings.Join(kept, "\n")+"\n"), 0o644)
		},
	}
}

func removeFileStep(name string, file func(c *Ctx) string) Step {
	return Step{
		Name: "remove " + name,
		Check: func(c *Ctx) (bool, string, error) {
			if !c.Sys.Stat(file(c)).Exists {
				return true, "", nil
			}
			return false, file(c), nil
		},
		Apply: func(c *Ctx) error { return c.Sys.Remove(file(c)) },
	}
}

// removeTreeStep deletes the data folder. It will not delete a folder that is shallow or does not look like this installation's: the
// configuration is validated, but a delete is not left to that.
func removeTreeStep() Step {
	return Step{
		Name: "the data folder",
		Check: func(c *Ctx) (bool, string, error) {
			if !c.Sys.Stat(c.L.Data).Exists {
				return true, "", nil
			}
			if strings.Count(c.L.Data, "/") < 2 || !c.Sys.Stat(c.L.ServerDir()).Exists && !c.Sys.Stat(path.Join(c.L.Data, "work")).Exists && !c.Sys.Stat(path.Join(c.L.Data, "ipc")).Exists {
				return false, "", fmt.Errorf("%s does not look like the data of this installation (no server folder in it): it is not deleted", c.L.Data)
			}
			return false, "delete " + c.L.Data + " and everything in it", nil
		},
		Apply: func(c *Ctx) error {
			if _, _, mounted := mountOf(c, c.L.WorkDir); mounted {
				return fmt.Errorf("%s is still mounted: it is not deleted", c.L.WorkDir)
			}
			return c.Sys.RemoveAll(c.L.Data)
		},
	}
}

func accountsStep() Step {
	return Step{
		Name: "the accounts and the group",
		Check: func(c *Ctx) (bool, string, error) {
			var left []string
			for _, name := range []string{c.L.ServerUser, c.L.RunnerUser} {
				if _, ok := c.Sys.Lookup(name); ok && (name == c.L.ServerUser || c.L.Workspace) {
					left = append(left, name)
				}
			}
			if _, ok := c.Sys.LookupGroup(c.L.IPCGroup); ok && c.L.Workspace {
				left = append(left, "group "+c.L.IPCGroup)
			}
			if len(left) == 0 {
				return true, "", nil
			}
			return false, "remove " + strings.Join(left, ", "), nil
		},
		Apply: func(c *Ctx) error {
			if c.L.Workspace {
				if _, ok := c.Sys.Lookup(c.L.RunnerUser); ok {
					_, _ = c.Sys.Run("loginctl", "disable-linger", c.L.RunnerUser)
					if _, err := c.Sys.Run("userdel", "--remove", c.L.RunnerUser); err != nil {
						return err
					}
				}
			}
			if _, ok := c.Sys.Lookup(c.L.ServerUser); ok {
				if _, err := c.Sys.Run("userdel", c.L.ServerUser); err != nil {
					return err
				}
			}
			if _, ok := c.Sys.LookupGroup(c.L.IPCGroup); ok && c.L.Workspace {
				if _, err := c.Sys.Run("groupdel", c.L.IPCGroup); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// ----- the services as they are

// ServiceState is what Status says of one service.
type ServiceState struct {
	Unit            string
	Active, Enabled bool
}

// Status reads the state of the services.
func Status(c *Ctx) []ServiceState {
	state := func(unit string, user bool) ServiceState {
		ctl := c.systemctl
		if user {
			ctl = c.userctl
		}
		active, _ := ctl("is-active", unit)
		enabled, _ := ctl("is-enabled", unit)
		return ServiceState{Unit: unit, Active: strings.TrimSpace(active) == "active", Enabled: strings.TrimSpace(enabled) == "enabled"}
	}
	var out []ServiceState
	if c.L.Workspace {
		out = append(out, state(quotaUnit, false))
		runner := state(runnerUnit, true)
		runner.Unit = runnerUnit + " (user " + c.L.RunnerUser + ")"
		out = append(out, runner)
	}
	return append(out, state(serverUnit, false))
}

// RunningContainers is how many containers the runner's runtime has running, which is how many students are in their workspaces.
func RunningContainers(c *Ctx) int {
	u, ok := c.Sys.Lookup(c.L.RunnerUser)
	if !c.L.Workspace || !ok {
		return 0
	}
	out, err := c.Sys.RunAs(c.L.RunnerUser, c.userEnv(u.UID, u.Home), "podman", "ps", "-q")
	if err != nil {
		return 0
	}
	return len(strings.Fields(out))
}

// Restart restarts the services, so that they run what is written now (a new program, a changed unit, new settings). Students' containers
// are the runtime's, and go on; but the connections of the students are cut for a moment, so a restart with containers running is refused
// unless force is said.
func Restart(c *Ctx, force bool) error {
	if n := RunningContainers(c); n > 0 && !force {
		return fmt.Errorf("%d student workspace(s) are running: a restart cuts their connections for a moment (the containers and the files are safe, and the application reconnects). Say --force to do it now", n)
	}
	if _, err := c.systemctl("daemon-reload"); err != nil {
		return err
	}
	if c.L.Workspace {
		if _, err := c.systemctl("restart", quotaUnit); err != nil {
			return err
		}
		if _, err := c.userctl("daemon-reload"); err != nil {
			return err
		}
		if _, err := c.userctl("restart", runnerUnit); err != nil {
			return err
		}
	}
	_, err := c.systemctl("restart", serverUnit)
	return err
}
