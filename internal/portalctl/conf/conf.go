// Package conf is the configuration file of cbe-portalctl: KEY=VALUE lines that say how a server is set up, so that a machine can be
// prepared without anyone answering questions. The file is parsed, never run: a line is a key, an equals sign and a value, and a value
// that is not what its key takes, or a key that does not exist, is an error that names the line.
package conf

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Kind is what a key takes.
type Kind int

const (
	// Path is an absolute path made of letters, digits and . _ / @ + - only: it goes into systemd units, which do not quote.
	Path Kind = iota
	Bool
	Int
	// Size is a number and a unit: M, G or T (powers of 1024), as in 64G.
	Size
	// Name is the name of an account or a group.
	Name
	// Address is host:port for the server to listen on; the host may be empty.
	Address
	// Text is a few words of server flags, in the same safe alphabet as a path plus spaces.
	Text
)

// Spec is one key.
type Spec struct {
	Name string
	Kind Kind
	// Default may refer to other keys as ${NAME}.
	Default string
	// Optional keys may be empty.
	Optional bool
	// Min and Max bound an Int, and Min a Size in bytes.
	Min, Max int64
	// Workspace keys only matter when WORKSPACE_ENABLED is yes.
	Workspace bool
	// Basic keys are the ones the configurator asks about when it has no file.
	Basic bool
	Help  string
}

// Specs are all the keys, in the order the sample and the saved file list them.
var Specs = []Spec{
	{Name: "INSTALL_DIR", Kind: Path, Default: "/opt/cbe-portal", Help: "Where the program files are (set by the installer; the configurator finds its own binaries here)."},
	{Name: "DATA_DIR", Kind: Path, Default: "/var/lib/cbe-portal", Basic: true, Help: "Where everything the server keeps lives: database, screenshots, snapshots and the students' folders. Never under INSTALL_DIR."},
	{Name: "WORKSPACE_ENABLED", Kind: Bool, Default: "no", Basic: true, Help: "yes: serve workspace exams (needs Podman and an XFS volume with project quotas). no: question exams only."},
	{Name: "BIND_ADDRESS", Kind: Address, Default: "0.0.0.0:8080", Basic: true, Help: "The address the exam server listens on (host:port)."},
	{Name: "SERVER_USER", Kind: Name, Default: "cbe-server", Help: "The account the exam server runs as (made if it does not exist; it cannot log in)."},
	{Name: "DATABASE_FILEPATH", Kind: Path, Default: "${DATA_DIR}/server/cbe-platform.db", Basic: true, Help: "The SQLite database. The server makes and upgrades it by itself; its folder must be one of its own (the server writes next to the file)."},
	{Name: "SCREENSHOTS_DIR", Kind: Path, Default: "${DATA_DIR}/server/screenshots", Help: "Where the students' screenshots are kept."},
	{Name: "ADMIN_TOKEN_FILE", Kind: Path, Default: "${DATA_DIR}/server/admin-token", Help: "The proctor's token. Made, with a random one, when the file does not exist."},
	{Name: "WORKSPACE_DIR", Kind: Path, Default: "${DATA_DIR}/server/workspace", Workspace: true, Help: "Where workspace bundles and snapshots are kept."},
	{Name: "SERVER_EXTRA_ARGS", Kind: Text, Optional: true, Help: "More flags for the server (cbe-portal-server -h lists them), for example -workspace-retention=48h."},
	{Name: "START_SERVICES", Kind: Bool, Default: "yes", Basic: true, Help: "yes: start the services (and enable them at boot) when the configuration is done."},

	{Name: "RUNNER_USER", Kind: Name, Default: "cbe-runner", Workspace: true, Help: "The account that holds the container runtime (rootless Podman); made if it does not exist."},
	{Name: "IPC_GROUP", Kind: Name, Default: "cbe-ipc", Workspace: true, Help: "The group that lets the exam server reach the runner's socket and nothing else of the runner's."},
	{Name: "WORK_DIR", Kind: Path, Default: "${DATA_DIR}/work", Workspace: true, Help: "Where the XFS volume is mounted: one size-limited folder for each student's workspace."},
	{Name: "CREATE_XFS_VOLUME", Kind: Bool, Default: "no", Workspace: true, Basic: true, Help: "yes: make the XFS volume (a file, or XFS_DEVICE) and mount it at WORK_DIR. no: WORK_DIR must already be an XFS mount with project quotas (prjquota)."},
	{Name: "XFS_VOLUME_FILE", Kind: Path, Default: "${DATA_DIR}/cbe-portal-work.img", Workspace: true, Basic: true, Help: "The file that holds the volume (sparse: it takes disk as it is used). Not inside WORK_DIR, which it is mounted on."},
	{Name: "XFS_VOLUME_SIZE", Kind: Size, Default: "64G", Min: 1 << 30, Workspace: true, Basic: true, Help: "The size of the volume file, for example 64G. 40 students at the default 100 MiB need 4 GiB; the rest is room."},
	{Name: "XFS_DEVICE", Kind: Path, Optional: true, Workspace: true, Help: "A spare partition to format instead of a file, for example /dev/sdb1. Everything on it is erased: it needs XFS_DEVICE_ERASE=yes."},
	{Name: "XFS_DEVICE_ERASE", Kind: Bool, Default: "no", Workspace: true, Help: "yes: allow XFS_DEVICE to be formatted. Nothing is ever formatted without it."},
	{Name: "HOST_MEMORY_MB", Kind: Int, Default: "0", Min: 0, Max: 4 << 20, Workspace: true, Help: "The most memory (MiB) the students' containers may add up to. 0 is 70% of the machine's memory."},
	{Name: "CONTAINER_STORAGE_DIR", Kind: Path, Optional: true, Workspace: true, Help: "Where the container runtime keeps images (default: under the runner account's home). Images need room: a Python image with its libraries is several GB. Existing images are not moved."},
}

