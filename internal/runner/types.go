// Package runner is cbe-runner (workspace plan, section 4): the one small service that holds the container runtime's socket. It
// gives the exam server a narrow, authenticated API with exactly the operations of Appendix E and nothing else, and it sets every
// container option itself: the exam server cannot ask for an image that is not approved, a mount, a capability, a network or a
// limit above the maximums, because there is no way to say it.
package runner

import (
	"errors"
	"time"
)

// Container states, as the runner reports them.
const (
	StateMissing = "missing"
	StateCreated = "created"
	StateRunning = "running"
	StatePaused  = "paused"
	StateExited  = "exited"
)

// Where things are inside the container (the image contract, images/README.md).
const (
	WorkPath   = "/work"
	SocketPath = "/run/cbe"
	// StudentUID and StudentGID are the student in every course image (decision P0-30).
	StudentUID = 1000
	StudentGID = 1000
)

// ErrNotFound is what a Runtime returns for a container or an image that does not exist.
var ErrNotFound = errors.New("not found")

// Resources are the limits of one container. The exam asks for them in `resources`; the runner keeps them within its maximums.
type Resources struct {
	CPU      float64 `json:"cpu"`
	MemoryMB int     `json:"memory_mb"`
	DiskMB   int     `json:"disk_mb"`
}

// CreateRequest is container.create: which session, of which exam, from which approved image, with which limits.
type CreateRequest struct {
	Session   string    `json:"session"`
	Exam      string    `json:"exam"`
	Image     string    `json:"image"`
	Resources Resources `json:"resources"`
}

// AgentStatus is what the runner last heard from the agent in a container.
type AgentStatus struct {
	// Connected says the runner's own connection to the agent is up.
	Connected bool `json:"connected"`
	// LastSeen is when the agent last spoke (hello or heartbeat); the zero time means never.
	LastSeen      time.Time `json:"last_seen"`
	Version       string    `json:"version,omitempty"`
	UptimeMS      int64     `json:"uptime_ms,omitempty"`
	OpenTerminals int       `json:"open_terminals,omitempty"`
	OOMKills      uint64    `json:"oom_kills,omitempty"`
	SlowPaths     bool      `json:"slow_paths,omitempty"`
}

// Status is container.status: the container's state and the agent's heartbeat.
type Status struct {
	Session   string      `json:"session"`
	Exam      string      `json:"exam,omitempty"`
	State     string      `json:"state"`
	ExitCode  *int        `json:"exit_code,omitempty"`
	StartedAt time.Time   `json:"started_at,omitempty"`
	Seeded    bool        `json:"seeded"`
	Resources Resources   `json:"resources"`
	Agent     AgentStatus `json:"agent"`
}

// Stats is container.stats: what a container uses now.
type Stats struct {
	Session     string  `json:"session"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	MemoryLimit uint64  `json:"memory_limit"`
	Pids        uint64  `json:"pids"`
}

// ImageStatus is image.inspect: whether an image is on the approved list and whether the runtime has it.
type ImageStatus struct {
	Name     string `json:"name"`
	Approved bool   `json:"approved"`
	Present  bool   `json:"present"`
	ID       string `json:"id,omitempty"`
}
