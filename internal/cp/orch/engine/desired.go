// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/data"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/routes"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Defaults placed into every host's HostConfig (docs/SPEC.md 8.1, 9.19).
const (
	imageRetention        = 3
	logRetentionDays      = 7
	logMaxBytesPerService = 1 << 30
	logMaxPctFS           = 10
	diskPruneAtPct        = 80
	meshMTU               = 1420
	streamCloseDelaySec   = 300
	appsSlice             = "5050-apps.slice"
	defaultStopGraceSec   = 10
	defaultPort           = 8080
	registryPort          = "5000"
	consoleUpstream       = "console"
)

// netCache holds the routes computed from every service instance; it is
// rebuilt when invalidated (a replica's health, a cutover, a domain change).
type netCache struct {
	mu     sync.Mutex
	valid  bool
	out    routes.Output
	snaps  map[string]cpapi.ServiceSnapshot // env/svc
	online map[string]bool                  // hosts that are routed (connected or within 60 s)
}

func (o *Orchestrator) invalidateNet() {
	o.net.mu.Lock()
	o.net.valid = false
	o.net.mu.Unlock()
}

// netState returns the cached routes and snapshots, recomputing them when needed.
func (o *Orchestrator) netState(ctx context.Context) (routes.Output, map[string]cpapi.ServiceSnapshot, map[string]bool, error) {
	o.net.mu.Lock()
	defer o.net.mu.Unlock()
	if o.net.valid {
		return o.net.out, o.net.snaps, o.net.online, nil
	}
	snaps, err := o.cfg.AllInstances(ctx)
	if err != nil {
		return routes.Output{}, nil, nil, err
	}
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return routes.Output{}, nil, nil, err
	}
	online := map[string]bool{}
	for _, h := range hosts {
		online[h.ID] = o.routed(h)
	}
	reps, err := o.queryReps(ctx, `desired IN ('running', 'stopped')`)
	if err != nil {
		return routes.Output{}, nil, nil, err
	}
	byKey := map[string][]routes.Replica{}
	for _, r := range reps {
		if r.Desired != desiredRunning && !r.Draining {
			continue
		}
		k := routes.Key(r.EnvID, r.ServiceID)
		byKey[k] = append(byKey[k], routes.Replica{ID: r.ID, ServiceID: r.ServiceID, EnvID: r.EnvID, HostID: r.HostID, IP: r.IP,
			State: r.State, Serving: r.Serving, Draining: r.Draining, HostOnline: online[r.HostID]})
	}
	m := map[string]cpapi.ServiceSnapshot{}
	var svcs []routes.Service
	for _, s := range snaps {
		k := routes.Key(s.EnvID, s.ServiceID)
		m[k] = s
		svcs = append(svcs, routes.Service{Snap: s, Replicas: byKey[k], Port: targetPort(s.Config, nil)})
	}
	out := routes.Compute(svcs)
	o.net.out, o.net.snaps, o.net.online, o.net.valid = out, m, online, true
	return out, m, online, nil
}

// routed reports whether a host's replicas stay in routes: connected, or
// disconnected for less than OfflineAfter.
func (o *Orchestrator) routed(h hub.HostRow) bool {
	if h.State == sched.StateLost || h.State == sched.StateUnknown {
		return false
	}
	if o.hub.Connected(h.ID) {
		return true
	}
	since := h.DisconnectedAt
	if since == 0 {
		since = h.LastSeen
	}
	if start := o.startedAt.UnixMilli(); since < start {
		since = start // agents get OfflineAfter to reconnect after a control plane restart
	}
	return o.now().UnixMilli()-since < o.t.OfflineAfter.Milliseconds()
}

// targetPort is the service's port: the setting, the variable PORT, else 8080.
func targetPort(cfg api.ServiceConfig, env map[string]string) int {
	if cfg.Net.Port > 0 {
		return cfg.Net.Port
	}
	if p, err := strconv.Atoi(env["PORT"]); err == nil && p > 0 && p < 65536 {
		return p
	}
	return defaultPort
}

// probePort is the port the agent health-checks. A worker (no target port,
// no domain) whose PORT is still the default has no port: nothing is
// expected to listen, so it is healthy once running 10 s (7.4) instead of
// waiting for a TCP answer that never comes. A health check path, a PORT the
// user set or one detected from the image keeps the probe.
func probePort(cfg api.ServiceConfig, env map[string]string, hasDomains bool, port int) int {
	if sched.Kind(cfg, hasDomains) != api.KindWorker || cfg.Deploy.HealthcheckPath != "" {
		return port
	}
	if p := env["PORT"]; p != "" && p != strconv.Itoa(defaultPort) {
		return port
	}
	return 0
}

