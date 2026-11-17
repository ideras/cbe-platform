package portalctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"exam-platform/internal/portalctl/conf"
	"exam-platform/internal/portalctl/sys"
)

func values(t *testing.T, text string) *conf.Values {
	t.Helper()
	v, err := conf.Parse(strings.NewReader(text), "test")
	if err != nil {
		t.Fatal(err)
	}
	if p := v.Validate(); !p.OK() {
		t.Fatalf("the configuration is not valid: %v", p.Errors)
	}
	return v
}

// machine is a machine the installer has been run on: the programs are in /opt/cbe-portal/bin.
func machine() *sys.Fake {
	f := sys.NewFake()
	for _, program := range []string{"cbe-portal-server", "cbe-runner", "cbe-quota-helperd", "cbe-agent"} {
		f.Install("/opt/cbe-portal/bin/"+program, "binary", 0o755)
	}
	return f
}

func run(t *testing.T, f *sys.Fake, v *conf.Values, opts Options) (Result, string, error) {
	t.Helper()
	var out bytes.Buffer
	c := NewCtx(f, v, &out)
	result, err := Run(c, ConfigurePlan(c.L), opts)
	return result, out.String(), err
}

const workspaceConfig = `WORKSPACE_ENABLED=yes
DATA_DIR=/data/cbe-platform
CREATE_XFS_VOLUME=yes
XFS_VOLUME_FILE=/data/cbe-platform/cbe-portal-work.img
XFS_VOLUME_SIZE=32G
`

// The simplest server: question exams only. It is a server account, its folders, a token, and one unit that is linked, enabled and started.
func TestAServerForQuestionExamsOnlyIsAnAccountFoldersATokenAndOneUnit(t *testing.T) {
	f := machine()
	v := values(t, "")
	result, out, err := run(t, f, v, Options{})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	server, ok := f.Lookup("cbe-server")
	if !ok {
		t.Fatal("the server's account was not made")
	}
	for _, dir := range []string{"/var/lib/cbe-portal/server", "/var/lib/cbe-portal/server/screenshots"} {
		if info := f.Stat(dir); !info.IsDir || info.UID != server.UID || info.Mode != 0o700 {
			t.Errorf("%s = %+v", dir, info)
		}
	}
	token := f.Text("/var/lib/cbe-portal/server/admin-token")
	if info := f.Stat("/var/lib/cbe-portal/server/admin-token"); info.Mode != 0o600 || info.UID != server.UID || len(strings.TrimSpace(token)) < 40 {
		t.Errorf("token: %+v %q", info, token)
	}
	unit := f.Text("/opt/cbe-portal/lib/systemd/cbe-server.service")
	for _, want := range []string{"User=cbe-server", "-bind 0.0.0.0:8080", "-database /var/lib/cbe-portal/server/cbe-platform.db", "ReadWritePaths=/var/lib/cbe-portal/server", "ExecStart=/opt/cbe-portal/bin/cbe-portal-server"} {
		if !strings.Contains(unit, want) {
			t.Errorf("the unit lacks %q:\n%s", want, unit)
		}
	}
	for _, not := range []string{"runner", "SupplementaryGroups", "workspace"} {
		if strings.Contains(unit, not) {
			t.Errorf("a server with no workspaces has %q in its unit", not)
		}
	}
	if link := f.Stat("/etc/systemd/system/cbe-server.service"); !link.IsLink || link.Target != "/opt/cbe-portal/lib/systemd/cbe-server.service" {
		t.Errorf("the unit is linked from /opt: %+v", link)
	}
	if !f.Enabled("cbe-server.service") || !f.Active("cbe-server.service") {
		t.Error("the server is enabled and started")
	}
	if _, found := f.Lookup("cbe-runner"); found || f.Stat("/data/cbe-platform").Exists {
		t.Error("nothing of the workspaces was made")
	}
	if result.Problems != 0 {
		t.Errorf("problems: %+v", result)
	}

	// Again: nothing is done, and the token is the same.
	again, out, err := run(t, f, v, Options{})
	if err != nil || again.Todo != 0 || again.Problems != 0 {
		t.Fatalf("a second run did %+v: %v\n%s", again, err, out)
	}
	if f.Text("/var/lib/cbe-portal/server/admin-token") != token {
		t.Error("a second run made a new token")
	}
}