var byName = func() map[string]*Spec {
	m := map[string]*Spec{}
	for i := range Specs {
		m[Specs[i].Name] = &Specs[i]
	}
	return m
}()

// Lookup returns the spec of a key.
func Lookup(name string) (Spec, bool) {
	spec, ok := byName[name]
	if !ok {
		return Spec{}, false
	}
	return *spec, true
}

// Values are the answers: what the file said, and for every other key its default.
type Values struct {
	set map[string]string // raw, as written (references not yet expanded)
}

// New has nothing set: every key has its default.
func New() *Values { return &Values{set: map[string]string{}} }

// IsSet says whether a key was given, rather than left to its default.
func (v *Values) IsSet(name string) bool { _, ok := v.set[name]; return ok }

// Set gives a key a value, checked against what the key takes.
func (v *Values) Set(name, value string) error {
	spec, ok := byName[name]
	if !ok {
		return unknownKey(name)
	}
	if _, err := normalize(*spec, v.expand(value, 0)); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	v.set[name] = value
	return nil
}

// Get is the effective value of a key (a default, or what was set), with references to other keys expanded and the value in its normal
// form (yes or no for a Bool, a clean path for a Path).
func (v *Values) Get(name string) string {
	spec, ok := byName[name]
	if !ok {
		return ""
	}
	raw, set := v.set[name]
	if !set {
		raw = spec.Default
	}
	value := v.expand(raw, 0)
	if normal, err := normalize(*spec, value); err == nil {
		return normal
	}
	return value
}

// Bool is a Bool key.
func (v *Values) Bool(name string) bool { return v.Get(name) == "yes" }

// Int is an Int key (0 when it is not a number: Validate says so).
func (v *Values) Int(name string) int {
	n, _ := strconv.Atoi(v.Get(name))
	return n
}

// Bytes is a Size key in bytes (0 when it is not a size: Validate says so).
func (v *Values) Bytes(name string) int64 {
	n, _ := parseSize(v.Get(name))
	return n
}

var reference = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)

// expand replaces ${NAME} with the value of NAME. A reference to a key that does not exist is left as it is (and fails the check of the
// kind), and so is one that goes round in a circle.
func (v *Values) expand(raw string, depth int) string {
	if depth > 8 {
		return raw
	}
	return reference.ReplaceAllStringFunc(raw, func(match string) string {
		name := match[2 : len(match)-1]
		spec, ok := byName[name]
		if !ok {
			return match
		}
		inner, set := v.set[name]
		if !set {
			inner = spec.Default
		}
		return v.expand(inner, depth+1)
	})
}