func (o *Orchestrator) registryAddr(h hub.HostRow) string {
	if h.MeshIndex == 1 {
		return "127.0.0.1:" + registryPort
	}
	return o.plan.MeshAddr(1).String() + ":" + registryPort
}

// imageRef is the pull reference of a deployment's image on a host.
func (o *Orchestrator) imageRef(h hub.HostRow, d depRow) string {
	if d.Image == "" {
		return ""
	}
	if !strings.HasPrefix(d.Image, "svc_") {
		return d.Image // adopted containers keep the reference they ran with
	}
	repo := d.Image
	if d.Digest != "" {
		if i := strings.LastIndexByte(repo, ':'); i > 0 {
			repo = repo[:i]
		}
		return o.registryAddr(h) + "/" + repo + "@" + d.Digest
	}
	return o.registryAddr(h) + "/" + repo
}

// DesiredState builds the full desired state of a host (hub.Handler).
func (o *Orchestrator) DesiredState(ctx context.Context, hostID string) (proto.DesiredState, error) {
	host, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return proto.DesiredState{}, err
	}
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return proto.DesiredState{}, err
	}
	members := make([]hub.HostRow, 0, len(hosts))
	for _, h := range hosts {
		if h.State != sched.StateUnknown && h.MeshIndex > 0 {
			members = append(members, h)
		}
	}
	inst, err := o.cfg.Install(ctx)
	if err != nil {
		return proto.DesiredState{}, err
	}
	creds, err := o.registryCreds(ctx)
	if err != nil {
		return proto.DesiredState{}, err
	}
	out, snaps, _, err := o.netState(ctx)
	if err != nil {
		return proto.DesiredState{}, err
	}
	ds := proto.DesiredState{
		Host: proto.HostConfig{
			Roles:                 host.Roles,
			RegistryPull:          proto.Registry{Address: o.registryAddr(host), Username: creds.PullUser, Password: creds.PullPass, Insecure: true},
			ImageRetention:        imageRetention,
			LogRetentionDays:      logRetentionDays,
			LogMaxBytesPerService: logMaxBytesPerService,
			LogMaxPctFS:           logMaxPctFS,
			DiskPruneAtPct:        diskPruneAtPct,
			Drain:                 host.Drain,
			Maintenance:           host.State == sched.StateMaintenance,
			Tools:                 o.tools(host.Facts.Arch),
			PodCIDR:               o.plan.Pod.String(),
			MeshCIDR:              o.plan.Mesh.String(),
			BuildTimeoutMin:       inst.BuildTimeoutMin,
		},
		Networks: []proto.Network{}, Replicas: []proto.ReplicaSpec{}, DNS: []proto.DNSEntry{}, Volumes: []proto.VolumeSpec{},
	}
	if ds.Host.BuildTimeoutMin == 0 {
		ds.Host.BuildTimeoutMin = 30
	}
	if host.HasRole("build") {
		ds.Host.RegistryPush = &proto.Registry{Address: o.registryAddr(host), Username: creds.PushUser, Password: creds.PushPass, Insecure: true}
	}
	multi := len(members) >= 2

	reps, err := o.queryReps(ctx, `host_id = ? AND desired IN ('running', 'stopped') ORDER BY created_at, id`, hostID)
	if err != nil {
		return ds, err
	}
	deps := map[string]depRow{}
	allowMeta := map[string][]string{}
	for _, r := range reps {
		d, ok := deps[r.DeploymentID]
		if !ok {
			d, err = o.loadDep(ctx, o.db, r.DeploymentID)
			if err != nil {
				o.log.Warn("replica without deployment", "replica", r.ID, "err", err)
				continue
			}
			deps[r.DeploymentID] = d
		}
		snap, ok := snaps[routes.Key(r.EnvID, r.ServiceID)]
		if !ok {
			snap = cpapi.ServiceSnapshot{EnvID: r.EnvID, ServiceID: r.ServiceID, ProjectID: d.ProjectID, Config: d.Config, PrivateName: routes.Slug(d.Config.Name)}
		}
		spec, err := o.replicaSpec(ctx, host, r, d, snap)
		if err != nil {
			o.log.Warn("replica spec", "replica", r.ID, "err", err)
			continue
		}
		ds.Replicas = append(ds.Replicas, spec)
		if snap.Config.Net.AllowMetadata && r.IP != "" {
			allowMeta[r.EnvID] = append(allowMeta[r.EnvID], r.IP)
		}
	}

	// Networks: every environment with a slot on this host.
	rows, err := o.db.QueryContext(ctx, `SELECT environment_id, slot FROM o_subnets WHERE host_id = ? ORDER BY slot`, hostID)
	if err != nil {
		return ds, err
	}
	type slotRow struct {
		env  string
		slot int
	}
	var slots []slotRow
	for rows.Next() {
		var s slotRow
		if err := rows.Scan(&s.env, &s.slot); err != nil {
			closeRows(rows)
			return ds, err
		}
		slots = append(slots, s)
	}
	closeRows(rows)
	meshIdx := map[string]int{}
	for _, h := range members {
		meshIdx[h.ID] = h.MeshIndex
	}
	envSnap := map[string]cpapi.ServiceSnapshot{}
	for _, s := range snaps {
		if _, ok := envSnap[s.EnvID]; !ok {
			envSnap[s.EnvID] = s
		}
	}
	for _, s := range slots {
		n := proto.Network{
			EnvironmentID: s.env,
			Name:          "5050-" + s.env,
			Subnet:        o.plan.SlotSubnet(host.MeshIndex, s.slot).String(),
			Gateway:       o.plan.Gateway(host.MeshIndex, s.slot).String(),
			Egress:        "allowed",
			PeerSubnets:   []string{},
		}
		if es, ok := envSnap[s.env]; ok {
			if es.Egress != "" {
				n.Egress = es.Egress
			}
			n.EgressAllow = es.EgressAllow
		}
		peers, err := ipam.EnvSlots(ctx, o.db, s.env)
		if err != nil {
			return ds, err
		}
		for hid, slot := range peers {
			if hid == hostID {
				continue
			}
			if k := meshIdx[hid]; k > 0 {
				n.PeerSubnets = append(n.PeerSubnets, o.plan.SlotSubnet(k, slot).String())
			}
		}
		sort.Strings(n.PeerSubnets)
		n.AllowMetadata = allowMeta[s.env]
		if multi {
			n.MTU = meshMTU
		}
		ds.Networks = append(ds.Networks, n)
		ds.DNS = append(ds.DNS, out.DNS[s.env]...)
	}

	// Mesh.
	ds.Mesh = proto.Mesh{Enabled: multi, MTU: meshMTU, Peers: []proto.MeshPeer{}, Self: proto.MeshSelf{
		Index: host.MeshIndex, Address: o.plan.MeshAddr(host.MeshIndex).String() + "/32", ListenPort: hub.MeshPort,
		HostSubnet: o.plan.HostSubnet(host.MeshIndex).String(),
	}}
	if multi {
		for _, h := range members {
			if h.ID == hostID || h.WGPublicKey == "" {
				continue
			}
			p := proto.MeshPeer{HostID: h.ID, PublicKey: h.WGPublicKey,
				AllowedIPs: []string{o.plan.MeshAddr(h.MeshIndex).String() + "/32", o.plan.HostSubnet(h.MeshIndex).String()}}
			if h.PublicIP != "" {
				p.Endpoint = h.PublicIP + ":" + strconv.Itoa(hub.MeshPort)
			}
			ds.Mesh.Peers = append(ds.Mesh.Peers, p)
		}
	}

	// Ingress and TCP proxies.
	if host.HasRole("ingress") {
		ing, err := o.ingressFor(ctx, host, members, inst, out)
		if err != nil {
			return ds, err
		}
		ds.Ingress = ing
		ds.TCPProxies = out.TCP
	}

	// Volumes placed here.
	vrows, err := o.db.QueryContext(ctx, `SELECT volume_id, service_id, trashed_at FROM o_volume_hosts WHERE host_id = ? ORDER BY volume_id`, hostID)
	if err != nil {
		return ds, err
	}
	for vrows.Next() {
		var id, svc string
		var trashed int64
		if err := vrows.Scan(&id, &svc, &trashed); err != nil {
			closeRows(vrows)
			return ds, err
		}
		ds.Volumes = append(ds.Volumes, proto.VolumeSpec{Name: "5050-" + id, ServiceID: svc, Trash: trashed > 0})
	}
	closeRows(vrows)
	return ds, nil
}

