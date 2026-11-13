package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Docker is the Runtime that talks the Docker Engine API to rootless Podman (its compatible API) or to Docker. It is the only place
// in the repository that knows the SDK.
type Docker struct {
	cli *client.Client
}

// NewDocker connects to the runtime at host, such as unix:///run/user/1001/podman/podman.sock.
func NewDocker(host string) (*Docker, error) {
	cli, err := client.New(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Docker{cli: cli}, nil
}

// Close releases the connection.
func (d *Docker) Close() error { return d.cli.Close() }

func notFound(err error) error {
	if err != nil && cerrdefs.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}

func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.cli.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true})
	return err
}

// hostConfig is the whole translation of a spec into the SDK's types, kept apart so a test can look at it.
func hostConfig(spec ContainerSpec) (*container.Config, *container.HostConfig) {
	binds := make([]string, 0, len(spec.Binds))
	for _, bind := range spec.Binds {
		binds = append(binds, bind.Source+":"+bind.Target)
	}
	pids := spec.PidsLimit
	config := &container.Config{Image: spec.Image, User: spec.User, WorkingDir: spec.WorkDir, Labels: spec.Labels}
	timeout := int(spec.StopTimeout / time.Second)
	config.StopTimeout = &timeout
	host := &container.HostConfig{
		Binds:          binds,
		NetworkMode:    container.NetworkMode(spec.NetworkMode),
		ReadonlyRootfs: spec.ReadonlyRootfs,
		CapDrop:        spec.CapDrop,
		SecurityOpt:    spec.SecurityOpt,
		Tmpfs:          spec.Tmpfs,
		Resources: container.Resources{
			Memory:     spec.MemoryBytes,
			MemorySwap: spec.MemorySwap,
			NanoCPUs:   spec.NanoCPUs,
			PidsLimit:  &pids,
		},
	}
	return config, host
}

func (d *Docker) Create(ctx context.Context, spec ContainerSpec) (string, error) {
	config, host := hostConfig(spec)
	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{Name: spec.Name, Config: config, HostConfig: host})
	return created.ID, err
}

func (d *Docker) Start(ctx context.Context, name string) error {
	_, err := d.cli.ContainerStart(ctx, name, client.ContainerStartOptions{})
	return notFound(err)
}

func (d *Docker) Pause(ctx context.Context, name string) error {
	_, err := d.cli.ContainerPause(ctx, name, client.ContainerPauseOptions{})
	return notFound(err)
}

func (d *Docker) Unpause(ctx context.Context, name string) error {
	_, err := d.cli.ContainerUnpause(ctx, name, client.ContainerUnpauseOptions{})
	return notFound(err)
}

func (d *Docker) Stop(ctx context.Context, name string, timeout time.Duration) error {
	seconds := int(timeout / time.Second)
	_, err := d.cli.ContainerStop(ctx, name, client.ContainerStopOptions{Timeout: &seconds})
	return notFound(err)
}

func (d *Docker) Remove(ctx context.Context, name string) error {
	_, err := d.cli.ContainerRemove(ctx, name, client.ContainerRemoveOptions{Force: true})
	return notFound(err)
}

func inspected(response container.InspectResponse) Inspected {
	out := Inspected{ID: response.ID, Name: strings.TrimPrefix(response.Name, "/")}
	if response.Config != nil {
		out.Labels = response.Config.Labels
	}
	if state := response.State; state != nil {
		out.ExitCode = state.ExitCode
		switch {
		case state.Paused:
			out.State = StatePaused
		case state.Running:
			out.State = StateRunning
		case state.Status == "created":
			out.State = StateCreated
		default:
			out.State = StateExited
		}
		if started, err := time.Parse(time.RFC3339Nano, state.StartedAt); err == nil && started.Year() > 1 {
			out.StartedAt = started
		}
	}
	return out
}

func (d *Docker) Inspect(ctx context.Context, name string) (Inspected, error) {
	result, err := d.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return Inspected{}, notFound(err)
	}
	return inspected(result.Container), nil
}

func (d *Docker) List(ctx context.Context, key, value string) ([]Inspected, error) {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", key+"="+value)})
	if err != nil {
		return nil, err
	}
	var out []Inspected
	for _, item := range list.Items {
		one, err := d.Inspect(ctx, item.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

func (d *Docker) Stats(ctx context.Context, name string) (RawStats, error) {
	reply, err := d.cli.ContainerStats(ctx, name, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		return RawStats{}, notFound(err)
	}
	defer reply.Body.Close()
	var sample container.StatsResponse
	if err := json.NewDecoder(reply.Body).Decode(&sample); err != nil {
		return RawStats{}, err
	}
	out := RawStats{MemoryBytes: sample.MemoryStats.Usage, MemoryLimit: sample.MemoryStats.Limit, Pids: sample.PidsStats.Current}
	cpuDelta := float64(sample.CPUStats.CPUUsage.TotalUsage) - float64(sample.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(sample.CPUStats.SystemUsage) - float64(sample.PreCPUStats.SystemUsage)
	if cpuDelta > 0 && systemDelta > 0 {
		cpus := float64(sample.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = float64(len(sample.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpus == 0 {
			cpus = 1
		}
		out.CPUPercent = cpuDelta / systemDelta * cpus * 100
	}
	return out, nil
}

func (d *Docker) CopyTo(ctx context.Context, name, folder string, archive io.Reader) error {
	_, err := d.cli.CopyToContainer(ctx, name, client.CopyToContainerOptions{DestinationPath: folder, Content: archive, CopyUIDGID: false})
	return notFound(err)
}

func (d *Docker) CopyFrom(ctx context.Context, name, path string) (io.ReadCloser, error) {
	copied, err := d.cli.CopyFromContainer(ctx, name, client.CopyFromContainerOptions{SourcePath: path})
	if err != nil {
		return nil, notFound(err)
	}
	return copied.Content, nil
}

func (d *Docker) ImageInspect(ctx context.Context, ref string) (ImageInfo, error) {
	result, err := d.cli.ImageInspect(ctx, ref)
	if err != nil {
		return ImageInfo{}, notFound(err)
	}
	info := ImageInfo{ID: result.ID}
	if result.Config != nil {
		info.Labels = result.Config.Labels
	}
	return info, nil
}

var _ Runtime = (*Docker)(nil)
