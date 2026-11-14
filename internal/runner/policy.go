package runner

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Errors a request can earn; the API turns them into status codes.
var (
	ErrBadRequest  = errors.New("bad request")
	ErrNotApproved = errors.New("the image is not on the approved list")
	ErrConflict    = errors.New("conflict")
)

func badRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, fmt.Sprintf(format, args...))
}

// ContainerName is the name of the container of a session: by exam and by session, as the plan says.
func ContainerName(exam, session string) string { return "cbe-" + exam + "-" + session }

// Labels the runner puts on every container it makes, and finds them by.
const (
	LabelManaged = "cbe.managed"
	LabelSession = "cbe.session"
	LabelExam    = "cbe.exam"
	LabelImage   = "cbe.image"
)

// Check validates a create request against the configuration and returns the limits the container gets, with the zero ones filled in.
func (c Config) Check(req CreateRequest) (Resources, string, error) {
	if !ValidID(req.Session) || !ValidID(req.Exam) {
		return Resources{}, "", badRequest("session and exam must be IDs of letters, digits, _ and -, at most 64")
	}
	reference, approved := c.Images[req.Image]
	if !approved {
		return Resources{}, "", fmt.Errorf("%w: %q", ErrNotApproved, req.Image)
	}
	r := req.Resources
	if r.CPU == 0 {
		r.CPU = c.Default.CPU
	}
	if r.MemoryMB == 0 {
		r.MemoryMB = c.Default.MemoryMB
	}
	if r.DiskMB == 0 {
		r.DiskMB = c.Default.DiskMB
	}
	switch {
	case r.CPU < 0.1 || r.CPU > c.Max.CPU:
		return Resources{}, "", badRequest("cpu must be 0.1 to %g", c.Max.CPU)
	case r.MemoryMB < 64 || r.MemoryMB > c.Max.MemoryMB:
		return Resources{}, "", badRequest("memory_mb must be 64 to %d", c.Max.MemoryMB)
	case r.DiskMB < 1 || r.DiskMB > c.Max.DiskMB:
		return Resources{}, "", badRequest("disk_mb must be 1 to %d", c.Max.DiskMB)
	}
	return r, reference, nil
}

// BuildSpec is the only place a container's options are decided. The request supplies an exam, a session, an approved image and
// limits within the maximums; everything else is here and does not depend on the request: no network, a read-only root file system,
// a small tmpfs at /tmp without exec, no capabilities, no new privileges, the student user, a limit on processes, no swap, and
// exactly two mounts, the student's /work and the agent's socket folder. The runtime's default seccomp profile is left on.
func BuildSpec(c Config, req CreateRequest, resources Resources, reference string) ContainerSpec {
	return ContainerSpec{
		Name:    ContainerName(req.Exam, req.Session),
		Image:   reference,
		User:    fmt.Sprintf("%d:%d", StudentUID, StudentGID),
		WorkDir: WorkPath,
		Labels:  map[string]string{LabelManaged: "1", LabelSession: req.Session, LabelExam: req.Exam, LabelImage: req.Image},
		Binds: []Bind{
			{Source: filepath.Join(c.WorkRoot, req.Session), Target: WorkPath},
			{Source: filepath.Join(c.RunRoot, req.Session), Target: SocketPath},
		},
		Tmpfs:          map[string]string{"/tmp": fmt.Sprintf("rw,noexec,nosuid,nodev,size=%dm,mode=1777", c.TmpMB)},
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
		PidsLimit:      int64(c.Pids),
		MemoryBytes:    int64(resources.MemoryMB) << 20,
		MemorySwap:     int64(resources.MemoryMB) << 20,
		NanoCPUs:       int64(resources.CPU * 1e9),
		StopTimeout:    c.StopTimeout(),
	}
}
