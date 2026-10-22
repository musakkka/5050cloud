// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ---------------------------------------------------------------- domains

type domainRow struct {
	api.Domain
	EnvID     string
	CheckedAt int64
	CreatedAt int64
}

const domainCols = `id, environment_id, service_id, domain, kind, port, dns_ok, cloudflare, message, checked_at, created_at`

func scanDomain(r interface{ Scan(...any) error }) (domainRow, error) {
	var d domainRow
	var dnsOK, cf int
	err := r.Scan(&d.ID, &d.EnvID, &d.ServiceID, &d.Domain.Domain, &d.Kind, &d.Port, &dnsOK, &cf, &d.Message, &d.CheckedAt, &d.CreatedAt)
	d.DNSOK, d.Cloudflare = dnsOK == 1, cf == 1
	return d, err
}

// domainView fills DNS instructions and the certificate state.
func (c *Core) domainView(ctx context.Context, d domainRow, in installSettings, generated string) api.Domain {
	out := d.Domain
	if out.Kind == "generated" {
		out.DNSOK = true
	} else {
		ip := in.PublicIP
		if ip != "" {
			out.DNS = append(out.DNS, api.DNSRecord{Type: "A", Name: out.Domain, Value: ip})
		}
		if generated != "" {
			out.DNS = append(out.DNS, api.DNSRecord{Type: "CNAME", Name: out.Domain, Value: generated})
		}
	}
	if !out.DNSOK {
		out.Status = "pending_dns"
		if out.Message == "" {
			out.Message = "Waiting for DNS"
		}
		return out
	}
	out.Status = "issuing"
	if c.orch != nil {
		if st, notAfter, err := c.orch.DomainCertStatus(ctx, out.Domain); err == nil && st != "" {
			out.Status = st
			out.CertOK = st == "active"
			out.CertExpiresAt = notAfter
		}
	}
	if out.Cloudflare && out.Message == "" {
		out.Message = "Cloudflare proxy detected: set SSL/TLS mode to Full (strict)"
	}
	return out
}

func (c *Core) envDomains(ctx context.Context, envID string) (map[string][]api.Domain, error) {
	rows, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (domainRow, error) { return scanDomain(r) },
		`SELECT `+domainCols+` FROM domains WHERE environment_id = ? ORDER BY created_at, id`, envID)
	if err != nil {
		return nil, err
	}
	in, _ := c.installRow(ctx, c.st.Reader)
	gen := map[string]string{}
	for _, d := range rows {
		if d.Kind == "generated" && gen[d.ServiceID] == "" {
			gen[d.ServiceID] = d.Domain.Domain
		}
	}
	out := map[string][]api.Domain{}
	for _, d := range rows {
		out[d.ServiceID] = append(out[d.ServiceID], c.domainView(ctx, d, in, gen[d.ServiceID]))
	}
	return out, nil
}

func (c *Core) domainByID(ctx context.Context, q store.Querier, id string) (domainRow, error) {
	d, err := scanDomain(q.QueryRowContext(ctx, `SELECT `+domainCols+` FROM domains WHERE id = ?`, id))
	if store.NoRows(err) {
		return d, fail.NotFound("The domain %s does not exist.", id)
	}
	return d, err
}

// DomainEnv returns the environment of a domain (for access checks).
func (c *Core) DomainEnv(ctx context.Context, id string) (string, error) {
	d, err := c.domainByID(ctx, c.st.Reader, id)
	return d.EnvID, err
}

// Domains is GET .../domains.
func (c *Core) Domains(ctx context.Context, envID, sid string) ([]api.Domain, error) {
	if _, err := c.liveInstance(ctx, c.st.Reader, envID, sid); err != nil {
		return nil, err
	}
	m, err := c.envDomains(ctx, envID)
	if err != nil {
		return nil, err
	}
	if m[sid] == nil {
		return []api.Domain{}, nil
	}
	return m[sid], nil
}

