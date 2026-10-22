// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxParallelCreates bounds pulls and container creations at once.
const maxParallelCreates = 4

// replica is the agent's view of one replica it runs or ran.
type replica struct {
	spec   proto.ReplicaSpec
	cid    string
	status proto.ReplicaStatus
	// busy is set while a create, stop or remove runs for it.
	busy bool
	// stoppedByAgent: the agent stopped it (desired stopped, maintenance),
	// so a later "running" starts it again.
	stoppedByAgent bool
	// removing: the agent is removing it; a "destroy" event is expected.
	removing bool
	// followStarted is the StartedAt of the container run the log follower
	// and health checker belong to.
	followStarted time.Time
	healthCancel  context.CancelFunc
	logCancel     context.CancelFunc
	proc          *logstore.Processor
	startedAt     time.Time
	// localUnhealthy is the health checker's view, used for upstreams while
	// the control plane is unreachable.
	localUnhealthy bool
}

func (a *Agent) createSem() chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sem == nil {
		a.sem = make(chan struct{}, maxParallelCreates)
	}
	return a.sem
}

// apply enforces a desired state in the order of 7.2: networks and
// firewall, volumes, files and replicas, ingress, upstreams and DNS
// answers. Container work runs in the background, guarded per replica, so
// a slow pull never holds up a route switch. It returns per-item errors and,
// in adopt mode, the containers the state does not know.
func (a *Agent) apply(ctx context.Context, ds *proto.DesiredState, allowRemove bool) ([]string, []proto.ReplicaStatus) {
	var errs []string
	a.tools.Set(ds.Host.Tools)
	a.o.Live.setImages(ds.Host.Images)

	if err := a.o.Network.Apply(ctx, ds.Host, ds.Mesh, ds.Networks, ds.DNS); err != nil {
		errs = append(errs, "Networking: "+sentence(err))
	}
	errs = append(errs, a.ensureInfra(ctx, ds)...)
	errs = append(errs, a.applyVolumes(ctx, ds, allowRemove)...)
	rerrs, unknown := a.reconcileReplicas(ctx, ds, allowRemove)
	errs = append(errs, rerrs...)
	if ds.Ingress != nil {
		if err := a.o.Ingress.Apply(ctx, *ds.Ingress, ds.TCPProxies); err != nil {
			errs = append(errs, "Ingress: "+sentence(err))
		}
		errs = append(errs, a.applyUpstreams(ds.Ingress.Upstreams)...)
	}
	a.mu.Lock()
	if !ds.Host.Maintenance && !a.maintOn && len(a.maintenance) > 0 {
		a.maintenance = map[string]bool{}
	}
	a.mu.Unlock()
	return errs, unknown
}