func unknownKey(name string) error {
	best, distance := "", 3
	for _, spec := range Specs {
		if d := levenshtein(name, spec.Name); d < distance {
			best, distance = spec.Name, d
		}
	}
	if best != "" {
		return fmt.Errorf("%s is not a setting (did you mean %s?)", name, best)
	}
	return fmt.Errorf("%s is not a setting", name)
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// Parse reads a configuration file. Each problem names its line; every problem found is reported, not only the first. source names the
// file in the messages.
func Parse(r io.Reader, source string) (*Values, error) {
	v := New()
	var problems []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		name, value, found := strings.Cut(text, "=")
		name = strings.TrimSpace(name)
		if !found {
			problems = append(problems, fmt.Sprintf("%s:%d: %q is not KEY=VALUE", source, line, clip(text)))
			continue
		}
		value, err := unquote(strings.TrimSpace(value))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: %s: %v", source, line, name, err))
			continue
		}
		spec, ok := byName[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s:%d: %v", source, line, unknownKey(name)))
			continue
		}
		if _, dup := v.set[name]; dup {
			problems = append(problems, fmt.Sprintf("%s:%d: %s is given twice", source, line, name))
			continue
		}
		if _, err := normalize(*spec, v.expand(value, 0)); err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: %s: %v", source, line, name, err))
			continue
		}
		v.set[name] = value
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, fmt.Sprintf("%s: %v", source, err))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return v, nil
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

// unquote takes the quotes off a value that has them, and the comment off one that does not ("value  # why").
func unquote(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if quote := value[0]; quote == '"' || quote == '\'' {
		end := strings.IndexByte(value[1:], quote)
		if end < 0 {
			return "", fmt.Errorf("the quote is not closed")
		}
		rest := strings.TrimSpace(value[end+2:])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", fmt.Errorf("there is text after the closing quote")
		}
		return value[1 : end+1], nil
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = value[:i]
	} else if strings.HasPrefix(value, "#") {
		value = ""
	}
	return strings.TrimSpace(value), nil
}

var (
	safePath = regexp.MustCompile(`^/[A-Za-z0-9._/@+-]*$`)
	nameForm = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	textForm = regexp.MustCompile(`^[A-Za-z0-9_./:=@,+ -]*$`)
	hostForm = regexp.MustCompile(`^[A-Za-z0-9.-]*$`)
)

// normalize checks a value against what its key takes, and returns it in its normal form.
func normalize(spec Spec, value string) (string, error) {
	if value == "" {
		if spec.Optional {
			return "", nil
		}
		return "", fmt.Errorf("it needs a value")
	}
	switch spec.Kind {
	case Path:
		if len(value) > 200 || !safePath.MatchString(value) {
			return "", fmt.Errorf("%q is not an absolute path of letters, digits and . _ / @ + - only", clip(value))
		}
		for _, part := range strings.Split(value, "/") {
			if part == ".." {
				return "", fmt.Errorf("%q has a .. in it", clip(value))
			}
		}
		clean := path.Clean(value)
		if clean == "/" {
			return "", fmt.Errorf("it cannot be the root folder")
		}
		return clean, nil
	case Bool:
		switch strings.ToLower(value) {
		case "yes", "true", "1", "on":
			return "yes", nil
		case "no", "false", "0", "off":
			return "no", nil
		}
		return "", fmt.Errorf("%q is not yes or no", clip(value))
	case Int:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("%q is not a whole number", clip(value))
		}
		if n < spec.Min || (spec.Max != 0 && n > spec.Max) {
			return "", fmt.Errorf("%d is out of range (%d to %d)", n, spec.Min, spec.Max)
		}
		return strconv.FormatInt(n, 10), nil
	case Size:
		n, err := parseSize(value)
		if err != nil {
			return "", err
		}
		if n < spec.Min {
			return "", fmt.Errorf("%s is too small (the least is %s)", value, formatSize(spec.Min))
		}
		return strings.ToUpper(value), nil
	case Name:
		if !nameForm.MatchString(value) {
			return "", fmt.Errorf("%q is not the name of an account (lower case letters, digits, _ and -)", clip(value))
		}
		return value, nil
	case Address:
		host, port, err := net.SplitHostPort(value)
		if err != nil {
			return "", fmt.Errorf("%q is not host:port", clip(value))
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("the port %q is not 1 to 65535", clip(port))
		}
		if !hostForm.MatchString(host) && net.ParseIP(host) == nil {
			return "", fmt.Errorf("the host %q is not an address or a name", clip(host))
		}
		return value, nil
	case Text:
		if len(value) > 400 || !textForm.MatchString(value) {
			return "", fmt.Errorf("it holds a character that is not allowed (letters, digits, spaces and _ . / : = @ , + - only)")
		}
		return strings.Join(strings.Fields(value), " "), nil
	}
	return value, nil
}

