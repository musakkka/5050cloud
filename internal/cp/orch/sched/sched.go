// SPDX-License-Identifier: AGPL-3.0-only

// Package sched holds the orchestrator's placement and scheduling rules
// (docs/SPEC.md 7.1, 7.3, 9.1, 9.2, 9.7, 9.16) as pure functions over
// snapshots, so every rule is unit-tested without a database or agents:
//
//   - Place picks a host for one replica: eligibility (online, not draining
//     or in maintenance, roles or pinned hosts, architecture, a free network
//     slot, memory reservation within capacity) and the score
//     (replicas of this service on the host, reserved/capacity, CPU in use/CPUs,
//     then host id).
//   - Phase turns a host's connection history into online, offline (routes
//     withdrawn at 60 s) and reschedule (another 120 s later).
//   - SurgeSize, Processes, ClusterWrap, Singleton and ReservationMB resolve
//     the settings of 9.7 and 7.3.
//   - DeployDeps orders a commit plan by references (cycles deploy in
//     parallel) and MaintenanceOrder stops apps before the databases they
//     reference.
package sched

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Host states (o_hosts.state).
const (
	StateOnline      = "online"
	StateOffline     = "offline"
	StateDraining    = "draining"
	StateMaintenance = "maintenance"
	StateLost        = "lost"
	StateJoining     = "joining"
	StateUnknown     = "unknown"
)

// Host is a placement candidate.
type Host struct {
	ID    string
	Name  string
	Roles []string
	State string
	// Drain is set while the host is draining (it may still be connected).
	Drain bool
	Arch  string
	CPUs  int
	// MemMB is the host's RAM; ReserveMB the host reserve (0 = default).
	MemMB     int
	ReserveMB int
	// ReservedMB is the sum of memory reservations already placed here.
	ReservedMB int
	CPUUsed    float64
	// SlotsUsed and EnvSlots describe network slots: a host can take a new
	// environment while SlotsUsed < 254.
	SlotsUsed int
	EnvSlots  map[string]bool
	// Unreachable lists hosts this one has no WireGuard handshake with; an
	// environment never spans two hosts that cannot reach each other.
	Unreachable map[string]bool
}

// HasRole reports whether h carries role r.
func (h Host) HasRole(r string) bool {
	for _, x := range h.Roles {
		if x == r {
			return true
		}
	}
	return false
}

// DefaultReserveMB is the host reserve when none is set: 10% of RAM plus
// 512 MB for Docker, 5050d, the agent, Caddy and BuildKit.
func DefaultReserveMB(memMB int) int { return memMB/10 + 512 }

// CapacityMB is what replicas may reserve on h.
func (h Host) CapacityMB() int {
	r := h.ReserveMB
	if r <= 0 {
		r = DefaultReserveMB(h.MemMB)
	}
	c := h.MemMB - r
	if c < 0 {
		return 0
	}
	return c
}

// FreeMB is capacity minus what is already reserved.
func (h Host) FreeMB() int {
	f := h.CapacityMB() - h.ReservedMB
	if f < 0 {
		return 0
	}
	return f
}

// Schedulable reports whether new replicas may land on h.
func (h Host) Schedulable() bool {
	return h.State == StateOnline && !h.Drain
}

// Request asks for a host for one replica.
type Request struct {
	EnvID     string
	ServiceID string
	// Roles every candidate must carry (default ["app"]); ignored when Hosts is set.
	Roles []string
	// Hosts pins the service to these host ids.
	Hosts []string
	// Arch is the service's architecture ("" = any, set by the first placement).
	Arch string
	// ReservationMB is the replica's memory reservation.
	ReservationMB int
	// PinnedHost is the host of the service's volume; the replica can only go there.
	PinnedHost string
	// Exclude lists hosts to skip (the host a replica is moving away from).
	Exclude map[string]bool
	// PeerHosts are hosts that already run replicas of this environment
	// (used with Host.Unreachable).
	PeerHosts []string
}

// NoEligibleError: no online host carries the roles.
type NoEligibleError struct {
	Roles []string
	Hosts []string
}

func (e *NoEligibleError) Error() string {
	if len(e.Hosts) > 0 {
		return fmt.Sprintf("None of the hosts this service is pinned to (%s) is online. Bring one online or change placement.", strings.Join(e.Hosts, ", "))
	}
	return fmt.Sprintf("No online host has the roles %s. Add a host or change placement.", strings.Join(e.Roles, ", "))
}

// NoCapacityError: eligible hosts exist but none has the reservation free.
type NoCapacityError struct {
	NeedMB        int
	LargestFreeMB int
}

func (e *NoCapacityError) Error() string {
	return fmt.Sprintf("No host has %d MB free for this replica. Free memory, lower the reservation or add a host.", e.NeedMB)
}