// sentence makes an error text end like a sentence.
func sentence(err error) string {
	s := strings.TrimSpace(err.Error())
	if s == "" {
		return "Unknown error."
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// applyUpstreams pushes upstream answers that changed. While the agent runs
// from its cache, local replicas its own health checker sees failing are
// left out, unless that would leave none: then every replica stays (the
// panic threshold of 7.4, fail open).
func (a *Agent) applyUpstreams(sets []proto.UpstreamSet) []string {
	a.mu.Lock()
	cached := a.cached
	bad := map[string]bool{}
	if cached {
		for _, r := range a.replicas {
			if r.localUnhealthy && r.spec.IP != "" {
				bad[r.spec.IP] = true
			}
		}
	}
	prev := a.upstreams
	a.mu.Unlock()
	next := map[string][]string{}
	var errs []string
	for _, s := range sets {
		ips, _ := FailOpen(s.IPs, bad)
		next[s.Name] = ips
		if reflect.DeepEqual(prev[s.Name], ips) {
			continue
		}
		if err := a.o.Ingress.SetUpstreams(s.Name, ips); err != nil {
			errs = append(errs, fmt.Sprintf("Upstream %s: %s", s.Name, sentence(err)))
			delete(next, s.Name)
		}
	}
	for name := range prev {
		if _, ok := next[name]; !ok {
			if err := a.o.Ingress.SetUpstreams(name, nil); err != nil {
				errs = append(errs, fmt.Sprintf("Upstream %s: %s", name, sentence(err)))
				next[name] = prev[name]
			}
		}
	}
	a.mu.Lock()
	a.upstreams = next
	a.mu.Unlock()
	return errs
}

// FailOpen removes unhealthy IPs from an upstream set unless none would be
// left; then it keeps them all and reports panic (7.4).
func FailOpen(ips []string, unhealthy map[string]bool) ([]string, bool) {
	if len(unhealthy) == 0 {
		return append([]string(nil), ips...), false
	}
	var ok []string
	for _, ip := range ips {
		if !unhealthy[ip] {
			ok = append(ok, ip)
		}
	}
	if len(ok) == 0 && len(ips) > 0 {
		return append([]string(nil), ips...), true
	}
	return ok, false
}

// ensureInfra starts BuildKit (build role), the registry (control host, and
// its forwarder on other build hosts) and Caddy (ingress role) when their
// image changed or the last try failed.
func (a *Agent) ensureInfra(ctx context.Context, ds *proto.DesiredState) []string {
	var errs []string
	a.mu.Lock()
	inf := a.infra
	a.mu.Unlock()
	if hasRole(ds.Host, "build") {
		img := ds.Host.Images[ImageKeyBuildKit]
		if !inf.buildOK || inf.buildkit != img {
			if err := a.o.Builder.EnsureRunning(ctx, img); err != nil {
				errs = append(errs, "Builder: "+sentence(err))
				inf.buildOK = false
			} else {
				inf.buildOK, inf.buildkit = true, img
			}
		}
	}
	// After the builder, so the build network gateway exists for the
	// registry listener on it.
	if err := a.ensureRegistry(ctx, ds); err != nil {
		errs = append(errs, "Registry: "+sentence(err))
	}
	if ds.Ingress != nil {
		img := ds.Host.Images[ImageKeyCaddy]
		if !inf.caddyOK || inf.caddy != img {
			if err := a.o.Ingress.EnsureRunning(ctx, img); err != nil {
				errs = append(errs, "Caddy: "+sentence(err))
				inf.caddyOK = false
			} else {
				inf.caddyOK, inf.caddy = true, img
			}
		}
	}
	a.mu.Lock()
	a.infra = inf
	a.mu.Unlock()
	return errs
}

// track returns the replica entry for a spec, creating it.
func (a *Agent) track(s proto.ReplicaSpec) *replica {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.replicas[s.ID]
	if !ok {
		r = &replica{status: proto.ReplicaStatus{ReplicaID: s.ID, DeploymentID: s.DeploymentID}}
		a.replicas[s.ID] = r
	}
	r.spec = s
	if r.proc == nil {
		r.proc = logstore.NewProcessor(logstore.Source{EnvironmentID: s.EnvironmentID, ServiceID: s.ServiceID, DeploymentID: s.DeploymentID, ReplicaID: s.ID}, logstore.NewMasker(s.Env))
	}
	return r
}

func (a *Agent) reconcileReplicas(ctx context.Context, ds *proto.DesiredState, allowRemove bool) ([]string, []proto.ReplicaStatus) {
	list, err := a.eng.List(ctx, engine.LabelReplica)
	if err != nil {
		return []string{"Docker: " + sentence(err)}, nil
	}
	byRep := map[string]engine.Container{}
	var strays []engine.Container // a second container claiming the same replica
	for _, c := range list {
		id := c.Labels[engine.LabelReplica]
		if prev, ok := byRep[id]; ok {
			if c.Name == ContainerName(id) {
				strays = append(strays, prev)
				byRep[id] = c
			} else {
				strays = append(strays, c)
			}
			continue
		}
		byRep[id] = c
	}
	want := map[string]bool{}
	for _, s := range ds.Replicas {
		want[s.ID] = true
		r := a.track(s)
		c, exists := byRep[s.ID]
		a.mu.Lock()
		busy := r.busy
		// In maintenance nothing is created or started (9.16), also after a
		// reboot while the host is still in maintenance.
		inMaint := a.maintenance[s.ID] || ds.Host.Maintenance
		a.mu.Unlock()
		if busy {
			continue
		}
		switch {
		case s.Desired == "stopped":
			if exists && c.Running() {
				if allowRemove {
					a.goStop(ctx, r, c)
				}
			} else if exists {
				a.observe(r, c)
			}
		case inMaint:
			if exists {
				a.observe(r, c)
			}
		case !exists:
			a.goCreate(ctx, r, ds, nil)
		case c.Labels[engine.LabelSpec] != SpecHash(s):
			if allowRemove {
				a.goCreate(ctx, r, ds, &c)
			} else {
				a.observe(r, c)
			}
		case !c.Running():
			if a.shouldStart(r, c) {
				a.goStart(ctx, r, c)
			} else {
				a.observe(r, c)
			}
		default:
			a.observe(r, c)
		}
	}
	var unknown []proto.ReplicaStatus
	for id, c := range byRep {
		if want[id] {
			continue
		}
		switch {
		case ds.Adopt:
			unknown = append(unknown, a.containerStatus(c))
		case allowRemove:
			a.goRemove(ctx, id, c)
		}
	}
	for _, c := range strays {
		if allowRemove && !ds.Adopt {
			a.goRemove(ctx, "", c)
		} else if ds.Adopt {
			unknown = append(unknown, a.containerStatus(c))
		}
	}
	sort.Slice(unknown, func(i, j int) bool { return unknown[i].ContainerID < unknown[j].ContainerID })
	// Forget replicas that neither the state nor Docker knows any more.
	a.mu.Lock()
	for id, r := range a.replicas {
		if _, ok := byRep[id]; !ok && !want[id] && !r.busy {
			a.stopFollowersLocked(r)
			delete(a.replicas, id)
		}
	}
	a.mu.Unlock()
	return nil, unknown
}

// shouldStart decides whether a stopped container of a running replica is
// started by the agent. Docker's restart policy owns crash restarts, so the
// agent starts only containers that never ran, that it stopped itself, or
// that stopped because the host rebooted.
func (a *Agent) shouldStart(r *replica, c engine.Container) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case c.State == "created":
		return true
	case r.stoppedByAgent:
		return true
	case !a.bootTime.IsZero() && !c.FinishedAt.IsZero() && c.FinishedAt.Before(a.bootTime):
		return true
	}
	return false
}

