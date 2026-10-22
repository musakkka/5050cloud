// SPDX-License-Identifier: AGPL-3.0-only

// Package engine is the narrow view of the Docker Engine the agent runtime
// needs. Docker implements it over github.com/docker/docker/client; Fake is
// an in-memory implementation for unit tests. Keeping the surface small
// lets the reconcile, job and health logic be tested without a daemon.
package engine

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Label keys the agent sets. Containers without LabelReplica, LabelJob or
// LabelInfra are never touched.
const (
	LabelReplica     = "5050.replica"
	LabelDeployment  = "5050.deployment"
	LabelService     = "5050.service"
	LabelEnvironment = "5050.environment"
	LabelProject     = "5050.project"
	LabelEpoch       = "5050.epoch"
	// LabelSpec holds a hash of the ReplicaSpec the container was created
	// from, so a changed spec under the same replica id is recreated.
	LabelSpec = "5050.spec"
	// LabelStopGrace is the stop grace in seconds, used when the container
	// is removed after it left the desired state.
	LabelStopGrace = "5050.stopgrace"
	LabelJob       = "5050.job"
	// LabelInfra marks infrastructure containers the runtime manages
	// (the registry); its value is the component name.
	LabelInfra = "5050.infra"
	// LabelConfig is a hash of an infrastructure container's settings.
	LabelConfig = "5050.config"
	LabelVolume = "5050.volume"
)

// ErrNotFound is returned (possibly wrapped) when a container, image or
// volume does not exist.
var ErrNotFound = errors.New("not found")

// ErrUnauthorized is returned by Pull when the registry refused the credentials.
var ErrUnauthorized = errors.New("unauthorized")

// Container is what the agent needs to know about one container.
type Container struct {
	ID      string
	Name    string // without the leading slash
	Image   string // the reference it was created from
	ImageID string
	Labels  map[string]string
	// State is Docker's: "created", "running", "restarting", "paused",
	// "exited", "dead" or "removing".
	State        string
	ExitCode     int
	OOMKilled    bool
	Pid          int
	RestartCount int
	Created      time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
	// IPs maps network name to the container's address on it.
	IPs map[string]string
	// Mounts lists the volume names mounted (for disk usage sampling).
	Volumes []string
	// MemLimit is the memory limit in bytes (0 = none).
	MemLimit int64
}

// Running reports whether the container's process is up.
func (c Container) Running() bool { return c.State == "running" || c.State == "restarting" }

// MountType is the kind of a mount.
type MountType string

const (
	MountBind   MountType = "bind"
	MountVolume MountType = "volume"
	MountTmpfs  MountType = "tmpfs"
)

// Mount is one mount of a container.
type Mount struct {
	Type     MountType
	Source   string // host path, volume name, empty for tmpfs
	Target   string
	ReadOnly bool
}

// PortBinding publishes a container port on a host address.
type PortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
}

// Restart is a Docker restart policy.
type Restart struct {
	Name       string // "no" | "on-failure" | "unless-stopped" | "always"
	MaxRetries int
}

// Spec describes a container to create.
type Spec struct {
	Name       string
	Image      string
	Entrypoint []string // nil keeps the image's
	Cmd        []string // nil keeps the image's
	Env        []string // KEY=value
	Labels     map[string]string
	User       string
	WorkingDir string

	Network string // network to join ("" = Docker default bridge)
	IP      string // static IPv4 address on Network
	Aliases []string
	DNS     []string

	Mounts []Mount
	Ports  []PortBinding

	MemoryBytes       int64
	MemorySwap        int64
	MemoryReservation int64
	NanoCPUs          int64
	CgroupParent      string
	ShmSize           int64
	Restart           Restart
	StopTimeoutSec    int
	// OpenStdin keeps stdin open for AttachStdin (jobs with input).
	OpenStdin bool
	// LogMaxSize and LogMaxFile cap Docker's json-file log (9.19).
	LogMaxSize string
	LogMaxFile int
	// NoNewPrivileges sets no-new-privileges, so setuid binaries cannot
	// gain privileges inside the container (docs/SPEC.md 15).
	NoNewPrivileges bool
}

// Image is a local image.
type Image struct {
	ID      string
	Tags    []string // repo:tag
	Digests []string // repo@sha256:...
	Created time.Time
	Size    int64
}

