package portalctl

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/portalctl/conf"
	"exam-platform/internal/portalctl/sys"
)

type session struct {
	out, err bytes.Buffer
}

func command(f *sys.Fake, stdin string, interactive bool, args ...string) (int, *session) {
	s := &session{}
	code := Main(args, Env{Sys: f, Stdin: strings.NewReader(stdin), Stdout: &s.out, Stderr: &s.err, Interactive: interactive, Exe: "/opt/cbe-portal/bin/cbe-portalctl", Version: "v0.17.0"})
	return code, s
}

// Unattended: the file says everything, nothing is asked, the machine is configured and the answers are kept for the next run.
func TestAnUnattendedConfigurationNeedsNoQuestions(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig+"HOST_MEMORY_MB=30000\n", 0o644)
	code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	for _, want := range []string{"This machine:", "ok    podman", "What is to be done:", "Configuring:", "doing    the XFS volume", "The answers are saved in /opt/cbe-portal/etc/cbe-portal.conf", "The exam server listens on 0.0.0.0:8080", "sudo cat /data/cbe-platform/server/admin-token"} {
		if !strings.Contains(s.out.String(), want) {
			t.Errorf("the output lacks %q:\n%s", want, s.out.String())
		}
	}
	saved := f.Text("/opt/cbe-portal/etc/cbe-portal.conf")
	if !strings.Contains(saved, "WORKSPACE_ENABLED=yes") || !strings.Contains(saved, "HOST_MEMORY_MB=30000") || !strings.Contains(saved, "INSTALL_DIR=/opt/cbe-portal") {
		t.Errorf("the saved answers:\n%s", saved)
	}
	// With no file the saved answers are used, and there is nothing to do.
	code, s = command(f, "", false, "configure")
	if code != 0 || !strings.Contains(s.out.String(), "Nothing to do: this machine is configured as asked.") {
		t.Errorf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
}

// With no file at a terminal, a person is asked: the basic things, with the defaults in brackets; and what they say is what is done.
func TestWithNoFileAtATerminalThePersonIsAsked(t *testing.T) {
	f := machine()
	// DATA_DIR, WORKSPACE_ENABLED, BIND_ADDRESS, DATABASE_FILEPATH, START_SERVICES, CREATE_XFS_VOLUME, XFS_VOLUME_FILE, XFS_VOLUME_SIZE, then "go ahead".
	answers := "/data/cbe-platform\n\n127.0.0.1:9000\n\nno\n\n\n16G\ny\n"
	code, s := command(f, answers, true, "configure")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	out := s.out.String()
	for _, want := range []string{"DATA_DIR [/var/lib/cbe-portal]", "WORKSPACE_ENABLED [yes]", "BIND_ADDRESS [0.0.0.0:8080]", "CREATE_XFS_VOLUME [yes]", "XFS_VOLUME_SIZE [64G]", "Go ahead?"} {
		if !strings.Contains(out, want) {
			t.Errorf("the questions lack %q:\n%s", want, out)
		}
	}
	saved := f.Text("/opt/cbe-portal/etc/cbe-portal.conf")
	for _, want := range []string{"DATA_DIR=/data/cbe-platform", "BIND_ADDRESS=127.0.0.1:9000", "START_SERVICES=no", "XFS_VOLUME_SIZE=16G"} {
		if !strings.Contains(saved, want) {
			t.Errorf("the saved answers lack %q:\n%s", want, saved)
		}
	}
	if info := f.Stat("/data/cbe-platform/cbe-portal-work.img"); info.Size != 16<<30 {
		t.Errorf("the volume file: %+v", info)
	}
	if f.Active("cbe-server.service") || f.Enabled("cbe-server.service") {
		t.Error("START_SERVICES=no: the services are not started or enabled")
	}
	if !strings.Contains(out, "The services are not started (START_SERVICES=no)") {
		t.Errorf("a person is told how to start them:\n%s", out)
	}
}