// observe brings the agent's view of a replica in line with its container:
// for a running container it attaches the log follower and health checker
// (after an agent restart); for a stopped one it records the final state.
func (a *Agent) observe(r *replica, c engine.Container) {
	a.mu.Lock()
	r.cid = c.ID
	needFollow := c.Running() && !c.StartedAt.Equal(r.followStarted)
	a.mu.Unlock()
	if needFollow {
		a.startFollowers(r, c)
		return
	}
	if !c.Running() {
		a.recordExit(r, c, false)
	}
}

// containerStatus is the status of a container from Docker alone.
func (a *Agent) containerStatus(c engine.Container) proto.ReplicaStatus {
	st := proto.ReplicaStatus{
		ReplicaID: c.Labels[engine.LabelReplica], DeploymentID: c.Labels[engine.LabelDeployment],
		ContainerID: c.ID, Restarts: c.RestartCount, OOMKilled: c.OOMKilled, Labels: c.Labels, Image: c.Image,
		At: time.Now().UnixMilli(),
	}
	if !c.StartedAt.IsZero() {
		st.StartedAt = c.StartedAt.UnixMilli()
	}
	switch {
	case c.Running():
		st.State = "running"
	case c.State == "created":
		st.State = "starting"
	default:
		st.State = "exited"
		code := c.ExitCode
		st.ExitCode = &code
	}
	return st
}