func consoleHost(inst api.Install, publicURL string) string {
	for _, s := range []string{inst.ConsoleURL, publicURL} {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			return strings.ToLower(u.Hostname())
		}
	}
	return ""
}

func (o *Orchestrator) ingressFor(ctx context.Context, host hub.HostRow, members []hub.HostRow, inst api.Install, out routes.Output) (*proto.Ingress, error) {
	var cands []routes.IngressHost
	for _, h := range members {
		if !h.HasRole("ingress") {
			continue
		}
		cands = append(cands, routes.IngressHost{ID: h.ID, Online: o.hub.Connected(h.ID), Control: h.MeshIndex == 1,
			CreatedAt: h.CreatedAt, MeshIP: o.plan.MeshAddr(h.MeshIndex).String()})
	}
	primary, addr := routes.ACMEPrimary(cands)
	ing := &proto.Ingress{
		ACMEEmail:           inst.ACMEEmail,
		ACMECA:              o.o.ACMECA,
		ACMEPrimary:         primary == host.ID,
		Routes:              o.withoutRetrying(append([]proto.Route{}, out.Routes...)),
		Upstreams:           append([]proto.UpstreamSet{}, out.Upstreams...),
		StreamCloseDelaySec: streamCloseDelaySec,
		TrustedProxies:      inst.TrustedProxies,
	}
	if !ing.ACMEPrimary {
		ing.ACMEPrimaryAddr = addr
		certs, err := o.certBundles(ctx)
		if err != nil {
			return nil, err
		}
		ing.Certificates = certs
	}
	if d := consoleHost(inst, o.o.PublicURL); d != "" {
		ing.Console = &proto.Route{Domain: d, UpstreamName: consoleUpstream, Port: consolePort(o.o.LocalURL), TLS: routes.TLSMode(d)}
		// The previous console name keeps working for 24 hours after a change.
		if old := strings.ToLower(inst.OldConsoleDomain); old != "" && old != d && inst.OldConsoleUntil > o.now().UnixMilli() {
			ing.Routes = append(ing.Routes, proto.Route{Domain: old, UpstreamName: consoleUpstream, Port: consolePort(o.o.LocalURL), TLS: routes.TLSMode(old)})
		}
		ip := "127.0.0.1"
		if host.MeshIndex != 1 {
			ip = o.plan.MeshAddr(1).String()
		}
		ing.Upstreams = append(ing.Upstreams, proto.UpstreamSet{Name: consoleUpstream, IPs: []string{ip}})
	}
	return ing, nil
}

