// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Docker implements Engine with the Docker Engine API. It negotiates the
// API version, so it works with Docker 24 to 29.
type Docker struct {
	c *client.Client
}

// NewDocker connects to the engine at host ("unix:///var/run/docker.sock";
// empty uses DOCKER_HOST or the default socket).
func NewDocker(host string) (*Docker, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("could not set up the Docker client for %s: %w", host, err)
	}
	return &Docker{c: c}, nil
}

// Client exposes the underlying client (integration tests).
func (d *Docker) Client() *client.Client { return d.c }

func wrapNotFound(err error) error {
	if err != nil && cerrdefs.IsNotFound(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	}
	return err
}

func (d *Docker) Version(ctx context.Context) (string, error) {
	v, err := d.c.ServerVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("could not reach the Docker Engine: %w", err)
	}
	return v.Version, nil
}

func (d *Docker) List(ctx context.Context, labelKey string) ([]Container, error) {
	list, err := d.c.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(filters.Arg("label", labelKey))})
	if err != nil {
		return nil, fmt.Errorf("could not list containers: %w", err)
	}
	out := make([]Container, 0, len(list))
	for _, s := range list {
		c, err := d.Inspect(ctx, s.ID)
		if errors.Is(err, ErrNotFound) {
			continue // removed in between
		}
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseDockerTime(s string) time.Time {
	if s == "" || strings.HasPrefix(s, "0001-") {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (d *Docker) Inspect(ctx context.Context, id string) (Container, error) {
	r, err := d.c.ContainerInspect(ctx, id)
	if err != nil {
		return Container{}, wrapNotFound(err)
	}
	c := Container{ID: r.ID, Name: strings.TrimPrefix(r.Name, "/"), ImageID: r.Image, IPs: map[string]string{}}
	if r.ContainerJSONBase != nil {
		c.RestartCount = r.RestartCount
		c.Created = parseDockerTime(r.Created)
		if r.State != nil {
			c.State = r.State.Status
			c.ExitCode = r.State.ExitCode
			c.OOMKilled = r.State.OOMKilled
			c.Pid = r.State.Pid
			c.StartedAt = parseDockerTime(r.State.StartedAt)
			c.FinishedAt = parseDockerTime(r.State.FinishedAt)
		}
		if r.HostConfig != nil {
			c.MemLimit = r.HostConfig.Memory
		}
	}
	if r.Config != nil {
		c.Image = r.Config.Image
		c.Labels = r.Config.Labels
	}
	if r.NetworkSettings != nil {
		for name, ep := range r.NetworkSettings.Networks {
			if ep != nil {
				c.IPs[name] = ep.IPAddress
			}
		}
	}
	for _, m := range r.Mounts {
		if m.Type == mount.TypeVolume {
			c.Volumes = append(c.Volumes, m.Name)
		}
	}
	return c, nil
}

func (d *Docker) Create(ctx context.Context, s Spec) (string, error) {
	cfg := &container.Config{
		Image:       s.Image,
		Env:         s.Env,
		Labels:      s.Labels,
		User:        s.User,
		WorkingDir:  s.WorkingDir,
		OpenStdin:   s.OpenStdin,
		StdinOnce:   s.OpenStdin,
		AttachStdin: s.OpenStdin,
	}
	if s.Entrypoint != nil {
		cfg.Entrypoint = s.Entrypoint
	}
	if s.Cmd != nil {
		cfg.Cmd = s.Cmd
		if s.Entrypoint == nil && len(s.Cmd) > 1 {
			// An image whose entrypoint is a shell with -c (Railpack builds use
			// ["/bin/bash", "-c"]) takes the command as one string; as an argv
			// the shell would run only the first word.
			if r, err := d.c.ImageInspect(ctx, s.Image); err == nil && r.Config != nil && ShellCEntrypoint(r.Config.Entrypoint) {
				cfg.Cmd = []string{ShellJoin(s.Cmd)}
			}
		}
	}
	if s.StopTimeoutSec > 0 {
		t := s.StopTimeoutSec
		cfg.StopTimeout = &t
	}
	hc := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(s.Restart.Name), MaximumRetryCount: s.Restart.MaxRetries},
		DNS:           s.DNS,
		ShmSize:       s.ShmSize,
		Resources: container.Resources{
			Memory:            s.MemoryBytes,
			MemorySwap:        s.MemorySwap,
			MemoryReservation: s.MemoryReservation,
			NanoCPUs:          s.NanoCPUs,
			CgroupParent:      s.CgroupParent,
		},
	}
	if s.Restart.Name == "" {
		hc.RestartPolicy.Name = container.RestartPolicyDisabled
	}
	if s.NoNewPrivileges {
		hc.SecurityOpt = append(hc.SecurityOpt, "no-new-privileges:true")
	}
	if s.LogMaxSize != "" {
		hc.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": s.LogMaxSize, "max-file": strconv.Itoa(max(1, s.LogMaxFile))}}
	}
	for _, m := range s.Mounts {
		dm := mount.Mount{Type: mount.Type(m.Type), Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly}
		hc.Mounts = append(hc.Mounts, dm)
	}
	if len(s.Ports) > 0 {
		cfg.ExposedPorts = nat.PortSet{}
		hc.PortBindings = nat.PortMap{}
		for _, p := range s.Ports {
			port := nat.Port(strconv.Itoa(p.ContainerPort) + "/tcp")
			cfg.ExposedPorts[port] = struct{}{}
			hc.PortBindings[port] = append(hc.PortBindings[port], nat.PortBinding{HostIP: p.HostIP, HostPort: strconv.Itoa(p.HostPort)})
		}
	}
	var nc *network.NetworkingConfig
	if s.Network != "" {
		ep := &network.EndpointSettings{Aliases: s.Aliases}
		if s.IP != "" {
			ep.IPAMConfig = &network.EndpointIPAMConfig{IPv4Address: s.IP}
		}
		nc = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{s.Network: ep}}
		hc.NetworkMode = container.NetworkMode(s.Network)
	}
	r, err := d.c.ContainerCreate(ctx, cfg, hc, nc, nil, s.Name)
	if err != nil {
		return "", fmt.Errorf("could not create container %s: %w", s.Name, wrapNotFound(err))
	}
	return r.ID, nil
}