func (c *Core) domainOne(ctx context.Context, id string) (api.Domain, error) {
	d, err := c.domainByID(ctx, c.st.Reader, id)
	if err != nil {
		return api.Domain{}, err
	}
	list, err := c.Domains(ctx, d.EnvID, d.ServiceID)
	if err != nil {
		return api.Domain{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return d.Domain, nil
}

// CreateDomain adds a generated or custom domain (applied at once).
func (c *Core) CreateDomain(ctx context.Context, a Actor, envID, sid string, req api.CreateDomainRequest) (api.Domain, error) {
	env, _, err := c.Environment(ctx, envID)
	if err != nil {
		return api.Domain{}, err
	}
	in, err := c.liveInstance(ctx, c.st.Reader, envID, sid)
	if err != nil {
		return api.Domain{}, err
	}
	port := req.Port
	if port == 0 {
		port = in.Config.Net.Port
	}
	if port == 0 {
		// The only detected listening port (docs/SPEC.md 9.11), else 8080.
		if r, ok := c.runtime(ctx, envID)[sid]; ok && len(r.ListeningPorts) == 1 {
			port = r.ListeningPorts[0]
		}
	}
	if port == 0 {
		port = 8080
	}
	if port < 1 || port > 65535 {
		return api.Domain{}, fail.Validation("port", "The port must be between 1 and 65535.")
	}
	inst, _ := c.installRow(ctx, c.st.Reader)
	var name string
	switch req.Kind {
	case "generated", "":
		req.Kind = "generated"
		if inst.BaseDomain == "" {
			return api.Domain{}, fail.Unavailable("This install has no base domain, so domains cannot be generated. Set one in Settings.")
		}
		slug := changes.Slug(in.Config.Name)
		if env.Name != "production" {
			slug += "-" + changes.Slug(env.Name)
		}
		name = slug + "-" + ids.Random(4, ids.LowerAlnum) + "." + inst.BaseDomain
	case "custom":
		name = strings.TrimSuffix(lower(trim(req.Domain)), ".")
		if strings.Contains(name, "*") {
			return api.Domain{}, fail.Validation("domain", "Wildcard domains need DNS validation through a DNS provider, which arrives in v0.2.")
		}
		if !config.ValidDomain(name) {
			return api.Domain{}, fail.Validation("domain", "%q is not a valid domain name.", req.Domain)
		}
	default:
		return api.Domain{}, fail.Validation("kind", "The domain kind must be generated or custom.")
	}
	id := ids.New(ids.Domain)
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO domains (id, environment_id, service_id, domain, kind, port, dns_ok, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, envID, sid, name, req.Kind, port, store.Bool(req.Kind == "generated"), c.Now())
		if isUnique(err) {
			return fail.Conflict("The domain %s is already used by a service.", name)
		}
		if err != nil {
			return err
		}
		return c.insertActivity(ctx, tx, envID, api.Activity{ServiceID: sid, Kind: "domain", Text: "Added domain " + name, Actor: a.Label(), Ref: id})
	})
	if err != nil {
		return api.Domain{}, err
	}
	c.networkChanged(ctx, envID, sid)
	if req.Kind == "custom" {
		go c.checkDomain(context.WithoutCancel(ctx), id)
	}
	return c.domainOne(ctx, id)
}

// UpdateDomain changes the target port.
func (c *Core) UpdateDomain(ctx context.Context, a Actor, id string, req api.UpdateDomainRequest) (api.Domain, error) {
	if req.Port < 1 || req.Port > 65535 {
		return api.Domain{}, fail.Validation("port", "The port must be between 1 and 65535.")
	}
	d, err := c.domainByID(ctx, c.st.Reader, id)
	if err != nil {
		return api.Domain{}, err
	}
	if err := c.st.Exec(ctx, `UPDATE domains SET port = ? WHERE id = ?`, req.Port, id); err != nil {
		return api.Domain{}, err
	}
	c.networkChanged(ctx, d.EnvID, d.ServiceID)
	return c.domainOne(ctx, id)
}

