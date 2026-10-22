// SPDX-License-Identifier: AGPL-3.0-only

// Package ipam is the orchestrator's address plan and IP allocator
// (docs/SPEC.md 8.4 and 9.2).
//
// The plan is fixed arithmetic over two ranges:
//
//	pod CIDR (default 10.128.0.0/9)
//	  host k (mesh index 1..127)      10.(128+k).0.0/16
//	  environment slot n on host k    10.(128+k).n.0/24, gateway .1
//	  replica addresses               .2 to .254 of that /24
//	  build network                   10.128.255.0/24 (index 0, slot 255, never routed)
//	mesh CIDR (default 100.96.0.0/16)
//	  host k tunnel address           100.96.0.k/32
//
// Allocation lives in two orchestrator tables: o_subnets gives each (host,
// environment) pair a slot and o_ips gives each replica or job a static
// address. Every allocation runs inside the caller's write transaction, so
// two deployments can never receive the same address. Addresses are released
// only when the agent confirms the container is gone (Release), never
// earlier, so a partitioned host's addresses are not handed out twice.
package ipam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
)

// Limits of the address plan (docs/SPEC.md 8.4, Reference > Limits).
const (
	MaxHosts  = 127 // mesh indexes 1..127
	MaxSlots  = 254 // environment slots 1..254 per host; 255 is the build network
	FirstHost = 2   // first replica address in a slot (.1 is the gateway)
	LastHost  = 254 // last replica address in a slot
	BuildSlot = 255
	// DefaultPodCIDR and DefaultMeshCIDR are the documented defaults.
	DefaultPodCIDR  = "10.128.0.0/9"
	DefaultMeshCIDR = "100.96.0.0/16"
)

// Plan is the address plan of one install.
type Plan struct {
	Pod  netip.Prefix
	Mesh netip.Prefix
}

// NewPlan parses the pod and mesh ranges. Empty strings use the defaults.
// The pod range must hold 128 /16 blocks (a prefix of /9 or shorter) and the
// mesh range at least 128 addresses (/24 or shorter).
func NewPlan(podCIDR, meshCIDR string) (Plan, error) {
	if podCIDR == "" {
		podCIDR = DefaultPodCIDR
	}
	if meshCIDR == "" {
		meshCIDR = DefaultMeshCIDR
	}
	pod, err := netip.ParsePrefix(podCIDR)
	if err != nil {
		return Plan{}, errorf("The pod range %q is not a valid CIDR: %w", podCIDR, err)
	}
	mesh, err := netip.ParsePrefix(meshCIDR)
	if err != nil {
		return Plan{}, errorf("The mesh range %q is not a valid CIDR: %w", meshCIDR, err)
	}
	if !pod.Addr().Is4() || !mesh.Addr().Is4() {
		return Plan{}, errorf("The pod and mesh ranges must be IPv4 ranges: 5050cloud v0.1 is IPv4 only.")
	}
	if pod.Bits() > 9 {
		return Plan{}, errorf("The pod range %s is too small: it must be a /9 or larger to hold 127 hosts.", pod)
	}
	if mesh.Bits() > 24 {
		return Plan{}, errorf("The mesh range %s is too small: it must be a /24 or larger.", mesh)
	}
	if pod.Overlaps(mesh) {
		return Plan{}, errorf("The pod range %s and the mesh range %s overlap. Pick ranges that do not.", pod, mesh)
	}
	return Plan{Pod: pod.Masked(), Mesh: mesh.Masked()}, nil
}

// MustDefault returns the default plan.
func MustDefault() Plan {
	p, err := NewPlan("", "")
	if err != nil {
		panic(err)
	}
	return p
}

