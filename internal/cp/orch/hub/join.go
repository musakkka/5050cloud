// SPDX-License-Identifier: AGPL-3.0-only

package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Join tokens are single use and valid for one hour (docs/SPEC.md 15).
const JoinTokenTTL = time.Hour

// Known host roles.
var validRoles = map[string]bool{"control": true, "build": true, "ingress": true, "app": true, "data": true}

// NormalizeRoles validates and sorts a role list; empty means ["app"].
func NormalizeRoles(roles []string) ([]string, error) {
	if len(roles) == 0 {
		return []string{"app"}, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range roles {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" || seen[r] {
			continue
		}
		if !validRoles[r] {
			return nil, errorf("The role %q does not exist. Use control, build, ingress, app or data.", r)
		}
		seen[r] = true
		out = append(out, r)
	}
	if len(out) == 0 {
		return []string{"app"}, nil
	}
	sort.Strings(out)
	return out, nil
}

// CreateJoinToken stores a single-use join token and returns it in plain
// text (only its hash is kept). replaceHostID scopes the token to
// re-adopting an existing host record (9.17.2).
func (h *Hub) CreateJoinToken(ctx context.Context, roles []string, name, replaceHostID string) (string, int64, error) {
	roles, err := NormalizeRoles(roles)
	if err != nil {
		return "", 0, err
	}
	tok := "5050J-" + NewToken()
	exp := h.now().Add(JoinTokenTTL).UnixMilli()
	rb, _ := json.Marshal(roles)
	err = h.o.Write(ctx, func(tx *sql.Tx) error {
		if replaceHostID != "" {
			if _, err := LoadHost(ctx, tx, replaceHostID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO o_join_tokens (id, token_hash, roles, name, expires_at, used_at, replace_host_id)
			VALUES (?, ?, ?, ?, ?, 0, ?)`, NewID("jtk"), HashToken(tok), string(rb), name, exp, replaceHostID)
		return err
	})
	if err != nil {
		return "", 0, err
	}
	return tok, exp, nil
}

// JoinCommand is the one-line command the Hosts page shows.
func JoinCommand(publicURL, token string, roles []string) string {
	return fmt.Sprintf("curl -fsSL https://get.5050cloud.com | sudo sh -s -- --join %s --token %s --roles %s",
		publicURL, token, strings.Join(roles, ","))
}

// ---------------------------------------------------------------- rate limit

type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string][]time.Time{}}
}

func (r *rateLimiter) allow(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cut := now.Add(-r.window)
	h := r.hits[key]
	i := 0
	for i < len(h) && h[i].Before(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= r.limit {
		r.hits[key] = h
		return false
	}
	r.hits[key] = append(h, now)
	if len(r.hits) > 10000 {
		for k, v := range r.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(r.hits, k)
			}
		}
	}
	return true
}

// clientIP returns the caller's address. X-Forwarded-For is used only when
// the request came from Caddy on loopback (docs/SPEC.md 9.11).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if c := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(c) != nil {
				return c
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(xr) != nil {
			return xr
		}
	}
	return host
}

// ---------------------------------------------------------------- join

func (h *Hub) serveJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, api.CodeValidation, "Use POST to join a host.")
		return
	}
	ip := clientIP(r)
	if !h.joinLimit.allow(ip, h.now()) {
		writeError(w, http.StatusTooManyRequests, api.CodeRateLimited, "Too many join attempts from this address. Wait a minute and try again.")
		return
	}
	var req proto.JoinRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, api.CodeValidation, "The join request is not valid JSON.")
		return
	}
	resp, err := h.join(r.Context(), req, ip)
	if err != nil {
		var je *joinError
		if errors.As(err, &je) {
			writeError(w, je.status, je.code, je.msg)
			return
		}
		h.log.Error("join failed", "err", err)
		writeError(w, http.StatusInternalServerError, api.CodeInternal, "The host could not be joined because of an internal error. Try again.")
		return
	}
	// Every other host needs the new peer in its mesh and firewall now.
	h.MarkDirty()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

type joinError struct {
	status int
	code   string
	msg    string
}

func (e *joinError) Error() string { return e.msg }

func refuse(status int, code, msg string) error {
	return &joinError{status: status, code: code, msg: msg}
}

func (h *Hub) join(ctx context.Context, req proto.JoinRequest, ip string) (proto.JoinResponse, error) {
	if strings.TrimSpace(req.Token) == "" {
		return proto.JoinResponse{}, refuse(http.StatusUnauthorized, api.CodeUnauthorized, "The join request has no token. Copy the full command from Add host.")
	}
	if strings.TrimSpace(req.MachineID) == "" || strings.TrimSpace(req.WGPublicKey) == "" {
		return proto.JoinResponse{}, refuse(http.StatusBadRequest, api.CodeValidation, "The join request needs the machine id and the WireGuard public key of the host.")
	}
	now := h.now()
	var resp proto.JoinResponse
	var joined HostRow
	err := h.o.Write(ctx, func(tx *sql.Tx) error {
		var id, rolesJSON, name, replace string
		var exp, used int64
		err := tx.QueryRowContext(ctx, `SELECT id, roles, name, expires_at, used_at, COALESCE(replace_host_id, '') FROM o_join_tokens WHERE token_hash = ?`,
			HashToken(req.Token)).Scan(&id, &rolesJSON, &name, &exp, &used, &replace)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(http.StatusUnauthorized, api.CodeUnauthorized, "This join token is not valid. Create a new one with Add host.")
		}
		if err != nil {
			return err
		}
		if used != 0 {
			return refuse(http.StatusUnauthorized, api.CodeUnauthorized, "This join token was already used. Join tokens work once; create a new one with Add host.")
		}
		if exp <= now.UnixMilli() {
			return refuse(http.StatusUnauthorized, api.CodeUnauthorized, "This join token has expired. Join tokens are valid for one hour; create a new one with Add host.")
		}
		if replace != req.ReplaceHostID {
			if replace == "" {
				return refuse(http.StatusUnauthorized, api.CodeUnauthorized, "This join token cannot replace a host. Use the one-time token printed by restore-state.")
			}
			return refuse(http.StatusUnauthorized, api.CodeUnauthorized, "This join token was issued to replace another host.")
		}
		var roles []string
		_ = json.Unmarshal([]byte(rolesJSON), &roles)
		token := NewToken()
		factsJSON, _ := json.Marshal(req.Facts)
		publicIP := req.Facts.PublicIP
		if publicIP == "" {
			publicIP = ip
		}
		if req.ReplaceHostID != "" {
			old, err := LoadHost(ctx, tx, req.ReplaceHostID)
			if err != nil {
				return refuse(http.StatusNotFound, api.CodeNotFound, "The host to replace does not exist in this install.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE o_hosts SET machine_id = ?, mesh_pubkey = ?, token_hash = ?, prev_token_hash = NULL,
				prev_valid_until = 0, pending_token_hash = NULL, token_rotated_at = ?, facts = ?, public_ip = ?, state = ?, version = ?
				WHERE id = ?`, req.MachineID, req.WGPublicKey, HashToken(token), now.UnixMilli(), string(factsJSON), publicIP,
				"joining", req.Facts.Version, old.ID); err != nil {
				return err
			}
			if err := ClearConflicts(ctx, tx, old.ID); err != nil {
				return err
			}
			joined = old
			joined.Roles = old.Roles
		} else {
			var other string
			err := tx.QueryRowContext(ctx, `SELECT name FROM o_hosts WHERE machine_id = ?`, req.MachineID).Scan(&other)
			if err == nil {
				return refuse(http.StatusConflict, api.CodeConflict, fmt.Sprintf("A host with this machine id already joined as %s. Remove it first, or give this machine a new /etc/machine-id.", other))
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			idx, err := nextMeshIndex(ctx, tx, contains(roles, "control"))
			if err != nil {
				return err
			}
			hostName := name
			if hostName == "" {
				hostName = req.Hostname
			}
			if hostName == "" {
				hostName = fmt.Sprintf("host-%d", idx)
			}
			rb, _ := json.Marshal(roles)
			joined = HostRow{ID: NewID("hst"), Name: hostName, Roles: roles, MeshIndex: idx}
			if _, err := tx.ExecContext(ctx, `INSERT INTO o_hosts (id, name, roles, state, token_hash, token_rotated_at, machine_id, facts,
				mesh_index, mesh_pubkey, public_ip, last_seen, drain, reserve_mb, version, created_at)
				VALUES (?, ?, ?, 'joining', ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`,
				joined.ID, hostName, string(rb), HashToken(token), now.UnixMilli(), req.MachineID, string(factsJSON), idx,
				req.WGPublicKey, publicIP, req.Facts.Version, now.UnixMilli()); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE o_join_tokens SET used_at = ? WHERE id = ?`, now.UnixMilli(), id); err != nil {
			return err
		}
		resp = proto.JoinResponse{
			HostID:    joined.ID,
			HostToken: token,
			Roles:     joined.Roles,
			Mesh: proto.MeshSelf{
				Index:      joined.MeshIndex,
				Address:    h.o.Plan.MeshAddr(joined.MeshIndex).String() + "/32",
				ListenPort: MeshPort,
				HostSubnet: h.o.Plan.HostSubnet(joined.MeshIndex).String(),
			},
			RegistryAddress: h.o.Plan.MeshAddr(1).String() + ":5000",
			Epoch:           h.Epoch(),
		}
		return nil
	})
	if err != nil {
		return proto.JoinResponse{}, err
	}
	h.event(ctx, EventJoined, joined.ID, fmt.Sprintf("Host %s joined from %s.", joined.Name, ip))
	return resp, nil
}

// MeshPort is the WireGuard UDP port.
const MeshPort = 51820

func nextMeshIndex(ctx context.Context, tx DB, control bool) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT mesh_index FROM o_hosts WHERE mesh_index IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer closeRows(rows)
	used := map[int]bool{}
	for rows.Next() {
		var i int
		if err := rows.Scan(&i); err != nil {
			return 0, err
		}
		used[i] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if control {
		if used[1] {
			return 0, refuse(http.StatusConflict, api.CodeConflict, "This install already has a control host. Join this host without the control role.")
		}
		return 1, nil
	}
	for i := 2; i <= ipam.MaxHosts; i++ {
		if !used[i] {
			return i, nil
		}
	}
	return 0, refuse(http.StatusConflict, api.CodeConflict, fmt.Sprintf("This install already has %d hosts, the most 5050cloud v0.1 supports.", ipam.MaxHosts))
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Error: msg, Code: code})
}
