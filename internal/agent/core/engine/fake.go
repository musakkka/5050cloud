// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Fake is an in-memory Engine for unit tests. Containers "run" as soon as
// they start and stay up until stopped, killed or ExitContainer is called.
// It records every mutating call in Calls.
type Fake struct {
	mu         sync.Mutex
	containers map[string]*fakeContainer
	images     map[string]Image // by ref
	imageCfg   map[string]ImageConfig
	volumes    map[string]Volume
	subs       []chan Event
	seq        int
	now        func() time.Time

	// Calls lists mutating calls as "create 5050-rep_1", "stop 5050-rep_1" and so on.
	Calls []string
	// PullErr makes Pull of a ref fail with that error.
	PullErr map[string]error
	// ExecFn answers Exec; the default prints nothing and exits 0.
	ExecFn func(containerName string, s ExecSpec, stdin []byte) (output string, exit int)
	// LogLines are returned by Logs per container name.
	LogLines map[string][]LogLine
	// StatsFn answers Stats.
	StatsFn func(name string) Stats
	// RunFn decides what a started container does: it returns the exit code
	// and true to exit right away (jobs), or false to keep running.
	RunFn func(c Container) (exit int, exits bool)
	// StartErr makes Start fail for a container name.
	StartErr map[string]error
	// Gateways answers NetworkGateway by network name.
	Gateways map[string]string
}

type fakeContainer struct {
	c       Container
	spec    Spec
	stdin   bytes.Buffer
	waiters []chan int
}

// NewFake returns an empty fake engine.
func NewFake() *Fake {
	return &Fake{
		containers: map[string]*fakeContainer{},
		images:     map[string]Image{},
		imageCfg:   map[string]ImageConfig{},
		volumes:    map[string]Volume{},
		PullErr:    map[string]error{},
		LogLines:   map[string][]LogLine{},
		StartErr:   map[string]error{},
		Gateways:   map[string]string{},
		now:        time.Now,
	}
}

func (f *Fake) record(s string) { f.Calls = append(f.Calls, s) }

// CallsSnapshot returns a copy of Calls.
func (f *Fake) CallsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Calls...)
}

// ResetCalls clears Calls.
func (f *Fake) ResetCalls() {
	f.mu.Lock()
	f.Calls = nil
	f.mu.Unlock()
}

// AddImage makes ref present locally.
func (f *Fake) AddImage(ref string, cfg ImageConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.images[ref] = Image{ID: fmt.Sprintf("sha256:%064d", f.seq), Tags: []string{ref}, Created: f.now().Add(time.Duration(f.seq) * time.Second)}
	f.imageCfg[ref] = cfg
}

// AddContainer inserts a container as if someone else created it.
func (f *Fake) AddContainer(c Container) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if c.ID == "" {
		c.ID = fmt.Sprintf("%064d", f.seq)
	}
	if c.State == "" {
		c.State = "running"
	}
	if c.IPs == nil {
		c.IPs = map[string]string{}
	}
	f.containers[c.ID] = &fakeContainer{c: c}
	return c.ID
}

// Container returns a container by name or id.
func (f *Fake) Container(name string) (Container, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(name)
	if fc == nil {
		return Container{}, false
	}
	return fc.c, true
}

// SpecOf returns the spec a container was created with.
func (f *Fake) SpecOf(name string) (Spec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(name)
	if fc == nil {
		return Spec{}, false
	}
	return fc.spec, true
}

// StdinOf returns what was written to a container's stdin.
func (f *Fake) StdinOf(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(name)
	if fc == nil {
		return ""
	}
	return fc.stdin.String()
}

// ExitContainer makes a running container exit with code, as a crash would.
func (f *Fake) ExitContainer(name string, code int, oom bool) {
	f.mu.Lock()
	fc := f.find(name)
	if fc == nil {
		f.mu.Unlock()
		return
	}
	f.exitLocked(fc, code, oom)
	f.mu.Unlock()
}

// SetRestartCount sets a container's restart count.
func (f *Fake) SetRestartCount(name string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fc := f.find(name); fc != nil {
		fc.c.RestartCount = n
	}
}