// DeleteDomain removes a domain at once.
func (c *Core) DeleteDomain(ctx context.Context, a Actor, id string) error {
	d, err := c.domainByID(ctx, c.st.Reader, id)
	if err != nil {
		return err
	}
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id); err != nil {
			return err
		}
		return c.insertActivity(ctx, tx, d.EnvID, api.Activity{ServiceID: d.ServiceID, Kind: "domain", Text: "Removed domain " + d.Domain.Domain, Actor: a.Label(), Ref: id})
	})
	if err == nil {
		c.networkChanged(ctx, d.EnvID, d.ServiceID)
	}
	return err
}

// CheckDomain runs the DNS check now.
func (c *Core) CheckDomain(ctx context.Context, id string) (api.Domain, error) {
	if _, err := c.domainByID(ctx, c.st.Reader, id); err != nil {
		return api.Domain{}, err
	}
	c.checkDomain(ctx, id)
	return c.domainOne(ctx, id)
}

// RetryCertificate asks the primary ingress for a new attempt.
func (c *Core) RetryCertificate(ctx context.Context, id string) (api.Domain, error) {
	d, err := c.domainByID(ctx, c.st.Reader, id)
	if err != nil {
		return api.Domain{}, err
	}
	if c.orch != nil {
		if err := c.orch.RetryCertificate(ctx, d.Domain.Domain); err != nil {
			return api.Domain{}, err
		}
	}
	return c.domainOne(ctx, id)
}

func (c *Core) networkChanged(ctx context.Context, envID, sid string) {
	if c.orch != nil {
		c.orch.NetworkChanged(ctx, envID)
	}
	if svc, err := c.Service(ctx, envID, sid); err == nil {
		c.publishService(ctx, svc)
	}
}

// resolver queries public resolvers so a stale local cache does not decide.
func publicResolver(addr string) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", addr)
	}}
}

// cloudflareRanges are Cloudflare's published IPv4 ranges.
var cloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

func isCloudflare(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, r := range cloudflareRanges {
		if p, err := netip.ParsePrefix(r); err == nil && p.Contains(a) {
			return true
		}
	}
	return false
}

// checkDomain resolves a custom domain and records whether it points here.
func (c *Core) checkDomain(ctx context.Context, id string) {
	d, err := c.domainByID(ctx, c.st.Reader, id)
	if err != nil || d.Kind != "custom" {
		return
	}
	in, _ := c.installRow(ctx, c.st.Reader)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var addrs []string
	for _, r := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		if a, err := publicResolver(r).LookupHost(ctx, d.Domain.Domain); err == nil && len(a) > 0 {
			addrs = a
			break
		}
	}
	ok, cf := false, false
	msg := ""
	for _, a := range addrs {
		if a == in.PublicIP {
			ok = true
		}
		if isCloudflare(a) {
			cf, ok = true, true
		}
	}
	switch {
	case len(addrs) == 0:
		msg = "Waiting for DNS: the name does not resolve yet"
	case !ok:
		msg = fmt.Sprintf("Waiting for DNS: the name points at %s, not %s", strings.Join(addrs, ", "), in.PublicIP)
	}
	_ = c.st.Exec(context.WithoutCancel(ctx), `UPDATE domains SET dns_ok = ?, cloudflare = ?, message = ?, checked_at = ? WHERE id = ?`,
		store.Bool(ok), store.Bool(cf), msg, c.Now(), id)
	if ok != d.DNSOK {
		c.networkChanged(context.WithoutCancel(ctx), d.EnvID, d.ServiceID)
	}
}