// A wrong answer is said and asked again; saying no to the question at the end changes nothing.
func TestAWrongAnswerIsAskedAgainAndNoChangesNothing(t *testing.T) {
	f := machine()
	code, s := command(f, "/data/cbe\nmaybe\nno\n\n\n\nn\n", true, "configure")
	if code != 1 || !strings.Contains(s.out.String(), `"maybe" is not yes or no`) || !strings.Contains(s.out.String(), "Nothing was changed.") {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if _, found := f.Lookup("cbe-server"); found {
		t.Error("the machine was changed after a no")
	}
}

func TestWithNoFileAndNoTerminalItIsAnError(t *testing.T) {
	code, s := command(machine(), "", false, "configure")
	if code != 2 || !strings.Contains(s.err.String(), "there is no configuration: pass --config FILE") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
}

func TestAConfigurationThatIsWrongIsRefusedWithItsLines(t *testing.T) {
	f := machine()
	f.Install("/root/bad.conf", "DATA_DIR=/data/cbe\nWORKSPACE_ENABLD=yes\nBIND_ADDRESS=nope\n", 0o644)
	code, s := command(f, "", false, "configure", "--config", "/root/bad.conf", "--yes")
	if code != 2 || !strings.Contains(s.err.String(), "/root/bad.conf:2: WORKSPACE_ENABLD is not a setting (did you mean WORKSPACE_ENABLED?)") || !strings.Contains(s.err.String(), "/root/bad.conf:3: BIND_ADDRESS:") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
	f.Install("/root/cross.conf", "DATABASE_FILEPATH=/var/lib/cbe-portal/exams.db\n", 0o644)
	code, s = command(f, "", false, "configure", "--config", "/root/cross.conf", "--yes")
	if code != 2 || !strings.Contains(s.err.String(), "must be inside a folder of its own under DATA_DIR") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
	if _, found := f.Lookup("cbe-server"); found {
		t.Error("a configuration that is not valid changed the machine")
	}
}

func TestADryRunNeedsNoRootAndChangesNothing(t *testing.T) {
	f := machine()
	f.Root = false
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--dry-run")
	if code != 0 || !strings.Contains(s.out.String(), "Nothing was changed.") || !strings.Contains(s.out.String(), "todo     the XFS volume") {
		t.Errorf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if _, found := f.Lookup("cbe-server"); found || f.Stat("/opt/cbe-portal/etc/cbe-portal.conf").Exists {
		t.Error("a dry run changed the machine")
	}
	// A real run is not for a person who is not root.
	code, s = command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes")
	if code != 1 || !strings.Contains(s.err.String(), "run it as root") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
}

// The agent is what the runner puts in the images of exams that bring a base of their own: a machine without it cannot do that, and says so, unless
// the configuration does not allow such exams.
func TestDoctorNeedsTheAgentWhenExamsMayBringTheirOwnBase(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "doctor", "--config", "/root/cbe.conf"); strings.Contains(s.out.String(), "program cbe-agent") == false || code != 0 {
		t.Errorf("with the agent: exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	g := sys.NewFake()
	for _, program := range []string{"cbe-portal-server", "cbe-runner", "cbe-quota-helperd"} {
		g.Install("/opt/cbe-portal/bin/"+program, "binary", 0o755)
	}
	g.Install("/root/cbe.conf", workspaceConfig, 0o644)
	g.Install("/root/no-custom.conf", workspaceConfig+"ALLOW_CUSTOM_BASE=no\n", 0o644)
	code, s := command(g, "", false, "doctor", "--config", "/root/cbe.conf")
	if code == 0 || !strings.Contains(s.out.String(), "/opt/cbe-portal/bin/cbe-agent is missing") {
		t.Errorf("without the agent: exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if code, s := command(g, "", false, "doctor", "--config", "/root/no-custom.conf"); strings.Contains(s.out.String(), "cbe-agent") || code != 0 {
		t.Errorf("with custom bases off the agent is not needed: exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
}

func TestAMachineThatCannotBeConfiguredSaysWhy(t *testing.T) {
	f := machine()
	f.Missing["podman"] = true
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes")
	if code != 1 || !strings.Contains(s.out.String(), "FAIL  podman: it is not installed") || !strings.Contains(s.err.String(), "cannot be configured yet") {
		t.Errorf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if _, found := f.Lookup("cbe-server"); found {
		t.Error("the machine was changed although the check failed")
	}
}

func TestTheConfigurationMustBeWhereTheProgramIs(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", "INSTALL_DIR=/srv/cbe\n", 0o644)
	code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes")
	if code != 2 || !strings.Contains(s.err.String(), "INSTALL_DIR is /srv/cbe in /root/cbe.conf, and this program is installed in /opt/cbe-portal") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
}

// Removing the services leaves the data; removing the data is a thing that has to be said, twice.
func TestUnconfigureKeepsTheDataUnlessItIsToldNotTo(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	f.Install("/data/cbe-platform/server/cbe-platform.db", "exams", 0o600)

	code, s := command(f, "", false, "unconfigure", "--yes")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if !strings.Contains(s.out.String(), "The data stays") {
		t.Errorf("a person is told the data stays:\n%s", s.out.String())
	}
	for _, unit := range []string{"cbe-server.service", "cbe-quota-helper.service", "cbe-runner:cbe-runner.service"} {
		if f.Active(unit) || f.Enabled(unit) {
			t.Errorf("%s is still on", unit)
		}
	}
	for _, link := range []string{"/etc/systemd/system/cbe-server.service", "/etc/systemd/system/cbe-quota-helper.service", "/home/cbe-runner/.config/systemd/user/cbe-runner.service",
		"/etc/systemd/system/user@1000.service.d/delegate.conf", "/etc/systemd/system/user-1000.slice.d/cbe-limits.conf"} {
		if f.Stat(link).Exists {
			t.Errorf("%s is still there", link)
		}
	}
	if f.Text("/data/cbe-platform/server/cbe-platform.db") != "exams" || !f.Mounted("/data/cbe-platform/work") || !f.Stat("/data/cbe-platform/cbe-portal-work.img").Exists ||
		!strings.Contains(f.Text("/etc/fstab"), "/data/cbe-platform/work") {
		t.Error("the data, the volume or its fstab line were touched")
	}
	if _, found := f.Lookup("cbe-server"); !found {
		t.Error("the accounts were removed with the services")
	}
	if kept := f.Text("/data/cbe-platform/cbe-portal.conf.last"); kept == "" || kept != f.Text("/opt/cbe-portal/etc/cbe-portal.conf") {
		t.Errorf("the answers were not kept with the data: %q", kept)
	}

	// A person at a terminal who does not type the word deletes nothing.
	code, s = command(f, "no\n", true, "unconfigure", "--remove-data")
	if code != 1 || !f.Stat("/data/cbe-platform/server/cbe-platform.db").Exists || !strings.Contains(s.out.String(), "ALL OF THE DATA OF THIS SERVER IS TO BE DELETED") {
		t.Errorf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	// Without --yes and without a terminal, nothing is deleted either.
	if code, _ = command(f, "", false, "unconfigure", "--remove-data"); code != 1 || !f.Stat("/data/cbe-platform/server/cbe-platform.db").Exists {
		t.Errorf("exit %d without a yes", code)
	}
	// Said: everything goes, the volume is unmounted and out of fstab (the rest of fstab is kept).
	f.Containers = "abc\n"
	code, s = command(f, "remove-data\n", true, "unconfigure", "--remove-data")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if f.Stat("/data/cbe-platform").Exists || f.Mounted("/data/cbe-platform/work") {
		t.Error("the data is still there")
	}
	if fstab := f.Text("/etc/fstab"); strings.Contains(fstab, "cbe-platform") || !strings.Contains(fstab, "UUID=1234 / ext4") {
		t.Errorf("fstab = %q", fstab)
	}
	if _, found := f.Lookup("cbe-server"); found {
		t.Error("the server's account is still there")
	}
	if _, found := f.Lookup("cbe-runner"); found || f.Containers != "" {
		t.Error("the runner's account or containers are still there")
	}
	if _, found := f.LookupGroup("cbe-ipc"); found {
		t.Error("the group is still there")
	}
}

// The delete will not go near a folder that is not this installation's data.
func TestTheDataIsNotDeletedWhenItDoesNotLookLikeTheDataOfThisInstallation(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", "DATA_DIR=/srv/data\n", 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	_ = f.RemoveAll("/srv/data/server")
	f.Install("/srv/data/precious", "x", 0o644)
	code, s := command(f, "", false, "unconfigure", "--remove-data", "--yes")
	if code != 1 || !f.Stat("/srv/data/precious").Exists || !strings.Contains(s.out.String()+s.err.String(), "does not look like the data of this installation") {
		t.Errorf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
}

func TestStatusAndRestart(t *testing.T) {
	f := machine()
	if code, _ := command(f, "", false, "status"); code != 1 {
		t.Error("a machine that was never configured has no status")
	}
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	code, s := command(f, "", false, "status")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	for _, want := range []string{"server    http://0.0.0.0:8080", "cbe-server.service", "running, at boot", "students' volume", "xfs, project quotas", "student workspaces running                   0"} {
		if !strings.Contains(s.out.String(), want) {
			t.Errorf("the status lacks %q:\n%s", want, s.out.String())
		}
	}
	// Students are working: a restart waits for a yes.
	f.Containers = "a\nb\nc\n"
	code, s = command(f, "", false, "restart")
	if code != 1 || !strings.Contains(s.err.String(), "3 student workspace(s) are running") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
	before := len(f.Calls)
	if code, s = command(f, "", false, "restart", "--force"); code != 0 {
		t.Fatalf("exit %d: %s", code, s.err.String())
	}
	restarted := strings.Join(f.Calls[before:], "\n")
	for _, want := range []string{"systemctl restart cbe-quota-helper.service", "[cbe-runner] systemctl --user restart cbe-runner.service", "systemctl restart cbe-server.service"} {
		if !strings.Contains(restarted, want) {
			t.Errorf("the restart lacks %q:\n%s", want, restarted)
		}
	}
}

// The runner is a user service of its own account, and only root can ask that account (runuser): without root its state and the student
// workspaces are unknown, and are never said to be stopped or 0 (found on a real server where status, run without sudo, said the runner
// that was running was stopped).
func TestStatusWithoutRootSaysTheRunnerIsUnknownNotStopped(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	f.Containers = "a\nb\n" // students are working: a count of 0 would be a lie
	f.Root = false
	before := len(f.Calls)
	code, s := command(f, "", false, "status")
	out := s.out.String()
	if code != 1 {
		t.Errorf("exit %d: a state that could not be read is not \"everything runs\"", code)
	}
	for _, want := range []string{
		"cbe-runner.service (user cbe-runner)         unknown (needs root)\n",
		"student workspaces running                   unknown (needs root)\n",
		"cbe-server.service                           running, at boot\n",
		"cbe-quota-helper.service                     running, at boot\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "stopped") || strings.Contains(out, "running                   0") {
		t.Errorf("a state that could not be read is shown as stopped or 0:\n%s", out)
	}
	if !strings.Contains(s.err.String(), `run "sudo cbe-portalctl status"`) {
		t.Errorf("a person is not told how to see the runner: %s", s.err.String())
	}
	for _, call := range f.Calls[before:] {
		if strings.HasPrefix(call, "[cbe-runner]") {
			t.Errorf("as another user, status tried to speak as the runner's account: %s", call)
		}
	}
	// As root it is all there.
	f.Root = true
	code, s = command(f, "", false, "status")
	if code != 0 || !strings.Contains(s.out.String(), "cbe-runner.service (user cbe-runner)         running, at boot") || !strings.Contains(s.out.String(), "student workspaces running                   2") || s.err.Len() != 0 {
		t.Errorf("as root: exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
}

// As root, a question that fails (the runner's user manager does not answer, the runtime does not) is said with its reason; a service that
// is stopped is still said to be stopped.
func TestStatusSaysWhyAStateCouldNotBeRead(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	f.Failures["systemctl --user is-active"] = errors.New("Failed to connect to bus: No such file or directory")
	f.Failures["podman ps"] = errors.New("podman: cannot open the storage")
	if _, err := f.Run("systemctl", "stop", "cbe-server.service"); err != nil {
		t.Fatal(err)
	}
	code, s := command(f, "", false, "status")
	out := s.out.String()
	for _, want := range []string{
		"cbe-runner.service (user cbe-runner)         unknown: Failed to connect to bus",
		"student workspaces running                   unknown: podman: cannot open the storage",
		"cbe-server.service                           stopped, at boot",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the status lacks %q:\n%s", want, out)
		}
	}
	if code != 1 || strings.Contains(s.err.String(), "sudo") {
		t.Errorf("exit %d; as root there is nothing to say about sudo: %s", code, s.err.String())
	}
}

// Restart protects the students' connections: when the workspaces cannot be counted, it does not take that as "nobody is working".
func TestRestartRefusesWhenTheWorkspacesCannotBeCounted(t *testing.T) {
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	f.Failures["podman ps"] = errors.New("podman: cannot open the storage")
	before := len(f.Calls)
	code, s := command(f, "", false, "restart")
	if code != 1 || !strings.Contains(s.err.String(), "the student workspaces could not be counted (podman: cannot open the storage)") || !strings.Contains(s.err.String(), "--force") {
		t.Errorf("exit %d: %s", code, s.err.String())
	}
	if strings.Contains(strings.Join(f.Calls[before:], "\n"), "restart") {
		t.Errorf("a service was restarted: %v", f.Calls[before:])
	}
	if code, s = command(f, "", false, "restart", "--force"); code != 0 {
		t.Errorf("--force: exit %d %s", code, s.err.String())
	}
}

func TestSampleConfigVersionAndUsage(t *testing.T) {
	f := machine()
	if code, s := command(f, "", false, "sample-config"); code != 0 || !strings.Contains(s.out.String(), "WORKSPACE_ENABLED=yes") {
		t.Errorf("sample: %d %s", code, s.out.String())
	}
	if code, s := command(f, "", false, "version"); code != 0 || s.out.String() != "cbe-portalctl v0.17.0\n" {
		t.Errorf("version: %d %q", code, s.out.String())
	}
	if code, s := command(f, "", false, "nope"); code != 2 || !strings.Contains(s.err.String(), `"nope" is not a command`) || !strings.Contains(s.err.String(), `Run "cbe-portalctl help"`) {
		t.Errorf("nope: %d %s", code, s.err.String())
	}
	// A slip of the keyboard is answered with the command that was meant.
	if code, s := command(f, "", false, "stauts"); code != 2 || !strings.Contains(s.err.String(), "(did you mean status?)") {
		t.Errorf("stauts: %d %s", code, s.err.String())
	}
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		code, s := command(f, "", false, args...)
		if code != 0 || s.err.Len() != 0 {
			t.Errorf("%v: exit %d %s", args, code, s.err.String())
		}
		for _, name := range commandNames() {
			if !strings.Contains(s.out.String(), "  "+name+" ") {
				t.Errorf("%v: the list of commands lacks %s:\n%s", args, name, s.out.String())
			}
		}
		for _, want := range []string{"Usage: cbe-portalctl COMMAND [OPTIONS]", "Run as root (sudo):", "Exit status:", `"cbe-portalctl help COMMAND"`} {
			if !strings.Contains(s.out.String(), want) {
				t.Errorf("%v: the overview lacks %q:\n%s", args, want, s.out.String())
			}
		}
	}
}

// Every command in the list has its help, the same by "help COMMAND" and by "COMMAND --help": how it is called, what it does, and its
// options with what they take and their defaults. Asking for help does nothing else, even for a command that changes the machine.
func TestEveryCommandHasItsHelp(t *testing.T) {
	for _, name := range commandNames() {
		if name == "help" {
			continue
		}
		f := machine()
		before := len(f.Calls)
		code, viaHelp := command(f, "", false, append([]string{"help"}, strings.Fields(name)...)...)
		if code != 0 || viaHelp.err.Len() != 0 {
			t.Errorf("help %s: exit %d %s", name, code, viaHelp.err.String())
		}
		if len(f.Calls) != before {
			t.Errorf("help %s ran programs: %v", name, f.Calls[before:])
		}
		code, viaFlag := command(f, "", false, append(strings.Fields(name), "--help")...)
		if code != 0 || viaFlag.out.String() != viaHelp.out.String() {
			t.Errorf("%s --help: exit %d, and it is not what help %s says:\n%s", name, code, name, viaFlag.out.String())
		}
		h, _ := helpFor(name)
		if out := viaHelp.out.String(); !strings.HasPrefix(out, usage("Usage: ", h)+"\n") {
			t.Errorf("help %s does not start with its usage line:\n%s", name, out)
		}
	}
	_, s := command(machine(), "", false, "help", "configure")
	for _, want := range []string{"Options:\n  --config FILE ", "the answers the last run saved", "--dry-run ", "Run it as root, unless with --dry-run.", "Examples:\n  cbe-portalctl sample-config > cbe.conf"} {
		if !strings.Contains(s.out.String(), want) {
			t.Errorf("help configure lacks %q:\n%s", want, s.out.String())
		}
	}
	_, s = command(machine(), "", false, "help", "image", "check")
	// The default agent is a path of the computer the check runs on (image check runs on an author's Windows or macOS too): its separators are that system's.
	agent := "(default: " + filepath.Join("/opt/cbe-portal", "bin", "cbe-agent") + ")"
	for _, want := range []string{"--podman COMMAND", "(default: podman)", agent, "--keep "} {
		if !strings.Contains(s.out.String(), want) {
			t.Errorf("help image check lacks %q:\n%s", want, s.out.String())
		}
	}
	_, s = command(machine(), "", false, "help", "status")
	if !strings.Contains(s.out.String(), "sudo journalctl _SYSTEMD_USER_UNIT=cbe-runner.service") || strings.Contains(s.out.String(), "Options:") {
		t.Errorf("help status says where the logs are, and has no options:\n%s", s.out.String())
	}
	if code, s := command(machine(), "", false, "help", "nope"); code != 2 || !strings.Contains(s.err.String(), `"nope" is not a command`) {
		t.Errorf("help nope: exit %d %s", code, s.err.String())
	}
}

// The help fits in a terminal of 80 columns, so that no line of it is broken in the middle of a word.
func TestTheHelpFitsIn80Columns(t *testing.T) {
	outputs := map[string]string{}
	_, s := command(machine(), "", false, "help")
	outputs["help"] = s.out.String()
	_, s = command(machine(), "", false, "image", "--help")
	outputs["image --help"] = s.out.String()
	for _, name := range commandNames() {
		_, s := command(machine(), "", false, append([]string{"help"}, strings.Fields(name)...)...)
		outputs["help "+name] = s.out.String()
	}
	for which, out := range outputs {
		for _, line := range strings.Split(out, "\n") {
			if len(line) > helpWidth {
				t.Errorf("%s: a line of %d columns:\n%s", which, len(line), line)
			}
		}
	}
}

// A wrong option or operand is said, with how the command is called and where its help is, and changes nothing (exit status 2).
func TestAWrongOptionSaysHowTheCommandIsCalled(t *testing.T) {
	for _, args := range [][]string{{"status", "extra"}, {"configure", "--bogus"}, {"restart", "now"}, {"version", "-x"}} {
		f := machine()
		code, s := command(f, "", false, args...)
		h, _ := helpFor(args[0])
		if code != 2 || !strings.Contains(s.err.String(), usage("usage: ", h)) || !strings.Contains(s.err.String(), `Run "cbe-portalctl help `+args[0]+`"`) || s.out.Len() != 0 {
			t.Errorf("%v: exit %d\nout: %s\nerr: %s", args, code, s.out.String(), s.err.String())
		}
	}
	// The flag package's words are put as the help puts the options.
	if _, s := command(machine(), "", false, "restart", "-bogus"); !strings.Contains(s.err.String(), "cbe-portalctl restart: --bogus is not an option\n") || strings.Contains(s.err.String(), "flag provided") {
		t.Errorf("restart -bogus: %s", s.err.String())
	}
	if _, s := command(machine(), "", false, "doctor", "--config"); !strings.Contains(s.err.String(), "cbe-portalctl doctor: --config needs a value\n") {
		t.Errorf("doctor --config: %s", s.err.String())
	}
	// What follows -- is an operand, even when it starts with a dash.
	code, s := command(machine(), "", false, "image", "check", "--", "--keep")
	if code != 1 || !strings.Contains(s.err.String(), "--keep") {
		t.Errorf("image check -- --keep: exit %d %s", code, s.err.String())
	}
}

// The guide says what every key is for: a key added without a line in it is a key nobody knows about.
func TestTheGuideListsEveryKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "deployment-linux.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range conf.Specs {
		if !strings.Contains(string(data), "| `"+spec.Name+"` |") {
			t.Errorf("docs/deployment-linux.md has no row for %s", spec.Name)
		}
	}
}

// Lingering keeps the runner's user manager running after its services are stopped, and userdel refuses an account that has a process (found on the first
// real removal of the data): the manager is ended first, and a process that is slow to go is ended and the deletion tried again.
func TestRemovingTheDataEndsTheRunnersUserManagerBeforeDeletingTheAccount(t *testing.T) {
	pause = func(time.Duration) {}
	f := machine()
	f.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(f, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	// The user manager of the runner is up (lingering started it): the simulated userdel refuses the account while it is.
	if _, err := f.Run("userdel", "--remove", "cbe-runner"); err == nil || !strings.Contains(err.Error(), "currently used by process") {
		t.Fatalf("the simulated machine does not refuse an account that has a process: %v", err)
	}
	before := len(f.Calls)
	code, s := command(f, "", false, "unconfigure", "--remove-data", "--yes")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
	calls := strings.Join(f.Calls[before:], "\n")
	terminate, stop, userdel := strings.Index(calls, "loginctl terminate-user cbe-runner"), strings.Index(calls, "systemctl stop user@1000.service"), strings.Index(calls, "userdel --remove cbe-runner")
	if terminate < 0 || stop < terminate || userdel < stop {
		t.Errorf("the user manager is ended (%d, %d) before the account is deleted (%d):\n%s", terminate, stop, userdel, calls)
	}
	if _, found := f.Lookup("cbe-runner"); found {
		t.Error("the runner's account is still there")
	}

	// Stubborn: the manager cannot be told to stop, and a process stays until it is killed.
	g := machine()
	g.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(g, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	g.Failures["loginctl terminate-user"] = errors.New("loginctl: failed")
	g.Failures["systemctl stop user@"] = errors.New("systemctl: failed")
	code, s = command(g, "", false, "unconfigure", "--remove-data", "--yes")
	if code != 0 || !strings.Contains(strings.Join(g.Calls, "\n"), "pkill -KILL -u cbe-runner") {
		t.Fatalf("exit %d: a process that stays is ended\n%s\n%s", code, s.out.String(), s.err.String())
	}
	if _, found := g.Lookup("cbe-runner"); found {
		t.Error("the runner's account is still there after the process was ended")
	}

	// Another reason for userdel to refuse is not retried: it is the error.
	h := machine()
	h.Install("/root/cbe.conf", workspaceConfig, 0o644)
	if code, s := command(h, "", false, "configure", "--config", "/root/cbe.conf", "--yes"); code != 0 {
		t.Fatalf("%s\n%s", s.out.String(), s.err.String())
	}
	h.Failures["userdel"] = errors.New("userdel: cannot lock /etc/passwd")
	code, s = command(h, "", false, "unconfigure", "--remove-data", "--yes")
	if code != 1 || !strings.Contains(s.err.String(), "cannot lock /etc/passwd") || strings.Contains(strings.Join(h.Calls, "\n"), "pkill") {
		t.Errorf("exit %d\n%s\n%s", code, s.out.String(), s.err.String())
	}
}