// Emit sends an event to every Events subscriber.
func (f *Fake) Emit(ev Event) {
	f.mu.Lock()
	subs := append([]chan Event(nil), f.subs...)
	f.mu.Unlock()
	for _, s := range subs {
		select {
		case s <- ev:
		default:
		}
	}
}

func (f *Fake) exitLocked(fc *fakeContainer, code int, oom bool) {
	fc.c.State = "exited"
	fc.c.ExitCode = code
	fc.c.OOMKilled = oom
	fc.c.Pid = 0
	fc.c.FinishedAt = f.now()
	for _, w := range fc.waiters {
		w <- code
		close(w)
	}
	fc.waiters = nil
	ev := Event{ContainerID: fc.c.ID, Action: "die", Labels: fc.c.Labels, ExitCode: fmt.Sprint(code), At: f.now()}
	go f.Emit(ev)
	if oom {
		go f.Emit(Event{ContainerID: fc.c.ID, Action: "oom", Labels: fc.c.Labels, At: f.now()})
	}
}

func (f *Fake) find(idOrName string) *fakeContainer {
	if fc, ok := f.containers[idOrName]; ok {
		return fc
	}
	for _, fc := range f.containers {
		if fc.c.Name == idOrName || strings.HasPrefix(fc.c.ID, idOrName) && len(idOrName) >= 12 {
			return fc
		}
	}
	return nil
}

func (f *Fake) Version(context.Context) (string, error) { return "28.0.0-fake", nil }