// Pending is the card sub-line while a wave waits for capacity (9.2).
func (e *NoCapacityError) Pending() string {
	return fmt.Sprintf("Pending: no capacity. Needs %d MB; the largest free host has %d MB.", e.NeedMB, e.LargestFreeMB)
}

// PinnedError: the volume's host cannot take the replica.
type PinnedError struct {
	Host   string
	Reason string
}

func (e *PinnedError) Error() string { return e.Reason }

// Place picks the best host for one replica. counts holds the number of
// replicas of this service already on each host (including ones placed
// earlier in the same wave).
func Place(hosts []Host, counts map[string]int, req Request) (string, error) {
	roles := req.Roles
	if len(roles) == 0 {
		roles = []string{"app"}
	}
	byID := map[string]Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	if req.PinnedHost != "" {
		h, ok := byID[req.PinnedHost]
		if !ok {
			return "", &PinnedError{Host: req.PinnedHost, Reason: "This volume was on a host that is gone. Restore it from a backup."}
		}
		switch {
		case h.State == StateLost:
			return "", &PinnedError{Host: h.Name, Reason: "This volume was on a host that is gone. Restore it from a backup."}
		case h.Drain || h.State == StateDraining:
			return "", &PinnedError{Host: h.Name, Reason: fmt.Sprintf("Pinned to a draining host: the volume lives on %s.", h.Name)}
		case h.State == StateMaintenance:
			return "", &PinnedError{Host: h.Name, Reason: fmt.Sprintf("Pinned to a host in maintenance: the volume lives on %s.", h.Name)}
		case h.State != StateOnline:
			return "", &PinnedError{Host: h.Name, Reason: fmt.Sprintf("The volume's host %s is offline. The service starts again when it is back.", h.Name)}
		}
		if !h.EnvSlots[req.EnvID] && h.SlotsUsed >= ipam.MaxSlots {
			return "", &ipam.NoSlotError{Host: h.Name}
		}
		if h.FreeMB() < req.ReservationMB {
			return "", &NoCapacityError{NeedMB: req.ReservationMB, LargestFreeMB: h.FreeMB()}
		}
		return h.ID, nil
	}

	pinned := map[string]bool{}
	for _, id := range req.Hosts {
		pinned[id] = true
	}
	var eligible []Host
	var slotFull *ipam.NoSlotError
	largest := 0
	capacityShort := false
	for _, h := range hosts {
		if !h.Schedulable() || req.Exclude[h.ID] {
			continue
		}
		if len(pinned) > 0 {
			if !pinned[h.ID] {
				continue
			}
		} else if !hasAll(h, roles) {
			continue
		}
		if req.Arch != "" && h.Arch != "" && h.Arch != req.Arch {
			continue
		}
		if unreachableFrom(h, req.PeerHosts) {
			continue
		}
		if !h.EnvSlots[req.EnvID] && h.SlotsUsed >= ipam.MaxSlots {
			slotFull = &ipam.NoSlotError{Host: h.Name}
			continue
		}
		if f := h.FreeMB(); f > largest {
			largest = f
		}
		if h.FreeMB() < req.ReservationMB {
			capacityShort = true
			continue
		}
		eligible = append(eligible, h)
	}
	if len(eligible) == 0 {
		switch {
		case capacityShort:
			return "", &NoCapacityError{NeedMB: req.ReservationMB, LargestFreeMB: largest}
		case slotFull != nil:
			return "", slotFull
		default:
			return "", &NoEligibleError{Roles: roles, Hosts: req.Hosts}
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return less(score(eligible[i], counts, req), score(eligible[j], counts, req), eligible[i].ID, eligible[j].ID)
	})
	return eligible[0].ID, nil
}

func hasAll(h Host, roles []string) bool {
	for _, r := range roles {
		if !h.HasRole(r) {
			return false
		}
	}
	return true
}

func unreachableFrom(h Host, peers []string) bool {
	for _, p := range peers {
		if p != h.ID && h.Unreachable[p] {
			return true
		}
	}
	return false
}

type scoreT [3]float64

func score(h Host, counts map[string]int, req Request) scoreT {
	c := h.CapacityMB()
	mem := 1.0
	if c > 0 {
		mem = float64(h.ReservedMB+req.ReservationMB) / float64(c)
	}
	cpu := 0.0
	if h.CPUs > 0 {
		cpu = h.CPUUsed / float64(h.CPUs)
	}
	return scoreT{float64(counts[h.ID]), mem, cpu}
}

func less(a, b scoreT, ida, idb string) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return a[i] < b[i]
		}
	}
	return ida < idb
}

