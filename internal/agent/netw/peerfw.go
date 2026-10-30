// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
)

// On a private host the installer's nftables table has a set of the addresses
// allowed to send to UDP 51820. The agent keeps it equal to the mesh peers.
const peerSet = "inet fifty_filter wg_peers"

// The same table's forward chain drops the metadata address unless the source
// is in this set: the replicas whose service opted in (AllowMetadata).
const metadataSet = "inet fifty_filter metadata_allow"

func peerAddrs(peers []meshPeerPlan) []netip.Addr {
	var ips []netip.Addr
	for _, p := range peers {
		if p.Endpoint == nil {
			continue
		}
		if a, ok := netip.AddrFromSlice(p.Endpoint.IP); ok && a.Unmap().Is4() {
			ips = append(ips, a.Unmap())
		}
	}
	return ips
}

func metadataAddrs(spec firewallSpec) []netip.Addr {
	var ips []netip.Addr
	for _, e := range spec.Envs {
		ips = append(ips, e.AllowMetadata...)
	}
	return ips
}

// setScript replaces a set's elements in one nft transaction.
func setScript(set string, ips []netip.Addr) string {
	slices.SortFunc(ips, netip.Addr.Compare)
	ips = slices.Compact(ips)
	s := "flush set " + set + "\n"
	if len(ips) > 0 {
		list := make([]string, len(ips))
		for i, a := range ips {
			list[i] = a.String()
		}
		s += "add element " + set + " { " + strings.Join(list, ", ") + " }\n"
	}
	return s
}

// syncPeerSet does nothing on a host without the set, that is every host not
// installed with --private.
func (n *Net) syncPeerSet(ctx context.Context, peers []meshPeerPlan) error {
	n.lastPeers = peers
	if err := n.syncSet(ctx, peerSet, peerAddrs(peers)); err != nil {
		return fmt.Errorf("updating the addresses allowed to reach WireGuard failed, so the previous list is still in place: %w", err)
	}
	return nil
}

func (n *Net) syncMetadataSet(ctx context.Context, spec firewallSpec) error {
	n.lastMetadata = metadataAddrs(spec)
	if err := n.syncSet(ctx, metadataSet, n.lastMetadata); err != nil {
		return fmt.Errorf("updating the services allowed to reach the metadata address failed, so the previous list is still in place: %w", err)
	}
	return nil
}

func (n *Net) syncSet(ctx context.Context, set string, ips []netip.Addr) error {
	if err := n.nft(ctx, "", append([]string{"list", "set"}, strings.Fields(set)...)...); err != nil {
		return nil //nolint:nilerr // no set, so not a private host
	}
	return n.nft(ctx, setScript(set, ips), "-f", "-")
}

// fwRestoredNotice is how long a reload of the firewall stays on the host row.
const fwRestoredNotice = 24 * time.Hour

// CheckFirewall is the agent's self-check. On a private host the firewall must
// be loaded with both chains dropping by default; if not, it loads it again and
// fills its sets. It returns the notes to show on the host row.
func (n *Net) CheckFirewall(ctx context.Context, now time.Time) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	if d, err := n.dockerLocked(); err == nil {
		if nets, err := d.NetworkList(ctx, network.ListOptions{}); err == nil {
			for _, nw := range nets {
				if nw.EnableIPv6 {
					out = append(out, fmt.Sprintf("Docker network %s has IPv6 turned on. 5050cloud isolates containers over IPv4 only, so turn IPv6 off in Docker.", nw.Name))
				}
			}
		}
	}
	if _, err := os.Stat(n.privateMarker); err != nil {
		return out
	}
	if !n.firewallLoaded(ctx) {
		n.o.Logger.Error("the host firewall was not loaded, so it is being loaded again")
		if err := n.nft(ctx, "", "-f", n.firewallConf); err != nil {
			return append(out, "The host firewall is not loaded, and loading it again failed: "+err.Error()+". This host may be open to the internet. Check it now.")
		}
		_ = n.syncPeerSet(ctx, n.lastPeers)
		_ = n.syncSet(ctx, metadataSet, n.lastMetadata)
		if !n.firewallLoaded(ctx) {
			return append(out, "The host firewall is not loaded even after loading it again. This host may be open to the internet. Check it now.")
		}
		n.fwRestored = now
	}
	if !n.fwRestored.IsZero() && now.Sub(n.fwRestored) < fwRestoredNotice {
		out = append(out, "The host firewall was found missing at "+n.fwRestored.UTC().Format("2006-01-02 15:04")+" UTC and was loaded again. See the boot log: journalctl -u 5050-firewall")
	}
	return out
}

func (n *Net) firewallLoaded(ctx context.Context) bool {
	for _, chain := range []string{"input", "forward"} {
		out, err := n.nftOut(ctx, "list", "chain", "inet", "fifty_filter", chain)
		if err != nil || !strings.Contains(out, "policy drop") {
			return false
		}
	}
	return true
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

func execNftOut(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nft", args...).Output() // #nosec G204 -- fixed program, no user input
	return string(out), err
}
