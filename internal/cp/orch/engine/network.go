// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/routes"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// NetworkChanged recomputes routes, TCP proxies, DNS and egress after
// domains, proxies or the environment's egress changed. Pushes coalesce per
// host (7.1), so callers may call it freely.
func (o *Orchestrator) NetworkChanged(ctx context.Context, envID string) {
	o.invalidateNet()
	o.hub.MarkDirty()
}

// storeCert keeps a certificate the ACME primary issued, encrypted, and
// pushes it to the other ingress hosts (8.4).
func (o *Orchestrator) storeCert(ctx context.Context, c proto.CertBundle) error {
	d := strings.ToLower(strings.TrimSpace(c.Domain))
	if d == "" || c.CertPEM == "" || c.KeyPEM == "" {
		return errorf("The issued certificate is incomplete.")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	enc, err := o.box.Seal(b, "o_certificates|"+d)
	if err != nil {
		return err
	}
	now := o.now().UnixMilli()
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO o_certificates (domain, cert_enc, not_after, updated_at, retry_at) VALUES (?, ?, ?, ?, 0)
			ON CONFLICT(domain) DO UPDATE SET cert_enc = excluded.cert_enc, not_after = excluded.not_after, updated_at = excluded.updated_at, retry_at = 0`,
			d, enc, c.NotAfter, now)
		return err
	}); err != nil {
		return err
	}
	o.mu.Lock()
	delete(o.certRetry, d)
	o.mu.Unlock()
	o.hub.MarkDirty(o.ingressHosts(ctx)...)
	return nil
}

func (o *Orchestrator) ingressHosts(ctx context.Context) []string {
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return nil
	}
	var out []string
	for _, h := range hosts {
		if h.HasRole("ingress") {
			out = append(out, h.ID)
		}
	}
	return out
}

// certBundles decrypts every stored certificate for non-primary ingress hosts.
func (o *Orchestrator) certBundles(ctx context.Context) ([]proto.CertBundle, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT domain, cert_enc FROM o_certificates ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	out := []proto.CertBundle{}
	now := o.now().UnixMilli()
	for rows.Next() {
		var d string
		var enc []byte
		if err := rows.Scan(&d, &enc); err != nil {
			return nil, err
		}
		b, err := o.box.Open(enc, "o_certificates|"+d)
		if err != nil {
			o.log.Warn("certificate cannot be decrypted with this master key", "domain", d)
			continue
		}
		var c proto.CertBundle
		if json.Unmarshal(b, &c) != nil || (c.NotAfter > 0 && c.NotAfter < now) {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DomainCertStatus returns "issuing", "active" or "error" and the expiry.
func (o *Orchestrator) DomainCertStatus(ctx context.Context, domain string) (string, int64, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	if routes.TLSMode(d) == "internal" {
		return "active", 0, nil
	}
	var notAfter, retry int64
	err := o.db.QueryRowContext(ctx, `SELECT not_after, retry_at FROM o_certificates WHERE domain = ?`, d).Scan(&notAfter, &retry)
	if errors.Is(err, sql.ErrNoRows) {
		return "issuing", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if notAfter > 0 && notAfter < o.now().UnixMilli() {
		return "error", notAfter, nil
	}
	return "active", notAfter, nil
}

// RetryCertificate forces a new issuance attempt: the route is withdrawn
// from the ACME primary for one push and put back, which makes Caddy load
// its config again and retry the certificate at once.
func (o *Orchestrator) RetryCertificate(ctx context.Context, domain string) error {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return errorf("Name the domain whose certificate to retry.")
	}
	out, _, _, err := o.netState(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, r := range out.Routes {
		if r.Domain == d {
			found = true
		}
	}
	if !found {
		return errorf("No service uses the domain %s.", d)
	}
	o.mu.Lock()
	o.certRetry[d] = true
	o.mu.Unlock()
	ing := o.ingressHosts(ctx)
	o.hub.MarkDirty(ing...)
	go func() {
		bctx := context.WithoutCancel(ctx)
		o.hub.WaitApplied(bctx, ing, o.t.SwitchWait)
		o.mu.Lock()
		delete(o.certRetry, d)
		o.mu.Unlock()
		o.hub.MarkDirty(ing...)
	}()
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_certificates SET retry_at = ? WHERE domain = ?`, o.now().UnixMilli(), d)
		return err
	})
	return nil
}

// withoutRetrying drops routes whose certificate is being retried.
func (o *Orchestrator) withoutRetrying(rs []proto.Route) []proto.Route {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.certRetry) == 0 {
		return rs
	}
	out := rs[:0:0]
	for _, r := range rs {
		if !o.certRetry[r.Domain] {
			out = append(out, r)
		}
	}
	return out
}

// certExpiryCheck warns about certificates that expire within 14 days.
func (o *Orchestrator) certExpiryCheck(ctx context.Context) {
	soon := o.now().Add(14 * 24 * time.Hour).UnixMilli()
	rows, err := o.db.QueryContext(ctx, `SELECT domain, not_after FROM o_certificates WHERE not_after > 0 AND not_after < ?`, soon)
	if err != nil {
		return
	}
	var expiring []string
	for rows.Next() {
		var d string
		var na int64
		if rows.Scan(&d, &na) == nil {
			expiring = append(expiring, d)
		}
	}
	closeRows(rows)
	for _, d := range expiring {
		o.log.Warn("certificate expires soon and was not renewed", "domain", d)
	}
}
