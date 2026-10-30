// SPDX-License-Identifier: AGPL-3.0-only

package webhooks

import (
	"net/netip"
	"testing"
)

// Prefix.Contains is false for an address with a zone, which used to pass.
func TestBlockedSeesThroughAZone(t *testing.T) {
	for _, s := range []string{"::1%lo", "fe80::1%eth0", "fd00:ec2::254%eth0"} {
		if !Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s was not blocked", s)
		}
	}
}
