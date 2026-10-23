// SPDX-License-Identifier: AGPL-3.0-only

package cfranges

import "testing"

func TestContains(t *testing.T) {
	for ip, want := range map[string]bool{
		"104.16.0.1": true, "2606:4700::1": true, "203.0.113.7": false, "2001:db8::1": false, "not an ip": false,
	} {
		if got := Contains(ip); got != want {
			t.Errorf("Contains(%q) = %v, want %v", ip, got, want)
		}
	}
}