func consolePort(local string) int {
	if u, err := url.Parse(local); err == nil {
		if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
			return p
		}
	}
	return 5050
}

// replicaSpec turns a replica row into what the agent runs.
func (o *Orchestrator) replicaSpec(ctx context.Context, host hub.HostRow, r repRow, d depRow, snap cpapi.ServiceSnapshot) (proto.ReplicaSpec, error) {
	cfg := d.Config
	env, err := o.openEnv(d.ID, d.EnvEnc)
	if err != nil {
		return proto.ReplicaSpec{}, err
	}
	rend := o.rendered(ctx, d, snap, host)
	port := targetPort(cfg, env)
	merged := map[string]string{}
	for k, v := range rend.Env {
		merged[k] = v
	}
	for k, v := range env {
		merged[k] = v
	}
	procs := sched.Processes(cfg, host.Facts.CPUs)
	inst, _ := o.cfg.Install(ctx)
	for k, v := range perReplicaVars(r.ID, host.Name, procs, inst.RailwayAliases) {
		if _, set := env[k]; !set || k == "FIFTY_REPLICA_ID" || k == "RAILWAY_REPLICA_ID" {
			merged[k] = v
		}
	}
	argv := startArgv(cfg.Deploy.StartCommand)
	if len(argv) == 0 && d.StartCmd != "" {
		argv = startArgv(d.StartCmd)
	}
	spec := proto.ReplicaSpec{
		ID: r.ID, DeploymentID: d.ID, ServiceID: r.ServiceID, EnvironmentID: r.EnvID, ProjectID: d.ProjectID,
		ServiceName: cfg.Name, Image: o.imageRef(host, d), Cmd: argv, Env: merged, Port: probePort(cfg, env, len(snap.Domains) > 0, port), IP: r.IP,
		Network: "5050-" + r.EnvID,
		Health: proto.HealthCheck{Path: cfg.Deploy.HealthcheckPath, Host: healthHost(snap), IntervalSec: 3, TimeoutSec: 10,
			DeadlineSec: healthDeadline(cfg), GraceSec: 5, Exec: rend.Health},
		Restart:      restartPolicy(cfg),
		CPULimit:     cfg.Deploy.CPULimit,
		MemLimitMB:   cfg.Deploy.MemoryLimitMB,
		CgroupParent: appsSlice,
		ShmSizeMB:    firstPositive(cfg.Deploy.ShmSizeMB, rend.ShmSizeMB),
		User:         cfg.Deploy.RunAsUser,
		Processes:    procs,
		Files:        rend.Files,
		Mounts:       append([]proto.Mount(nil), rend.Mounts...),
		StopGraceSec: firstPositive(rend.StopGraceSec, defaultStopGraceSec),
		Probe:        rend.Probe,
		Desired:      r.Desired,
		Labels: map[string]string{
			"5050.ip":      r.IP,
			"5050.port":    strconv.Itoa(port),
			"5050.project": d.ProjectID,
			"5050.host":    host.ID,
		},
	}
	spec.ClusterWrap = procs > 1 && sched.ClusterWrap(d.IsNode, argv)
	resMB := sched.ReservationMB(cfg, isDatabase(cfg))
	if rend.MemoryReservationMB > 0 && cfg.Deploy.MemoryReservationMB == 0 {
		resMB = rend.MemoryReservationMB
	}
	spec.MemoryReservation = int64(resMB) << 20
	if cfg.Deploy.MemoryLimitMB > 0 {
		spec.MemorySwap = int64(cfg.Deploy.MemoryLimitMB) << 20
		if spec.MemoryReservation > spec.MemorySwap {
			spec.MemoryReservation = spec.MemorySwap
		}
	}
	if spec.User == "" {
		spec.User = env["RAILWAY_RUN_UID"]
		if v := env["FIFTY_RUN_UID"]; v != "" {
			spec.User = v
		}
	}
	if snap.Volume != nil {
		if vh, _ := o.volumeHost(ctx, snap.Volume.ID); vh == host.ID {
			spec.Mounts = append(spec.Mounts, proto.Mount{Kind: "volume", Source: "5050-" + snap.Volume.ID, Target: snap.Volume.MountPath})
		}
	}
	return spec, nil
}

