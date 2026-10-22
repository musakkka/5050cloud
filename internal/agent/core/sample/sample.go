// SPDX-License-Identifier: AGPL-3.0-only

// Package sample reads host and container resource usage from procfs and
// cgroup v2 files (docs/SPEC.md 8.1): CPU from /proc/stat deltas and cgroup
// cpu.stat deltas, memory from /proc/meminfo and memory.current/max,
// network counters from /proc/<pid>/net/dev, listening ports from
// /proc/<pid>/net/tcp and tcp6, disks from statfs, and volume usage by
// walking a directory within a time budget. Every reader takes its root
// directory, so the parsers are tested against fixture trees.
package sample

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Roots points the readers at procfs and the cgroup v2 mount.
type Roots struct {
	Proc   string // "/proc"
	Cgroup string // "/sys/fs/cgroup"
}

// DefaultRoots are the real system paths.
var DefaultRoots = Roots{Proc: "/proc", Cgroup: "/sys/fs/cgroup"}

// CPUTimes is the aggregate line of /proc/stat in clock ticks.
type CPUTimes struct {
	Total, Idle uint64
}

// ReadCPU reads the aggregate cpu line of /proc/stat.
func (r Roots) ReadCPU() (CPUTimes, int, error) {
	b, err := os.ReadFile(filepath.Join(r.Proc, "stat"))
	if err != nil {
		return CPUTimes{}, 0, err
	}
	var t CPUTimes
	cpus := 0
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		if f[0] == "cpu" {
			found = true
			for i, v := range f[1:] {
				n, _ := strconv.ParseUint(v, 10, 64)
				// guest and guest_nice (fields 9 and 10) are already in user and nice.
				if i >= 8 {
					break
				}
				t.Total += n
				if i == 3 || i == 4 { // idle, iowait
					t.Idle += n
				}
			}
		} else if strings.HasPrefix(f[0], "cpu") {
			cpus++
		}
	}
	if !found {
		return CPUTimes{}, 0, errors.New("no cpu line in /proc/stat")
	}
	return t, cpus, nil
}

// BootTime reads btime from /proc/stat.
func (r Roots) BootTime() (time.Time, error) {
	b, err := os.ReadFile(filepath.Join(r.Proc, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "btime ") {
			n, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(n, 0), nil
		}
	}
	return time.Time{}, errors.New("no btime in /proc/stat")
}

// Memory reads MemTotal and MemAvailable from /proc/meminfo (bytes).
func (r Roots) Memory() (total, used int64, err error) {
	b, err := os.ReadFile(filepath.Join(r.Proc, "meminfo"))
	if err != nil {
		return 0, 0, err
	}
	var avail int64 = -1
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			avail = n * 1024
		}
	}
	if total == 0 {
		return 0, 0, errors.New("no MemTotal in /proc/meminfo")
	}
	if avail < 0 {
		avail = 0
	}
	return total, total - avail, nil
}

// Load1 reads the one-minute load average.
func (r Roots) Load1() float64 {
	b, err := os.ReadFile(filepath.Join(r.Proc, "loadavg"))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// HostCPU turns two CPUTimes samples into used vCPUs.
func HostCPU(prev, cur CPUTimes, cpus int) float64 {
	dt := float64(cur.Total - prev.Total)
	if cur.Total <= prev.Total || dt == 0 {
		return 0
	}
	busy := dt - float64(cur.Idle-prev.Idle)
	if busy < 0 {
		busy = 0
	}
	return busy / dt * float64(cpus)
}

// CgroupPath returns the cgroup v2 directory of a process from
// /proc/<pid>/cgroup ("0::/system.slice/docker-<id>.scope").
func (r Roots) CgroupPath(pid int) (string, error) {
	b, err := os.ReadFile(filepath.Join(r.Proc, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "0::") {
			return filepath.Join(r.Cgroup, strings.TrimSpace(line[3:])), nil
		}
	}
	return "", fmt.Errorf("process %d is not in a cgroup v2 hierarchy", pid)
}

// CgroupUsage is one read of a container's cgroup.
type CgroupUsage struct {
	CPUUsec  uint64
	MemBytes int64
	MemMax   int64 // 0 when unlimited
}

// ReadCgroup reads cpu.stat usage_usec, memory.current and memory.max.
func ReadCgroup(dir string) (CgroupUsage, error) {
	var u CgroupUsage
	b, err := os.ReadFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return u, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "usage_usec ") {
			u.CPUUsec, _ = strconv.ParseUint(strings.TrimSpace(line[len("usage_usec "):]), 10, 64)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "memory.current")); err == nil {
		u.MemBytes, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "memory.max")); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "max" {
			u.MemMax, _ = strconv.ParseInt(s, 10, 64)
		}
	}
	return u, nil
}

