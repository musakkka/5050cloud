// SPDX-License-Identifier: AGPL-3.0-only

package webhooks

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
)

// blocked are the ranges an outbound request may not reach unless the owner
// allowed private destinations (docs/SPEC.md 9.15): loopback, link-local
// (including the cloud metadata address), private, carrier-grade NAT (which
// holds the mesh 100.96.0.0/16), the pod range inside 10.0.0.0/8, and // check-public: allow
// addresses that are never valid destinations.
var blocked = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", // check-public: allow
	"172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", // check-public: allow
	"224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "64:ff9b::/96",
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// Blocked reports whether ip is in a range outbound requests may not reach.
func Blocked(ip netip.Addr) bool {
	// Prefix.Contains is false for any address with a zone.
	ip = ip.WithZone("").Unmap()
	for _, p := range blocked {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// ErrBlocked is returned when a destination resolves to a refused address.
type ErrBlocked struct{ Addr string }

func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("The destination %s is a private, loopback or metadata address, which outbound requests may not reach. An owner can allow private destinations in Settings.", e.Addr)
}

// Control checks the address at connect time, after DNS resolution, so a
// name that resolves to a public address first and a private one later
// (DNS rebinding) is still refused.
func Control(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return &ErrBlocked{Addr: host}
		}
		if Blocked(ip) {
			return &ErrBlocked{Addr: ip.String()}
		}
		return nil
	}
}

// NewClient returns an HTTP client whose every connection, including
// redirects and proxies, passes the guard. allowPrivate turns the guard off.
func NewClient(allowPrivate bool, timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: Control(allowPrivate)}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fail.Sentence("The webhook redirected more than 5 times.")
			}
			return nil
		},
	}
}

// DialContext is a guarded dialer for other clients (GitHub public API, S3 tests).
func DialContext(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: Control(allowPrivate)}
	return d.DialContext
}
