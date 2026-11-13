package runner

import (
	"context"
	"io"
	"time"
)

// Bind mounts a host folder into the container.
type Bind struct {
	Source string
	Target string
}

// ContainerSpec is everything the runtime is told about a container. It is built in one place (BuildSpec), from a validated request
// and the runner's own settings, and handed to the runtime as it is: the runtime adds nothing and a caller cannot reach it with
// anything else.
type ContainerSpec struct {
	Name    string
	Image   string
	User    string
	WorkDir string
	Labels  map[string]string
	Binds   []Bind
	// Tmpfs maps a path to its mount options.
	Tmpfs          map[string]string
	NetworkMode    string
	ReadonlyRootfs bool
	CapDrop        []string
	SecurityOpt    []string
	PidsLimit      int64
	MemoryBytes    int64
	MemorySwap     int64
	NanoCPUs       int64
	// StopTimeout is how long a stop waits for the agent to end before it kills it.
	StopTimeout time.Duration
}

// Inspected is what the runtime says about a container.
type Inspected struct {
	ID        string
	Name      string
	Labels    map[string]string
	State     string
	ExitCode  int
	StartedAt time.Time
}

// RawStats is one sample of a container's use.
type RawStats struct {
	CPUPercent  float64
	MemoryBytes uint64
	MemoryLimit uint64
	Pids        uint64
}

// ImageInfo is what the runtime says about an image.
type ImageInfo struct {
	ID     string
	Labels map[string]string
}

// Runtime is the container runtime. Its only implementation talks the Docker Engine API (to rootless Podman or to Docker) through the
// Docker Go SDK, against a socket path that is a deployment setting (decision D10). Nothing outside this package is runtime-specific.
type Runtime interface {
	Ping(ctx context.Context) error
	Create(ctx context.Context, spec ContainerSpec) (id string, err error)
	Start(ctx context.Context, name string) error
	Pause(ctx context.Context, name string) error
	Unpause(ctx context.Context, name string) error
	Stop(ctx context.Context, name string, timeout time.Duration) error
	Remove(ctx context.Context, name string) error
	// Inspect returns ErrNotFound when there is no such container.
	Inspect(ctx context.Context, name string) (Inspected, error)
	// List returns the containers that carry the label key=value.
	List(ctx context.Context, key, value string) ([]Inspected, error)
	Stats(ctx context.Context, name string) (RawStats, error)
	// CopyTo extracts a tar archive into a folder of the container; CopyFrom reads a path of the container as a tar archive.
	CopyTo(ctx context.Context, name, folder string, archive io.Reader) error
	CopyFrom(ctx context.Context, name, path string) (io.ReadCloser, error)
	// ImageInspect returns ErrNotFound when the runtime does not have the image.
	ImageInspect(ctx context.Context, ref string) (ImageInfo, error)
}