func addOffset(base netip.Addr, off uint32) netip.Addr {
	b := base.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v += off
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// HostSubnet is the /16 container range of host index k (1..127).
func (p Plan) HostSubnet(k int) netip.Prefix {
	return netip.PrefixFrom(addOffset(p.Pod.Addr(), hostBase(p, k)), 16)
}

// hostBase returns the offset of host k inside the pod range. For the
// default 10.128.0.0/9 host k starts at 10.(128+k).0.0, that is k<<16 above
// the base address.
func hostBase(_ Plan, k int) uint32 { return uint32(k) << 16 }

// SlotSubnet is the /24 of environment slot n (1..254) on host k.
func (p Plan) SlotSubnet(k, n int) netip.Prefix {
	return netip.PrefixFrom(addOffset(p.Pod.Addr(), hostBase(p, k)+uint32(n)<<8), 24)
}

// Gateway is the first address of a slot, where the agent's DNS listens.
func (p Plan) Gateway(k, n int) netip.Addr {
	return addOffset(p.Pod.Addr(), hostBase(p, k)+uint32(n)<<8+1)
}

// Addr is address .h (2..254) of slot n on host k.
func (p Plan) Addr(k, n, h int) netip.Addr {
	return addOffset(p.Pod.Addr(), hostBase(p, k)+uint32(n)<<8+uint32(h))
}

// BuildSubnet is the build network, identical on every host and never routed.
func (p Plan) BuildSubnet() netip.Prefix {
	return netip.PrefixFrom(addOffset(p.Pod.Addr(), uint32(BuildSlot)<<8), 24)
}

// MeshAddr is the WireGuard tunnel address of host k.
func (p Plan) MeshAddr(k int) netip.Addr {
	return addOffset(p.Mesh.Addr(), uint32(k))
}

// Locate maps an address back to (host index, slot, host part). ok is false
// for addresses outside the per-host ranges.
func (p Plan) Locate(a netip.Addr) (k, n, h int, ok bool) {
	if !p.Pod.Contains(a) || !a.Is4() {
		return 0, 0, 0, false
	}
	b := p.Pod.Addr().As4()
	base := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	x := a.As4()
	v := uint32(x[0])<<24 | uint32(x[1])<<16 | uint32(x[2])<<8 | uint32(x[3])
	off := v - base
	k = int(off >> 16)
	n = int(off>>8) & 0xff
	h = int(off & 0xff)
	if k < 1 || k > MaxHosts {
		return 0, 0, 0, false
	}
	return k, n, h, true
}

// ---------------------------------------------------------------- allocation

// DB is what the allocator needs: a *sql.Tx (writes) or *sql.DB (reads).
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NoSlotError is returned when a host already carries 254 environments.
type NoSlotError struct{ Host string }

func (e *NoSlotError) Error() string {
	return fmt.Sprintf("No network slot left on host %s: it already runs %d environments.", e.Host, MaxSlots)
}

// FullError is returned when an environment slot has no free address.
type FullError struct{ Host string }

func (e *FullError) Error() string {
	return fmt.Sprintf("No free address left for this environment on host %s: it already runs %d replicas of it.", e.Host, LastHost-FirstHost+1)
}

// Slot returns the slot of (host, environment), or 0 when none is allocated.
func Slot(ctx context.Context, db DB, hostID, envID string) (int, error) {
	var slot int
	err := db.QueryRowContext(ctx, `SELECT slot FROM o_subnets WHERE host_id = ? AND environment_id = ?`, hostID, envID).Scan(&slot)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return slot, err
}

// SlotsUsed returns the number of environment slots allocated on a host.
func SlotsUsed(ctx context.Context, db DB, hostID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM o_subnets WHERE host_id = ?`, hostID).Scan(&n)
	return n, err
}

// EnsureSlot returns the slot of (host, environment), allocating the lowest
// free slot when there is none. hostName is used in the error sentence.
func EnsureSlot(ctx context.Context, tx DB, hostID, hostName, envID string) (int, error) {
	slot, err := Slot(ctx, tx, hostID, envID)
	if err != nil || slot != 0 {
		return slot, err
	}
	slots, err := ints(ctx, tx, `SELECT slot FROM o_subnets WHERE host_id = ? ORDER BY slot`, hostID)
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, s := range slots {
		used[s] = true
	}
	for s := 1; s <= MaxSlots; s++ {
		if !used[s] {
			if _, err := tx.ExecContext(ctx, `INSERT INTO o_subnets (host_id, environment_id, slot) VALUES (?, ?, ?)`, hostID, envID, s); err != nil {
				return 0, err
			}
			return s, nil
		}
	}
	return 0, &NoSlotError{Host: hostName}
}

// Allocate gives owner (a replica or job id) a static address in the slot of
// (host, environment), allocating the slot when needed. An owner that already
// holds an address on that host keeps it.
func Allocate(ctx context.Context, tx DB, p Plan, hostID, hostName string, meshIndex int, envID, owner string) (string, error) {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT ip FROM o_ips WHERE replica_id = ? AND host_id = ?`, owner, hostID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	slot, err := EnsureSlot(ctx, tx, hostID, hostName, envID)
	if err != nil {
		return "", err
	}
	prefix := p.SlotSubnet(meshIndex, slot)
	ips, err := strs(ctx, tx, `SELECT ip FROM o_ips WHERE host_id = ? AND environment_id = ?`, hostID, envID)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for _, ip := range ips {
		used[ip] = true
	}
	for h := FirstHost; h <= LastHost; h++ {
		a := p.Addr(meshIndex, slot, h)
		if !prefix.Contains(a) {
			break
		}
		if used[a.String()] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO o_ips (ip, host_id, environment_id, replica_id) VALUES (?, ?, ?, ?)`,
			a.String(), hostID, envID, owner); err != nil {
			return "", err
		}
		return a.String(), nil
	}
	return "", &FullError{Host: hostName}
}

// Reserve records an address an agent already uses (adopt mode). It fails
// with ok=false when another owner holds it.
func Reserve(ctx context.Context, tx DB, p Plan, hostID string, meshIndex int, envID, owner, ip string) (bool, error) {
	a, perr := netip.ParseAddr(ip)
	if perr != nil {
		return false, nil //nolint:nilerr // an unparsable label is simply not reusable
	}
	k, n, h, ok := p.Locate(a)
	if !ok || k != meshIndex || h < FirstHost || h > LastHost {
		return false, nil
	}
	var holder string
	err := tx.QueryRowContext(ctx, `SELECT replica_id FROM o_ips WHERE ip = ?`, ip).Scan(&holder)
	if err == nil {
		return holder == owner, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	slot, err := Slot(ctx, tx, hostID, envID)
	if err != nil {
		return false, err
	}
	if slot == 0 {
		var taken int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM o_subnets WHERE host_id = ? AND slot = ?`, hostID, n).Scan(&taken); err != nil {
			return false, err
		}
		if taken > 0 {
			return false, nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO o_subnets (host_id, environment_id, slot) VALUES (?, ?, ?)`, hostID, envID, n); err != nil {
			return false, err
		}
	} else if slot != n {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO o_ips (ip, host_id, environment_id, replica_id) VALUES (?, ?, ?, ?)`, ip, hostID, envID, owner); err != nil {
		return false, err
	}
	return true, nil
}

// Release frees every address held by owner and drops slots that no longer
// hold any address. Call it only after the agent confirmed the container is
// removed.
func Release(ctx context.Context, tx DB, owner string) error {
	type pair struct{ host, env string }
	var pairs []pair
	err := each(ctx, tx, func(r *sql.Rows) error {
		var p pair
		if err := r.Scan(&p.host, &p.env); err != nil {
			return err
		}
		pairs = append(pairs, p)
		return nil
	}, `SELECT host_id, environment_id FROM o_ips WHERE replica_id = ?`, owner)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM o_ips WHERE replica_id = ?`, owner); err != nil {
		return err
	}
	for _, p := range pairs {
		if err := releaseSlotIfUnused(ctx, tx, p.host, p.env); err != nil {
			return err
		}
	}
	return nil
}

func releaseSlotIfUnused(ctx context.Context, tx DB, hostID, envID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM o_subnets WHERE host_id = ? AND environment_id = ?
		AND NOT EXISTS (SELECT 1 FROM o_ips WHERE host_id = ? AND environment_id = ?)`, hostID, envID, hostID, envID)
	return err
}