func parseSize(value string) (int64, error) {
	if len(value) < 2 {
		return 0, fmt.Errorf("%q is not a size such as 64G", clip(value))
	}
	unit := map[byte]int64{'M': 1 << 20, 'G': 1 << 30, 'T': 1 << 40}[strings.ToUpper(value)[len(value)-1]]
	n, err := strconv.ParseInt(value[:len(value)-1], 10, 64)
	if unit == 0 || err != nil || n <= 0 || n > 1<<20 {
		return 0, fmt.Errorf("%q is not a size such as 64G (a number and M, G or T)", clip(value))
	}
	return n * unit, nil
}

func formatSize(n int64) string {
	switch {
	case n%(1<<40) == 0:
		return fmt.Sprintf("%dT", n>>40)
	case n%(1<<30) == 0:
		return fmt.Sprintf("%dG", n>>30)
	}
	return fmt.Sprintf("%dM", n>>20)
}

// Problems is what Validate found.
type Problems struct {
	Errors   []string
	Warnings []string
}

// OK says there is nothing to stop the configuration.
func (p Problems) OK() bool { return len(p.Errors) == 0 }

// Validate checks every key and the keys against each other. It looks at nothing on the machine: that is the configurator's check of the
// machine (doctor).
func (v *Values) Validate() Problems {
	var p Problems
	for _, spec := range Specs {
		if spec.Workspace && !v.Bool("WORKSPACE_ENABLED") {
			if v.IsSet(spec.Name) {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s is set, and ignored: WORKSPACE_ENABLED is no", spec.Name))
			}
			continue
		}
		if _, err := normalize(spec, v.expand(v.rawOrDefault(spec), 0)); err != nil {
			p.Errors = append(p.Errors, fmt.Sprintf("%s: %v", spec.Name, err))
		}
	}
	if !p.OK() {
		return p
	}
	data := v.Get("DATA_DIR")
	install := v.Get("INSTALL_DIR")
	// The units run with ProtectHome: a folder under /home is not there for them.
	for _, key := range []string{"DATA_DIR", "INSTALL_DIR"} {
		for _, home := range []string{"/home", "/root", "/run/user"} {
			if inside(v.Get(key), home) {
				p.Errors = append(p.Errors, fmt.Sprintf("%s cannot be under %s: the services run with ProtectHome, which hides it from them", key, home))
			}
		}
	}
	if inside(data, install) || inside(install, data) {
		p.Errors = append(p.Errors, "DATA_DIR and INSTALL_DIR cannot be inside each other: what the program is replaced with must never hold the exams")
	}
	// The server writes its database next to the file (the journal), so the folder must be its own; DATA_DIR holds the runner's folders.
	reserved := []string{v.Get("DATA_DIR")}
	if v.Bool("WORKSPACE_ENABLED") {
		for _, name := range []string{"ipc", "run", "runner", "grading"} {
			reserved = append(reserved, path.Join(data, name))
		}
		reserved = append(reserved, v.Get("WORK_DIR"))
	}
	for _, key := range []string{"DATABASE_FILEPATH", "ADMIN_TOKEN_FILE"} {
		dir := path.Dir(v.Get(key))
		if !inside(dir, data) || dir == data {
			p.Errors = append(p.Errors, fmt.Sprintf("%s must be inside a folder of its own under DATA_DIR, for example %s/server/%s: the server account writes next to it, and DATA_DIR holds the runner's folders (%s)", key, data, path.Base(v.Get(key)), v.Get(key)))
		}
		for _, folder := range reserved[1:] {
			if inside(dir, folder) {
				p.Errors = append(p.Errors, fmt.Sprintf("%s is inside %s, which is not the server's", key, folder))
			}
		}
	}
	for _, key := range []string{"SCREENSHOTS_DIR", "WORKSPACE_DIR"} {
		if key == "WORKSPACE_DIR" && !v.Bool("WORKSPACE_ENABLED") {
			continue
		}
		dir := v.Get(key)
		if !inside(dir, data) || dir == data {
			p.Errors = append(p.Errors, fmt.Sprintf("%s must be a folder under DATA_DIR (%s)", key, dir))
		}
		for _, folder := range reserved[1:] {
			if inside(dir, folder) {
				p.Errors = append(p.Errors, fmt.Sprintf("%s is inside %s, which is not the server's", key, folder))
			}
		}
	}
	if v.Bool("WORKSPACE_ENABLED") {
		work := v.Get("WORK_DIR")
		if work == data {
			p.Errors = append(p.Errors, "WORK_DIR cannot be DATA_DIR itself: the volume is mounted over it")
		}
		device := v.Get("XFS_DEVICE")
		create := v.Bool("CREATE_XFS_VOLUME")
		switch {
		case device != "" && !create:
			p.Errors = append(p.Errors, "XFS_DEVICE is set, and CREATE_XFS_VOLUME is no: say yes to format it, or take it out")
		case device != "" && !v.Bool("XFS_DEVICE_ERASE"):
			p.Errors = append(p.Errors, fmt.Sprintf("XFS_DEVICE=%s would be erased: add XFS_DEVICE_ERASE=yes if that is meant", device))
		case device != "" && !strings.HasPrefix(device, "/dev/"):
			p.Errors = append(p.Errors, "XFS_DEVICE must be a device under /dev")
		}
		if create && device == "" {
			file := v.Get("XFS_VOLUME_FILE")
			if inside(file, work) {
				p.Errors = append(p.Errors, fmt.Sprintf("XFS_VOLUME_FILE (%s) is inside WORK_DIR, which it is mounted on: the file would be hidden by its own volume", file))
			}
			if !inside(file, data) {
				p.Warnings = append(p.Warnings, fmt.Sprintf("XFS_VOLUME_FILE (%s) is not under DATA_DIR: it is kept apart from the rest of the data", file))
			}
		}
		if !create && v.IsSet("XFS_VOLUME_FILE") {
			p.Warnings = append(p.Warnings, "XFS_VOLUME_FILE is set, and ignored: CREATE_XFS_VOLUME is no (WORK_DIR must already be an XFS mount)")
		}
	}
	if v.Get("RUNNER_USER") == v.Get("SERVER_USER") && v.Bool("WORKSPACE_ENABLED") {
		p.Errors = append(p.Errors, "RUNNER_USER and SERVER_USER must be two accounts: the exam server must never hold the container runtime")
	}
	return p
}

