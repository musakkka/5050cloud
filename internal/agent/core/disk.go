// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/internal/agent/core/sample"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Disk thresholds (6.6, 9.19).
const (
	diskHighPct     = 75
	diskHigherPct   = 85
	diskCriticalPct = 95
	ballastBackPct  = 85
	trashKeep       = 48 * time.Hour
	jobOrphanAfter  = time.Hour
)

// diskGuard raises disk events on upward threshold crossings, releases the
// ballast file at 95% and re-creates it below 85%.
type diskGuard struct {
	a     *Agent
	mu    sync.Mutex
	level map[string]int // mount -> last threshold reported
	// topLogService names the service using the most log space, for disk.high.
	topLogService string
	topLogBytes   int64
	statfs        func(string) (int64, int64, error)
}

func newDiskGuard(a *Agent) *diskGuard {
	return &diskGuard{a: a, level: map[string]int{}, statfs: sample.Statfs}
}

func pct(d proto.Disk) int {
	if d.Total <= 0 {
		return 0
	}
	return int(d.Used * 100 / d.Total)
}

func (g *diskGuard) check(ctx context.Context, disks []proto.Disk) {
	for _, d := range disks {
		p := pct(d)
		g.mu.Lock()
		prev := g.level[d.Mount]
		lvl := 0
		switch {
		case p >= diskCriticalPct:
			lvl = diskCriticalPct
		case p >= diskHigherPct:
			lvl = diskHigherPct
		case p >= diskHighPct:
			lvl = diskHighPct
		}
		g.level[d.Mount] = lvl
		top, topBytes := g.topLogService, g.topLogBytes
		g.mu.Unlock()
		if lvl > prev {
			kind := "disk.high"
			text := fmt.Sprintf("Disk %s is %d%% full.", d.Mount, p)
			if lvl == diskCriticalPct {
				kind = "disk.critical"
				text = fmt.Sprintf("Disk %s is %d%% full; the 1 GB ballast file was released to make room.", d.Mount, p)
			}
			if top != "" {
				text += fmt.Sprintf(" The service using the most log space on this host is %s (%d MB).", top, topBytes>>20)
			}
			g.a.send(proto.TEvent, "", proto.Event{Kind: kind, Text: text, At: time.Now().UnixMilli()})
		}
	}
	g.ballast(disks)
}

// ballast releases <DataDir>/ballast when the disk holding it reaches 95%
// and re-creates it once use is below 85% (6.6).
func (g *diskGuard) ballast(disks []proto.Disk) {
	path := filepath.Join(g.a.o.DataDir, "ballast")
	used, total, err := g.statfs(g.a.o.DataDir)
	if err != nil || total <= 0 {
		return
	}
	p := int(used * 100 / total)
	_, statErr := os.Stat(path)
	exists := statErr == nil
	switch {
	case p >= diskCriticalPct && exists:
		if err := os.Remove(path); err == nil {
			g.a.log.Warn("released the ballast file", "disk_pct", p)
		}
	case p < ballastBackPct && !exists:
		size := g.a.t.ballastBytes
		if total-used < 3*size {
			return // not enough room to take it back yet
		}
		if err := allocate(path, size); err != nil {
			g.a.log.Warn("could not create the ballast file", "err", err)
		}
	}
}