// setStatus updates a replica's status and sends it when it changed.
func (a *Agent) setStatus(r *replica, fn func(*proto.ReplicaStatus)) {
	a.mu.Lock()
	old := r.status
	st := old
	st.ReplicaID = r.spec.ID
	st.DeploymentID = r.spec.DeploymentID
	if r.cid != "" {
		st.ContainerID = r.cid
	}
	if !r.startedAt.IsZero() {
		st.StartedAt = r.startedAt.UnixMilli()
	}
	fn(&st)
	st.At = old.At
	changed := !reflect.DeepEqual(old, st)
	if changed {
		st.At = time.Now().UnixMilli()
		r.status = st
	}
	a.mu.Unlock()
	if changed {
		a.send(proto.TReplicaStatus, "", st)
	}
}

// sendAllStatuses re-sends every known replica's status (after hello).
func (a *Agent) sendAllStatuses() {
	a.mu.Lock()
	var out []proto.ReplicaStatus
	for _, r := range a.replicas {
		if r.status.State != "" {
			out = append(out, r.status)
		}
	}
	a.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ReplicaID < out[j].ReplicaID })
	for _, st := range out {
		a.send(proto.TReplicaStatus, "", st)
	}
}

func (a *Agent) systemLine(r *replica, level, msg string) {
	a.mu.Lock()
	p := r.proc
	env := r.spec.EnvironmentID
	a.mu.Unlock()
	if p == nil {
		return
	}
	_ = a.logs.Append(env, p.System(level, msg))
}

func (a *Agent) setBusy(r *replica, v bool) bool {
	a.mu.Lock()
	if v && r.busy {
		a.mu.Unlock()
		return false
	}
	r.busy = v
	a.mu.Unlock()
	if !v {
		// A desired state that arrived while this replica was busy was
		// skipped for it; reconcile again now that it is free.
		a.trigger()
	}
	return true
}

// goCreate pulls, creates and starts a replica in the background. With old
// set, that container is stopped and removed first (a changed spec).
func (a *Agent) goCreate(ctx context.Context, r *replica, ds *proto.DesiredState, old *engine.Container) {
	if !a.setBusy(r, true) {
		return
	}
	sem := a.createSem()
	go func() {
		defer a.setBusy(r, false)
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-sem }()
		if old != nil {
			a.mu.Lock()
			r.removing = true
			a.mu.Unlock()
			a.stopFollowers(r)
			a.stopAndRemove(ctx, *old)
			a.mu.Lock()
			r.removing = false
			a.mu.Unlock()
		}
		a.createReplica(ctx, r, ds)
		a.trigger()
	}()
}