func (v *Values) rawOrDefault(spec Spec) string {
	if raw, ok := v.set[spec.Name]; ok {
		return raw
	}
	return spec.Default
}

// inside says whether child is parent or below it.
func inside(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// Render is the file that holds these answers: every key that matters, with its effective value and what it is for.
func (v *Values) Render() string {
	var b strings.Builder
	b.WriteString("# cbe-portalctl configuration. Written by `cbe-portalctl configure`; edit it and run `cbe-portalctl configure --config FILE` again.\n")
	workspace := v.Bool("WORKSPACE_ENABLED")
	for _, spec := range Specs {
		if spec.Workspace && !workspace {
			continue
		}
		fmt.Fprintf(&b, "\n# %s\n", spec.Help)
		fmt.Fprintf(&b, "%s=%s\n", spec.Name, v.renderValue(spec))
	}
	return b.String()
}

func (v *Values) renderValue(spec Spec) string {
	// What was written with a reference keeps it (so DATA_DIR can still move the rest); a default is written out.
	if raw, ok := v.set[spec.Name]; ok {
		return raw
	}
	return spec.Default
}

// Sample is a documented file with every key at its default, for a person to start from.
func Sample() string {
	v := New()
	_ = v.Set("WORKSPACE_ENABLED", "yes")
	_ = v.Set("CREATE_XFS_VOLUME", "yes")
	text := v.Render()
	return strings.Replace(text, "# cbe-portalctl configuration. Written by `cbe-portalctl configure`; edit it and run `cbe-portalctl configure --config FILE` again.",
		"# cbe-portalctl configuration (sample). Keys are KEY=VALUE, one a line; ${NAME} means the value of another key. Keys left out take their defaults.\n# Run: sudo cbe-portalctl configure --config this-file --yes   (--dry-run says what it would do and changes nothing)", 1)
}

// Names are the keys, sorted, for messages.
func Names() []string {
	names := make([]string, 0, len(Specs))
	for _, spec := range Specs {
		names = append(names, spec.Name)
	}
	sort.Strings(names)
	return names
}