func (d *Docker) Start(ctx context.Context, id string) error {
	if err := d.c.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("could not start container %s: %w", short(id), wrapNotFound(err))
	}
	return nil
}

func (d *Docker) Stop(ctx context.Context, id string, grace time.Duration) error {
	secs := int(grace / time.Second)
	if err := d.c.ContainerStop(ctx, id, container.StopOptions{Timeout: &secs}); err != nil {
		return fmt.Errorf("could not stop container %s: %w", short(id), wrapNotFound(err))
	}
	return nil
}

func (d *Docker) Kill(ctx context.Context, id, signal string) error {
	if err := d.c.ContainerKill(ctx, id, signal); err != nil {
		return fmt.Errorf("could not signal container %s: %w", short(id), wrapNotFound(err))
	}
	return nil
}

func (d *Docker) Remove(ctx context.Context, id string, force bool) error {
	if err := d.c.ContainerRemove(ctx, id, container.RemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("could not remove container %s: %w", short(id), wrapNotFound(err))
	}
	return nil
}

func (d *Docker) Wait(ctx context.Context, id string) (int, error) {
	okc, errc := d.c.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case r := <-okc:
		if r.Error != nil && r.Error.Message != "" {
			return int(r.StatusCode), errors.New(r.Error.Message)
		}
		return int(r.StatusCode), nil
	case err := <-errc:
		return -1, wrapNotFound(err)
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

type hijackWriter struct {
	conn net.Conn
	cw   interface{ CloseWrite() error }
}

func (h hijackWriter) Write(p []byte) (int, error) { return h.conn.Write(p) }
func (h hijackWriter) Close() error {
	if h.cw != nil {
		_ = h.cw.CloseWrite()
	}
	return h.conn.Close()
}

func (d *Docker) AttachStdin(ctx context.Context, id string) (io.WriteCloser, error) {
	r, err := d.c.ContainerAttach(ctx, id, container.AttachOptions{Stream: true, Stdin: true})
	if err != nil {
		return nil, fmt.Errorf("could not attach to container %s: %w", short(id), wrapNotFound(err))
	}
	cw, _ := r.Conn.(interface{ CloseWrite() error })
	return hijackWriter{conn: r.Conn, cw: cw}, nil
}

func (d *Docker) Logs(ctx context.Context, id string, since time.Time, follow bool, fn func(LogLine)) error {
	opts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: follow, Timestamps: true}
	if !since.IsZero() {
		opts.Since = strconv.FormatFloat(float64(since.UnixNano())/1e9, 'f', 9, 64)
	}
	rc, err := d.c.ContainerLogs(ctx, id, opts)
	if err != nil {
		return fmt.Errorf("could not read the logs of container %s: %w", short(id), wrapNotFound(err))
	}
	defer rc.Close()
	return DemuxLines(rc, true, fn)
}

