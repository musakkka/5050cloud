// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/sample"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// FactSources says where host facts are read from. Root prefixes /etc,
// /sys and /var/run (tests point it at a temp dir); Roots are procfs and
// cgroup.
type FactSources struct {
	Root     string
	Roots    sample.Roots
	DataDir  string
	Version  string
	PublicIP string
	Engine   engine.Engine // nil leaves Facts.Docker empty
}

// GatherFacts collects what the control plane needs to know about a host
// (join and hello, docs/SPEC.md 7.1).
func GatherFacts(ctx context.Context, src FactSources) proto.Facts {
	if src.Roots.Proc == "" {
		src.Roots = sample.DefaultRoots
	}
	root := src.Root
	f := proto.Facts{
		Version:  src.Version,
		Arch:     runtime.GOARCH,
		OS:       osRelease(filepath.Join(root, "etc", "os-release")),
		CPUs:     runtime.NumCPU(),
		PublicIP: src.PublicIP,
		DataDir:  src.DataDir,
	}
	if b, err := os.ReadFile(filepath.Join(src.Roots.Proc, "sys", "kernel", "osrelease")); err == nil {
		f.Kernel = strings.TrimSpace(string(b))
	}
	if _, cpus, err := src.Roots.ReadCPU(); err == nil && cpus > 0 {
		f.CPUs = cpus
	}
	if total, _, err := src.Roots.Memory(); err == nil {
		f.MemBytes = total
	}
	if src.Engine != nil {
		if v, err := src.Engine.Version(ctx); err == nil {
			f.Docker = v
		}
	}
	for _, m := range factMounts(src.DataDir) {
		if used, total, err := sample.Statfs(m); err == nil {
			f.Disks = append(f.Disks, proto.Disk{Mount: m, Used: used, Total: total})
		}
	}
	f.WireGuard = exists(filepath.Join(root, "sys", "module", "wireguard")) || moduleAvailable(root, src.Roots.Proc, "wireguard")
	f.RebootRequired = exists(filepath.Join(root, "var", "run", "reboot-required")) || exists(filepath.Join(root, "run", "reboot-required"))
	return f
}

func factMounts(dataDir string) []string {
	out := []string{"/"}
	if st, err := os.Stat("/data"); err == nil && st.IsDir() && !sample.SameDevice("/", "/data") {
		out = append(out, "/data")
	}
	if dataDir != "" && !sample.SameDevice("/", dataDir) && !(len(out) > 1 && sample.SameDevice("/data", dataDir)) {
		out = append(out, dataDir)
	}
	return out
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// moduleAvailable reports whether a kernel module is built in or can be
// loaded for the running kernel.
func moduleAvailable(root, proc, name string) bool {
	rel, err := os.ReadFile(filepath.Join(proc, "sys", "kernel", "osrelease"))
	if err != nil {
		return false
	}
	dir := filepath.Join(root, "lib", "modules", strings.TrimSpace(string(rel)))
	for _, list := range []string{"modules.builtin", "modules.dep"} {
		f, err := os.Open(filepath.Join(dir, list))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			mod, _, _ := strings.Cut(sc.Text(), ":")
			base := filepath.Base(mod)
			if base == name+".ko" || strings.HasPrefix(base, name+".ko.") {
				f.Close()
				return true
			}
		}
		f.Close()
	}
	return false
}

// osRelease returns "ubuntu 24.04" from /etc/os-release.
func osRelease(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return runtime.GOOS
	}
	kv := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			kv[k] = strings.Trim(v, `"'`)
		}
	}
	id, ver := kv["ID"], kv["VERSION_ID"]
	switch {
	case id != "" && ver != "":
		return id + " " + ver
	case id != "":
		return id
	}
	return runtime.GOOS
}

// buildHello is the first frame on every connection: facts, the last
// applied generation and epoch, whether the agent runs from its cache, and
// every container labelled 5050.replica on the host (7.1 step 3).
func (a *Agent) buildHello(ctx context.Context) proto.Hello {
	h := proto.Hello{
		HostID:    a.o.HostID,
		MachineID: a.o.MachineID,
		Facts: GatherFacts(ctx, FactSources{
			Root: a.o.FactsRoot, Roots: a.roots, DataDir: a.o.DataDir, Version: a.o.Version,
			PublicIP: a.o.PublicIP, Engine: a.eng,
		}),
		Replicas: []proto.ReplicaStatus{},
	}
	if k, err := a.o.Network.PublicKey(); err == nil {
		h.WGPublicKey = k
	} else {
		a.log.Warn("could not read the WireGuard public key", "err", err)
	}
	a.mu.Lock()
	h.Applied, h.Epoch, h.Cached = a.applied, a.epoch, a.cached || a.desired == nil
	a.mu.Unlock()
	h.Replicas = a.replicaReport(ctx)
	return h
}

// replicaReport lists every replica container with the agent's status
// when it has one, else what Docker says.
func (a *Agent) replicaReport(ctx context.Context) []proto.ReplicaStatus {
	out := []proto.ReplicaStatus{}
	list, err := a.eng.List(ctx, engine.LabelReplica)
	if err != nil {
		a.log.Warn("could not list containers for hello", "err", err)
		return out
	}
	a.mu.Lock()
	for _, c := range list {
		id := c.Labels[engine.LabelReplica]
		st := proto.ReplicaStatus{}
		if r := a.replicas[id]; r != nil && r.status.State != "" && (r.cid == "" || r.cid == c.ID) {
			st = r.status
			st.ContainerID = c.ID
			st.Labels = c.Labels
			st.Image = c.Image
		} else {
			st = a.containerStatus(c)
		}
		out = append(out, st)
	}
	a.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReplicaID != out[j].ReplicaID {
			return out[i].ReplicaID < out[j].ReplicaID
		}
		return out[i].ContainerID < out[j].ContainerID
	})
	return out
}

// Live is state the agent shares with parts built before it: the current
// host token (rotated, 7.1) and the infrastructure image overrides of the
// last desired state (HostConfig.Images). cmd/5050-agent hands its
// methods to the builder.
type Live struct {
	mu     sync.RWMutex
	token  string
	images map[string]string
}

// NewLive returns a Live holding token.
func NewLive(token string) *Live { return &Live{token: token, images: map[string]string{}} }

// Token returns the current host token.
func (l *Live) Token() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.token
}

// Images returns a copy of HostConfig.Images.
func (l *Live) Images() map[string]string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]string, len(l.images))
	for k, v := range l.images {
		out[k] = v
	}
	return out
}

func (l *Live) setToken(t string) {
	l.mu.Lock()
	l.token = t
	l.mu.Unlock()
}

func (l *Live) setImages(m map[string]string) {
	l.mu.Lock()
	l.images = make(map[string]string, len(m))
	for k, v := range m {
		l.images[k] = v
	}
	l.mu.Unlock()
}