// The whole of a workspace server, on a machine that has nothing: accounts, folders, a volume of XFS in a file, in fstab and mounted, the runner's
// limits, the runtime's socket, the settings, three services. And a second run that does nothing.
func TestAWorkspaceServerIsMadeFromNothingAndASecondRunChangesNothing(t *testing.T) {
	f := machine()
	v := values(t, workspaceConfig)
	result, out, err := run(t, f, v, Options{})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	runner, _ := f.Lookup("cbe-runner")
	server, _ := f.Lookup("cbe-server")
	if runner.UID == 0 || server.UID == 0 {
		t.Fatalf("accounts: runner %+v server %+v", runner, server)
	}
	if got := f.Members("cbe-ipc"); strings.Join(got, ",") != "cbe-runner,cbe-server" {
		t.Errorf("members of cbe-ipc = %v", got)
	}
	ipc, _ := f.LookupGroup("cbe-ipc")
	if info := f.Stat("/data/cbe-platform/ipc"); info.UID != runner.UID || info.GID != ipc || info.Mode != fs.ModeSetgid|0o710 {
		t.Errorf("ipc = %+v", info)
	}
	for _, name := range []string{"run", "runner", "grading"} {
		if info := f.Stat("/data/cbe-platform/" + name); info.UID != runner.UID || info.Mode != 0o700 {
			t.Errorf("%s = %+v", name, info)
		}
	}
	// The volume: a sparse file of the size asked, root-only, formatted, in fstab (once, with the file as it was kept) and mounted with quotas.
	if info := f.Stat("/data/cbe-platform/cbe-portal-work.img"); info.Mode != 0o600 || info.Size != 32<<30 || info.UID != 0 {
		t.Errorf("volume file = %+v", info)
	}
	if !strings.Contains(strings.Join(f.Calls, "\n"), "mkfs.xfs -q -L cbework /data/cbe-platform/cbe-portal-work.img") {
		t.Error("the volume was not formatted as XFS")
	}
	fstab := f.Text("/etc/fstab")
	if strings.Count(fstab, "/data/cbe-platform/work") != 1 || !strings.Contains(fstab, "xfs loop,prjquota,nofail,x-systemd.requires-mounts-for=/data/cbe-platform 0 0") {
		t.Errorf("fstab = %q", fstab)
	}
	if f.Text("/etc/fstab.bak-cbe") != "UUID=1234 / ext4 defaults 0 1\n" {
		t.Errorf("the old fstab was not kept: %q", f.Text("/etc/fstab.bak-cbe"))
	}
	if !f.Mounted("/data/cbe-platform/work") {
		t.Error("the volume is not mounted")
	}
	if info := f.Stat("/data/cbe-platform/work"); info.Mode != 0o711 || info.UID != 0 {
		t.Errorf("work = %+v", info)
	}
	// The runner's slice.
	delegate := f.Text("/etc/systemd/system/user@1000.service.d/delegate.conf")
	if !strings.Contains(delegate, "Delegate=cpu cpuset io memory pids") || !strings.Contains(delegate, "RequiresMountsFor=/data/cbe-platform/work") {
		t.Errorf("delegation = %q", delegate)
	}
	limits := f.Text("/etc/systemd/system/user-1000.slice.d/cbe-limits.conf")
	// 64000 MiB of memory: the budget is 70% (44800), and the slice may have that and 9216 MiB of room for what else runs in it.
	if !strings.Contains(limits, "MemoryMax=54016M") || !strings.Contains(limits, "TasksMax=12000") || !strings.Contains(limits, "CPUWeight=50") {
		t.Errorf("limits = %q", limits)
	}
	if !f.Active("user@1000.service") || f.Text("/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/cgroup.controllers") != "cpuset cpu io memory pids\n" {
		t.Error("the runner's user manager runs without the delegation")
	}
	// The runtime's socket for the runner, and the settings.
	if !f.Enabled("cbe-runner:podman.socket") || f.Stat("/run/user/1000/podman/podman.sock").Exists == false {
		t.Error("podman.socket is not on for the runner")
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(f.Text("/opt/cbe-portal/etc/runner.json")), &settings); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"socket": "/data/cbe-platform/ipc/runner.sock", "socket_group": "cbe-ipc", "secret_file": "/data/cbe-platform/ipc/runner.secret",
		"runtime_socket": "unix:///run/user/1000/podman/podman.sock", "work_root": "/data/cbe-platform/work", "run_root": "/data/cbe-platform/run",
		"state_root": "/data/cbe-platform/runner", "grade_root": "/data/cbe-platform/grading", "host_memory_mb": float64(0),
		"agent_binary": "/opt/cbe-portal/bin/cbe-agent", "custom_base": true,
	} {
		if settings[key] != want {
			t.Errorf("runner.json %s = %v, want %v", key, settings[key], want)
		}
	}
	if _, ok := settings["images"]; !ok {
		t.Error("the settings keep the images")
	}
	if registries, ok := settings["base_registries"].([]any); !ok || len(registries) != 0 {
		t.Errorf("base_registries is %v: with none, every registry is allowed, and it is written as []", settings["base_registries"])
	}
	// The three services.
	server1 := f.Text("/opt/cbe-portal/lib/systemd/cbe-server.service")
	for _, want := range []string{"SupplementaryGroups=cbe-ipc", "-runner-socket /data/cbe-platform/ipc/runner.sock", "-runner-secret-file /data/cbe-platform/ipc/runner.secret",
		"-workspace-dir /data/cbe-platform/server/workspace", "After=network-online.target cbe-quota-helper.service", "ReadOnlyPaths=/data/cbe-platform/ipc", "ReadWritePaths=/data/cbe-platform/server"} {
		if !strings.Contains(server1, want) {
			t.Errorf("the server's unit lacks %q:\n%s", want, server1)
		}
	}
	if q := f.Text("/opt/cbe-portal/lib/systemd/cbe-quota-helper.service"); !strings.Contains(q, "-volume /data/cbe-platform/work -state /data/cbe-platform -socket /run/cbe-quota/helper.sock -runner cbe-runner") || !strings.Contains(q, "RequiresMountsFor=/data/cbe-platform/work") {
		t.Errorf("quota helper unit:\n%s", q)
	}
	if r := f.Text("/opt/cbe-portal/lib/systemd/cbe-runner.service"); !strings.Contains(r, "ExecStart=/opt/cbe-portal/bin/cbe-runner -config /opt/cbe-portal/etc/runner.json") || !strings.Contains(r, "KillMode=process") {
		t.Errorf("runner unit:\n%s", r)
	}
	if link := f.Stat("/home/cbe-runner/.config/systemd/user/cbe-runner.service"); !link.IsLink || link.Target != "/opt/cbe-portal/lib/systemd/cbe-runner.service" {
		t.Errorf("the runner's unit link: %+v", link)
	}
	for _, unit := range []string{"cbe-quota-helper.service", "cbe-server.service", "cbe-runner:cbe-runner.service"} {
		if !f.Enabled(unit) || !f.Active(unit) {
			t.Errorf("%s: enabled %v, started %v", unit, f.Enabled(unit), f.Active(unit))
		}
	}
	if result.Problems != 0 {
		t.Errorf("problems: %+v", result)
	}

	token := f.Text("/data/cbe-platform/server/admin-token")
	again, out, err := run(t, f, v, Options{})
	if err != nil || again.Todo != 0 || again.Problems != 0 {
		t.Fatalf("a second run did %+v: %v\n%s", again, err, out)
	}
	if f.Text("/data/cbe-platform/server/admin-token") != token {
		t.Error("a second run made a new token")
	}
}

