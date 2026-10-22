// SPDX-License-Identifier: AGPL-3.0-only

// Package agentapi holds the in-process interfaces between the agent's
// parts, so the runtime (internal/agent/core), the builder
// (internal/agent/build) and networking (internal/agent/netw) can be built in
// parallel. Frozen for v0.1; see docs/SPEC.md section 8.
//
// Wiring in cmd/5050-agent/main.go:
//
//	nw  := netw.New(netw.Options{DataDir: dataDir, DockerHost: dockerHost})          // agentapi.Network
//	ing := netw.NewIngress(netw.IngressOptions{DataDir: dataDir, DockerHost: dockerHost}) // agentapi.Ingress
//	b   := build.New(build.Options{DataDir: dataDir, DockerHost: dockerHost, Tools: tools}) // agentapi.Builder
//	core.Run(ctx, core.Options{..., Network: nw, Ingress: ing, Builder: b})
//
// Order per desired state (7.2): Network.Apply, volumes, files, replicas,
// Ingress.Apply plus SetUpstreams, then DNS answers (part of Network.Apply).
// At boot the core applies the cached state through the same calls before
// it connects, and 5050-net-restore calls Network.Apply alone before Docker
// starts.
package agentapi

import (
	"context"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Tools fetches and verifies pinned helper binaries (railpack, wal-g,
// restic, litestream) into <DataDir>/tools and returns the local path.
// Implemented by core.
type Tools interface {
	Path(ctx context.Context, name string) (string, error)
	Set(tools []proto.Tool)
}

// Builder turns a BuildJob into an image in the registry. It must stream
// log lines through logf as they happen, honour ctx cancellation (abort),
// never put variable values on argv, into image layers or into logs, and
// fill Diagnosis on failure with a sentence from docs/SPEC.md 9.18.
type Builder interface {
	// Build uses reg, the push credential (HostConfig.RegistryPush).
	Build(ctx context.Context, job proto.BuildJob, reg proto.Registry, logf func(line string)) proto.BuildResult
	// Prune trims build cache to keepBytes. Called by core when disk is above the prune threshold.
	Prune(ctx context.Context, keepBytes int64) error
	// EnsureRunning starts or upgrades the BuildKit builder (hosts with the
	// build role): docker-container driver, --oci-worker-net=bridge on the
	// 5050-build network, no network.host entitlement.
	EnsureRunning(ctx context.Context, image string) error
}

// Network owns docker networks per environment, the WireGuard mesh, the
// private DNS resolver and the firewall (environment isolation, egress
// quarantine, metadata and host access, the build network). Apply is called
// with every DesiredState before replicas are reconciled and must be
// idempotent and atomic per rule set (one iptables-restore --noflush).
type Network interface {
	Apply(ctx context.Context, host proto.HostConfig, mesh proto.Mesh, nets []proto.Network, dns []proto.DNSEntry) error
	// PublicKey returns this host's WireGuard public key (generated once, kept in DataDir).
	PublicKey() (string, error)
	// DNSServer returns the resolver address containers in an environment use.
	DNSServer(environmentID string) string
	// AllowEgress applies per-container egress rules before the container joins any route.
	AllowEgress(ctx context.Context, containerIP string, n proto.Network) error
	// BuildNetwork ensures the 5050-build bridge and its firewall rules and returns its name.
	BuildNetwork(ctx context.Context) (string, error)
}

// Ingress owns Caddy (HTTP/HTTPS, ACME), the upstream resolver on
// 127.0.0.1:5354 and the built-in TCP proxy.
type Ingress interface {
	// EnsureRunning starts caddy:<image> with host networking, --resume and
	// the admin API on the unix socket <DataDir>/caddy/admin.sock (0600).
	EnsureRunning(ctx context.Context, image string) error
	// Apply replaces the route set, certificates, ACME settings and TCP
	// proxies atomically. It calls Caddy's /load only when the rendered
	// config changed; upstream IPs are not part of the Caddy config.
	Apply(ctx context.Context, in proto.Ingress, tcp []proto.TCPProxy) error
	// SetUpstreams changes what the upstream resolver answers for
	// "<name>.upstream.5050". It never reloads Caddy.
	SetUpstreams(name string, ips []string) error
	// CertStatus reports per-domain certificate state ("issuing" | "active" |
	// "error") and expiry for the UI.
	CertStatus(ctx context.Context) (map[string]CertState, error)
	// IssuedCerts streams certificates the ACME primary obtained or renewed;
	// core forwards them as proto.TCertIssued.
	IssuedCerts(ctx context.Context, out chan<- proto.CertBundle) error
	// AccessLogs streams parsed HTTP access logs as LogRecords (stream "http").
	AccessLogs(ctx context.Context, out chan<- proto.LogRecord) error
}

// CertState is one domain's certificate state.
type CertState struct {
	Status   string // "issuing" | "active" | "error"
	NotAfter int64  // Unix ms, 0 when unknown
	Error    string // one sentence when Status is "error"
}