// ImageConfig is the part of an image's config the agent reads.
type ImageConfig struct {
	Entrypoint []string
	Cmd        []string
	WorkingDir string
}

// Volume is a local volume.
type Volume struct {
	Name       string
	Mountpoint string
	Labels     map[string]string
	CreatedAt  time.Time
}

// Event is a container event.
type Event struct {
	ContainerID string
	// Action is Docker's: "start", "die", "oom", "restart", "stop", "kill",
	// "destroy", "health_status: healthy" and so on.
	Action   string
	Labels   map[string]string
	ExitCode string
	At       time.Time
}

// LogLine is one line of a container's output.
type LogLine struct {
	Stream string // "stdout" | "stderr"
	TS     time.Time
	Text   []byte // without the trailing newline
}

// ExecSpec describes a command run inside a container.
type ExecSpec struct {
	Cmd   []string
	Env   []string
	User  string
	TTY   bool
	Stdin bool
	Cols  int
	Rows  int
}

// ExecSession is a running exec. Reading Output returns the process output
// (stdout and stderr interleaved; the TTY stream when TTY is set). Writing
// Input feeds stdin; CloseInput closes it.
type ExecSession interface {
	Output() io.Reader
	Input() io.Writer
	CloseInput() error
	Resize(ctx context.Context, cols, rows int) error
	// Wait blocks until the process exits and returns its exit code.
	Wait(ctx context.Context) (int, error)
	Close() error
}

// Stats is a one-shot resource sample, the fallback when the agent cannot
// read the container's cgroup files directly (for example when it runs in a
// container itself).
type Stats struct {
	CPUNanos    uint64 // cumulative container CPU time
	SystemNanos uint64 // cumulative host CPU time
	OnlineCPUs  int
	MemBytes    int64
	MemLimit    int64
	RxBytes     int64
	TxBytes     int64
}

// Engine is the Docker Engine API subset the agent uses.
type Engine interface {
	// Info returns the engine version.
	Version(ctx context.Context) (string, error)

	// List returns every container (any state) that has the label key.
	List(ctx context.Context, labelKey string) ([]Container, error)
	Inspect(ctx context.Context, idOrName string) (Container, error)
	Create(ctx context.Context, s Spec) (string, error)
	Start(ctx context.Context, id string) error
	// Stop sends SIGTERM and SIGKILL after grace.
	Stop(ctx context.Context, id string, grace time.Duration) error
	Kill(ctx context.Context, id, signal string) error
	Remove(ctx context.Context, id string, force bool) error
	// Wait blocks until the container stops and returns its exit code.
	Wait(ctx context.Context, id string) (int, error)
	// AttachStdin returns a writer to the stdin of a created container
	// (Spec.OpenStdin); close it to send EOF.
	AttachStdin(ctx context.Context, id string) (io.WriteCloser, error)
	// Logs streams log lines (with timestamps) since the given time until
	// ctx ends, or until the container stops when follow is false.
	Logs(ctx context.Context, id string, since time.Time, follow bool, fn func(LogLine)) error
	Stats(ctx context.Context, id string) (Stats, error)

	Exec(ctx context.Context, id string, s ExecSpec) (ExecSession, error)

	ImageExists(ctx context.Context, ref string) (bool, error)
	ImageConfig(ctx context.Context, ref string) (ImageConfig, error)
	// Pull pulls ref with optional credentials. It returns an error wrapping
	// ErrNotFound or ErrUnauthorized when the registry says so.
	Pull(ctx context.Context, ref string, auth *proto.Registry) error
	Images(ctx context.Context) ([]Image, error)
	RemoveImage(ctx context.Context, id string) error
	// PruneImages removes dangling images and returns the bytes reclaimed.
	PruneImages(ctx context.Context) (int64, error)

	VolumeCreate(ctx context.Context, name string, labels map[string]string) (Volume, error)
	VolumeInspect(ctx context.Context, name string) (Volume, error)
	Volumes(ctx context.Context) ([]Volume, error)
	VolumeRemove(ctx context.Context, name string) error

	// NetworkGateway returns the IPv4 gateway of a Docker network. It
	// returns an error wrapping ErrNotFound when the network does not exist.
	NetworkGateway(ctx context.Context, name string) (string, error)

	// Events streams container events until ctx ends. The error channel
	// receives one value when the stream breaks.
	Events(ctx context.Context) (<-chan Event, <-chan error)
}