// checkDomainsLoop re-checks custom domains: every 30 s for 10 minutes
// after creation, then every 10 minutes.
func (c *Core) checkDomainsLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := c.Now()
		rows, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (domainRow, error) { return scanDomain(r) },
			`SELECT `+domainCols+` FROM domains WHERE kind = 'custom'`)
		if err != nil {
			continue
		}
		for _, d := range rows {
			young := now-d.CreatedAt < 10*minute
			if (young && now-d.CheckedAt >= 25*1000) || now-d.CheckedAt >= 10*minute {
				c.checkDomain(ctx, d.ID)
			}
		}
	}
}

// ---------------------------------------------------------------- TCP proxies

func (c *Core) envProxies(ctx context.Context, q store.Querier, envID, base string) (map[string][]api.TCPProxy, error) {
	type row struct {
		sid string
		p   api.TCPProxy
	}
	rows, err := store.All(ctx, q, func(r *sql.Rows) (row, error) {
		var x row
		return x, r.Scan(&x.p.ID, &x.sid, &x.p.TargetPort, &x.p.HostPort)
	}, `SELECT id, service_id, target_port, host_port FROM tcp_proxies WHERE environment_id = ? ORDER BY created_at`, envID)
	if err != nil {
		return nil, err
	}
	out := map[string][]api.TCPProxy{}
	for _, x := range rows {
		x.p.Address = fmt.Sprintf("%s:%d", base, x.p.HostPort)
		out[x.sid] = append(out[x.sid], x.p)
	}
	return out, nil
}

// CreateTCPProxy picks a free host port in 20000-29999.
func (c *Core) CreateTCPProxy(ctx context.Context, a Actor, envID, sid string, req api.CreateTCPProxyRequest) (api.TCPProxy, error) {
	if req.TargetPort < 1 || req.TargetPort > 65535 {
		return api.TCPProxy{}, fail.Validation("targetPort", "The target port must be between 1 and 65535.")
	}
	if _, err := c.liveInstance(ctx, c.st.Reader, envID, sid); err != nil {
		return api.TCPProxy{}, err
	}
	id := ids.New(ids.TCPProxy)
	var hostPort int
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		used := map[int]bool{}
		ports, err := store.All(ctx, tx, func(r *sql.Rows) (int, error) { var p int; return p, r.Scan(&p) }, `SELECT host_port FROM tcp_proxies`)
		if err != nil {
			return err
		}
		for _, p := range ports {
			used[p] = true
		}
		if len(used) >= 10000 {
			return fail.Unavailable("Every TCP proxy port between 20000 and 29999 is in use.")
		}
		for {
			hostPort = 20000 + rand.IntN(10000)
			if !used[hostPort] {
				break
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tcp_proxies (id, environment_id, service_id, target_port, host_port, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, envID, sid, req.TargetPort, hostPort, c.Now()); err != nil {
			return err
		}
		return c.insertActivity(ctx, tx, envID, api.Activity{ServiceID: sid, Kind: "tcp_proxy", Text: fmt.Sprintf("Added a TCP proxy to port %d", req.TargetPort), Actor: a.Label(), Ref: id})
	})
	if err != nil {
		return api.TCPProxy{}, err
	}
	c.networkChanged(ctx, envID, sid)
	in, _ := c.installRow(ctx, c.st.Reader)
	base := in.BaseDomain
	if base == "" {
		base = in.PublicIP
	}
	return api.TCPProxy{ID: id, TargetPort: req.TargetPort, HostPort: hostPort, Address: fmt.Sprintf("%s:%d", base, hostPort)}, nil
}

