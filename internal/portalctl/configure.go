package portalctl

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// ConfigurePlan is the steps that make a machine the server the layout describes, in the order they depend on each other.
func ConfigurePlan(l Layout) []Step {
	var steps []Step
	add := func(more ...Step) { steps = append(steps, more...) }

	add(dirStep("folder "+l.Install+"/etc", path.Join(l.Install, "etc"), rootUser, rootUser, 0o755))
	add(dirStep("folder "+l.Data, l.Data, rootUser, rootUser, 0o755))
	if l.Workspace {
		add(groupStep(l.IPCGroup))
	}
	add(userStep(l.ServerUser, path.Dir(l.Database), "CBE exam server", true))
	if l.Workspace {
		add(memberStep(l.ServerUser, l.IPCGroup))
		add(userStep(l.RunnerUser, "", "CBE runner (container host)", false))
		add(memberStep(l.RunnerUser, l.IPCGroup), subIDStep(l.RunnerUser))
	}
	for _, folder := range l.AllServerFolders() {
		add(dirStep("folder "+folder, folder, l.ServerUser, l.ServerUser, 0o700))
	}
	add(tokenStep())

	if l.Workspace {
		// The runner's folders. ipc is the one the exam server reaches through the group, by name: it cannot list it.
		add(dirStep("folder "+l.IPC(), l.IPC(), l.RunnerUser, l.IPCGroup, fs.ModeSetgid|0o710))
		for _, name := range []string{"run", "runner", "grading"} {
			add(dirStep("folder "+path.Join(l.Data, name), path.Join(l.Data, name), l.RunnerUser, l.RunnerUser, 0o700))
		}
		add(volumeStep())
		add(dirStep("folder "+l.WorkDir, l.WorkDir, rootUser, rootUser, 0o711))
		add(lingerStep(), delegationStep(), limitsStep(), managerStep(), podmanStep())
		if l.StorageDir != "" {
			add(storageStep())
		}
		add(runnerConfigStep())
		add(systemUnitStep("cbe-quota-helper.service", l.StartServices))
		add(userUnitStep("cbe-runner.service", l.StartServices))
	}
	add(systemUnitStep("cbe-server.service", l.StartServices))

	if l.StartServices {
		if l.Workspace {
			add(startSystemStep("cbe-quota-helper.service"), startUserStep("cbe-runner.service"))
		}
		add(startSystemStep("cbe-server.service"))
	}
	return steps
}

// Programs are the files of the installation that the services run: what the installer put in INSTALL_DIR/bin.
func (l Layout) Programs() []string {
	programs := []string{"cbe-portal-server"}
	if l.Workspace {
		programs = append(programs, "cbe-runner", "cbe-quota-helperd")
	}
	return programs
}

// Next is what a person does after the configuration: where the server is and how to reach the proctor's token.
func (l Layout) Next() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nThe exam server listens on %s.\n", l.Bind)
	fmt.Fprintf(&b, "The proctor's token is in %s (only root and %s can read it):\n  sudo cat %s\n", l.Token, l.ServerUser, l.Token)
	fmt.Fprintf(&b, "Give it to the proctor application as its token file (Server connection), with the server's address.\n")
	if !l.StartServices {
		fmt.Fprintf(&b, "The services are not started (START_SERVICES=no): systemctl start cbe-server")
		if l.Workspace {
			fmt.Fprintf(&b, " cbe-quota-helper, and as %s: systemctl --user start cbe-runner", l.RunnerUser)
		}
		fmt.Fprintln(&b, ".")
	}
	if l.Workspace {
		fmt.Fprintf(&b, "Workspace exams are on. An exam's image is built from its package (image/Containerfile) when it is published.\n")
	}
	return b.String()
}