// DemuxLines reads Docker's multiplexed stream (8-byte frame headers) and
// calls fn once per complete line, keeping stdout and stderr apart. With
// timestamps, each line starts with an RFC 3339 time and a space.
func DemuxLines(r io.Reader, timestamps bool, fn func(LogLine)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var hdr [8]byte
	partial := map[byte][]byte{}
	emit := func(stream byte, line []byte) {
		ll := LogLine{Stream: "stdout"}
		if stream == 2 {
			ll.Stream = "stderr"
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if timestamps {
			if sp := bytes.IndexByte(line, ' '); sp > 0 {
				if t, err := time.Parse(time.RFC3339Nano, string(line[:sp])); err == nil {
					ll.TS = t
					line = line[sp+1:]
				}
			}
		}
		ll.Text = line
		fn(ll)
	}
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			for s, p := range partial {
				if len(p) > 0 {
					emit(s, p)
				}
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		stream := hdr[0]
		size := binary.BigEndian.Uint32(hdr[4:])
		buf := make([]byte, size)
		if _, err := io.ReadFull(br, buf); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		data := append(partial[stream], buf...)
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			emit(stream, data[:i])
			data = data[i+1:]
		}
		// Keep an unfinished line, capped so a stream without newlines cannot grow without bound.
		if len(data) > 1<<20 {
			emit(stream, data)
			data = nil
		}
		partial[stream] = append([]byte(nil), data...)
	}
}

func (d *Docker) Stats(ctx context.Context, id string) (Stats, error) {
	r, err := d.c.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return Stats{}, wrapNotFound(err)
	}
	defer r.Body.Close()
	var s container.StatsResponse
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		return Stats{}, fmt.Errorf("could not read stats of container %s: %w", short(id), err)
	}
	out := Stats{
		CPUNanos:    s.CPUStats.CPUUsage.TotalUsage,
		SystemNanos: s.CPUStats.SystemUsage,
		OnlineCPUs:  int(s.CPUStats.OnlineCPUs),
		MemBytes:    int64(s.MemoryStats.Usage),
		MemLimit:    int64(s.MemoryStats.Limit),
	}
	for _, n := range s.Networks {
		out.RxBytes += int64(n.RxBytes)
		out.TxBytes += int64(n.TxBytes)
	}
	return out, nil
}

type dockerExec struct {
	d      *Docker
	id     string
	conn   net.Conn
	out    io.Reader
	closed sync.Once
}

func (e *dockerExec) Output() io.Reader { return e.out }
func (e *dockerExec) Input() io.Writer  { return e.conn }
func (e *dockerExec) CloseInput() error {
	if cw, ok := e.conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (e *dockerExec) Resize(ctx context.Context, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return e.d.c.ContainerExecResize(ctx, e.id, container.ResizeOptions{Width: uint(cols), Height: uint(rows)})
}

func (e *dockerExec) Wait(ctx context.Context) (int, error) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		r, err := e.d.c.ContainerExecInspect(ctx, e.id)
		if err != nil {
			return -1, err
		}
		if !r.Running {
			return r.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-t.C:
		}
	}
}

func (e *dockerExec) Close() error {
	var err error
	e.closed.Do(func() { err = e.conn.Close() })
	return err
}