// allocate creates a file of size bytes that really takes disk space.
func allocate(path string, size int64) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := fallocate(f, size); err != nil {
		// Fall back to writing zeros.
		buf := make([]byte, 1<<20)
		var written int64
		for written < size && err == nil {
			n := int64(len(buf))
			if size-written < n {
				n = size - written
			}
			_, err = f.Write(buf[:n])
			written += n
		}
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// overPrune reports whether the data disk is above HostConfig.DiskPruneAtPct.
func (g *diskGuard) overPrune() bool {
	a := g.a
	a.mu.Lock()
	limit := 80
	if a.desired != nil && a.desired.Host.DiskPruneAtPct > 0 {
		limit = a.desired.Host.DiskPruneAtPct
	}
	a.mu.Unlock()
	for _, p := range a.diskMounts() {
		used, total, err := g.statfs(p)
		if err == nil && total > 0 && int(used*100/total) > limit {
			return true
		}
	}
	return false
}

// beforePull prunes unused images when the disk is above the threshold.
func (g *diskGuard) beforePull(ctx context.Context) {
	if g.overPrune() {
		g.a.pruneImages(ctx, 1)
	}
}

// beforeBuild prunes images and build cache when the disk is above the
// threshold (8.1).
func (g *diskGuard) beforeBuild(ctx context.Context) {
	if !g.overPrune() {
		return
	}
	g.a.pruneImages(ctx, 1)
	if err := g.a.o.Builder.Prune(ctx, 5<<30); err != nil {
		g.a.log.Warn("could not prune the build cache", "err", err)
	}
}

// retentionLoop prunes old images every hour (9.19).
func (a *Agent) retentionLoop(ctx context.Context) {
	every(ctx, a.t.retentionEvery, func() {
		keep := 3
		a.mu.Lock()
		if a.desired != nil && a.desired.Host.ImageRetention > 0 {
			keep = a.desired.Host.ImageRetention
		}
		a.mu.Unlock()
		a.pruneImages(ctx, keep)
	})
}

// pruneImages keeps the newest keep images per service repository of the
// internal registry, plus every image a container uses, and removes the
// rest. Images from other registries are left alone, except dangling ones.
func (a *Agent) pruneImages(ctx context.Context, keep int) {
	a.mu.Lock()
	var prefixes []string
	if a.desired != nil {
		if p := a.desired.Host.RegistryPull.Address; p != "" {
			prefixes = append(prefixes, p+"/")
		}
		if a.desired.Host.RegistryPush != nil && a.desired.Host.RegistryPush.Address != "" {
			prefixes = append(prefixes, a.desired.Host.RegistryPush.Address+"/")
		}
	}
	a.mu.Unlock()
	if _, err := a.eng.PruneImages(ctx); err != nil {
		a.log.Warn("could not prune dangling images", "err", err)
	}
	if len(prefixes) == 0 {
		return
	}
	imgs, err := a.eng.Images(ctx)
	if err != nil {
		return
	}
	inUse := map[string]bool{}
	for _, key := range []string{engine.LabelReplica, engine.LabelJob, engine.LabelInfra} {
		cs, err := a.eng.List(ctx, key)
		if err != nil {
			return // never prune on an incomplete view
		}
		for _, c := range cs {
			inUse[c.ImageID] = true
		}
	}
	byRepo := map[string][]engine.Image{}
	for _, im := range imgs {
		repo := ""
		for _, ref := range append(append([]string{}, im.Tags...), im.Digests...) {
			for _, p := range prefixes {
				if strings.HasPrefix(ref, p) {
					repo = repoOf(ref)
				}
			}
		}
		if repo != "" {
			byRepo[repo] = append(byRepo[repo], im)
		}
	}
	for _, list := range byRepo {
		sort.Slice(list, func(i, j int) bool { return list[i].Created.After(list[j].Created) })
		for i, im := range list {
			if i < keep || inUse[im.ID] {
				continue
			}
			if err := a.eng.RemoveImage(ctx, im.ID); err != nil {
				a.log.Debug("could not remove an old image", "image", im.ID, "err", err)
			}
		}
	}
}

// applyVolumes creates the volumes of the state and moves trashed ones
// aside (9.12). Trash is a change that removes data from use, so it waits
// for a fresh state like every removal.
func (a *Agent) applyVolumes(ctx context.Context, ds *proto.DesiredState, allowRemove bool) []string {
	var errs []string
	for _, v := range ds.Volumes {
		if v.Trash {
			if allowRemove {
				if err := a.trashVolume(ctx, v.Name); err != nil {
					errs = append(errs, fmt.Sprintf("Volume %s: %s", v.Name, sentence(err)))
				}
			}
			continue
		}
		if _, err := a.eng.VolumeCreate(ctx, v.Name, map[string]string{engine.LabelVolume: v.Name, engine.LabelService: v.ServiceID}); err != nil {
			errs = append(errs, fmt.Sprintf("Volume %s: %s", v.Name, sentence(err)))
		}
	}
	return errs
}

// TrashName is the name a deleted volume is kept under for 48 hours.
func TrashName(volume string, at time.Time) string {
	return "5050-trash-" + strings.TrimPrefix(volume, "5050-") + "-" + strconv.FormatInt(at.Unix(), 10)
}

// trashVolume renames a volume by moving its data into a new volume named
// 5050-trash-<id>-<ts>; Docker volumes cannot be renamed in place.
func (a *Agent) trashVolume(ctx context.Context, name string) error {
	old, err := a.eng.VolumeInspect(ctx, name)
	if errors.Is(err, engine.ErrNotFound) {
		return nil // already gone
	}
	if err != nil {
		return err
	}
	cs, err := a.eng.List(ctx, engine.LabelReplica)
	if err != nil {
		return err
	}
	for _, c := range cs {
		for _, v := range c.Volumes {
			if v == name {
				return fmt.Errorf("the volume is still mounted by container %s", c.Name)
			}
		}
	}
	trash := TrashName(name, time.Now())
	nv, err := a.eng.VolumeCreate(ctx, trash, map[string]string{engine.LabelVolume: trash, "5050.trashed": strconv.FormatInt(time.Now().Unix(), 10)})
	if err != nil {
		return err
	}
	if old.Mountpoint == "" || nv.Mountpoint == "" {
		return errors.New("the volume data directory is unknown")
	}
	if err := os.Remove(nv.Mountpoint); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not prepare the trash volume: %w", err)
	}
	if err := os.Rename(old.Mountpoint, nv.Mountpoint); err != nil {
		_ = os.MkdirAll(nv.Mountpoint, 0o755)
		return fmt.Errorf("could not move the volume data aside: %w", err)
	}
	if err := os.MkdirAll(old.Mountpoint, 0o755); err != nil {
		return err
	}
	return a.eng.VolumeRemove(ctx, name)
}

