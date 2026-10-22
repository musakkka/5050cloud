// SPDX-License-Identifier: AGPL-3.0-only

package ipam

import "testing"

func TestPlanArithmetic(t *testing.T) {
	p := MustDefault()
	if got := p.HostSubnet(1).String(); got != "10.129.0.0/16" {
		t.Fatalf("host subnet 1 = %s", got)
	}
	if got := p.SlotSubnet(1, 3).String(); got != "10.129.3.0/24" {
		t.Fatalf("slot subnet = %s", got)
	}
	a := p.Addr(2, 5, 7)
	k, n, h, ok := p.Locate(a)
	if !ok || k != 2 || n != 5 || h != 7 {
		t.Fatalf("locate %s = %d %d %d %v", a, k, n, h, ok)
	}
	if _, err := NewPlan("10.0.0.0/24", ""); err == nil {
		t.Fatal("a pod range too small must be refused")
	}
}