func (d *Docker) Exec(ctx context.Context, id string, s ExecSpec) (ExecSession, error) {
	opts := container.ExecOptions{
		Cmd: s.Cmd, Env: s.Env, User: s.User, Tty: s.TTY,
		AttachStdin: s.Stdin, AttachStdout: true, AttachStderr: true,
	}
	var size *[2]uint
	if s.TTY && s.Cols > 0 && s.Rows > 0 {
		size = &[2]uint{uint(s.Rows), uint(s.Cols)}
		opts.ConsoleSize = size
	}
	cr, err := d.c.ContainerExecCreate(ctx, id, opts)
	if err != nil {
		return nil, fmt.Errorf("could not start a command in container %s: %w", short(id), wrapNotFound(err))
	}
	hr, err := d.c.ContainerExecAttach(ctx, cr.ID, container.ExecAttachOptions{Tty: s.TTY, ConsoleSize: size})
	if err != nil {
		return nil, fmt.Errorf("could not attach to a command in container %s: %w", short(id), err)
	}
	e := &dockerExec{d: d, id: cr.ID, conn: hr.Conn}
	if s.TTY {
		e.out = hr.Reader
	} else {
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(demuxRaw(hr.Reader, pw))
		}()
		e.out = pr
	}
	return e, nil
}

// demuxRaw copies the payload of a multiplexed stream to w, interleaving
// stdout and stderr.
func demuxRaw(r io.Reader, w io.Writer) error {
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		n := int64(binary.BigEndian.Uint32(hdr[4:]))
		if _, err := io.CopyN(w, r, n); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (d *Docker) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, err := d.c.ImageInspect(ctx, ref)
	if err == nil {
		return true, nil
	}
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("could not inspect image %s: %w", ref, err)
}

func (d *Docker) ImageConfig(ctx context.Context, ref string) (ImageConfig, error) {
	r, err := d.c.ImageInspect(ctx, ref)
	if err != nil {
		return ImageConfig{}, wrapNotFound(err)
	}
	if r.Config == nil {
		return ImageConfig{}, nil
	}
	return ImageConfig{Entrypoint: r.Config.Entrypoint, Cmd: r.Config.Cmd, WorkingDir: r.Config.WorkingDir}, nil
}

// pullMessage is one line of the pull progress stream.
type pullMessage struct {
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// ClassifyPullError maps a registry error text to ErrNotFound or
// ErrUnauthorized when it says so.
func ClassifyPullError(msg string) error {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "manifest unknown"), strings.Contains(low, "not found"),
		strings.Contains(low, "does not exist"), strings.Contains(low, "name unknown"):
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	case strings.Contains(low, "unauthorized"), strings.Contains(low, "denied"),
		strings.Contains(low, "authentication required"), strings.Contains(low, "no basic auth credentials"):
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	}
	return errors.New(msg)
}

func (d *Docker) Pull(ctx context.Context, ref string, auth *proto.Registry) error {
	opts := image.PullOptions{}
	if auth != nil && auth.Username != "" {
		enc, err := registry.EncodeAuthConfig(registry.AuthConfig{Username: auth.Username, Password: auth.Password, ServerAddress: auth.Address})
		if err != nil {
			return err
		}
		opts.RegistryAuth = enc
	}
	rc, err := d.c.ImagePull(ctx, ref, opts)
	if err != nil {
		return ClassifyPullError(err.Error())
	}
	defer rc.Close()
	dec := json.NewDecoder(rc)
	for {
		var m pullMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("the pull of %s was interrupted: %w", ref, err)
		}
		if m.Error != "" {
			return ClassifyPullError(m.Error)
		}
	}
}

func (d *Docker) Images(ctx context.Context) ([]Image, error) {
	list, err := d.c.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not list images: %w", err)
	}
	out := make([]Image, 0, len(list))
	for _, s := range list {
		out = append(out, Image{ID: s.ID, Tags: s.RepoTags, Digests: s.RepoDigests, Created: time.Unix(s.Created, 0), Size: s.Size})
	}
	return out, nil
}

