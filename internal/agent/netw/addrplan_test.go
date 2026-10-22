// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"net/netip"
	"strings"
	"testing"
)

func TestAddressPlanDefaults(t *testing.T) {
	p, err := NewAddressPlan("", "")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		got  func() (netip.Prefix, error)
		want string
	}{
		{"host 1", func() (netip.Prefix, error) { return p.HostSubnet(1) }, "10.129.0.0/16"},
		{"host 127", func() (netip.Prefix, error) { return p.HostSubnet(127) }, "10.255.0.0/16"},
		{"env 4 on host 1", func() (netip.Prefix, error) { return p.EnvSubnet(1, 4) }, "10.129.4.0/24"},
		{"env 254 on host 2", func() (netip.Prefix, error) { return p.EnvSubnet(2, 254) }, "10.130.254.0/24"},
		{"mesh 1", func() (netip.Prefix, error) { return p.MeshAddress(1) }, "100.96.0.1/32"},
		{"mesh 3", func() (netip.Prefix, error) { return p.MeshAddress(3) }, "100.96.0.3/32"},
	}
	for _, c := range cases {
		got, err := c.got()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.String() != c.want {
			t.Errorf("%s = %s, want %s", c.name, got, c.want)
		}
	}
	if got := p.BuildSubnet().String(); got != "10.128.255.0/24" {
		t.Errorf("build subnet = %s", got)
	}
	if got := Gateway(netip.MustParsePrefix("10.129.4.0/24")).String(); got != "10.129.4.1" {
		t.Errorf("gateway = %s", got)
	}
}

func TestAddressPlanCustomAndLimits(t *testing.T) {
	p, err := NewAddressPlan("172.0.0.0/8", "100.64.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := p.EnvSubnet(3, 9); s.String() != "172.3.9.0/24" {
		t.Errorf("custom env subnet = %s", s)
	}
	for _, k := range []int{0, 128, -1} {
		if _, err := p.HostSubnet(k); err == nil {
			t.Errorf("host %d accepted", k)
		}
		if _, err := p.MeshAddress(k); err == nil {
			t.Errorf("mesh %d accepted", k)
		}
	}
	for _, s := range []int{0, 255, 300} {
		if _, err := p.EnvSubnet(1, s); err == nil {
			t.Errorf("slot %d accepted", s)
		}
	}
	if _, err := p.EnvSubnet(0, 1); err == nil {
		t.Error("host 0 accepted for an environment")
	}
}

func TestAddressPlanRejectsBadRanges(t *testing.T) {
	for _, c := range [][2]string{
		{"10.128.0.0/10", ""},         // too small for 127 hosts
		{"fd00::/8", ""},              // IPv6
		{"nonsense", ""},              // not a CIDR
		{"", "100.96.0.0/25"},         // mesh too small
		{"", "bad"},                   // not a CIDR
		{"", "fd00::/64"},             // IPv6 mesh
		{"10.0.0.0/8", "10.1.0.0/16"}, // overlap
	} {
		if _, err := NewAddressPlan(c[0], c[1]); err == nil {
			t.Errorf("NewAddressPlan(%q, %q) accepted", c[0], c[1])
		}
	}
}

func TestSlotExhaustedSentence(t *testing.T) {
	got := SlotExhaustedError("worker-2").Error()
	want := "No network slot left on host worker-2: it already runs 254 environments."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "\u2014") {
		t.Error("em dash in user-facing copy")
	}
}
