// SPDX-License-Identifier: AGPL-3.0-only

package routes

import "testing"

func TestTLSModeAndSlug(t *testing.T) {
	if TLSMode("app.example.com") != "acme" || TLSMode("db.internal") != "internal" || TLSMode("localhost") != "internal" {
		t.Fatal("TLS mode mismatch")
	}
	if got := Slug("My API_Server"); got != "my-api-server" {
		t.Fatalf("slug = %q", got)
	}
	if out := Compute(nil); len(out.Routes) != 0 {
		t.Fatalf("no services must give no routes, got %d", len(out.Routes))
	}
}