// A dry run says what would be done and does none of it.
func TestADryRunChangesNothing(t *testing.T) {
	f := machine()
	before := len(f.Calls)
	result, out, err := run(t, f, values(t, workspaceConfig), Options{DryRun: true})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if result.Todo < 10 || result.Problems != 0 {
		t.Errorf("a dry run of a machine with nothing: %+v\n%s", result, out)
	}
	for _, call := range f.Calls[before:] {
		for _, change := range []string{"useradd", "groupadd", "usermod", "mkfs", "mount", "truncate", "touch", "enable", "start", "link", "daemon-reload", "loginctl enable"} {
			if strings.HasPrefix(call, change) || strings.Contains(call, " "+change) && !strings.Contains(call, "is-") {
				t.Errorf("a dry run ran %q", call)
			}
		}
	}
	if _, found := f.Lookup("cbe-server"); found || f.Stat("/data/cbe-platform").Exists || f.Stat("/etc/systemd/system/cbe-server.service").Exists {
		t.Error("a dry run changed the machine")
	}
	for _, want := range []string{"todo     account cbe-server", "todo     the XFS volume for the students' folders (make /data/cbe-platform/cbe-portal-work.img (32G, XFS)"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan lacks %q:\n%s", want, out)
		}
	}
}

// What is on the machine that is not this configuration's is left alone, and said.
func TestAFolderThatBelongsToSomeoneElseIsNotChanged(t *testing.T) {
	f := machine()
	f.Install("/var/lib/cbe-portal/server/old.db", "x", 0o644)
	f.AddUser("alice", 1500, "/home/alice")
	_ = f.Chown("/var/lib/cbe-portal/server", 1500, 1500)
	_, out, err := run(t, f, values(t, ""), Options{})
	if err == nil || !strings.Contains(err.Error(), "belongs to 1500:1500, not to cbe-server:cbe-server") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if info := f.Stat("/var/lib/cbe-portal/server"); info.UID != 1500 {
		t.Errorf("the folder was taken from its owner: %+v", info)
	}
}

