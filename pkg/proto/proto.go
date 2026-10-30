// SPDX-License-Identifier: MIT

// Package proto is the wire contract between 5050d (control plane) and
// 5050-agent. It is frozen for v0.1; see docs/SPEC.md section 7.
//
// Transport: the agent dials out. One WebSocket per agent at
//
//	wss://<console>/api/agent/v1/connect   (also wss://agents.<console>/...)
//	Authorization: Bearer <host token>
//	X-5050-Agent-Version: v0.1.0
//
// v0.1 has no agent mTLS. The channel is TLS to the console certificate (or a
// pinned internal CA certificate) plus a host token that is bound to the
// host's machine-id and WireGuard public key and rotated every 30 days
// (TTokenRotate).
//
// Text frames carry one JSON Envelope each, at most MaxMessageBytes, with
// per-message deflate. Binary frames carry stream data for exec sessions and
// TCP tunnels: byte 0 = BinExec|BinTCP, bytes 1..8 = stream id (uint64 big
// endian), rest = payload. A zero-length payload on a TCP stream means
// half-close. Each side keeps at most 1 MiB unacknowledged per stream.
//
// Keepalive: WebSocket ping every 20 s from both sides; 60 s silence = dead.
// Reconnect: exponential backoff 1 s to 30 s with full jitter. Containers keep
// running while the agent or control plane is down.
//
// Desired state: the control plane owns what should run. After hello it sends
// HelloAck and then a full DesiredState with
// Generation = max(stored generation for the host, Hello.Applied) + 1, and it
// re-sends the full state on every change (coalesced to one push per 250 ms
// per host). The agent reconciles to it idempotently and reports
// StateApplied. The agent never removes a container before it has received a
// DesiredState on the current connection, refuses a lower Generation unless
// Epoch changed, and with Adopt set removes nothing (docs/SPEC.md 7.2,
// 9.17.1). Builds, jobs, log queries, exec and tunnels are requests with an
// Envelope.ID; their replies carry Ref = that ID.
package proto

import (
	"encoding/json"

	"github.com/musakkka/5050cloud/pkg/manifest"
)

// ProtocolVersion is the wire protocol version. The control plane advertises
// it in HelloAck; a newer agent speaks the advertised version and ignores
// fields it does not get (docs/SPEC.md 9.16).
const ProtocolVersion = 1

// MaxMessageBytes caps one text frame (a 2,000 replica state is about 3 MB).
const MaxMessageBytes = 16 << 20

const (
	BinExec byte = 1
	BinTCP  byte = 2
)

// WebSocket close reasons sent by the control plane when it refuses a
// connection. The agent shows them in its log and does not retry
// DuplicateHost or UnknownHost faster than once a minute.
const (
	CloseDuplicateHost = "duplicate_host" // another machine already uses this host identity (7.1)
	CloseUnknownHost   = "unknown_host"   // token not in the restored state (9.17.1)
	CloseRevoked       = "revoked"        // host removed
)

// Envelope is the JSON text frame.
type Envelope struct {
	T   string          `json:"t"`             // message type, constants below
	ID  string          `json:"id,omitempty"`  // request id (sender-unique)
	Ref string          `json:"ref,omitempty"` // id of the request this answers
	D   json.RawMessage `json:"d,omitempty"`   // payload
}

// Message types. "cp>" = control plane to agent, "a>" = agent to control plane.
const (
	// a> first frame after connect (Hello)
	THello = "hello"
	// cp> reply to hello (HelloAck)
	THelloAck = "hello_ack"
	// cp> full desired state for this host (DesiredState)
	TDesired = "desired_state"
	// a> reconcile result for a generation (StateApplied)
	TStateApplied = "state_applied"
	// a> replica lifecycle change, also re-sent for all replicas after hello (ReplicaStatus)
	TReplicaStatus = "replica_status"
	// a> host, container, HTTP and database samples every 30 s (Metrics)
	TMetrics = "metrics"
	// a> folded container and host events (Event)
	TEvent = "event"

	// cp> start a build (BuildJob); a> build_log (many) then build_result
	TBuild       = "build"
	TBuildLog    = "build_log"
	TBuildResult = "build_result"
	TBuildCancel = "build_cancel"

	// cp> run a one-off container (Job): pre-deploy, cron run, backup,
	// restore, restore test, retention, exec in a replica
	TJob       = "job"
	TJobLog    = "job_log"
	TJobResult = "job_result"
	TJobCancel = "job_cancel"

	// cp> query stored logs (LogsQuery); a> logs_result (LogsResult)
	TLogsQuery  = "logs_query"
	TLogsResult = "logs_result"
	// cp> live tail (LogsSubscribe); a> log_lines (LogLines) until unsubscribe
	TLogsSubscribe   = "logs_subscribe"
	TLogsUnsubscribe = "logs_unsubscribe"
	TLogLines        = "log_lines"
	// a> every 10 s: error-level and deploy-log lines of the last batch, at
	// most 200 lines (HostLogsTail). The control plane keeps 15 minutes per
	// host to answer queries while the host is offline (9.4).
	THostLogsTail = "host_logs_tail"

	// cp> open an interactive exec in a replica; data flows as BinExec frames
	TExecOpen   = "exec_open"
	TExecResize = "exec_resize"
	TExecClose  = "exec_close"
	TExecExit   = "exec_exit" // a>

	// cp> open a TCP stream to a replica port (5050 connect); data as BinTCP frames
	TTCPOpen  = "tcp_open"
	TTCPClose = "tcp_close"

	// cp> self-upgrade the agent (Upgrade)
	TUpgrade = "upgrade"

	// a> the ACME primary ingress host issued or renewed a certificate (CertIssued)
	TCertIssued = "cert_issued"
	// a> the ACME primary's certificate states changed (CertStatus)
	TCertStatus = "cert_status"
	// cp> new host token over the open socket (TokenRotate); a> ack after it is on disk
	TTokenRotate = "token_rotate"
	// cp> host maintenance order (Maintenance, 9.16); a> ack when done
	TMaintenance = "maintenance"

	// generic a> reply for requests without a richer result (Ack)
	TAck = "ack"
)

