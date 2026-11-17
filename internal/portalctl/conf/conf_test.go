package conf

import (
	"strings"
	"testing"
)

func parse(t *testing.T, text string) *Values {
	t.Helper()
	v, err := Parse(strings.NewReader(text), "test.conf")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

// A key left out has its default, and a default may refer to other keys, so moving DATA_DIR moves what is under it.
func TestRegistriesAreAListOfRegistriesInTheirNormalForm(t *testing.T) {
	spec := Spec{Kind: Registries, Optional: true}
	for in, want := range map[string]string{"docker.io": "docker.io", "docker.io, ghcr.io": "docker.io,ghcr.io", "docker.io,ghcr.io  localhost:5000": "docker.io,ghcr.io,localhost:5000", "": ""} {
		if got, err := normalize(spec, in); err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"Docker.io", "docker.io/library", "https://docker.io", "docker.io:", "-x", "a b$"} {
		if _, err := normalize(spec, bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	// In a file: the keys are known, and a bad registry names its line.
	if v, err := Parse(strings.NewReader("ALLOW_CUSTOM_BASE=no\nBASE_REGISTRIES=docker.io,ghcr.io\n"), "f"); err != nil || v.Bool("ALLOW_CUSTOM_BASE") || v.Get("BASE_REGISTRIES") != "docker.io,ghcr.io" {
		t.Errorf("%v", err)
	}
	if _, err := Parse(strings.NewReader("BASE_REGISTRIES=Docker.io\n"), "f"); err == nil || !strings.Contains(err.Error(), "f:1: BASE_REGISTRIES") {
		t.Errorf("a bad registry: %v", err)
	}
	if New().Bool("ALLOW_CUSTOM_BASE") != true || New().Get("BASE_REGISTRIES") != "" {
		t.Error("the defaults: custom bases allowed, any registry")
	}
}

func TestDefaultsFollowTheKeysTheyRefer(t *testing.T) {
	v := New()
	if got := v.Get("DATABASE_FILEPATH"); got != "/var/lib/cbe-portal/server/cbe-platform.db" {
		t.Errorf("database = %s", got)
	}
	_ = v.Set("DATA_DIR", "/data/cbe-platform")
	if got := v.Get("DATABASE_FILEPATH"); got != "/data/cbe-platform/server/cbe-platform.db" {
		t.Errorf("database after DATA_DIR moved = %s", got)
	}
	if got := v.Get("XFS_VOLUME_FILE"); got != "/data/cbe-platform/cbe-portal-work.img" {
		t.Errorf("volume file = %s", got)
	}
	if v.Bool("WORKSPACE_ENABLED") || !v.Bool("START_SERVICES") || v.Int("HOST_MEMORY_MB") != 0 || v.Bytes("XFS_VOLUME_SIZE") != 64<<30 {
		t.Errorf("defaults: workspace %v start %v memory %d size %d", v.Bool("WORKSPACE_ENABLED"), v.Bool("START_SERVICES"), v.Int("HOST_MEMORY_MB"), v.Bytes("XFS_VOLUME_SIZE"))
	}
}

// The file is read, not run: comments, quotes, references and blank lines; and nothing else.
func TestAFileIsReadLineByLine(t *testing.T) {
	v := parse(t, `
# a comment
DATA_DIR = /data/cbe-platform        # where it all is
WORKSPACE_ENABLED=Yes
DATABASE_FILEPATH="${DATA_DIR}/db/exams.db"
BIND_ADDRESS='127.0.0.1:9090'
SERVER_EXTRA_ARGS=-workspace-retention=48h   -workspace-grade-parallel=4
HOST_MEMORY_MB=20000
XFS_VOLUME_SIZE=100g
`)
	for key, want := range map[string]string{
		"DATA_DIR": "/data/cbe-platform", "WORKSPACE_ENABLED": "yes", "DATABASE_FILEPATH": "/data/cbe-platform/db/exams.db", "BIND_ADDRESS": "127.0.0.1:9090",
		"SERVER_EXTRA_ARGS": "-workspace-retention=48h -workspace-grade-parallel=4", "HOST_MEMORY_MB": "20000", "XFS_VOLUME_SIZE": "100G",
	} {
		if got := v.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if !v.IsSet("DATA_DIR") || v.IsSet("SERVER_USER") {
		t.Error("IsSet says what was given")
	}
}

// Every problem is reported with its line, and a key that does not exist is named with the nearest that does.
func TestEveryProblemIsReportedWithItsLine(t *testing.T) {
	_, err := Parse(strings.NewReader(`DATA_DIR=relative/path
WORKSPACE_ENABLD=yes
this is not a setting
START_SERVICES=maybe
BIND_ADDRESS=localhost
HOST_MEMORY_MB=-5
SERVER_USER=first
XFS_VOLUME_SIZE=10
SERVER_USER=Root
SERVER_EXTRA_ARGS=-a;rm -rf /
DATABASE_FILEPATH="/a b"
BIND_ADDRESS='1.2.3.4:99999'
SCREENSHOTS_DIR='/unclosed
`), "cfg")
	if err == nil {
		t.Fatal("a file full of mistakes was taken")
	}
	text := err.Error()
	for _, want := range []string{
		"cfg:1: DATA_DIR: ", "cfg:2: WORKSPACE_ENABLD is not a setting (did you mean WORKSPACE_ENABLED?)", `cfg:3: "this is not a setting" is not KEY=VALUE`,
		"cfg:4: START_SERVICES:", "cfg:5: BIND_ADDRESS:", "cfg:6: HOST_MEMORY_MB:", "cfg:8: XFS_VOLUME_SIZE:", "cfg:9: SERVER_USER is given twice",
		"cfg:10: SERVER_EXTRA_ARGS:", "cfg:11: DATABASE_FILEPATH:", "cfg:12: BIND_ADDRESS:", "cfg:13: SCREENSHOTS_DIR: the quote is not closed",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
}

// Paths go into systemd units, which do not quote: a path that would need it, or climb, is refused.
func TestPathsAreSafeForUnitFiles(t *testing.T) {
	for _, bad := range []string{"", "/", "relative", "/a b", "/a\tb", "/a/../b", "/a;b", "/a$b", "/a'b", "/a\\b", "/é"} {
		if _, err := normalize(Spec{Kind: Path}, bad); err == nil {
			t.Errorf("%q was taken as a path", bad)
		}
	}
	if got, err := normalize(Spec{Kind: Path}, "/data//cbe-platform/./x/"); err != nil || got != "/data/cbe-platform/x" {
		t.Errorf("a path is cleaned: %q, %v", got, err)
	}
}

func TestSizesAndAddresses(t *testing.T) {
	for text, want := range map[string]int64{"64G": 64 << 30, "512m": 512 << 20, "2T": 2 << 40} {
		if got, err := parseSize(text); err != nil || got != want {
			t.Errorf("%s = %d, %v", text, got, err)
		}
	}
	for _, bad := range []string{"", "G", "64", "0G", "-1G", "1.5G", "64K", "64GB"} {
		if _, err := parseSize(bad); err == nil {
			t.Errorf("%q was taken as a size", bad)
		}
	}
	for _, good := range []string{"0.0.0.0:8080", ":8080", "127.0.0.1:1", "[::1]:8080", "exams.local:443"} {
		if _, err := normalize(Spec{Kind: Address}, good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"8080", "host", "host:0", "host:70000", "ho st:80", "host:x"} {
		if _, err := normalize(Spec{Kind: Address}, bad); err == nil {
			t.Errorf("%q was taken as an address", bad)
		}
	}
}

func problems(t *testing.T, text string) Problems {
	t.Helper()
	return parse(t, text).Validate()
}

// The cross-checks: the folders of the server and of the runner are kept apart, the program is not inside the data, and nothing is
// formatted without being asked.
func TestTheKeysAreCheckedAgainstEachOther(t *testing.T) {
	if p := problems(t, ""); !p.OK() {
		t.Errorf("the defaults are not valid: %v", p.Errors)
	}
	if p := problems(t, "WORKSPACE_ENABLED=yes\nCREATE_XFS_VOLUME=yes\nDATA_DIR=/data/cbe-platform\nXFS_VOLUME_FILE=/data/cbe-platform/work.img\n"); !p.OK() {
		t.Errorf("the owner's layout: %v", p.Errors)
	}
	for name, test := range map[string]struct{ config, want string }{
		"database in DATA_DIR itself": {"DATA_DIR=/data/cbe-platform\nDATABASE_FILEPATH=/data/cbe-platform/cbe-platform.db\n", "DATABASE_FILEPATH must be inside a folder of its own under DATA_DIR, for example /data/cbe-platform/server/cbe-platform.db"},
		"database outside":            {"DATABASE_FILEPATH=/srv/exams.db\n", "DATABASE_FILEPATH must be inside a folder of its own under DATA_DIR"},
		"database in the runner's":    {"WORKSPACE_ENABLED=yes\nDATABASE_FILEPATH=/var/lib/cbe-portal/ipc/exams.db\n", "is inside /var/lib/cbe-portal/ipc, which is not the server's"},
		"data and install nested":     {"DATA_DIR=/opt/cbe-portal/data\n", "DATA_DIR and INSTALL_DIR cannot be inside each other"},
		"install inside data":         {"INSTALL_DIR=/var/lib/cbe-portal/program\n", "DATA_DIR and INSTALL_DIR cannot be inside each other"},
		"volume file in the mount":    {"WORKSPACE_ENABLED=yes\nCREATE_XFS_VOLUME=yes\nXFS_VOLUME_FILE=/var/lib/cbe-portal/work/vol.img\n", "is inside WORK_DIR, which it is mounted on"},
		"device without creating":     {"WORKSPACE_ENABLED=yes\nXFS_DEVICE=/dev/sdb1\n", "XFS_DEVICE is set, and CREATE_XFS_VOLUME is no"},
		"device without erase":        {"WORKSPACE_ENABLED=yes\nCREATE_XFS_VOLUME=yes\nXFS_DEVICE=/dev/sdb1\n", "would be erased: add XFS_DEVICE_ERASE=yes"},
		"device not under /dev":       {"WORKSPACE_ENABLED=yes\nCREATE_XFS_VOLUME=yes\nXFS_DEVICE=/home/x/disk\nXFS_DEVICE_ERASE=yes\n", "XFS_DEVICE must be a device under /dev"},
		"work is data":                {"WORKSPACE_ENABLED=yes\nWORK_DIR=/var/lib/cbe-portal\n", "WORK_DIR cannot be DATA_DIR"},
		"data in a home":              {"DATA_DIR=/home/ideras/cbe\n", "DATA_DIR cannot be under /home"},
		"install in a home":           {"INSTALL_DIR=/home/ideras/cbe-portal\n", "INSTALL_DIR cannot be under /home"},
		"one account for both":        {"WORKSPACE_ENABLED=yes\nRUNNER_USER=cbe-server\n", "must be two accounts"},
		"screenshots in the work dir": {"WORKSPACE_ENABLED=yes\nSCREENSHOTS_DIR=/var/lib/cbe-portal/work/shots\n", "SCREENSHOTS_DIR is inside /var/lib/cbe-portal/work"},
	} {
		p := problems(t, test.config)
		if p.OK() || !strings.Contains(strings.Join(p.Errors, "\n"), test.want) {
			t.Errorf("%s: errors = %q, want one with %q", name, p.Errors, test.want)
		}
	}
	// A device that is allowed is valid, and the workspace keys of a server with no workspaces are ignored with a warning.
	if p := problems(t, "WORKSPACE_ENABLED=yes\nCREATE_XFS_VOLUME=yes\nXFS_DEVICE=/dev/sdb1\nXFS_DEVICE_ERASE=yes\n"); !p.OK() {
		t.Errorf("a device that may be erased: %v", p.Errors)
	}
	p := problems(t, "HOST_MEMORY_MB=1000\nCREATE_XFS_VOLUME=yes\n")
	if !p.OK() || len(p.Warnings) != 2 || !strings.Contains(p.Warnings[0], "ignored: WORKSPACE_ENABLED is no") {
		t.Errorf("a server with no workspaces: %+v", p)
	}
}

// What a person is shown, and what is saved, reads back as the same configuration.
func TestTheSavedFileAndTheSampleReadBackAsTheSameAnswers(t *testing.T) {
	v := parse(t, "DATA_DIR=/data/cbe-platform\nWORKSPACE_ENABLED=yes\nCREATE_XFS_VOLUME=yes\nXFS_VOLUME_FILE=${DATA_DIR}/work.img\nHOST_MEMORY_MB=30000\n")
	saved := v.Render()
	again, err := Parse(strings.NewReader(saved), "saved")
	if err != nil {
		t.Fatalf("the saved file does not read: %v\n%s", err, saved)
	}
	for _, spec := range Specs {
		if v.Bool("WORKSPACE_ENABLED") || !spec.Workspace {
			if v.Get(spec.Name) != again.Get(spec.Name) {
				t.Errorf("%s: %q became %q", spec.Name, v.Get(spec.Name), again.Get(spec.Name))
			}
		}
	}
	if !strings.Contains(saved, "XFS_VOLUME_FILE=${DATA_DIR}/work.img") {
		t.Error("a reference is kept, so that moving DATA_DIR moves the file")
	}
	sample, err := Parse(strings.NewReader(Sample()), "sample")
	if err != nil {
		t.Fatalf("the sample does not read: %v", err)
	}
	if p := sample.Validate(); !p.OK() {
		t.Errorf("the sample is not valid: %v", p.Errors)
	}
	if !strings.Contains(Sample(), "WORKSPACE_ENABLED=yes") || !strings.Contains(Sample(), "--dry-run") {
		t.Error("the sample says how to use it")
	}
	if no := parse(t, "").Render(); strings.Contains(no, "XFS_") || strings.Contains(no, "RUNNER_USER") {
		t.Error("a server with no workspaces is not asked about volumes")
	}
}

func TestSetChecksWhatAKeyTakes(t *testing.T) {
	v := New()
	if err := v.Set("START_SERVICES", "perhaps"); err == nil {
		t.Error("a Bool took a word")
	}
	if err := v.Set("NOPE", "1"); err == nil || !strings.Contains(err.Error(), "not a setting") {
		t.Errorf("an unknown key: %v", err)
	}
	if err := v.Set("START_SERVICES", "no"); err != nil || v.Bool("START_SERVICES") {
		t.Errorf("a good value: %v", err)
	}
	// A reference that goes round in a circle does not hang.
	_ = v.Set("DATA_DIR", "${WORK_DIR}/x")
	_ = v.Set("WORK_DIR", "${DATA_DIR}/y")
	_ = v.Get("DATA_DIR")
}