// DeleteTCPProxy removes a proxy at once.
func (c *Core) DeleteTCPProxy(ctx context.Context, a Actor, envID, sid, id string) error {
	var n int64
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM tcp_proxies WHERE id = ? AND environment_id = ? AND service_id = ?`, id, envID, sid)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		if n == 0 {
			return fail.NotFound("The TCP proxy %s does not exist.", id)
		}
		return c.insertActivity(ctx, tx, envID, api.Activity{ServiceID: sid, Kind: "tcp_proxy", Text: "Removed a TCP proxy", Actor: a.Label(), Ref: id})
	})
	if err == nil {
		c.networkChanged(ctx, envID, sid)
	}
	return err
}

// ---------------------------------------------------------------- volumes (staged)

// CreateVolume stages a volume for a service.
func (c *Core) CreateVolume(ctx context.Context, a Actor, envID, sid string, req api.CreateVolumeRequest) (api.Volume, error) {
	if err := changes.CheckMountPath(req.MountPath); err != nil {
		return api.Volume{}, err
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		in, err := c.liveInstance(ctx, tx, envID, sid)
		if err != nil {
			return err
		}
		vols, err := c.volumes(ctx, tx, envID)
		if err != nil {
			return err
		}
		if _, ok := vols[sid]; ok {
			return fail.Conflict("This service already has a volume.")
		}
		name := cleanName(req.Name)
		if name == "" {
			name = changes.Slug(in.Config.Name) + "-volume"
		}
		return c.stage(ctx, tx, envID, sid, changes.KindVolumeCreate, a.Label(), func(r *changeRow) {
			r.Patch = map[string]any{"name": name, "mountPath": req.MountPath}
		})
	})
	if err != nil {
		return api.Volume{}, err
	}
	svc, err := c.afterStage(ctx, envID, sid)
	if err != nil || svc.Volume == nil {
		return api.Volume{}, err
	}
	return *svc.Volume, nil
}

// UpdateVolume renames at once, stages a mount path change, sets the alert threshold.
func (c *Core) UpdateVolume(ctx context.Context, a Actor, envID, sid string, req api.UpdateVolumeRequest) (api.Volume, error) {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		vols, err := c.volumes(ctx, tx, envID)
		if err != nil {
			return err
		}
		v, ok := vols[sid]
		if !ok {
			cs, err := c.envChanges(ctx, tx, envID)
			if err != nil {
				return err
			}
			r := indexChanges(cs).get(sid, changes.KindVolumeCreate)
			if r == nil {
				return fail.NotFound("This service has no volume.")
			}
			return c.stage(ctx, tx, envID, sid, changes.KindVolumeCreate, a.Label(), func(cur *changeRow) {
				if req.Name != nil {
					cur.Patch["name"] = cleanName(*req.Name)
				}
				if req.MountPath != nil {
					cur.Patch["mountPath"] = *req.MountPath
				}
			})
		}
		if req.Name != nil {
			n := cleanName(*req.Name)
			if n == "" || len(n) > 64 {
				return fail.Validation("name", "A volume name must be 1 to 64 characters.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE volumes SET name = ? WHERE id = ?`, n, v.ID); err != nil {
				return err
			}
		}
		if req.AlertAtPct != nil {
			if *req.AlertAtPct < 1 || *req.AlertAtPct > 100 {
				return fail.Validation("alertAtPct", "The alert threshold must be between 1 and 100 percent.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE volumes SET alert_at_pct = ? WHERE id = ?`, *req.AlertAtPct, v.ID); err != nil {
				return err
			}
		}
		if req.MountPath != nil && *req.MountPath != v.MountPath {
			if err := changes.CheckMountPath(*req.MountPath); err != nil {
				return err
			}
			return c.stage(ctx, tx, envID, sid, changes.KindVolumeUpdate, a.Label(), func(cur *changeRow) {
				cur.Patch = map[string]any{"mountPath": *req.MountPath}
			})
		}
		return nil
	})
	if err != nil {
		return api.Volume{}, err
	}
	svc, err := c.afterStage(ctx, envID, sid)
	if err != nil || svc.Volume == nil {
		return api.Volume{}, err
	}
	return *svc.Volume, nil
}