// ---------------------------------------------------------------- join

// JoinRequest: POST /api/agent/v1/join (no auth header; Token is single-use,
// valid 1 hour, rate limited to 10 joins per minute per IP).
type JoinRequest struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
	Facts    Facts  `json:"facts"`
	// MachineID is /etc/machine-id. The host identity is bound to it and to
	// WGPublicKey; a second machine presenting the same token is refused (7.1).
	MachineID   string `json:"machineId"`
	WGPublicKey string `json:"wgPublicKey"`
	// ReplaceHostID re-adopts an existing host record (rebuild of a dead
	// control host, 9.17.2). Needs a one-time token printed by restore-state.
	ReplaceHostID string `json:"replaceHostId,omitempty"`
}

type JoinResponse struct {
	HostID    string   `json:"hostId"`
	HostToken string   `json:"hostToken"` // stored 0600 in /etc/5050/agent.json; server keeps a SHA-256
	Roles     []string `json:"roles"`
	Mesh      MeshSelf `json:"mesh"`
	// RegistryAddress is what the installer writes to Docker's
	// insecure-registries on joined hosts ("<control mesh ip>:5000").
	RegistryAddress string `json:"registryAddress"`
	Epoch           string `json:"epoch"`
}

type Facts struct {
	Version   string `json:"version"`
	Arch      string `json:"arch"`
	OS        string `json:"os"` // "ubuntu 24.04"
	Kernel    string `json:"kernel"`
	CPUs      int    `json:"cpus"`
	MemBytes  int64  `json:"memBytes"`
	Docker    string `json:"docker"` // engine version
	PublicIP  string `json:"publicIp"`
	EgressIP  string `json:"egressIp"`
	DataDir   string `json:"dataDir"` // "/var/lib/5050" (bound to the data disk when there is one)
	Disks     []Disk `json:"disks"`
	WireGuard bool   `json:"wireguard"` // kernel module available
	// RebootRequired is set when the OS reports a pending kernel update (shown on the Hosts page).
	RebootRequired bool `json:"rebootRequired"`
}

