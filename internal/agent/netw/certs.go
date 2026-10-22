// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// certScanInterval is how often the ACME primary looks for new or renewed certificates.
var certScanInterval = 30 * time.Second

// storedCert is a certificate Caddy keeps in its storage.
type storedCert struct {
	CertPEM, KeyPEM string
	NotAfter        time.Time
	Issuer          string // storage directory: "acme-v02.api.letsencrypt.org-directory", "local", ...
}

func certNotAfter(certPEM []byte) (time.Time, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, fmt.Errorf("no certificate in PEM")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return c.NotAfter, nil
}

// findStoredCert returns the certificate with the latest expiry for a domain
// across every issuer directory of Caddy's file storage.
func findStoredCert(storageRoot, domain string, withKey bool) (storedCert, bool) {
	matches, _ := filepath.Glob(filepath.Join(storageRoot, "certificates", "*", domain, domain+".crt"))
	var best storedCert
	found := false
	for _, crt := range matches {
		b, err := os.ReadFile(crt)
		if err != nil {
			continue
		}
		na, err := certNotAfter(b)
		if err != nil || (found && !na.After(best.NotAfter)) {
			continue
		}
		sc := storedCert{CertPEM: string(b), NotAfter: na, Issuer: filepath.Base(filepath.Dir(filepath.Dir(crt)))}
		if withKey {
			k, err := os.ReadFile(strings.TrimSuffix(crt, ".crt") + ".key")
			if err != nil {
				continue
			}
			sc.KeyPEM = string(k)
		}
		best, found = sc, true
	}
	return best, found
}

// tlsLogErrors reads the tail of Caddy's TLS log and returns, per domain, the
// last issuance error that was not followed by a success.
func tlsLogErrors(path string, maxBytes int64) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxBytes {
		_, _ = f.Seek(fi.Size()-maxBytes, io.SeekStart)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"identifier"`)) {
			continue
		}
		var e struct {
			Level      string `json:"level"`
			Msg        string `json:"msg"`
			Identifier string `json:"identifier"`
			Error      string `json:"error"`
		}
		if json.Unmarshal(line, &e) != nil || e.Identifier == "" {
			continue
		}
		switch {
		case strings.Contains(e.Msg, "obtained successfully") || strings.Contains(e.Msg, "renewed successfully"):
			delete(out, e.Identifier)
		case e.Error != "" && (e.Level == "error" || e.Level == "warn"):
			out[e.Identifier] = e.Error
		}
	}
	return out
}

func certErrorSentence(domain, raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 300 {
		raw = raw[:300] + "..."
	}
	return fmt.Sprintf("The certificate for %s could not be issued yet: %s", domain, raw)
}

// CertStatus reports each TLS domain's certificate state for the UI:
// "active" with its expiry, "issuing" while waiting for DNS or the CA, or
// "error" with Caddy's last issuance error.
func (g *Ingress) CertStatus(ctx context.Context) (map[string]agentapi.CertState, error) {
	in, domains := g.snapshot()
	now := g.now()
	pushed := map[string]time.Time{}
	for _, c := range in.Certificates {
		if na, err := certNotAfter([]byte(c.CertPEM)); err == nil {
			pushed[strings.ToLower(c.Domain)] = na
		} else if c.NotAfter > 0 {
			pushed[strings.ToLower(c.Domain)] = time.UnixMilli(c.NotAfter)
		}
	}
	errs := tlsLogErrors(g.params.tlsLog(), 512<<10)
	out := map[string]agentapi.CertState{}
	for _, d := range sortedKeys(domains) {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		mode := domains[d]
		if mode == "acme" && !in.ACMEPrimary {
			if na, ok := pushed[d]; ok && na.After(now) {
				out[d] = agentapi.CertState{Status: "active", NotAfter: na.UnixMilli()}
			} else {
				out[d] = agentapi.CertState{Status: "issuing"}
			}
			continue
		}
		if sc, ok := findStoredCert(g.params.storage(), d, false); ok && sc.NotAfter.After(now) {
			out[d] = agentapi.CertState{Status: "active", NotAfter: sc.NotAfter.UnixMilli()}
			continue
		}
		if e, ok := errs[d]; ok {
			out[d] = agentapi.CertState{Status: "error", Error: certErrorSentence(d, e)}
			continue
		}
		out[d] = agentapi.CertState{Status: "issuing"}
	}
	return out, nil
}

// IssuedCerts streams the certificates this host obtains or renews while it
// is the ACME primary, so the control plane can push them to the other
// ingress hosts. Each certificate is sent once per expiry date. It returns
// when ctx ends.
func (g *Ingress) IssuedCerts(ctx context.Context, out chan<- proto.CertBundle) error {
	sent := map[string]time.Time{}
	t := time.NewTicker(certScanInterval)
	defer t.Stop()
	for {
		in, domains := g.snapshot()
		if in.ACMEPrimary {
			for _, d := range sortedKeys(domains) {
				if domains[d] != "acme" {
					continue
				}
				sc, ok := findStoredCert(g.params.storage(), d, true)
				if !ok || sc.Issuer == "local" || sc.NotAfter.Equal(sent[d]) {
					continue
				}
				b := proto.CertBundle{Domain: d, CertPEM: sc.CertPEM, KeyPEM: sc.KeyPEM, NotAfter: sc.NotAfter.UnixMilli()}
				select {
				case out <- b:
					sent[d] = sc.NotAfter
				case <-ctx.Done():
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