// BuildHost picks an online build host, least busy first, with at most
// max(1, cpus/4) concurrent builds. running holds builds per host.
func BuildHost(hosts []Host, running map[string]int, arch string) (string, error) {
	var best *Host
	var bestLoad float64
	anyBuild := false
	for i := range hosts {
		h := hosts[i]
		if !h.HasRole("build") || h.State != StateOnline {
			continue
		}
		if arch != "" && h.Arch != "" && h.Arch != arch {
			continue
		}
		anyBuild = true
		limit := h.CPUs / 4
		if limit < 1 {
			limit = 1
		}
		if running[h.ID] >= limit {
			continue
		}
		load := float64(running[h.ID]) / float64(limit)
		if best == nil || load < bestLoad || (load == bestLoad && h.ID < best.ID) {
			best, bestLoad = &hosts[i], load
		}
	}
	if best != nil {
		return best.ID, nil
	}
	if !anyBuild {
		if arch != "" {
			return "", errorf("No online host has the role build for %s. Bring one online or give a host the build role.", arch)
		}
		return "", ErrNoBuildHost
	}
	return "", ErrBuildersBusy
}

// ErrNoBuildHost and ErrBuildersBusy explain why a build waits.
var (
	ErrNoBuildHost  = errorf("No online host has the role build. Bring one online or give a host the build role.")
	ErrBuildersBusy = errorf("Waiting for a build host: every build host is running as many builds as it can.")
)

// ---------------------------------------------------------------- liveness

// Liveness timings (docs/SPEC.md 7.1).
const (
	OfflineAfter    = 60 * time.Second
	RescheduleAfter = 120 * time.Second
)

// Phase of a host's connection.
type Phase int

const (
	PhaseOnline     Phase = iota
	PhaseGrace            // disconnected for less than 60 s: still routed
	PhaseOffline          // routes and DNS withdrawn
	PhaseReschedule       // replicas without volumes and not singleton move
)

// HostPhase returns the phase of a host that disconnected at since (zero
// when connected), with the protocol's timings.
func HostPhase(connected bool, since, now time.Time) Phase {
	return HostPhaseWith(connected, since, now, OfflineAfter, RescheduleAfter)
}

// HostPhaseWith is HostPhase with explicit timings (tests shorten them).
func HostPhaseWith(connected bool, since, now time.Time, offlineAfter, rescheduleAfter time.Duration) Phase {
	if connected {
		return PhaseOnline
	}
	if since.IsZero() {
		return PhaseReschedule
	}
	d := now.Sub(since)
	switch {
	case d < offlineAfter:
		return PhaseGrace
	case d < offlineAfter+rescheduleAfter:
		return PhaseOffline
	default:
		return PhaseReschedule
	}
}

// ---------------------------------------------------------------- settings

// Kind derives the service kind: a template database, a cron schedule, no
// port and no domain (worker), else web.
func Kind(cfg api.ServiceConfig, hasDomains bool) api.ServiceKind {
	if cfg.Kind != "" {
		return cfg.Kind
	}
	if cfg.Deploy.CronSchedule != "" {
		return api.KindCron
	}
	if cfg.Net.Port == 0 && !hasDomains {
		return api.KindWorker
	}
	return api.KindWeb
}

// Singleton: explicit setting, else on for workers and off for the rest (7.1).
func Singleton(cfg api.ServiceConfig, kind api.ServiceKind) bool {
	if cfg.Deploy.Singleton != nil {
		return *cfg.Deploy.Singleton
	}
	return kind == api.KindWorker
}

// Replicas is the configured replica count (default 1).
func Replicas(cfg api.ServiceConfig) int {
	if cfg.Deploy.Replicas <= 0 {
		return 1
	}
	return cfg.Deploy.Replicas
}

// SurgeSize resolves maxSurge ("1" default, a number, or "25%") for a
// replica count; the result is at least 1 and at most replicas.
func SurgeSize(maxSurge string, replicas int) int {
	if replicas < 1 {
		replicas = 1
	}
	s := strings.TrimSpace(maxSurge)
	n := 1
	if strings.HasSuffix(s, "%") {
		p, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err == nil && p > 0 {
			n = int(math.Ceil(p / 100 * float64(replicas)))
		}
	} else if s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			n = v
		}
	}
	if n < 1 {
		n = 1
	}
	if n > replicas {
		n = replicas
	}
	return n
}

// ReservationMB is the memory a replica reserves (9.2): the explicit
// reservation, else the memory limit, else 256 (512 for databases).
func ReservationMB(cfg api.ServiceConfig, database bool) int {
	if cfg.Deploy.MemoryReservationMB > 0 {
		return cfg.Deploy.MemoryReservationMB
	}
	if cfg.Deploy.MemoryLimitMB > 0 {
		return cfg.Deploy.MemoryLimitMB
	}
	if database {
		return 512
	}
	return 256
}

// MaxProcesses caps Processes "auto".
const MaxProcesses = 32