// ReleaseHost frees every slot and address of a removed host.
func ReleaseHost(ctx context.Context, tx DB, hostID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM o_ips WHERE host_id = ?`, hostID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM o_subnets WHERE host_id = ?`, hostID)
	return err
}

// EnvSlots returns the slot of an environment on every host that has one.
func EnvSlots(ctx context.Context, db DB, envID string) (map[string]int, error) {
	out := map[string]int{}
	err := each(ctx, db, func(r *sql.Rows) error {
		var h string
		var s int
		if err := r.Scan(&h, &s); err != nil {
			return err
		}
		out[h] = s
		return nil
	}, `SELECT host_id, slot FROM o_subnets WHERE environment_id = ?`, envID)
	return out, err
}

// Owner returns who holds an address ("" when it is free).
func Owner(ctx context.Context, db DB, ip string) (string, error) {
	var o string
	err := db.QueryRowContext(ctx, `SELECT replica_id FROM o_ips WHERE ip = ?`, ip).Scan(&o)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return o, err
}

// each runs fn for every row of a query and closes the rows.
func each(ctx context.Context, db DB, fn func(*sql.Rows) error, query string, args ...any) (err error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
	}()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func ints(ctx context.Context, db DB, query string, args ...any) ([]int, error) {
	var out []int
	err := each(ctx, db, func(r *sql.Rows) error {
		var v int
		if err := r.Scan(&v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	}, query, args...)
	return out, err
}

func strs(ctx context.Context, db DB, query string, args ...any) ([]string, error) {
	var out []string
	err := each(ctx, db, func(r *sql.Rows) error {
		var v string
		if err := r.Scan(&v); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	}, query, args...)
	return out, err
}
