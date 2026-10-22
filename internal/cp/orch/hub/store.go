// SPDX-License-Identifier: AGPL-3.0-only

package hub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// DB is a *sql.DB or *sql.Tx.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WriteFunc runs fn in a write transaction on the orchestrator's single
// writer (docs/SPEC.md 6.5).
type WriteFunc func(ctx context.Context, fn func(tx *sql.Tx) error) error

// HostRow is one o_hosts record.
type HostRow struct {
	ID             string
	Name           string
	Roles          []string
	State          string
	MachineID      string
	WGPublicKey    string
	Facts          proto.Facts
	MeshIndex      int
	PublicIP       string
	LastSeen       int64
	DisconnectedAt int64
	Drain          bool
	ReserveMB      int
	Version        string
	CreatedAt      int64
	TokenRotatedAt int64
}

// HasRole reports whether the host carries role r.
func (h HostRow) HasRole(r string) bool {
	for _, x := range h.Roles {
		if x == r {
			return true
		}
	}
	return false
}

const hostCols = `id, name, roles, state, machine_id, mesh_pubkey, facts, COALESCE(mesh_index, 0), public_ip,
	last_seen, disconnected_at, drain, reserve_mb, version, created_at, token_rotated_at`

func scanHost(sc interface{ Scan(...any) error }) (HostRow, error) {
	var h HostRow
	var roles, facts string
	var drain int
	if err := sc.Scan(&h.ID, &h.Name, &roles, &h.State, &h.MachineID, &h.WGPublicKey, &facts, &h.MeshIndex,
		&h.PublicIP, &h.LastSeen, &h.DisconnectedAt, &drain, &h.ReserveMB, &h.Version, &h.CreatedAt, &h.TokenRotatedAt); err != nil {
		return h, err
	}
	_ = json.Unmarshal([]byte(roles), &h.Roles)
	_ = json.Unmarshal([]byte(facts), &h.Facts)
	h.Drain = drain != 0
	return h, nil
}

// LoadHosts returns every host, oldest first.
func LoadHosts(ctx context.Context, db DB) ([]HostRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+hostCols+` FROM o_hosts ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []HostRow
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ErrNoHost is returned for an unknown host id.
var ErrNoHost = errorf("This host does not exist.")

// LoadHost returns one host.
func LoadHost(ctx context.Context, db DB, id string) (HostRow, error) {
	h, err := scanHost(db.QueryRowContext(ctx, `SELECT `+hostCols+` FROM o_hosts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrNoHost
	}
	return h, err
}

// SetHostState records a host state ("online", "offline", "maintenance", ...).
func SetHostState(ctx context.Context, tx DB, id, state string) error {
	_, err := tx.ExecContext(ctx, `UPDATE o_hosts SET state = ? WHERE id = ?`, state, id)
	return err
}

// HostUpdate changes settings of a host; nil fields stay.
type HostUpdate struct {
	Name      *string
	Roles     *[]string
	Drain     *bool
	ReserveMB *int
}

// UpdateHost applies a HostUpdate.
func UpdateHost(ctx context.Context, tx DB, id string, u HostUpdate) error {
	if u.Name != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE o_hosts SET name = ? WHERE id = ?`, *u.Name, id); err != nil {
			return err
		}
	}
	if u.Roles != nil {
		b, _ := json.Marshal(*u.Roles)
		if _, err := tx.ExecContext(ctx, `UPDATE o_hosts SET roles = ? WHERE id = ?`, string(b), id); err != nil {
			return err
		}
	}
	if u.Drain != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE o_hosts SET drain = ? WHERE id = ?`, b2i(*u.Drain), id); err != nil {
			return err
		}
	}
	if u.ReserveMB != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE o_hosts SET reserve_mb = ? WHERE id = ?`, *u.ReserveMB, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteHost removes a host record and its tokens; its socket must be closed separately.
func DeleteHost(ctx context.Context, tx DB, id string) error {
	for _, q := range []string{
		`DELETE FROM o_hosts WHERE id = ?`,
		`DELETE FROM o_host_conflicts WHERE host_id = ?`,
		`DELETE FROM o_state WHERE key = 'gen:' || ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// Conflict is a machine that presented another host's identity (7.1).
type Conflict struct {
	MachineID   string
	WGPublicKey string
	IP          string
	SeenAt      int64
}

// Conflicts lists the duplicate-host records of a host.
func Conflicts(ctx context.Context, db DB, hostID string) ([]Conflict, error) {
	rows, err := db.QueryContext(ctx, `SELECT machine_id, wg_pubkey, ip, seen_at FROM o_host_conflicts WHERE host_id = ? ORDER BY seen_at`, hostID)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []Conflict
	for rows.Next() {
		var c Conflict
		if err := rows.Scan(&c.MachineID, &c.WGPublicKey, &c.IP, &c.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClearConflicts removes the duplicate-host records of a host.
func ClearConflicts(ctx context.Context, tx DB, hostID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM o_host_conflicts WHERE host_id = ?`, hostID)
	return err
}

// ---------------------------------------------------------------- o_state

// GetState reads a JSON value from o_state into v; ok is false when unset.
func GetState(ctx context.Context, db DB, key string, v any) (bool, error) {
	var s string
	err := db.QueryRowContext(ctx, `SELECT value FROM o_state WHERE key = ?`, key).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(s), v)
}

// PutState writes a JSON value to o_state.
func PutState(ctx context.Context, tx DB, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO o_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}

// ---------------------------------------------------------------- ids and tokens

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// NewID returns "<prefix>_<26 char ULID, lowercase>" (docs/SPEC.md 6).
func NewID(prefix string) string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err)
	}
	// 128 bits -> 26 base32 characters (the first carries 3 bits).
	var out [26]byte
	var acc uint64
	var bits uint
	j := 25
	for i := 15; i >= 0; i-- {
		acc |= uint64(b[i]) << bits
		bits += 8
		for bits >= 5 && j >= 0 {
			out[j] = crockford[acc&31]
			acc >>= 5
			bits -= 5
			j--
		}
	}
	for j >= 0 {
		out[j] = crockford[acc&31]
		acc >>= 5
		j--
	}
	return prefix + "_" + string(out[:])
}

// NewToken returns 32 random bytes, base64url encoded.
func NewToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// HashToken is the stored form of a token (SHA-256, hex).
func HashToken(t string) string {
	s := sha256.Sum256([]byte(strings.TrimSpace(t)))
	return hex.EncodeToString(s[:])
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func closeRows(r *sql.Rows) { _ = r.Close() }