func (a *Agent) createReplica(ctx context.Context, r *replica, ds *proto.DesiredState) {
	a.mu.Lock()
	s := r.spec
	a.mu.Unlock()
	fail := func(diag string) {
		a.systemLine(r, logstore.LevelError, diag)
		a.setStatus(r, func(st *proto.ReplicaStatus) {
			st.State = "crashed"
			st.Diagnosis = diag
		})
	}
	have, err := a.eng.ImageExists(ctx, s.Image)
	if err != nil {
		fail("Docker could not check the image " + s.Image + ": " + sentence(err))
		return
	}
	if !have {
		a.setStatus(r, func(st *proto.ReplicaStatus) { st.State = "pulling"; st.Diagnosis = ""; st.ExitCode = nil })
		a.systemLine(r, logstore.LevelInfo, "Pulling image "+s.Image)
		a.disk.beforePull(ctx)
		if err := a.eng.Pull(ctx, s.Image, a.pullAuth(ds.Host, s.Image)); err != nil {
			fail(PullDiagnosis(s.Image, err))
			return
		}
	}
	in := specInput{Epoch: ds.Epoch, DataDir: a.o.DataDir, AssetPaths: a.assetPaths, ToolPaths: map[string]string{}}
	in.DNS = a.o.Network.DNSServer(s.EnvironmentID)
	if s.ClusterWrap && len(s.Entrypoint) == 0 && len(s.Cmd) == 0 {
		if cfg, err := a.eng.ImageConfig(ctx, s.Image); err == nil {
			in.ImageArgv = append(append([]string{}, cfg.Entrypoint...), cfg.Cmd...)
		}
	}
	for _, m := range s.Mounts {
		if m.Kind == "tool" {
			p, err := a.tools.Path(ctx, m.Source)
			if err != nil {
				fail("The tool " + m.Source + " could not be prepared: " + sentence(err))
				return
			}
			in.ToolPaths[m.Source] = p
		}
	}
	fp, err := a.writeFiles(filepath.Join(a.o.DataDir, "replicas", s.ID, "files"), s.Files)
	if err != nil {
		fail(sentence(err))
		return
	}
	in.FilePaths = fp
	sp, notes, err := buildSpec(s, in)
	if err != nil {
		fail(sentence(err))
		return
	}
	for _, n := range notes {
		a.systemLine(r, logstore.LevelWarn, n)
	}
	a.setStatus(r, func(st *proto.ReplicaStatus) {
		st.State = "starting"
		st.Diagnosis = ""
		st.ExitCode = nil
		st.OOMKilled = false
	})
	id, err := a.eng.Create(ctx, sp)
	if err != nil {
		// A container with that name may exist without our labels.
		if c, ierr := a.eng.Inspect(ctx, sp.Name); ierr == nil && c.Labels[engine.LabelReplica] == s.ID {
			id = c.ID
		} else {
			fail("The container could not be created: " + sentence(err))
			return
		}
	}
	a.mu.Lock()
	r.cid = id
	a.mu.Unlock()
	if s.IP != "" {
		n := findNetwork(ds.Networks, s.Network)
		if err := a.o.Network.AllowEgress(ctx, s.IP, n); err != nil {
			if n.Egress == "blocked" {
				fail("The egress rules for this replica could not be applied, so it was not started: " + sentence(err))
				return
			}
			a.log.Warn("could not apply egress rules", "replica", s.ID, "err", err)
		}
	}
	if err := a.eng.Start(ctx, id); err != nil {
		fail("The container could not start: " + sentence(err))
		return
	}
	c, err := a.eng.Inspect(ctx, id)
	if err != nil {
		c = engine.Container{ID: id, State: "running", StartedAt: time.Now(), Labels: sp.Labels}
	}
	a.mu.Lock()
	r.stoppedByAgent = false
	a.mu.Unlock()
	a.systemLine(r, logstore.LevelInfo, "Started container "+sp.Name)
	a.startFollowers(r, c)
}

func findNetwork(nets []proto.Network, name string) proto.Network {
	for _, n := range nets {
		if n.Name == name {
			return n
		}
	}
	return proto.Network{Name: name, Egress: "allowed"}
}

func (a *Agent) pullAuth(h proto.HostConfig, image string) *proto.Registry {
	reg := h.RegistryPull
	if reg.Address != "" && strings.HasPrefix(image, reg.Address+"/") {
		return &reg
	}
	if h.RegistryPush != nil && h.RegistryPush.Address != "" && strings.HasPrefix(image, h.RegistryPush.Address+"/") {
		p := *h.RegistryPush
		return &p
	}
	return nil
}

// writeFiles writes template files under dir and returns container path to
// host path. Files are written through a temporary file and a rename.
func (a *Agent) writeFiles(dir string, files []proto.FileSpec) (map[string]string, error) {
	out := map[string]string{}
	if len(files) == 0 {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create %s: %w", dir, err)
	}
	for _, f := range files {
		if err := validateFile(f); err != nil {
			return nil, err
		}
		mode := os.FileMode(f.Mode & 0o777)
		if mode == 0 {
			mode = 0o644
		}
		p := filepath.Join(dir, fileName(f.Path))
		if err := writeAtomic(p, f.Content, mode); err != nil {
			return nil, err
		}
		out[f.Path] = p
	}
	return out, nil
}

