// SPDX-License-Identifier: AGPL-3.0-only

// Package cfranges holds Cloudflare's published proxy address ranges. The
// control plane uses them to spot a proxied DNS record and the console offers
// them as the trusted proxy preset, so both read this one list.
package cfranges

import "net/netip"

// IPv4 are Cloudflare's IPv4 ranges.
var IPv4 = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

// IPv6 are Cloudflare's IPv6 ranges.
var IPv6 = []string{
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

// All returns every range, IPv4 first.
func All() []string {
	return append(append([]string{}, IPv4...), IPv6...)
}

var prefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, r := range All() {
		if p, err := netip.ParsePrefix(r); err == nil {
			out = append(out, p)
		}
	}
	return out
}()

// Contains reports whether ip lies inside a Cloudflare range.
func Contains(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
