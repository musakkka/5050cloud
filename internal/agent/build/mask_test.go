// SPDX-License-Identifier: AGPL-3.0-only

package build

import "testing"

func TestMasker(t *testing.T) {
	m := newMasker("s3cr3t-token", "s3cr3t", "on", "true", "", "-----BEGIN KEY-----\nAAAABBBBCCCC\n-----END KEY-----")
	cases := map[string]string{
		"token is s3cr3t-token here": "token is ***** here",
		"plain s3cr3t":               "plain *****",
		"flag on and true stay":      "flag on and true stay",
		"line AAAABBBBCCCC":          "line *****",
		"nothing to hide":            "nothing to hide",
	}
	for in, want := range cases {
		if got := m.Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
	var empty *masker
	if empty.Mask("x") != "x" || newMasker().Mask("y") != "y" {
		t.Errorf("an empty masker must return the input")
	}
}
