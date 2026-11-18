// Package dockerrt is the runner's runner.Runtime for the Docker Engine API: rootless Podman through its compatible API, or Docker. It is the
// only package that knows the Docker Go SDK, and only cmd/cbe-runner imports it, so the exam server does not carry it.
package dockerrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"exam-platform/internal/runner"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Docker is the runner.Runtime that talks the Docker Engine API to rootless Podman (its compatible API) or to Docker. It is the only place
// in the repository that knows the SDK.
type Docker struct {
	cli *client.Client
}

// New connects to the runtime at host, such as unix:///run/user/<uid>/podman/podman.sock.
func New(host string) (*Docker, error) {
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
		return runner.ErrNotFound
	}
	return err
}

func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.cli.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true})
	return err
}

// hostConfig is the whole translation of a spec into the SDK's types, kept apart so a test can look at it.
func hostConfig(spec runner.ContainerSpec) (*container.Config, *container.HostConfig) {
	binds := make([]string, 0, len(spec.Binds))
	for _, bind := range spec.Binds {
		entry := bind.Source + ":" + bind.Target
		if bind.ReadOnly {
			entry += ":ro"
		}
		binds = append(binds, entry)
	}
	pids := spec.PidsLimit
	config := &container.Config{Image: spec.Image, User: spec.User, WorkingDir: spec.WorkDir, Labels: spec.Labels, Entrypoint: spec.Entrypoint, Cmd: spec.Cmd, Env: spec.Env}
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

func (d *Docker) Create(ctx context.Context, spec runner.ContainerSpec) (string, error) {
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

func inspected(response container.InspectResponse) runner.Inspected {
	out := runner.Inspected{ID: response.ID, Name: strings.TrimPrefix(response.Name, "/")}
	if response.Config != nil {
		out.Labels = response.Config.Labels
	}
	if state := response.State; state != nil {
		out.ExitCode = state.ExitCode
		out.OOMKilled = state.OOMKilled
		switch {
		case state.Paused:
			out.State = runner.StatePaused
		case state.Running:
			out.State = runner.StateRunning
		case state.Status == "created":
			out.State = runner.StateCreated
		default:
			out.State = runner.StateExited
		}
		if started, err := time.Parse(time.RFC3339Nano, state.StartedAt); err == nil && started.Year() > 1 {
			out.StartedAt = started
		}
	}
	return out
}

func (d *Docker) Inspect(ctx context.Context, name string) (runner.Inspected, error) {
	result, err := d.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return runner.Inspected{}, notFound(err)
	}
	return inspected(result.Container), nil
}

func (d *Docker) List(ctx context.Context, key, value string) ([]runner.Inspected, error) {
	list, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", key+"="+value)})
	if err != nil {
		return nil, err
	}
	var out []runner.Inspected
	for _, item := range list.Items {
		one, err := d.Inspect(ctx, item.ID)
		if errors.Is(err, runner.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

func (d *Docker) Stats(ctx context.Context, name string) (runner.RawStats, error) {
	reply, err := d.cli.ContainerStats(ctx, name, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		return runner.RawStats{}, notFound(err)
	}
	defer reply.Body.Close()
	var sample container.StatsResponse
	if err := json.NewDecoder(reply.Body).Decode(&sample); err != nil {
		return runner.RawStats{}, err
	}
	return runner.RawStats{CPUNanos: sample.CPUStats.CPUUsage.TotalUsage, MemoryBytes: sample.MemoryStats.Usage, MemoryLimit: sample.MemoryStats.Limit, Pids: sample.PidsStats.Current}, nil
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

// Wait blocks until the container is not running (it may already not be) and returns its exit code.
func (d *Docker) Wait(ctx context.Context, name string) (int, error) {
	waited := d.cli.ContainerWait(ctx, name, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-waited.Result:
		if result.Error != nil {
			return 0, errors.New(result.Error.Message)
		}
		return int(result.StatusCode), nil
	case err := <-waited.Error:
		return 0, notFound(err)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// capped keeps the first limit bytes written to it and counts the rest as cut: it still takes everything, so the stream is read to its end.
type capped struct {
	mu        sync.Mutex
	limit     int64
	data      []byte
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.limit - int64(len(c.data))
	switch {
	case room >= int64(len(p)):
		c.data = append(c.data, p...)
	case room > 0:
		c.data = append(c.data, p[:room]...)
		c.truncated = true
	default:
		c.truncated = true
	}
	return len(p), nil
}

func (c *capped) read() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data...), c.truncated
}

// attached is a connection to a container's output; the runtime sends the two streams in one, tagged, and stdcopy tells them apart.
type attached struct {
	response    client.HijackedResponse
	out, errors *capped
	done        chan error
}

// Attach connects to the container's output before it is started. What it prints is read as it prints it, to the end of the stream.
func (d *Docker) Attach(ctx context.Context, name string, limit int64) (runner.Collector, error) {
	result, err := d.cli.ContainerAttach(ctx, name, client.ContainerAttachOptions{Stream: true, Stdout: true, Stderr: true})
	if err != nil {
		return nil, notFound(err)
	}
	a := &attached{response: result.HijackedResponse, out: &capped{limit: limit}, errors: &capped{limit: limit}, done: make(chan error, 1)}
	go func() {
		_, err := stdcopy.StdCopy(a.out, a.errors, a.response.Reader)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		a.done <- err
	}()
	return a, nil
}

func (a *attached) Collect(ctx context.Context) (runner.Output, error) {
	var err error
	select {
	case err = <-a.done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	stdout, stdoutCut := a.out.read()
	stderr, stderrCut := a.errors.read()
	return runner.Output{Stdout: stdout, Stderr: stderr, StdoutTruncated: stdoutCut, StderrTruncated: stderrCut}, err
}

func (a *attached) Close() error {
	a.response.Close()
	return nil
}

func (d *Docker) ImageInspect(ctx context.Context, ref string) (runner.ImageInfo, error) {
	result, err := d.cli.ImageInspect(ctx, ref)
	if err != nil {
		return runner.ImageInfo{}, notFound(err)
	}
	info := runner.ImageInfo{ID: result.ID, RepoDigests: result.RepoDigests, Layers: result.RootFS.Layers}
	if result.Config != nil {
		info.Labels = result.Config.Labels
		info.User, info.Entrypoint, info.Cmd, info.WorkDir = result.Config.User, result.Config.Entrypoint, result.Config.Cmd, result.Config.WorkingDir
		for volume := range result.Config.Volumes {
			info.Volumes = append(info.Volumes, volume)
		}
		sort.Strings(info.Volumes)
	}
	return info, nil
}

// ImageBuild builds the Containerfile in a tar context through the runtime's build call. The runtime answers with a stream of JSON objects:
// {"stream": text} for what the build prints and {"error": …} when a step fails; the text goes to log as it comes.
func (d *Docker) ImageBuild(ctx context.Context, spec runner.ImageBuildSpec, buildContext io.Reader, log io.Writer) error {
	response, err := d.cli.ImageBuild(ctx, buildContext, client.ImageBuildOptions{Tags: []string{spec.Tag}, Dockerfile: "Containerfile", Remove: true, ForceRemove: true,
		NoCache: spec.NoCache, Labels: spec.Labels, Memory: spec.MemoryBytes, MemorySwap: spec.MemoryBytes})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	for {
		var line struct {
			Stream      string `json:"stream"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := decoder.Decode(&line); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if line.Stream != "" {
			_, _ = io.WriteString(log, line.Stream)
		}
		if message := strings.TrimSpace(line.Error + line.ErrorDetail.Message); message != "" {
			if line.Error != "" && line.ErrorDetail.Message != "" {
				message = strings.TrimSpace(line.Error)
			}
			return fmt.Errorf("%w: %s", runner.ErrBuildFailed, message)
		}
	}
}

func (d *Docker) ImageTag(ctx context.Context, ref, tag string) error {
	_, err := d.cli.ImageTag(ctx, client.ImageTagOptions{Source: ref, Target: tag})
	return notFound(err)
}

func (d *Docker) ImageRemove(ctx context.Context, ref string) error {
	_, err := d.cli.ImageRemove(ctx, ref, client.ImageRemoveOptions{})
	return notFound(err)
}

var _ runner.Runtime = (*Docker)(nil)
