package portalctl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestSampleConfigVersionAndUsage(t *testing.T) {
	f := machine()
	if code, s := command(f, "", false, "sample-config"); code != 0 || !strings.Contains(s.out.String(), "WORKSPACE_ENABLED=yes") {
		t.Errorf("sample: %d %s", code, s.out.String())
	}
	if code, s := command(f, "", false, "version"); code != 0 || s.out.String() != "cbe-portalctl v0.17.0\n" {
		t.Errorf("version: %d %q", code, s.out.String())
	}
	if code, s := command(f, "", false, "nope"); code != 2 || !strings.Contains(s.err.String(), `"nope" is not a command`) {
		t.Errorf("nope: %d %s", code, s.err.String())
	}
	if code, s := command(f, "", false); code != 0 || !strings.Contains(s.out.String(), "cbe-portalctl configure") {
		t.Errorf("usage: %d", code)
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
