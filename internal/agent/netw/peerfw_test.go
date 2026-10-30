// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPeerSetScript(t *testing.T) {
	if got := setScript(peerSet, peerAddrs(nil)); got != "flush set inet fifty_filter wg_peers\n" {
		t.Errorf("a single host must leave the set empty, got %q", got)
	}
	got := setScript(peerSet, peerAddrs([]meshPeerPlan{
		{Endpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.9"), Port: 51820}},
		{Endpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.4"), Port: 51820}},
		{Endpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.4"), Port: 1}},
		{}, // a peer behind NAT has no endpoint
	}))
	want := "flush set inet fifty_filter wg_peers\nadd element inet fifty_filter wg_peers { 198.51.100.4, 198.51.100.9 }\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSyncPeerSetOnlyOnPrivateHost(t *testing.T) {
	n, _, _, _ := newTestNet(t)
	var calls []string
	exists := false
	n.nft = func(_ context.Context, stdin string, args ...string) error {
		calls = append(calls, strings.Join(args, " ")+"|"+stdin)
		if !exists {
			return errors.New("no such set")
		}
		return nil
	}
	if err := n.syncPeerSet(context.Background(), nil); err != nil || len(calls) != 1 {
		t.Fatalf("without the set: err=%v calls=%v", err, calls)
	}
	exists = true
	calls = nil
	if err := n.syncPeerSet(context.Background(), nil); err != nil || len(calls) != 2 || !strings.HasPrefix(calls[1], "-f -|flush set") {
		t.Fatalf("with the set: err=%v calls=%v", err, calls)
	}
}

// fakeFirewall stands in for the nft binary on a private host.
func fakeFirewall(t *testing.T, n *Net) (loaded *bool, calls *[]string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n.privateMarker = marker
	on := true
	var got []string
	n.nft = func(_ context.Context, stdin string, args ...string) error {
		got = append(got, strings.Join(args, " ")+"|"+stdin)
		if len(args) == 2 && args[0] == "-f" && args[1] != "-" {
			on = true
		}
		return nil
	}
	n.nftOut = func(context.Context, ...string) (string, error) {
		if !on {
			return "", errors.New("no such table")
		}
		return "chain { type filter hook input priority 0; policy drop; }", nil
	}
	return &on, &got
}

func TestCheckFirewallReloadsAndReportsAMissingTable(t *testing.T) {
	n, _, _, _ := newTestNet(t)
	loaded, calls := fakeFirewall(t, n)
	n.lastMetadata = []netip.Addr{netip.MustParseAddr("10.210.0.5")}
	now := time.Now()

	if w := n.CheckFirewall(context.Background(), now); len(w) != 0 || len(*calls) != 0 {
		t.Fatalf("a loaded firewall: warnings=%v calls=%v", w, *calls)
	}

	*loaded = false
	w := n.CheckFirewall(context.Background(), now)
	if !*loaded || len(w) != 1 || !strings.Contains(w[0], "loaded again") {
		t.Fatalf("a missing firewall: loaded=%v warnings=%v", *loaded, w)
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "add element "+metadataSet+" { 10.210.0.5 }") {
		t.Fatalf("the metadata set was not filled again: %v", *calls)
	}

	if w := n.CheckFirewall(context.Background(), now.Add(time.Hour)); len(w) != 1 {
		t.Fatalf("the note must stay on the host row: %v", w)
	}
	if w := n.CheckFirewall(context.Background(), now.Add(fwRestoredNotice+time.Hour)); len(w) != 0 {
		t.Fatalf("the note must clear after a day: %v", w)
	}
}

func TestCheckFirewallWrongPolicyCountsAsMissing(t *testing.T) {
	n, _, _, _ := newTestNet(t)
	fakeFirewall(t, n)
	reloaded := false
	n.nft = func(_ context.Context, _ string, args ...string) error {
		reloaded = reloaded || (len(args) == 2 && args[0] == "-f" && args[1] != "-")
		return nil
	}
	n.nftOut = func(context.Context, ...string) (string, error) { return "policy accept;", nil }
	w := n.CheckFirewall(context.Background(), time.Now())
	if !reloaded || len(w) != 1 || !strings.Contains(w[0], "even after") {
		t.Fatalf("reloaded=%v warnings=%v", reloaded, w)
	}
}

func TestCheckFirewallLeavesANormalHostAlone(t *testing.T) {
	n, _, _, _ := newTestNet(t)
	n.privateMarker = filepath.Join(t.TempDir(), "absent")
	n.nft = func(context.Context, string, ...string) error {
		t.Fatal("nft must not run on a host that is not private")
		return nil
	}
	if w := n.CheckFirewall(context.Background(), time.Now()); len(w) != 0 {
		t.Fatalf("warnings=%v", w)
	}
}

func TestRestoreBaselineDropsMetadataOnceAndYieldsToTheAgent(t *testing.T) {
	n, ipt, _, _ := newTestNet(t)
	if err := n.restoreBaseline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ipt.restores) != 1 {
		t.Fatalf("restores=%d", len(ipt.restores))
	}
	r := ipt.restores[0]
	for _, want := range []string{":DOCKER-USER - [0:0]", ":5050-ISOLATE - [0:0]", "-A 5050-ISOLATE -d " + MetadataAddr + "/32 -j DROP", "-I DOCKER-USER 1 -j 5050-ISOLATE"} {
		if !strings.Contains(r, want) {
			t.Errorf("no %q in:\n%s", want, r)
		}
	}
	// Run again with the chain there: it must not flush what the cache restored.
	if err := n.restoreBaseline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ipt.restores) != 1 {
		t.Fatalf("a second run changed the rules: %d", len(ipt.restores))
	}
}
