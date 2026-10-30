// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// On a private host the installer's nftables table has a set of the addresses
// allowed to send to UDP 51820. The agent keeps it equal to the mesh peers.
const peerSet = "inet fifty_filter wg_peers"

// peerSetScript replaces the set's elements in one nft transaction.
func peerSetScript(peers []meshPeerPlan) string {
	var ips []netip.Addr
	for _, p := range peers {
		if p.Endpoint == nil {
			continue
		}
		if a, ok := netip.AddrFromSlice(p.Endpoint.IP); ok && a.Unmap().Is4() {
			ips = append(ips, a.Unmap())
		}
	}
	slices.SortFunc(ips, netip.Addr.Compare)
	ips = slices.Compact(ips)
	s := "flush set " + peerSet + "\n"
	if len(ips) > 0 {
		list := make([]string, len(ips))
		for i, a := range ips {
			list[i] = a.String()
		}
		s += "add element " + peerSet + " { " + strings.Join(list, ", ") + " }\n"
	}
	return s
}

// syncPeerSet does nothing on a host without the set, that is every host not
// installed with --private.
func (n *Net) syncPeerSet(ctx context.Context, peers []meshPeerPlan) error {
	if err := n.nft(ctx, "", append([]string{"list", "set"}, strings.Fields(peerSet)...)...); err != nil {
		return nil //nolint:nilerr // no set, so not a private host
	}
	if err := n.nft(ctx, peerSetScript(peers), "-f", "-"); err != nil {
		return fmt.Errorf("updating the addresses allowed to reach WireGuard failed, so the previous list is still in place: %w", err)
	}
	return nil
}

func execNft(ctx context.Context, stdin string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "nft", args...) // #nosec G204 -- fixed program, no user input
	c.Stdin = strings.NewReader(stdin)
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("nft: %s", firstLine(stderr.String(), err))
	}
	return nil
}