// A volume that is there is used as it is; one that is not there and not to be made is a problem that says what to do.
func TestAnExistingXFSMountIsUsedAndAMissingOneIsAProblem(t *testing.T) {
	f := machine()
	f.AddMount("/dev/loop3", "/data/cbe-platform/work", "xfs", "prjquota")
	v := values(t, "WORKSPACE_ENABLED=yes\nDATA_DIR=/data/cbe-platform\n")
	if _, out, err := run(t, f, v, Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, call := range f.Calls {
		if strings.HasPrefix(call, "mkfs") || strings.HasPrefix(call, "mount") || strings.HasPrefix(call, "truncate") {
			t.Errorf("an existing volume was touched: %s", call)
		}
	}
	if strings.Contains(f.Text("/etc/fstab"), "work") {
		t.Error("fstab was changed for a volume that was there")
	}

	g := machine()
	_, out, err := run(t, g, v, Options{})
	if err == nil || !strings.Contains(err.Error(), "set CREATE_XFS_VOLUME=yes") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	h := machine()
	h.AddMount("/dev/sda3", "/data/cbe-platform/work", "ext4", "rw")
	if _, _, err := run(t, h, v, Options{}); err == nil || !strings.Contains(err.Error(), "is ext4, not xfs") {
		t.Errorf("an ext4 mount: %v", err)
	}
	k := machine()
	k.AddMount("/dev/sda3", "/data/cbe-platform/work", "xfs", "rw")
	if _, _, err := run(t, k, v, Options{}); err == nil || !strings.Contains(err.Error(), "without project quotas") {
		t.Errorf("an xfs mount without quotas: %v", err)
	}
}

// btrfs would copy a loop image on every write: the file is made without copy-on-write, before it has any size.
func TestAVolumeFileOnBtrfsIsMadeWithoutCopyOnWrite(t *testing.T) {
	f := machine()
	f.FS["/data"] = "btrfs"
	if _, out, err := run(t, f, values(t, workspaceConfig), Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := strings.Join(f.Calls, "\n")
	touch, chattr, truncate := strings.Index(calls, "touch /data/cbe-platform/cbe-portal-work.img"), strings.Index(calls, "chattr +C /data/cbe-platform/cbe-portal-work.img"), strings.Index(calls, "truncate -s 32G")
	if touch < 0 || chattr < touch || truncate < chattr {
		t.Errorf("the order of touch, chattr and truncate is %d, %d, %d:\n%s", touch, chattr, truncate, calls)
	}
	g := machine()
	if _, _, err := run(t, g, values(t, workspaceConfig), Options{}); err != nil || strings.Contains(strings.Join(g.Calls, "\n"), "chattr") {
		t.Errorf("chattr is for btrfs only: %v", err)
	}
}

// Nothing is formatted that has something on it.
func TestNothingIsFormattedThatHasSomethingOnIt(t *testing.T) {
	v := values(t, workspaceConfig)
	// A file that is there with another file system.
	f := machine()
	f.Install("/data/cbe-platform/cbe-portal-work.img", "", 0o600)
	f.Install("/etc/x", "", 0o644)
	if _, err := f.Run("mkfs.xfs", "-q", "/data/cbe-platform/cbe-portal-work.img"); err != nil {
		t.Fatal(err)
	}
	g := machine()
	g.Install("/data/cbe-platform/cbe-portal-work.img", "something", 0o600)
	_, out, err := run(t, g, v, Options{})
	if err == nil || !strings.Contains(err.Error(), "has no file system") || strings.Contains(strings.Join(g.Calls, "\n"), "mkfs") {
		t.Errorf("a file with no signature that this run did not make: %v\n%s", err, out)
	}
	// A file that is XFS already is used, not formatted again.
	if _, out, err := run(t, f, v, Options{}); err != nil || strings.Count(strings.Join(f.Calls, "\n"), "mkfs.xfs") != 1 {
		t.Errorf("an XFS file is mounted as it is: %v\n%s", err, out)
	}
	if !f.Mounted("/data/cbe-platform/work") {
		t.Error("it was not mounted")
	}
}

// A device is formatted only when it was named and erasing it was said, never when it is mounted; and it is in fstab by its UUID.
func TestADeviceIsFormattedOnlyWhenItIsNamedAndNotMounted(t *testing.T) {
	v := values(t, "WORKSPACE_ENABLED=yes\nDATA_DIR=/data/cbe-platform\nCREATE_XFS_VOLUME=yes\nXFS_DEVICE=/dev/sdb1\nXFS_DEVICE_ERASE=yes\n")
	f := machine()
	f.Install("/dev/sdb1", "", 0o660)
	if _, out, err := run(t, f, v, Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(strings.Join(f.Calls, "\n"), "mkfs.xfs -q -f -L cbework /dev/sdb1") {
		t.Error("the device was not formatted")
	}
	if !strings.Contains(f.Text("/etc/fstab"), "UUID=uuid-of-sdb1 /data/cbe-platform/work xfs prjquota,nofail 0 0") {
		t.Errorf("fstab = %q", f.Text("/etc/fstab"))
	}
	g := machine()
	g.Install("/dev/sdb1", "", 0o660)
	g.AddMount("/dev/sdb1", "/mnt/data", "ext4", "rw")
	if _, _, err := run(t, g, v, Options{}); err == nil || !strings.Contains(err.Error(), "is mounted") || strings.Contains(strings.Join(g.Calls, "\n"), "mkfs") {
		t.Errorf("a mounted device: %v", err)
	}
}

// Whether an exam may bring a base image of its own, and from which registries, are decided by the configuration (ALLOW_CUSTOM_BASE, BASE_REGISTRIES): the
// runner's settings say it, and a file that said otherwise is put right.
func TestTheRunnersSettingsSayWhichBasesAnExamMayHave(t *testing.T) {
	f := machine()
	f.Install("/opt/cbe-portal/etc/runner.json", `{"custom_base": true, "base_registries": ["quay.io"], "agent_binary": "/somewhere/else"}`, 0o644)
	config := workspaceConfig + "ALLOW_CUSTOM_BASE=no\nBASE_REGISTRIES=docker.io, ghcr.io\n"
	if _, out, err := run(t, f, values(t, config), Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var settings struct {
		CustomBase     bool     `json:"custom_base"`
		BaseRegistries []string `json:"base_registries"`
		AgentBinary    string   `json:"agent_binary"`
	}
	if err := json.Unmarshal([]byte(f.Text("/opt/cbe-portal/etc/runner.json")), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.CustomBase || strings.Join(settings.BaseRegistries, ",") != "docker.io,ghcr.io" || settings.AgentBinary != "/opt/cbe-portal/bin/cbe-agent" {
		t.Errorf("the runner's settings: %+v", settings)
	}
	// A second run changes nothing.
	result, out, err := run(t, f, values(t, config), Options{DryRun: true})
	if err != nil || result.Todo != 0 || result.Problems != 0 {
		t.Errorf("a second run: %+v %v\n%s", result, err, out)
	}
}

// What an administrator changed in the runner's settings stays; what the configuration decides is put right.
func TestTheRunnersSettingsKeepWhatAnAdministratorChanged(t *testing.T) {
	f := machine()
	f.Install("/opt/cbe-portal/etc/runner.json", `{"max": {"cpu": 4, "memory_mb": 4096, "disk_mb": 800}, "images": {"cbe/python": "localhost/cbe/python:latest"}, "work_root": "/elsewhere", "host_memory_mb": 1}`, 0o644)
	if _, out, err := run(t, f, values(t, workspaceConfig+"HOST_MEMORY_MB=30000\n"), Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(f.Text("/opt/cbe-portal/etc/runner.json")), &settings); err != nil {
		t.Fatal(err)
	}
	if max := settings["max"].(map[string]any); max["cpu"] != float64(4) || max["disk_mb"] != float64(800) {
		t.Errorf("the limits were lost: %v", max)
	}
	if images := settings["images"].(map[string]any); images["cbe/python"] == nil {
		t.Errorf("the images were lost: %v", images)
	}
	if settings["work_root"] != "/data/cbe-platform/work" || settings["host_memory_mb"] != float64(30000) {
		t.Errorf("what the configuration decides was not put right: %v %v", settings["work_root"], settings["host_memory_mb"])
	}
	// And a settings file that is not JSON is a problem, never overwritten.
	g := machine()
	g.Install("/opt/cbe-portal/etc/runner.json", "{broken", 0o644)
	if _, _, err := run(t, g, values(t, workspaceConfig), Options{}); err == nil || !strings.Contains(err.Error(), "is not JSON") || g.Text("/opt/cbe-portal/etc/runner.json") != "{broken" {
		t.Errorf("a broken settings file: %v", err)
	}
}

// Units that the first deployment's scripts put in place are kept, aside, and replaced by links.
func TestAnOlderUnitIsKeptAsideAndLinked(t *testing.T) {
	f := machine()
	f.Install("/etc/systemd/system/cbe-quota-helper.service", "[Unit]\nDescription=old\n", 0o644)
	if _, out, err := run(t, f, values(t, workspaceConfig), Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if f.Text("/etc/systemd/system/cbe-quota-helper.service.before-cbe-portalctl") != "[Unit]\nDescription=old\n" {
		t.Error("the older unit was not kept")
	}
	if link := f.Stat("/etc/systemd/system/cbe-quota-helper.service"); !link.IsLink {
		t.Errorf("it is not a link now: %+v", link)
	}
}

// The spike's temporary cap is taken out of the slice's drop-ins and kept.
func TestTheSpikesCapIsRemovedAndKept(t *testing.T) {
	f := machine()
	f.AddUser("cbe-runner", 1001, "/home/cbe-runner")
	f.Install("/etc/systemd/system/user-1001.slice.d/spike-backstop.conf", "[Slice]\nCPUQuota=400%\n", 0o644)
	if _, out, err := run(t, f, values(t, workspaceConfig), Options{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if f.Stat("/etc/systemd/system/user-1001.slice.d/spike-backstop.conf").Exists || f.Text("/etc/systemd/system/user-1001.slice.d/spike-backstop.conf.removed") == "" {
		t.Error("the spike's cap is not removed and kept")
	}
}

// A user manager that has to be restarted for the limits, with containers running, is not restarted.
func TestARunningClassIsNeverStoppedByTheConfiguration(t *testing.T) {
	f := machine()
	f.AddUser("cbe-runner", 1001, "/home/cbe-runner")
	f.Install("/var/lib/systemd/linger/cbe-runner", "", 0o644)
	f.Install("/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/cgroup.controllers", "pids\n", 0o444)
	_, _ = f.Run("systemctl", "start", "user@1001.service")
	f.Install("/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/cgroup.controllers", "pids\n", 0o444)
	f.Containers = "abc\ndef\n"
	_, out, err := run(t, f, values(t, workspaceConfig), Options{})
	if err == nil || !strings.Contains(err.Error(), "stops the 2 container(s) that are running") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, call := range f.Calls {
		if call == "systemctl restart user@1001.service" {
			t.Error("the user manager was restarted under a running class")
		}
	}
}

// A failure stops the plan at its step, and says which.
func TestAFailureStopsThePlanAtItsStep(t *testing.T) {
	f := machine()
	f.Failures["useradd"] = errors.New("useradd: cannot lock /etc/passwd")
	_, out, err := run(t, f, values(t, ""), Options{})
	if err == nil || !strings.Contains(err.Error(), "account cbe-server") || !strings.Contains(out, "FAILED   account cbe-server") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if strings.Contains(out, "the proctor's token") {
		t.Error("the plan went on after the failure")
	}
}

// The checks that look at the machine before anything is changed.
func TestTheMachineIsCheckedBeforeAnythingIsDone(t *testing.T) {
	check := func(f *sys.Fake, config string) []Finding {
		return Doctor(NewCtx(f, values(t, config), &bytes.Buffer{}))
	}
	if findings := check(machine(), workspaceConfig); Failed(findings) {
		t.Errorf("a machine that has what is needed fails: %v", findings)
	}
	f := machine()
	f.Missing["podman"] = true
	f.Missing["mkfs.xfs"] = true
	f.Missing["newuidmap"] = true
	findings := check(f, workspaceConfig)
	text := ""
	for _, finding := range findings {
		text += finding.String() + "\n"
	}
	for _, want := range []string{"FAIL  podman", "FAIL  program newuidmap", "FAIL  program mkfs.xfs"} {
		if !strings.Contains(text, want) {
			t.Errorf("the check lacks %q:\n%s", want, text)
		}
	}
	// Question exams need none of it.
	g := machine()
	g.Missing["podman"] = true
	if findings := check(g, ""); Failed(findings) {
		t.Errorf("a server for question exams needs no runtime: %v", findings)
	}
	// The program files are there.
	h := sys.NewFake()
	if findings := check(h, ""); !Failed(findings) {
		t.Error("a machine without the program files passes")
	}
	// Room.
	k := machine()
	k.Free["/data"] = 1 << 30
	if findings := check(k, workspaceConfig); !Failed(findings) {
		t.Errorf("a volume file with 1 GiB of room passes: %v", findings)
	}
	m := machine()
	m.Free["/data"] = 20 << 30
	if findings := check(m, workspaceConfig); Failed(findings) || !strings.Contains(findingText(findings), "warn  room for the volume file") {
		t.Errorf("a 32G file with 20 GiB of room warns: %v", findings)
	}
}

func findingText(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}