// DeleteVolume stages a delete (type the volume name).
func (c *Core) DeleteVolume(ctx context.Context, a Actor, envID, sid, confirm string) error {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		vols, err := c.volumes(ctx, tx, envID)
		if err != nil {
			return err
		}
		v, ok := vols[sid]
		if !ok {
			res, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE environment_id = ? AND service_id = ? AND kind = ?`, envID, sid, changes.KindVolumeCreate)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fail.NotFound("This service has no volume.")
			}
			return nil
		}
		if confirm != v.Name {
			return fail.Validation("confirm", "Type the volume name %q to confirm.", v.Name)
		}
		return c.stage(ctx, tx, envID, sid, changes.KindVolumeDelete, a.Label(), func(*changeRow) {})
	})
	if err != nil {
		return err
	}
	_, err = c.afterStage(ctx, envID, sid)
	return err
}

// VolumeDetail is GET .../volume.
func (c *Core) VolumeDetail(ctx context.Context, envID, sid string) (api.VolumeDetail, error) {
	svc, err := c.Service(ctx, envID, sid)
	if err != nil {
		return api.VolumeDetail{}, err
	}
	if svc.Volume == nil {
		return api.VolumeDetail{}, fail.NotFound("This service has no volume.")
	}
	d := api.VolumeDetail{Volume: *svc.Volume}
	if c.orch != nil && svc.Volume.ID != "" {
		if rt, err := c.orch.VolumeRuntime(ctx, envID, sid); err == nil {
			d.HostID, d.HostName, d.UsedBytes, d.HostLost = rt.HostID, rt.HostName, rt.UsedBytes, rt.HostLost
			d.HostFreeBytes, d.HostTotalBytes = rt.HostFreeBytes, rt.HostTotalBytes
		}
	}
	return d, nil
}

// VolumeName returns the committed volume name (wipe confirmation).
func (c *Core) VolumeName(ctx context.Context, envID, sid string) (string, error) {
	vols, err := c.volumes(ctx, c.st.Reader, envID)
	if err != nil {
		return "", err
	}
	v, ok := vols[sid]
	if !ok {
		return "", fail.NotFound("This service has no volume.")
	}
	return v.Name, nil
}

// ---------------------------------------------------------------- monitors

var monitorMetrics = map[string]bool{"cpu": true, "memory": true, "disk": true, "volume": true, "http_5xx_rate": true, "restarts": true}

func scanMonitor(r interface{ Scan(...any) error }) (api.Monitor, error) {
	var m api.Monitor
	var ch string
	err := r.Scan(&m.ID, &m.EnvironmentID, &m.ServiceID, &m.Metric, &m.Threshold, &m.Unit, &m.ForMinutes, &ch, &m.State, &m.LastFired)
	_ = json.Unmarshal([]byte(ch), &m.Channels)
	return m, err
}

const monitorCols = `id, environment_id, service_id, metric, threshold, unit, for_minutes, channels, state, last_fired_at`

// Monitors implements cpapi.Config.
func (c *Core) Monitors(ctx context.Context) ([]api.Monitor, error) {
	return store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.Monitor, error) { return scanMonitor(r) },
		`SELECT `+monitorCols+` FROM monitors ORDER BY created_at`)
}

// ServiceMonitors is GET .../monitors, with live state from the orchestrator.
func (c *Core) ServiceMonitors(ctx context.Context, envID, sid string) ([]api.Monitor, error) {
	list, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.Monitor, error) { return scanMonitor(r) },
		`SELECT `+monitorCols+` FROM monitors WHERE environment_id = ? AND service_id = ? ORDER BY created_at`, envID, sid)
	if err != nil {
		return nil, err
	}
	if c.orch != nil {
		if live, err := c.orch.Monitors(ctx, envID, sid); err == nil {
			for i := range list {
				if s, ok := live[list[i].ID]; ok {
					list[i].State = s
				}
			}
		}
	}
	if list == nil {
		list = []api.Monitor{}
	}
	return list, nil
}

func checkMonitor(metric string, threshold float64, unit string, forMin int) error {
	if !monitorMetrics[metric] {
		return fail.Validation("metric", "The metric must be cpu, memory, disk, volume, http_5xx_rate or restarts.")
	}
	if unit != "" && unit != "pct" && unit != "gb" {
		return fail.Validation("unit", "The unit must be pct or gb.")
	}
	if threshold <= 0 {
		return fail.Validation("threshold", "The threshold must be above zero.")
	}
	if forMin < 1 || forMin > 60 {
		return fail.Validation("forMinutes", "The duration must be between 1 and 60 minutes.")
	}
	return nil
}

// CreateMonitor adds a monitor (applied at once).
func (c *Core) CreateMonitor(ctx context.Context, a Actor, envID, sid string, req api.CreateMonitorRequest) (api.Monitor, error) {
	if req.Unit == "" {
		req.Unit = "pct"
	}
	if err := checkMonitor(req.Metric, req.Threshold, req.Unit, req.ForMinutes); err != nil {
		return api.Monitor{}, err
	}
	if _, err := c.liveInstance(ctx, c.st.Reader, envID, sid); err != nil {
		return api.Monitor{}, err
	}
	m := api.Monitor{ID: ids.New(ids.Monitor), EnvironmentID: envID, ServiceID: sid, Metric: req.Metric, Threshold: req.Threshold,
		Unit: req.Unit, ForMinutes: req.ForMinutes, Channels: req.Channels, State: "ok"}
	err := c.st.Exec(ctx, `INSERT INTO monitors (id, environment_id, service_id, metric, threshold, unit, for_minutes, channels, state, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'ok', ?)`,
		m.ID, envID, sid, m.Metric, m.Threshold, m.Unit, m.ForMinutes, jsonString(m.Channels), c.Now())
	return m, err
}

func (c *Core) monitorByID(ctx context.Context, id string) (api.Monitor, error) {
	m, err := scanMonitor(c.st.Reader.QueryRowContext(ctx, `SELECT `+monitorCols+` FROM monitors WHERE id = ?`, id))
	if store.NoRows(err) {
		return m, fail.NotFound("The monitor %s does not exist.", id)
	}
	return m, err
}

// MonitorEnv returns the environment of a monitor (access checks).
func (c *Core) MonitorEnv(ctx context.Context, id string) (string, error) {
	m, err := c.monitorByID(ctx, id)
	return m.EnvironmentID, err
}

// UpdateMonitor edits a monitor.
func (c *Core) UpdateMonitor(ctx context.Context, id string, req api.UpdateMonitorRequest) (api.Monitor, error) {
	m, err := c.monitorByID(ctx, id)
	if err != nil {
		return m, err
	}
	if req.Threshold != nil {
		m.Threshold = *req.Threshold
	}
	if req.Unit != nil {
		m.Unit = *req.Unit
	}
	if req.ForMinutes != nil {
		m.ForMinutes = *req.ForMinutes
	}
	if req.Channels != nil {
		m.Channels = *req.Channels
	}
	if err := checkMonitor(m.Metric, m.Threshold, m.Unit, m.ForMinutes); err != nil {
		return m, err
	}
	err = c.st.Exec(ctx, `UPDATE monitors SET threshold = ?, unit = ?, for_minutes = ?, channels = ? WHERE id = ?`, m.Threshold, m.Unit, m.ForMinutes, jsonString(m.Channels), id)
	return m, err
}

// DeleteMonitor removes a monitor.
func (c *Core) DeleteMonitor(ctx context.Context, id string) error {
	if _, err := c.monitorByID(ctx, id); err != nil {
		return err
	}
	return c.st.Exec(ctx, `DELETE FROM monitors WHERE id = ?`, id)
}

// SetMonitorState implements cpapi.Config.
func (c *Core) SetMonitorState(ctx context.Context, id, state string, at int64) error {
	if state == "firing" {
		return c.st.Exec(ctx, `UPDATE monitors SET state = ?, last_fired_at = ? WHERE id = ?`, state, at, id)
	}
	return c.st.Exec(ctx, `UPDATE monitors SET state = ? WHERE id = ?`, state, id)
}
