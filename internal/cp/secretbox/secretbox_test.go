// SPDX-License-Identifier: AGPL-3.0-only

package secretbox

import (
	"errors"
	"testing"
)

func TestRoundTripAndBinding(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(k)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := b.Seal([]byte("s3cret\nline2"), "variables|env_a|svc_b|KEY")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := b.Open(ct, "variables|env_a|svc_b|KEY")
	if err != nil || string(pt) != "s3cret\nline2" {
		t.Fatalf("open: %q %v", pt, err)
	}
	if _, err := b.Open(ct, "variables|env_a|svc_b|OTHER"); !errors.Is(err, ErrOpen) {
		t.Fatalf("aad not bound: %v", err)
	}
	ct[len(ct)-1] ^= 1
	if _, err := b.Open(ct, "variables|env_a|svc_b|KEY"); !errors.Is(err, ErrOpen) {
		t.Fatalf("tamper not detected: %v", err)
	}
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}