// NetDev sums the rx and tx byte counters of every interface but lo in
// /proc/<pid>/net/dev (the container's network namespace).
func (r Roots) NetDev(pid int) (rx, tx int64, err error) {
	b, err := os.ReadFile(filepath.Join(r.Proc, strconv.Itoa(pid), "net", "dev"))
	if err != nil {
		return 0, 0, err
	}
	return ParseNetDev(b)
}

// ParseNetDev parses the content of /proc/net/dev.
func ParseNetDev(b []byte) (rx, tx int64, err error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "lo" {
			continue
		}
		f := strings.Fields(line[colon+1:])
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseInt(f[0], 10, 64)
		t, _ := strconv.ParseInt(f[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, sc.Err()
}

// Ports lists TCP ports in LISTEN state (0A) in a process's network
// namespace. loopback holds those bound only to 127.0.0.1 or ::1.
func (r Roots) Ports(pid int) (listening, loopback []int, err error) {
	seenAny := map[int]bool{}
	seenLoop := map[int]bool{}
	var firstErr error
	for _, name := range []string{"tcp", "tcp6"} {
		b, err := os.ReadFile(filepath.Join(r.Proc, strconv.Itoa(pid), "net", name))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		parseListen(b, seenAny, seenLoop)
	}
	if len(seenAny) == 0 && len(seenLoop) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	for p := range seenAny {
		listening = append(listening, p)
	}
	for p := range seenLoop {
		if !seenAny[p] {
			loopback = append(loopback, p)
		}
	}
	sort.Ints(listening)
	sort.Ints(loopback)
	return listening, loopback, nil
}

// parseListen reads /proc/net/tcp(6). seenAny gets ports bound to a
// non-loopback address (including the wildcard), seenLoop ports bound to
// loopback.
func parseListen(b []byte, seenAny, seenLoop map[int]bool) {
	lines := strings.Split(string(b), "\n")
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		addr, port, ok := parseHexAddr(f[1])
		if !ok {
			continue
		}
		if addr.IsLoopback() {
			seenLoop[port] = true
		} else {
			seenAny[port] = true
		}
	}
}

// parseHexAddr parses "0100007F:0BB8" (little-endian words).
func parseHexAddr(s string) (net.IP, int, bool) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return nil, 0, false
	}
	raw, err := hex.DecodeString(s[:colon])
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return nil, 0, false
	}
	port, err := strconv.ParseUint(s[colon+1:], 16, 16)
	if err != nil {
		return nil, 0, false
	}
	ip := make(net.IP, len(raw))
	for i := 0; i < len(raw); i += 4 {
		ip[i], ip[i+1], ip[i+2], ip[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	return ip, int(port), true
}

// DirSize walks dir and sums file sizes, giving up after budget. complete is
// false when the walk ran out of time (the result is then a lower bound).
func DirSize(dir string, budget time.Duration) (size int64, complete bool, err error) {
	deadline := time.Now().Add(budget)
	stop := errors.New("budget")
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil // unreadable entries are skipped
		}
		if time.Now().After(deadline) {
			return stop
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	if errors.Is(err, stop) {
		return size, false, nil
	}
	return size, err == nil, err
}

// CPUTracker turns cumulative CPU counters into vCPU per key.
type CPUTracker struct {
	mu   sync.Mutex
	prev map[string]cpuPoint
}

type cpuPoint struct {
	usec uint64
	at   time.Time
}

// NewCPUTracker returns an empty tracker.
func NewCPUTracker() *CPUTracker { return &CPUTracker{prev: map[string]cpuPoint{}} }

// VCPU records a cumulative usage (microseconds) and returns the vCPU used
// since the previous call for the key (0 on the first call).
func (t *CPUTracker) VCPU(key string, usec uint64, at time.Time) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.prev[key]
	t.prev[key] = cpuPoint{usec: usec, at: at}
	if !ok || usec < p.usec || !at.After(p.at) {
		return 0
	}
	return float64(usec-p.usec) / float64(at.Sub(p.at).Microseconds())
}

// Forget drops keys not in keep.
func (t *CPUTracker) Forget(keep map[string]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.prev {
		if !keep[k] {
			delete(t.prev, k)
		}
	}
}