func writeAtomic(p string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), mode)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), p)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("could not write %s: %w", p, werr)
	}
	return nil
}

func (a *Agent) goStart(ctx context.Context, r *replica, c engine.Container) {
	if !a.setBusy(r, true) {
		return
	}
	go func() {
		defer a.setBusy(r, false)
		if err := a.eng.Start(ctx, c.ID); err != nil {
			a.setStatus(r, func(st *proto.ReplicaStatus) {
				st.State = "crashed"
				st.Diagnosis = "The container could not start: " + sentence(err)
			})
			return
		}
		a.mu.Lock()
		r.stoppedByAgent = false
		r.cid = c.ID
		a.mu.Unlock()
		a.setStatus(r, func(st *proto.ReplicaStatus) { st.State = "starting"; st.Diagnosis = ""; st.ExitCode = nil })
		if nc, err := a.eng.Inspect(ctx, c.ID); err == nil {
			c = nc
		}
		a.startFollowers(r, c)
	}()
}

func grace(c engine.Container, spec *proto.ReplicaSpec) time.Duration {
	if spec != nil && spec.StopGraceSec > 0 {
		return time.Duration(spec.StopGraceSec) * time.Second
	}
	if n, err := strconv.Atoi(c.Labels[engine.LabelStopGrace]); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultStopGraceSec * time.Second
}

// goStop stops a replica that is desired "stopped" (drain end, 7.3).
func (a *Agent) goStop(ctx context.Context, r *replica, c engine.Container) {
	if !a.setBusy(r, true) {
		return
	}
	go func() {
		defer a.setBusy(r, false)
		a.stopReplica(ctx, r, c)
	}()
}

func (a *Agent) stopReplica(ctx context.Context, r *replica, c engine.Container) {
	a.mu.Lock()
	r.stoppedByAgent = true
	spec := r.spec
	a.mu.Unlock()
	a.stopHealth(r)
	if err := a.eng.Stop(ctx, c.ID, grace(c, &spec)); err != nil && !errors.Is(err, engine.ErrNotFound) {
		a.log.Warn("could not stop a replica", "replica", spec.ID, "err", err)
		return
	}
	a.setStatus(r, func(st *proto.ReplicaStatus) { st.State = "stopped"; st.Diagnosis = "" })
}

// goRemove stops (SIGTERM, then SIGKILL after the grace) and removes a
// container that left the desired state.
func (a *Agent) goRemove(ctx context.Context, id string, c engine.Container) {
	a.mu.Lock()
	r := a.replicas[id]
	if r != nil {
		if r.busy {
			a.mu.Unlock()
			return
		}
		r.busy, r.removing = true, true
	}
	a.mu.Unlock()
	go func() {
		if r != nil {
			a.stopFollowers(r)
		}
		a.stopAndRemove(ctx, c)
		if id != "" {
			_ = os.RemoveAll(filepath.Join(a.o.DataDir, "replicas", id))
		}
		if r == nil {
			return
		}
		a.setStatus(r, func(st *proto.ReplicaStatus) { st.State = "removed"; st.Diagnosis = "" })
		a.mu.Lock()
		r.busy, r.removing = false, false
		if a.replicas[id] == r {
			delete(a.replicas, id)
		}
		a.mu.Unlock()
	}()
}

func (a *Agent) stopAndRemove(ctx context.Context, c engine.Container) {
	if c.Running() {
		if err := a.eng.Stop(ctx, c.ID, grace(c, nil)); err != nil && !errors.Is(err, engine.ErrNotFound) {
			a.log.Warn("could not stop a container", "container", c.Name, "err", err)
		}
	}
	if err := a.eng.Remove(ctx, c.ID, true); err != nil && !errors.Is(err, engine.ErrNotFound) {
		a.log.Warn("could not remove a container", "container", c.Name, "err", err)
	}
}
