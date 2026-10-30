// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestPeerSetScript(t *testing.T) {
	if got := peerSetScript(nil); got != "flush set inet fifty_filter wg_peers\n" {
		t.Errorf("a single host must leave the set empty, got %q", got)
	}
	got := peerSetScript([]meshPeerPlan{
		{Endpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.9"), Port: 51820}},
		{Endpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.4"), Port: 51820}},
		{Endpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.4"), Port: 1}},
		{}, // a peer behind NAT has no endpoint
	})
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