type Disk struct {
	Mount string `json:"mount"`
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

// ---------------------------------------------------------------- hello

type Hello struct {
	HostID      string `json:"hostId"`
	MachineID   string `json:"machineId"`
	WGPublicKey string `json:"wgPublicKey"`
	Facts       Facts  `json:"facts"`
	// Epoch is the cluster epoch of the last applied desired state.
	Epoch   string `json:"epoch"`
	Applied int64  `json:"applied"` // last generation applied
	// Cached is true when the agent booted from its local desired-state
	// cache and has not yet received a fresh state (7.2).
	Cached bool `json:"cached"`
	// Replicas lists every container labelled 5050.replica on the host,
	// including ones not in the last desired state.
	Replicas []ReplicaStatus `json:"replicas"`
}

type HelloAck struct {
	HostID          string `json:"hostId"`
	ServerVersion   string `json:"serverVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
	Epoch           string `json:"epoch"`
	// UpgradeTo is set when the agent should upgrade (a TUpgrade follows).
	UpgradeTo string `json:"upgradeTo,omitempty"`
}

// TokenRotate replaces the host token. The agent writes it to agent.json,
// then answers TAck. The old token stays valid for 10 minutes.
type TokenRotate struct {
	Token string `json:"token"`
}

// ---------------------------------------------------------------- desired state

type DesiredState struct {
	Generation int64 `json:"generation"`
	// Epoch changes when the control plane state is restored (9.17). A state
	// with a new epoch is accepted even when its generation is lower.
	Epoch string `json:"epoch"`
	// Adopt: apply additions and changes, remove nothing, and report
	// containers that are not in the state in StateApplied.Unknown (9.17.1).
	Adopt      bool          `json:"adopt"`
	Host       HostConfig    `json:"host"`
	Networks   []Network     `json:"networks"`
	Replicas   []ReplicaSpec `json:"replicas"`
	Ingress    *Ingress      `json:"ingress,omitempty"` // only for hosts with the ingress role
	TCPProxies []TCPProxy    `json:"tcpProxies,omitempty"`
	Mesh       Mesh          `json:"mesh"`
	DNS        []DNSEntry    `json:"dns"` // private names this host's resolvers answer
	Volumes    []VolumeSpec  `json:"volumes"`
}

type HostConfig struct {
	Roles []string `json:"roles"`
	// RegistryPull is the pull-only credential every host gets.
	RegistryPull Registry `json:"registryPull"`
	// RegistryPush is the push credential, set only on hosts with the build role.
	RegistryPush     *Registry `json:"registryPush,omitempty"`
	ImageRetention   int       `json:"imageRetention"`   // images kept per service on this host, default 3
	LogRetentionDays int       `json:"logRetentionDays"` // default 7
	// LogMaxBytesPerService caps the agent log store per service on this host (default 1 GiB).
	LogMaxBytesPerService int64 `json:"logMaxBytesPerService"`
	// LogMaxPctFS caps the whole agent log store as a share of the filesystem (default 10).
	LogMaxPctFS    int  `json:"logMaxPctFs"`
	DiskPruneAtPct int  `json:"diskPruneAtPct"` // default 80
	Drain          bool `json:"drain"`
	// Maintenance is true while the host is in maintenance (9.16).
	Maintenance bool `json:"maintenance"`
	// Tools lists pinned helper binaries the agent fetches and verifies (railpack, wal-g, restic).
	Tools []Tool `json:"tools"`
	// Images pins infrastructure images (caddy, buildkit, railpack frontend,
	// registry) or points them at mirrors; empty keys use the agent's pins.
	Images map[string]string `json:"images"`
	// PodCIDR and MeshCIDR default to 10.128.0.0/9 and 100.96.0.0/16.
	PodCIDR  string `json:"podCidr"`
	MeshCIDR string `json:"meshCidr"`
	// BuildTimeoutMin is the install default when a service sets none (default 30).
	BuildTimeoutMin int `json:"buildTimeoutMin"`
}

type Tool struct {
	Name    string `json:"name"` // "railpack" | "wal-g" | "restic" | "litestream"
	Version string `json:"version"`
	URL     string `json:"url"`    // per-arch download URL
	SHA256  string `json:"sha256"` // of the downloaded file
	Path    string `json:"path"`   // path inside the archive, empty when the URL is the binary
}

type Registry struct {
	Address  string `json:"address"` // "127.0.0.1:5000" on the control host, "<mesh ip>:5000" elsewhere
	Username string `json:"username"`
	Password string `json:"password"`
	Insecure bool   `json:"insecure"` // plain HTTP inside the mesh
}

// Network is one environment's private network on this host.
type Network struct {
	EnvironmentID string `json:"environmentId"`
	Name          string `json:"name"`    // docker network name "5050-<envid>"
	Subnet        string `json:"subnet"`  // this host's /24 for the environment, e.g. "10.130.4.0/24"
	Gateway       string `json:"gateway"` // first address; the agent's DNS listens here
	Egress        string `json:"egress"`  // "allowed" | "blocked"
	// PeerSubnets are this environment's subnets on other hosts (routed over the mesh).
	PeerSubnets []string `json:"peerSubnets"`
	// EgressAllow lists CIDRs or hostnames allowed when Egress is blocked.
	EgressAllow []string `json:"egressAllow,omitempty"`
	// AllowMetadata lists replica IPs in this network whose service has
	// networking.allowMetadata (they may reach 169.254.169.254 and the mesh).
	AllowMetadata []string `json:"allowMetadata,omitempty"`
	// MTU is the bridge MTU; 0 means Docker's default (single host).
	MTU int `json:"mtu,omitempty"`
}

// ReplicaSpec is one container that must run on this host.
type ReplicaSpec struct {
	ID            string `json:"id"` // "rep_..."; container name "5050-<id>"
	DeploymentID  string `json:"deploymentId"`
	ServiceID     string `json:"serviceId"`
	EnvironmentID string `json:"environmentId"`
	ProjectID     string `json:"projectId"`
	ServiceName   string `json:"serviceName"`
	Image         string `json:"image"` // registry ref with digest
	// Cmd/Entrypoint are argv arrays; never shell strings built by us. A
	// start command with shell operators arrives as ["sh","-c",cmd].
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	Env        map[string]string `json:"env"` // fully resolved
	Port       int               `json:"port"`
	IP         string            `json:"ip"` // static IP inside Network.Subnet, assigned by the control plane
	Network    string            `json:"network"`
	Aliases    []string          `json:"aliases"`
	Health     HealthCheck       `json:"health"`
	Restart    RestartPolicy     `json:"restart"`
	CPULimit   float64           `json:"cpuLimit,omitempty"`
	MemLimitMB int               `json:"memLimitMb,omitempty"`
	// MemoryReservation is Docker's --memory-reservation in bytes (0 = none).
	MemoryReservation int64 `json:"memoryReservation,omitempty"`
	// MemorySwap is Docker's --memory-swap in bytes; equal to the memory
	// limit so an out-of-memory app is killed instead of swapping (8.1).
	MemorySwap int64 `json:"memorySwap,omitempty"`
	// CgroupParent is "5050-apps.slice" for app containers.
	CgroupParent string     `json:"cgroupParent"`
	ShmSizeMB    int        `json:"shmSizeMb,omitempty"`
	User         string     `json:"user,omitempty"`
	Processes    int        `json:"processes"` // resolved count; >1 with ClusterWrap wraps with cluster.cjs
	ClusterWrap  bool       `json:"clusterWrap"`
	Mounts       []Mount    `json:"mounts,omitempty"`
	Files        []FileSpec `json:"files,omitempty"` // rendered template files, written under <DataDir>/replicas/<id>/files and bind-mounted read-only
	StopGraceSec int        `json:"stopGraceSec"`
	// Labels are added to the container next to the 5050.* labels the agent
	// always sets (5050.replica, 5050.deployment, 5050.service,
	// 5050.environment, 5050.epoch).
	Labels map[string]string `json:"labels"`
	// Probe is an exec probe the agent runs every 60 s (Postgres metrics and
	// WAL guard, 9.10, 9.13); its JSON output arrives as a DBSample.
	Probe []string `json:"probe,omitempty"`
	// Desired "running" or "stopped". A replica absent from the list is removed.
	Desired string `json:"desired"`
}

type HealthCheck struct {
	Path        string `json:"path,omitempty"` // empty = TCP connect to Port
	Host        string `json:"host,omitempty"` // Host header (first domain, or <name>.internal)
	IntervalSec int    `json:"intervalSec"`    // default 3
	TimeoutSec  int    `json:"timeoutSec"`     // per probe, default 10
	DeadlineSec int    `json:"deadlineSec"`    // until unhealthy, default 300
	GraceSec    int    `json:"graceSec"`       // readiness delay after first pass, default 5
	// Exec is an extra command that must exit 0 (pg_isready for Postgres).
	Exec []string `json:"exec,omitempty"`
}

type RestartPolicy struct {
	Policy     string `json:"policy"` // "on_failure" | "always" | "never"
	MaxRetries int    `json:"maxRetries"`
}

type Mount struct {
	Kind     string `json:"kind"`   // "volume" | "tool" | "asset" | "tmpfs"
	Source   string `json:"source"` // volume name, tool name, asset name
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

// FileSpec is a small file (<= 256 KiB) the agent writes and mounts read-only.
type FileSpec struct {
	Path    string `json:"path"`    // target path in the container
	Content []byte `json:"content"` // base64 in JSON
	Mode    uint32 `json:"mode"`
}

type VolumeSpec struct {
	Name      string `json:"name"` // docker volume "5050-<volid>"
	ServiceID string `json:"serviceId"`
	// Trash: the agent renames the volume to "5050-trash-<id>-<ts>" and a
	// janitor removes it after 48 hours (9.12).
	Trash bool `json:"trash,omitempty"`
}

// Ingress is the HTTP(S) entry point on a host with the ingress role.
type Ingress struct {
	ACMEEmail string `json:"acmeEmail,omitempty"`
	// ACMECA is the ACME directory: "production" (default), "staging" or a URL.
	ACMECA string `json:"acmeCa,omitempty"`
	// ACMEPrimary: only this ingress host runs ACME. Others proxy
	// /.well-known/acme-challenge/* to ACMEPrimaryAddr and load Certificates.
	ACMEPrimary     bool   `json:"acmePrimary"`
	ACMEPrimaryAddr string `json:"acmePrimaryAddr,omitempty"` // "<mesh ip>:80" of the primary
	// Certificates are pushed to non-primary ingress hosts and loaded as static certificates.
	Certificates []CertBundle `json:"certificates,omitempty"`
	Routes       []Route      `json:"routes"`
	// Upstreams are the answers of the agent's upstream resolver
	// (127.0.0.1:5354) for "<name>.upstream.5050". Changing them never reloads Caddy.
	Upstreams []UpstreamSet `json:"upstreams"`
	// Console is the route to the control plane UI and API; every ingress host serves it.
	Console *Route `json:"console,omitempty"`
	// StreamCloseDelaySec keeps websockets open across a config reload (default 300).
	StreamCloseDelaySec int `json:"streamCloseDelaySec"`
	// TrustedProxies are CIDRs whose X-Forwarded-For Caddy trusts (9.11).
	TrustedProxies []string `json:"trustedProxies,omitempty"`
	// Private: a Cloudflare tunnel terminates TLS. Caddy serves plain HTTP on
	// loopback only, orders no certificates and runs no TCP proxies.
	Private bool `json:"private,omitempty"`
}

type Route struct {
	Domain string `json:"domain"`
	// UpstreamName is answered by the upstream resolver ("svc-abc" ->
	// "svc-abc.upstream.5050"). Caddy's dynamic "a" upstream reads it every second.
	UpstreamName string `json:"upstreamName"`
	Port         int    `json:"port"` // target port on the replicas
	TLS          string `json:"tls"`  // "acme" | "internal" | "off" | "pending" (served on :80, no certificate yet)
	ServiceID    string `json:"serviceId"`
	// Panic is set when every running replica fails its health check: the
	// route then keeps all running replicas instead of none (7.4).
	Panic bool `json:"panic"`
}

// UpstreamSet is one upstream name and the replica IPs it resolves to.
type UpstreamSet struct {
	Name string   `json:"name"`
	IPs  []string `json:"ips"`
}

// CertBundle is a certificate the ACME primary issued, pushed to the other
// ingress hosts. The control plane stores it encrypted (secretbox); the
// agent's desired-state cache is encrypted with the host key.
type CertBundle struct {
	Domain   string `json:"domain"`
	CertPEM  string `json:"certPem"` // full chain
	KeyPEM   string `json:"keyPem"`
	NotAfter int64  `json:"notAfter"`
}

// CertIssued is sent by the ACME primary after it obtains or renews a certificate.
type CertIssued struct {
	CertBundle
}

// CertStatus is sent by the ACME primary when any domain's certificate state
// changes. Domains holds every domain it manages; Error is Caddy's last
// issuance error sentence while Status is "error".
type CertStatus struct {
	Domains map[string]CertDomainState `json:"domains"`
}

type CertDomainState struct {
	Status string `json:"status"` // "issuing" | "active" | "error"
	Error  string `json:"error,omitempty"`
}

type TCPProxy struct {
	HostPort  int      `json:"hostPort"`
	Upstreams []string `json:"upstreams"` // "10.130.4.7:5432"
}

type Mesh struct {
	Enabled bool       `json:"enabled"`
	Self    MeshSelf   `json:"self"`
	Peers   []MeshPeer `json:"peers"`
	MTU     int        `json:"mtu"` // 1420 or the discovered path MTU minus 80
}

// MeshSelf describes this host in the mesh. The WireGuard private key is
// generated on the host (<DataDir>/wg.key, 0600) and never leaves it; the
// public key travels in JoinRequest and Hello.
type MeshSelf struct {
	Index      int    `json:"index"`      // 1..127; 1 is the control host
	Address    string `json:"address"`    // "100.96.0.3/32" tunnel address
	ListenPort int    `json:"listenPort"` // UDP 51820
	HostSubnet string `json:"hostSubnet"` // "10.131.0.0/16" container range of this host
}

type MeshPeer struct {
	HostID     string   `json:"hostId"`
	PublicKey  string   `json:"publicKey"`
	Endpoint   string   `json:"endpoint"` // "198.51.100.4:51820"
	AllowedIPs []string `json:"allowedIps"`
}

type DNSEntry struct {
	EnvironmentID string `json:"environmentId"`
	// Name is "api.internal"; the agent also answers "api.railway.internal".
	Name string `json:"name"`
	// Aliases are old names kept for 24 hours after a rename (9.5).
	Aliases []string `json:"aliases,omitempty"`
	IPs     []string `json:"ips"` // healthy replicas, shuffled per answer, TTL 5
	// Draining IPs are never returned for new queries but stay routable until the drain ends (7.3).
	Draining []string `json:"draining,omitempty"`
}

type StateApplied struct {
	Generation int64    `json:"generation"`
	Epoch      string   `json:"epoch"`
	Errors     []string `json:"errors,omitempty"`
	// Unknown lists containers labelled 5050.replica that are not in the
	// state; filled only in adopt mode, where nothing is removed.
	Unknown []ReplicaStatus `json:"unknown,omitempty"`
}

// Maintenance orders a host into or out of maintenance (9.16). Replicas
// without volumes have already been moved by the time it arrives.
type Maintenance struct {
	On bool `json:"on"`
	// Order lists replica ids in the order to stop them (On) or start them (off).
	Order []string `json:"order"`
}

// ---------------------------------------------------------------- replica status, metrics, events

type ReplicaStatus struct {
	ReplicaID    string `json:"replicaId"`
	DeploymentID string `json:"deploymentId"`
	ContainerID  string `json:"containerId"`
	State        string `json:"state"` // "pulling" | "starting" | "healthy" | "unhealthy" | "running" | "exited" | "crashed" | "stopped" | "removed"
	ExitCode     *int   `json:"exitCode,omitempty"`
	OOMKilled    bool   `json:"oomKilled"`
	Restarts     int    `json:"restarts"`
	// Diagnosis is one sentence when the replica failed, e.g.
	// "Health check /health returned 404. Check the path in Settings > Deploy."
	Diagnosis      string `json:"diagnosis,omitempty"`
	ListeningPorts []int  `json:"listeningPorts,omitempty"`
	// LoopbackOnly lists ports the app listens on only at 127.0.0.1 (9.18).
	LoopbackOnly []int `json:"loopbackOnly,omitempty"`
	// Labels are the container's labels; sent for containers the state does not know (adopt).
	Labels    map[string]string `json:"labels,omitempty"`
	Image     string            `json:"image,omitempty"`
	StartedAt int64             `json:"startedAt,omitempty"`
	At        int64             `json:"at"`
}

type Metrics struct {
	At         int64             `json:"at"`
	Host       HostSample        `json:"host"`
	Containers []ContainerSample `json:"containers"`
	HTTP       []HTTPSample      `json:"http,omitempty"` // ingress hosts only, per 30 s window
	DB         []DBSample        `json:"db,omitempty"`   // database probes, every 60 s
}

type HostSample struct {
	CPUUsed  float64 `json:"cpuUsed"` // vCPU
	CPUs     int     `json:"cpus"`
	MemUsed  int64   `json:"memUsed"`
	MemTotal int64   `json:"memTotal"`
	Disks    []Disk  `json:"disks"`
	Load1    float64 `json:"load1"`
	// MeshHandshakes maps peer host id to the Unix ms of the last WireGuard handshake.
	MeshHandshakes map[string]int64 `json:"meshHandshakes,omitempty"`
	// Warnings are notes for the host row, such as a firewall that had to be loaded again.
	Warnings []string `json:"warnings,omitempty"`
}

type ContainerSample struct {
	ReplicaID string  `json:"replicaId"`
	CPU       float64 `json:"cpu"` // vCPU, from cgroup v2 cpu.stat deltas
	MemBytes  int64   `json:"memBytes"`
	MemLimit  int64   `json:"memLimit"`
	RxBytes   int64   `json:"rxBytes"` // counters; rates are computed by the control plane
	TxBytes   int64   `json:"txBytes"`
	// TxExternalBytes counts bytes sent to non-pod addresses (egress for usage, 9.23).
	TxExternalBytes int64 `json:"txExternalBytes"`
	DiskBytes       int64 `json:"diskBytes"` // volume usage, sampled every 5 min
}

// HTTPSample aggregates Caddy access logs per service over one window.
type HTTPSample struct {
	ServiceID string  `json:"serviceId"`
	Req2xx    int     `json:"req2xx"`
	Req3xx    int     `json:"req3xx"`
	Req4xx    int     `json:"req4xx"`
	Req5xx    int     `json:"req5xx"`
	P50Ms     float64 `json:"p50Ms"`
	P90Ms     float64 `json:"p90Ms"`
	P95Ms     float64 `json:"p95Ms"`
	P99Ms     float64 `json:"p99Ms"`
}

// DBSample is the output of a database replica's Probe.
type DBSample struct {
	ReplicaID     string         `json:"replicaId"`
	Connections   map[string]int `json:"connections"` // by state: "active", "idle", ...
	CacheHitRatio float64        `json:"cacheHitRatio"`
	// WALBytes is the size of pg_wal; OldestUnarchivedSec the age of the
	// oldest segment not yet archived (WAL guard, 9.13).
	WALBytes            int64       `json:"walBytes"`
	OldestUnarchivedSec int64       `json:"oldestUnarchivedSec"`
	TopQueries          []QueryStat `json:"topQueries,omitempty"`
}

type QueryStat struct {
	Query   string  `json:"query"` // normalized by pg_stat_statements
	Calls   int64   `json:"calls"`
	TotalMs float64 `json:"totalMs"`
	MeanMs  float64 `json:"meanMs"`
}

// Event is a folded container or host event. Host events have no ReplicaID.
type Event struct {
	ReplicaID string `json:"replicaId,omitempty"`
	// Kind: "die" | "oom" | "restart" | "health_status" for replicas;
	// "disk.high" | "disk.critical" | "mesh.unreachable" | "upgrade.failed" for the host.
	Kind string `json:"kind"`
	Text string `json:"text"`
	At   int64  `json:"at"`
}

// HostLogsTail ships recent error and deploy lines (at most 200 per batch).
type HostLogsTail struct {
	Lines []LogRecord `json:"lines"`
}

// ---------------------------------------------------------------- builds

type BuildJob struct {
	BuildID   string `json:"buildId"` // equals the deployment id
	ServiceID string `json:"serviceId"`
	// Source: exactly one of Git, TarballURL or SourceImage. SourceImage
	// "builds" are imports: the image is copied by digest into the internal
	// registry (go-containerregistry), so every host runs the same bytes and
	// rollbacks survive upstream deletes.
	SourceImage     string        `json:"sourceImage,omitempty"`
	SourceImageAuth *RegistryAuth `json:"sourceImageAuth,omitempty"`
	Git             *GitSource    `json:"git,omitempty"`
	TarballURL      string        `json:"tarballUrl,omitempty"` // https://<console>/api/agent/v1/uploads/<id> (host token auth)
	RootDir         string        `json:"rootDir"`
	// ConfigPath is build.configPath; empty means look for 5050.yaml,
	// 5050.json, railway.json, railway.toml in RootDir (9.20).
	ConfigPath string `json:"configPath,omitempty"`
	// Builder: "auto" | "railpack" | "dockerfile" | "nixpacks" (built with Railpack).
	Builder         string `json:"builder"`
	RailpackVersion string `json:"railpackVersion,omitempty"` // empty = current pin
	DockerfilePath  string `json:"dockerfilePath,omitempty"`
	BuildCommand    string `json:"buildCommand,omitempty"`
	StartCommand    string `json:"startCommand,omitempty"`
	// Env holds every service variable, resolved. Values never go on argv:
	// the builder passes them in the child process environment and argv
	// carries names only (Railpack `--env NAME`, buildx `--build-arg NAME`,
	// `--secret id=NAME,env=NAME`). Dockerfile builds see only names declared by ARG.
	Env map[string]string `json:"env"`
	// EnvFile switches to a 0600 env file outside the build context, deleted
	// after the build, for tools that cannot read names from the environment.
	// The pinned Railpack reads bare names from its environment, so the
	// control plane sends false (docs/SPEC.md 8.2).
	EnvFile    bool   `json:"envFile"`
	Image      string `json:"image"`    // target ref "<registry>/<svc id>:<deployment id>"
	CacheRef   string `json:"cacheRef"` // "<registry>/<svc id>:buildcache"
	Platform   string `json:"platform"` // "linux/amd64" | "linux/arm64" (target host arch)
	NoCache    bool   `json:"noCache"`
	TimeoutSec int    `json:"timeoutSec"` // from build.timeoutMin, default 1800
}

// RegistryAuth is a credential for a private source registry.
type RegistryAuth struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type GitSource struct {
	URL    string `json:"url"`             // https clone URL
	Token  string `json:"token,omitempty"` // GitHub App installation token, short-lived
	Commit string `json:"commit"`          // full SHA to check out
	Branch string `json:"branch"`
}

type BuildLog struct {
	BuildID string   `json:"buildId"`
	Lines   []string `json:"lines"`
}

type BuildResult struct {
	BuildID  string `json:"buildId"`
	OK       bool   `json:"ok"`
	Image    string `json:"image,omitempty"`
	Digest   string `json:"digest,omitempty"`
	Provider string `json:"provider,omitempty"` // railpack provider, "dockerfile", "image"
	// StartCommand detected by Railpack, used when the service has none.
	StartCommand string `json:"startCommand,omitempty"`
	IsNode       bool   `json:"isNode"`              // enables the processes wrap
	Diagnosis    string `json:"diagnosis,omitempty"` // one sentence from the 9.18 catalog
	// ConfigFile is the config-as-code file found in the source, if any.
	// The control plane merges it with Config.MergeConfigFile (9.20).
	ConfigFile *ConfigOverlay `json:"configFile,omitempty"`
	// Notices are one-line build notes shown in the deployment Details,
	// e.g. "railway.json: sleepApplication is not supported in 5050cloud v0.1 and was ignored."
	Notices    []string `json:"notices,omitempty"`
	DurationMs int64    `json:"durationMs"`
}

// ConfigOverlay is a parsed config-as-code file (pkg/configfile).
type ConfigOverlay struct {
	Path   string                 `json:"path"`   // relative to the repo root, e.g. "railway.json"
	Format string                 `json:"format"` // "json" | "toml" | "yaml"
	Config manifest.RailwayConfig `json:"config"`
	// Lines maps a field path ("deploy.startCommand",
	// "environments.production.deploy.numReplicas") to its 1-based line.
	Lines map[string]int `json:"lines"`
	// Nixpacks holds hints read from nixpacks.toml (9.20.2), when present.
	Nixpacks *manifest.NixpacksHints `json:"nixpacks,omitempty"`
}

// ---------------------------------------------------------------- jobs

type Job struct {
	JobID string `json:"jobId"`
	// Kind: "pre_deploy" | "cron" | "backup" | "restore" | "restore_test" | "retention" | "exec" | "maintenance".
	Kind       string            `json:"kind"`
	Image      string            `json:"image"`
	Cmd        []string          `json:"cmd"`
	Env        map[string]string `json:"env"`
	Network    string            `json:"network,omitempty"`
	IP         string            `json:"ip,omitempty"` // static IP when the job joins an environment network
	Mounts     []Mount           `json:"mounts,omitempty"`
	Files      []FileSpec        `json:"files,omitempty"`
	Volumes    []string          `json:"volumes,omitempty"` // docker volume names created for this job (restore into a new volume)
	User       string            `json:"user,omitempty"`
	TimeoutSec int               `json:"timeoutSec"`
	MemLimitMB int               `json:"memLimitMb,omitempty"`
	ReplicaID  string            `json:"replicaId,omitempty"` // "exec" kind: run inside this running replica instead of a new container
	// Stdin is fed to the command (small inputs only, at most 1 MiB).
	Stdin  []byte            `json:"stdin,omitempty"`
	Labels map[string]string `json:"labels"`
}

type JobLog struct {
	JobID string   `json:"jobId"`
	Lines []string `json:"lines"`
}

type JobResult struct {
	JobID    string `json:"jobId"`
	ExitCode int    `json:"exitCode"`
	TimedOut bool   `json:"timedOut"`
	Error    string `json:"error,omitempty"`
	// Output holds the last 64 KiB of combined output (backup jobs print a JSON summary as their last line).
	Output     string `json:"output,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// ---------------------------------------------------------------- logs

type LogsQuery struct {
	ServiceIDs   []string `json:"serviceIds"`
	DeploymentID string   `json:"deploymentId,omitempty"`
	Streams      []string `json:"streams,omitempty"` // "stdout","stderr","http","system"; empty = runtime
	Since        int64    `json:"since"`
	Until        int64    `json:"until"`
	Filter       string   `json:"filter,omitempty"` // filter syntax in docs/SPEC.md 9.4
	Limit        int      `json:"limit"`            // newest N within the window, max 5000
	// Around returns Limit lines centred on this record id (View in Context).
	Around string `json:"around,omitempty"`
}

// LogRecord is what the agent stores (JSON lines) and returns.
type LogRecord struct {
	ID           string         `json:"id"` // unique per host: "<file>:<offset>"
	TS           int64          `json:"ts"`
	ServiceID    string         `json:"svc"`
	DeploymentID string         `json:"dep"`
	ReplicaID    string         `json:"rep"`
	Stream       string         `json:"stream"`
	Level        string         `json:"level"`
	Msg          string         `json:"msg"`
	Attrs        map[string]any `json:"attrs,omitempty"`
}

type LogsResult struct {
	Lines     []LogRecord `json:"lines"`
	Truncated bool        `json:"truncated"`
}

type LogsSubscribe struct {
	SubID string    `json:"subId"`
	Query LogsQuery `json:"query"` // Since/Until/Limit/Around ignored
}

type LogLines struct {
	SubID string      `json:"subId"`
	Lines []LogRecord `json:"lines"`
	// Dropped counts lines dropped by the bounded queue since the last message.
	Dropped int `json:"dropped,omitempty"`
}

// ---------------------------------------------------------------- exec, tcp, upgrade

type ExecOpen struct {
	StreamID  uint64   `json:"streamId"`
	ReplicaID string   `json:"replicaId"`
	Cmd       []string `json:"cmd"` // default ["sh"] with bash preferred when present
	TTY       bool     `json:"tty"`
	Cols      int      `json:"cols"`
	Rows      int      `json:"rows"`
	// Session names a reattachable session kept for 10 minutes after a disconnect.
	Session string `json:"session,omitempty"`
}

type ExecResize struct {
	StreamID uint64 `json:"streamId"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

type ExecExit struct {
	StreamID uint64 `json:"streamId"`
	Code     int    `json:"code"`
}

type TCPOpen struct {
	StreamID uint64 `json:"streamId"`
	Target   string `json:"target"` // "10.130.4.7:5432"
}

type StreamClose struct {
	StreamID uint64 `json:"streamId"`
	Error    string `json:"error,omitempty"`
}

type Upgrade struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

type Ack struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