// janitorLoop runs the log janitor, removes trashed volumes after 48 hours
// and job containers nobody waits for.
func (a *Agent) janitorLoop(ctx context.Context) {
	every(ctx, a.t.janitorEvery, func() { a.janitor(ctx) })
}

func (a *Agent) janitor(ctx context.Context) {
	a.mu.Lock()
	var b logstore.Budgets
	if a.desired != nil {
		b.RetentionDays = a.desired.Host.LogRetentionDays
		b.MaxBytesPerSvc = a.desired.Host.LogMaxBytesPerService
		b.MaxPctFS = a.desired.Host.LogMaxPctFS
	}
	a.mu.Unlock()
	if _, total, err := a.disk.statfs(filepath.Join(a.o.DataDir, "logs")); err == nil {
		b.FilesystemBytes = total
	}
	u, err := a.logs.Janitor(b)
	if err != nil {
		a.log.Warn("log janitor", "err", err)
	}
	a.disk.mu.Lock()
	a.disk.topLogService, a.disk.topLogBytes = u.TopService, u.TopBytes
	a.disk.mu.Unlock()

	if vols, err := a.eng.Volumes(ctx); err == nil {
		for _, v := range vols {
			if !strings.HasPrefix(v.Name, "5050-trash-") {
				continue
			}
			i := strings.LastIndexByte(v.Name, '-')
			ts, err := strconv.ParseInt(v.Name[i+1:], 10, 64)
			if err != nil || time.Since(time.Unix(ts, 0)) < trashKeep {
				continue
			}
			if err := a.eng.VolumeRemove(ctx, v.Name); err == nil {
				a.log.Info("removed a deleted volume after 48 hours", "volume", v.Name)
			}
		}
	}
	if jobs, err := a.eng.List(ctx, engine.LabelJob); err == nil {
		a.mu.Lock()
		running := map[string]bool{}
		for id := range a.jobs {
			running[id] = true
		}
		a.mu.Unlock()
		for _, c := range jobs {
			if running[c.Labels[engine.LabelJob]] || c.Running() {
				continue
			}
			if !c.FinishedAt.IsZero() && time.Since(c.FinishedAt) > jobOrphanAfter {
				_ = a.eng.Remove(ctx, c.ID, true)
			}
		}
	}
	if cs, err := a.eng.List(ctx, engine.LabelReplica); err == nil {
		ids := map[string]bool{}
		for _, c := range cs {
			ids[c.ID] = true
		}
		a.cursors.keep(ids)
	}
}

var _ io.Writer = (*tailBuffer)(nil)