func (f *Fake) List(_ context.Context, labelKey string) ([]Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Container
	for _, fc := range f.containers {
		if _, ok := fc.c.Labels[labelKey]; ok {
			out = append(out, fc.c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) Inspect(_ context.Context, id string) (Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(id)
	if fc == nil {
		return Container{}, fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	return fc.c, nil
}

func (f *Fake) Create(_ context.Context, s Spec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.find(s.Name) != nil {
		return "", fmt.Errorf("conflict: the container name %s is already in use", s.Name)
	}
	if _, ok := f.images[s.Image]; !ok {
		return "", fmt.Errorf("%w: image %s", ErrNotFound, s.Image)
	}
	f.seq++
	id := fmt.Sprintf("%064x", f.seq)
	c := Container{ID: id, Name: s.Name, Image: s.Image, ImageID: f.images[s.Image].ID, Labels: s.Labels, State: "created", Created: f.now(), IPs: map[string]string{}, MemLimit: s.MemoryBytes}
	if s.Network != "" {
		c.IPs[s.Network] = s.IP
	}
	for _, m := range s.Mounts {
		if m.Type == MountVolume {
			c.Volumes = append(c.Volumes, m.Source)
		}
	}
	f.containers[id] = &fakeContainer{c: c, spec: s}
	f.record("create " + s.Name)
	return id, nil
}

func (f *Fake) Start(_ context.Context, id string) error {
	f.mu.Lock()
	fc := f.find(id)
	if fc == nil {
		f.mu.Unlock()
		return fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	if err := f.StartErr[fc.c.Name]; err != nil {
		f.mu.Unlock()
		return err
	}
	f.record("start " + fc.c.Name)
	fc.c.State = "running"
	fc.c.ExitCode = 0
	fc.c.OOMKilled = false
	f.seq++
	fc.c.Pid = 10000 + f.seq
	fc.c.StartedAt = f.now()
	c := fc.c
	run := f.RunFn
	f.mu.Unlock()
	go f.Emit(Event{ContainerID: c.ID, Action: "start", Labels: c.Labels, At: f.now()})
	if run != nil {
		if code, exits := run(c); exits {
			go func() {
				time.Sleep(5 * time.Millisecond)
				f.mu.Lock()
				if fc := f.find(c.ID); fc != nil && fc.c.State == "running" {
					f.exitLocked(fc, code, false)
				}
				f.mu.Unlock()
			}()
		}
	}
	return nil
}

func (f *Fake) Stop(_ context.Context, id string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(id)
	if fc == nil {
		return fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	f.record("stop " + fc.c.Name)
	if fc.c.State == "running" {
		f.exitLocked(fc, 143, false)
	}
	return nil
}

func (f *Fake) Kill(_ context.Context, id, signal string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(id)
	if fc == nil {
		return fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	f.record("kill " + fc.c.Name)
	if fc.c.State == "running" {
		f.exitLocked(fc, 137, false)
	}
	return nil
}

func (f *Fake) Remove(_ context.Context, id string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(id)
	if fc == nil {
		return fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	if fc.c.State == "running" && !force {
		return fmt.Errorf("conflict: container %s is running", fc.c.Name)
	}
	f.record("remove " + fc.c.Name)
	for _, w := range fc.waiters {
		w <- fc.c.ExitCode
		close(w)
	}
	delete(f.containers, fc.c.ID)
	return nil
}

func (f *Fake) Wait(ctx context.Context, id string) (int, error) {
	f.mu.Lock()
	fc := f.find(id)
	if fc == nil {
		f.mu.Unlock()
		return -1, fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	if fc.c.State != "running" && fc.c.State != "created" {
		code := fc.c.ExitCode
		f.mu.Unlock()
		return code, nil
	}
	ch := make(chan int, 1)
	fc.waiters = append(fc.waiters, ch)
	f.mu.Unlock()
	select {
	case code := <-ch:
		return code, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

type fakeStdin struct {
	f  *Fake
	id string
}

func (w fakeStdin) Write(p []byte) (int, error) {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	if fc := w.f.find(w.id); fc != nil {
		fc.stdin.Write(p)
	}
	return len(p), nil
}
func (w fakeStdin) Close() error { return nil }

func (f *Fake) AttachStdin(_ context.Context, id string) (io.WriteCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.find(id) == nil {
		return nil, fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	return fakeStdin{f: f, id: id}, nil
}

func (f *Fake) Logs(ctx context.Context, id string, since time.Time, follow bool, fn func(LogLine)) error {
	f.mu.Lock()
	fc := f.find(id)
	if fc == nil {
		f.mu.Unlock()
		return fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	lines := append([]LogLine(nil), f.LogLines[fc.c.Name]...)
	f.mu.Unlock()
	for _, l := range lines {
		if !since.IsZero() && !l.TS.After(since) {
			continue
		}
		fn(l)
	}
	if follow {
		// Follow until the container stops, like Docker does.
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Millisecond):
			}
			f.mu.Lock()
			fc := f.find(id)
			running := fc != nil && fc.c.State == "running"
			f.mu.Unlock()
			if !running {
				return nil
			}
		}
	}
	return nil
}

func (f *Fake) Stats(_ context.Context, id string) (Stats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.find(id)
	if fc == nil {
		return Stats{}, fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	if f.StatsFn != nil {
		return f.StatsFn(fc.c.Name), nil
	}
	return Stats{}, nil
}

type fakeExec struct {
	f     *Fake
	name  string
	spec  ExecSpec
	in    bytes.Buffer
	outR  *io.PipeReader
	outW  *io.PipeWriter
	done  chan int
	start sync.Once
	mu    sync.Mutex
}

func (e *fakeExec) run() {
	e.start.Do(func() {
		go func() {
			e.mu.Lock()
			stdin := append([]byte(nil), e.in.Bytes()...)
			e.mu.Unlock()
			out, code := "", 0
			if e.f.ExecFn != nil {
				out, code = e.f.ExecFn(e.name, e.spec, stdin)
			}
			_, _ = io.WriteString(e.outW, out)
			_ = e.outW.Close()
			e.done <- code
		}()
	})
}

func (e *fakeExec) Output() io.Reader {
	if !e.spec.Stdin {
		e.run()
	}
	return e.outR
}

func (e *fakeExec) Input() io.Writer {
	return writerFunc(func(p []byte) (int, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.in.Write(p) })
}
func (e *fakeExec) CloseInput() error {
	e.run()
	return nil
}
func (e *fakeExec) Resize(context.Context, int, int) error { return nil }
func (e *fakeExec) Wait(ctx context.Context) (int, error) {
	e.run()
	select {
	case c := <-e.done:
		e.done <- c
		return c, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}
func (e *fakeExec) Close() error { return e.outR.Close() }

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

func (f *Fake) Exec(_ context.Context, id string, s ExecSpec) (ExecSession, error) {
	f.mu.Lock()
	fc := f.find(id)
	if fc == nil {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: container %s", ErrNotFound, id)
	}
	if fc.c.State != "running" {
		f.mu.Unlock()
		return nil, fmt.Errorf("conflict: container %s is not running", fc.c.Name)
	}
	f.record("exec " + fc.c.Name + " " + strings.Join(s.Cmd, " "))
	name := fc.c.Name
	f.mu.Unlock()
	r, w := io.Pipe()
	return &fakeExec{f: f, name: name, spec: s, outR: r, outW: w, done: make(chan int, 1)}, nil
}

func (f *Fake) ImageExists(_ context.Context, ref string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.images[ref]
	return ok, nil
}

func (f *Fake) ImageConfig(_ context.Context, ref string) (ImageConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.imageCfg[ref]
	if !ok {
		return ImageConfig{}, fmt.Errorf("%w: image %s", ErrNotFound, ref)
	}
	return cfg, nil
}

func (f *Fake) Pull(_ context.Context, ref string, auth *proto.Registry) error {
	f.mu.Lock()
	f.record("pull " + ref)
	err := f.PullErr[ref]
	f.mu.Unlock()
	if err != nil {
		return err
	}
	f.mu.Lock()
	_, ok := f.images[ref]
	f.mu.Unlock()
	if !ok {
		f.AddImage(ref, ImageConfig{})
	}
	return nil
}

func (f *Fake) Images(context.Context) ([]Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Image
	for _, im := range f.images {
		out = append(out, im)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *Fake) RemoveImage(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ref, im := range f.images {
		if im.ID == id || ref == id {
			for _, fc := range f.containers {
				if fc.c.ImageID == im.ID {
					return fmt.Errorf("conflict: image %s is used by container %s", ref, fc.c.Name)
				}
			}
			delete(f.images, ref)
			f.record("rmi " + ref)
			return nil
		}
	}
	return fmt.Errorf("%w: image %s", ErrNotFound, id)
}

func (f *Fake) PruneImages(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("prune images")
	return 0, nil
}

func (f *Fake) VolumeCreate(_ context.Context, name string, labels map[string]string) (Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.volumes[name]; ok {
		return v, nil
	}
	v := Volume{Name: name, Mountpoint: "/var/lib/docker/volumes/" + name + "/_data", Labels: labels, CreatedAt: f.now()}
	f.volumes[name] = v
	f.record("volume create " + name)
	return v, nil
}

// SetVolume inserts or replaces a volume (tests set Mountpoint to a temp dir).
func (f *Fake) SetVolume(v Volume) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes[v.Name] = v
}

func (f *Fake) VolumeInspect(_ context.Context, name string) (Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volumes[name]
	if !ok {
		return Volume{}, fmt.Errorf("%w: volume %s", ErrNotFound, name)
	}
	return v, nil
}

func (f *Fake) Volumes(context.Context) ([]Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Volume
	for _, v := range f.volumes {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) VolumeRemove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.volumes[name]; !ok {
		return fmt.Errorf("%w: volume %s", ErrNotFound, name)
	}
	delete(f.volumes, name)
	f.record("volume rm " + name)
	return nil
}

func (f *Fake) NetworkGateway(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	gw, ok := f.Gateways[name]
	if !ok {
		return "", fmt.Errorf("%w: network %s", ErrNotFound, name)
	}
	return gw, nil
}

func (f *Fake) Events(ctx context.Context) (<-chan Event, <-chan error) {
	ch := make(chan Event, 256)
	errc := make(chan error, 1)
	f.mu.Lock()
	f.subs = append(f.subs, ch)
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		for i, s := range f.subs {
			if s == ch {
				f.subs = append(f.subs[:i], f.subs[i+1:]...)
				break
			}
		}
		f.mu.Unlock()
	}()
	return ch, errc
}

var _ Engine = (*Fake)(nil)