func (d *Docker) RemoveImage(ctx context.Context, id string) error {
	_, err := d.c.ImageRemove(ctx, id, image.RemoveOptions{PruneChildren: true})
	if err != nil {
		return fmt.Errorf("could not remove image %s: %w", short(id), wrapNotFound(err))
	}
	return nil
}

func (d *Docker) PruneImages(ctx context.Context) (int64, error) {
	r, err := d.c.ImagesPrune(ctx, filters.NewArgs(filters.Arg("dangling", "true")))
	if err != nil {
		return 0, fmt.Errorf("could not prune images: %w", err)
	}
	return int64(r.SpaceReclaimed), nil
}

func toVolume(v volume.Volume) Volume {
	return Volume{Name: v.Name, Mountpoint: v.Mountpoint, Labels: v.Labels, CreatedAt: parseDockerTime(v.CreatedAt)}
}

func (d *Docker) VolumeCreate(ctx context.Context, name string, labels map[string]string) (Volume, error) {
	v, err := d.c.VolumeCreate(ctx, volume.CreateOptions{Name: name, Driver: "local", Labels: labels})
	if err != nil {
		return Volume{}, fmt.Errorf("could not create volume %s: %w", name, err)
	}
	return toVolume(v), nil
}

func (d *Docker) VolumeInspect(ctx context.Context, name string) (Volume, error) {
	v, err := d.c.VolumeInspect(ctx, name)
	if err != nil {
		return Volume{}, wrapNotFound(err)
	}
	return toVolume(v), nil
}

func (d *Docker) Volumes(ctx context.Context) ([]Volume, error) {
	r, err := d.c.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not list volumes: %w", err)
	}
	out := make([]Volume, 0, len(r.Volumes))
	for _, v := range r.Volumes {
		if v != nil {
			out = append(out, toVolume(*v))
		}
	}
	return out, nil
}

func (d *Docker) VolumeRemove(ctx context.Context, name string) error {
	if err := d.c.VolumeRemove(ctx, name, false); err != nil {
		return fmt.Errorf("could not remove volume %s: %w", name, wrapNotFound(err))
	}
	return nil
}

func (d *Docker) NetworkGateway(ctx context.Context, name string) (string, error) {
	n, err := d.c.NetworkInspect(ctx, name, network.InspectOptions{})
	if err != nil {
		return "", fmt.Errorf("could not inspect network %s: %w", name, wrapNotFound(err))
	}
	for _, c := range n.IPAM.Config {
		if ip := net.ParseIP(c.Gateway); ip != nil && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("the network %s has no IPv4 gateway", name)
}

func (d *Docker) Events(ctx context.Context) (<-chan Event, <-chan error) {
	msgs, errs := d.c.Events(ctx, events.ListOptions{Filters: filters.NewArgs(filters.Arg("type", string(events.ContainerEventType)))})
	out := make(chan Event, 64)
	oerr := make(chan error, 1)
	go func() {
		defer close(out)
		for {
			select {
			case m, ok := <-msgs:
				if !ok {
					oerr <- errors.New("the Docker event stream closed")
					return
				}
				ev := Event{ContainerID: m.Actor.ID, Action: string(m.Action), Labels: m.Actor.Attributes, At: time.Unix(0, m.TimeNano)}
				if m.Actor.Attributes != nil {
					ev.ExitCode = m.Actor.Attributes["exitCode"]
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case err := <-errs:
				oerr <- err
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, oerr
}

func short(id string) string {
	if len(id) > 12 && !strings.HasPrefix(id, "5050-") {
		return id[:12]
	}
	return id
}

var _ Engine = (*Docker)(nil)

// ShellCEntrypoint reports whether an entrypoint is a shell run with -c.
func ShellCEntrypoint(ep []string) bool {
	if len(ep) != 2 || ep[1] != "-c" {
		return false
	}
	base := ep[0][strings.LastIndexByte(ep[0], '/')+1:]
	return base == "sh" || base == "bash" || base == "ash" || base == "dash"
}

// ShellJoin quotes each argument for a POSIX shell and joins them.
func ShellJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@,+%") == "" {
			out[i] = a
			continue
		}
		out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}
