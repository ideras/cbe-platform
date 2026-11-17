// Package portalctl is the configurator of a cbe-portal server: it takes a machine that has the program files in INSTALL_DIR (the installer
// put them there) and makes it a server, from a configuration (internal/portalctl/conf). What it does is a plan of steps; each step knows how
// to tell whether it is already done, so that running it again changes nothing, and a dry run says what would be done.
package portalctl

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	"exam-platform/internal/portalctl/conf"
)

//go:embed templates/*.tmpl
var templateFiles embed.FS

// Layout is every path and name the configuration comes to, in one place.
type Layout struct {
	Workspace     bool
	Install       string // INSTALL_DIR
	Data          string // DATA_DIR
	ServerUser    string
	RunnerUser    string
	IPCGroup      string
	Bind          string
	Database      string
	Screenshots   string
	Token         string
	WorkspaceDir  string
	Extra         string
	WorkDir       string
	StartServices bool

	CreateVolume bool
	VolumeFile   string
	VolumeSize   string
	VolumeDevice string
	HostMemoryMB int
	StorageDir   string
}

// NewLayout reads the configuration.
func NewLayout(v *conf.Values) Layout {
	return Layout{
		Workspace: v.Bool("WORKSPACE_ENABLED"), Install: v.Get("INSTALL_DIR"), Data: v.Get("DATA_DIR"),
		ServerUser: v.Get("SERVER_USER"), RunnerUser: v.Get("RUNNER_USER"), IPCGroup: v.Get("IPC_GROUP"), Bind: v.Get("BIND_ADDRESS"),
		Database: v.Get("DATABASE_FILEPATH"), Screenshots: v.Get("SCREENSHOTS_DIR"), Token: v.Get("ADMIN_TOKEN_FILE"), WorkspaceDir: v.Get("WORKSPACE_DIR"),
		Extra: v.Get("SERVER_EXTRA_ARGS"), WorkDir: v.Get("WORK_DIR"), StartServices: v.Bool("START_SERVICES"),
		CreateVolume: v.Bool("CREATE_XFS_VOLUME"), VolumeFile: v.Get("XFS_VOLUME_FILE"), VolumeSize: v.Get("XFS_VOLUME_SIZE"), VolumeDevice: v.Get("XFS_DEVICE"),
		HostMemoryMB: v.Int("HOST_MEMORY_MB"), StorageDir: v.Get("CONTAINER_STORAGE_DIR"),
	}
}

// Bin is a program of the installation.
func (l Layout) Bin(name string) string { return path.Join(l.Install, "bin", name) }

// UnitDir is where the units are kept, and from where they are linked.
func (l Layout) UnitDir() string { return path.Join(l.Install, "lib", "systemd") }

// RunnerConfig is the runner's settings.
func (l Layout) RunnerConfig() string { return path.Join(l.Install, "etc", "runner.json") }

// SavedConfig is where the answers are kept, for the next run and for the uninstaller.
func (l Layout) SavedConfig() string { return path.Join(l.Install, "etc", "cbe-portal.conf") }

// ServerDir is the folder the server's files are in: the one that holds the database.
func (l Layout) ServerDir() string { return path.Dir(l.Database) }

// ServerFolders are the folders the server account owns and writes: the database's, the screenshots', the workspace bundles' and the
// token's, without repeats and without one inside another.
func (l Layout) ServerFolders() []string {
	folders := []string{path.Dir(l.Database), l.Screenshots, path.Dir(l.Token)}
	if l.Workspace {
		folders = append(folders, l.WorkspaceDir)
	}
	sort.Strings(folders)
	var out []string
	for _, folder := range folders {
		if len(out) > 0 && (folder == out[len(out)-1] || strings.HasPrefix(folder, out[len(out)-1]+"/")) {
			continue
		}
		out = append(out, folder)
	}
	return out
}

// AllServerFolders are every folder the server account owns, parents first: the ones to make.
func (l Layout) AllServerFolders() []string {
	folders := []string{path.Dir(l.Database), l.Screenshots, path.Dir(l.Token)}
	if l.Workspace {
		folders = append(folders, l.WorkspaceDir)
	}
	sort.Strings(folders)
	var out []string
	for _, folder := range folders {
		if len(out) == 0 || out[len(out)-1] != folder {
			out = append(out, folder)
		}
	}
	return out
}

// Writable is what the server's unit may write (what ReadWritePaths says).
func (l Layout) Writable() string { return strings.Join(l.ServerFolders(), " ") }

// RunnerSocketPath is where the runner listens; the secret is next to it.
func (l Layout) IPC() string { return path.Join(l.Data, "ipc") }

// Unit is a unit file made from its template.
func (l Layout) Unit(name string) (string, error) {
	text, err := templateFiles.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(text))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	err = tmpl.Execute(&b, struct {
		Layout
		ServerDir     string
		InstallDir    string
		WritablePaths string
	}{l, l.ServerDir(), l.Install, l.Writable()})
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return b.String(), nil
}