func (o *Orchestrator) rendered(ctx context.Context, d depRow, snap cpapi.ServiceSnapshot, host hub.HostRow) data.Rendered {
	if d.Config.Template == "" {
		return data.Rendered{}
	}
	key := d.ID + "|" + host.ID
	o.mu.Lock()
	r, ok := o.render[key]
	o.mu.Unlock()
	if ok {
		return r
	}
	snap.Config = d.Config
	r, err := o.data.Render(ctx, snap, host.Facts)
	if err != nil {
		o.log.Warn("render template", "deployment", d.ID, "template", d.Config.Template, "err", err)
		r = data.Rendered{}
	}
	o.mu.Lock()
	if len(o.render) > 4096 {
		o.render = map[string]data.Rendered{}
	}
	o.render[key] = r
	o.mu.Unlock()
	return r
}

func isDatabase(cfg api.ServiceConfig) bool {
	if cfg.Kind == api.KindDatabase {
		return true
	}
	switch cfg.Template {
	case "postgres", "mysql", "redis", "mongodb", "pgvector", "postgis":
		return true
	}
	return false
}

func healthHost(s cpapi.ServiceSnapshot) string {
	if len(s.Domains) > 0 && s.Domains[0].Domain != "" {
		return s.Domains[0].Domain
	}
	return routes.PrivateDomain(s)
}

func healthDeadline(cfg api.ServiceConfig) int {
	if cfg.Deploy.HealthcheckTimeoutSec > 0 {
		return cfg.Deploy.HealthcheckTimeoutSec
	}
	return 300
}

func restartPolicy(cfg api.ServiceConfig) proto.RestartPolicy {
	p := cfg.Deploy.RestartPolicy
	switch strings.ToLower(p) {
	case "always", "never":
		p = strings.ToLower(p)
	default:
		p = "on_failure"
	}
	n := cfg.Deploy.RestartMaxRetries
	if n <= 0 && p == "on_failure" {
		n = 10
	}
	return proto.RestartPolicy{Policy: p, MaxRetries: n}
}

func firstPositive(vs ...int) int {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

// tools lists the pinned helper binaries of templates/tools.json for one
// architecture (data.Tools reads the file).
func (o *Orchestrator) tools(arch string) []proto.Tool {
	if o.o.Templates == nil {
		return []proto.Tool{}
	}
	out, err := data.Tools(o.o.Templates, arch)
	if err != nil {
		o.log.Warn("helper tools unavailable", "arch", arch, "err", err)
		return []proto.Tool{}
	}
	return out
}
