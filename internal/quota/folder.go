package quota

// Folder is a folder the helper made. The type is here, free of the Linux-only code of the helper, because the runner's package and
// with it the exam server's name it, and they build on every system.
type Folder struct {
	Session string `json:"session"`
	Path    string `json:"path"`
	Project int    `json:"project"`
	SizeMB  int    `json:"size_mb"`
	Inodes  int    `json:"inodes"`
}