// Processes resolves the Processes setting for one replica (9.7): "auto" is
// floor(cpuLimit), or the host's CPU count when unlimited, between 1 and 32.
// Empty or invalid means 1.
func Processes(cfg api.ServiceConfig, hostCPUs int) int {
	p := strings.TrimSpace(cfg.Deploy.Processes)
	if p == "" {
		return 1
	}
	if strings.EqualFold(p, "auto") {
		n := hostCPUs
		if cfg.Deploy.CPULimit > 0 {
			n = int(math.Floor(cfg.Deploy.CPULimit))
		}
		return clamp(n, 1, MaxProcesses)
	}
	v, err := strconv.Atoi(p)
	if err != nil {
		return 1
	}
	return clamp(v, 1, MaxProcesses)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClusterWrap reports whether a Node start command can run under cluster.cjs:
// the build reported Node and the command is `node [flags] <file> [args]`.
func ClusterWrap(isNode bool, argv []string) bool {
	if !isNode || len(argv) < 2 {
		return false
	}
	if argv[0] != "node" && !strings.HasSuffix(argv[0], "/node") {
		return false
	}
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "-") {
			// flags with a separate value are not supported here except
			// the common "--require x" / "-r x" forms
			continue
		}
		return looksLikeScript(a)
	}
	return false
}

func looksLikeScript(a string) bool {
	for _, ext := range []string{".js", ".cjs", ".mjs", ".ts"} {
		if strings.HasSuffix(a, ext) {
			return true
		}
	}
	return !strings.ContainsAny(a, " ;|&")
}

// ProcessesText is the one-line explanation the UI shows (9.7).
func ProcessesText(n int, wrap bool, port int) string {
	if n <= 1 {
		return ""
	}
	if wrap {
		return fmt.Sprintf("Runs %d Node processes sharing port %d (cluster mode)", n, port)
	}
	return fmt.Sprintf("Sets WEB_CONCURRENCY=%d; your server must read it", n)
}

// ---------------------------------------------------------------- ordering

// DeployDeps returns, for each service in a commit plan, the services in the
// plan it must wait for (the ones it references), leaving out references
// inside a cycle so cycles deploy in parallel (9.1).
func DeployDeps(plan []string, refs map[string][]string) map[string][]string {
	in := map[string]bool{}
	for _, s := range plan {
		in[s] = true
	}
	comp := sccs(plan, refs, in)
	out := map[string][]string{}
	for _, s := range plan {
		seen := map[string]bool{}
		for _, r := range refs[s] {
			if !in[r] || r == s || comp[r] == comp[s] || seen[r] {
				continue
			}
			seen[r] = true
			out[s] = append(out[s], r)
		}
		sort.Strings(out[s])
	}
	return out
}

// sccs labels strongly connected components (Tarjan).
func sccs(nodes []string, edges map[string][]string, in map[string]bool) map[string]int {
	index := 0
	idx := map[string]int{}
	low := map[string]int{}
	on := map[string]bool{}
	var stack []string
	comp := map[string]int{}
	c := 0
	var visit func(v string)
	visit = func(v string) {
		idx[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		on[v] = true
		for _, w := range edges[v] {
			if !in[w] {
				continue
			}
			if _, ok := idx[w]; !ok {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if on[w] && idx[w] < low[v] {
				low[v] = idx[w]
			}
		}
		if low[v] == idx[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				comp[w] = c
				if w == v {
					break
				}
			}
			c++
		}
	}
	sorted := append([]string(nil), nodes...)
	sort.Strings(sorted)
	for _, n := range sorted {
		if _, ok := idx[n]; !ok {
			visit(n)
		}
	}
	return comp
}

// MaintenanceOrder returns the order to stop services on a host: services
// that reference others stop before the services they reference (apps
// before their databases). Start order is the reverse. Cycles keep a stable
// order by id.
func MaintenanceOrder(services []string, refs map[string][]string) []string {
	in := map[string]bool{}
	for _, s := range services {
		in[s] = true
	}
	// referencedBy counts how many services in the set reference s.
	indeg := map[string]int{}
	for _, s := range services {
		for _, r := range refs[s] {
			if in[r] && r != s {
				indeg[r]++
			}
		}
	}
	sorted := append([]string(nil), services...)
	sort.Strings(sorted)
	done := map[string]bool{}
	var out []string
	for len(out) < len(sorted) {
		progressed := false
		for _, s := range sorted {
			if done[s] || indeg[s] > 0 {
				continue
			}
			done[s] = true
			out = append(out, s)
			progressed = true
			for _, r := range refs[s] {
				if in[r] && r != s {
					indeg[r]--
				}
			}
		}
		if !progressed {
			// a cycle: take the lowest remaining id and continue
			for _, s := range sorted {
				if !done[s] {
					done[s] = true
					out = append(out, s)
					for _, r := range refs[s] {
						if in[r] && r != s {
							indeg[r]--
						}
					}
					break
				}
			}
		}
	}
	return out
}
