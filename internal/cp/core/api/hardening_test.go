// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/pkg/api"
)

// postFrom posts JSON with the headers a request from that address and page has.
func postFrom(t *testing.T, e *env, path, ip, origin string, in any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", e.srv.URL+path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip) // httptest connects over loopback, so this is trusted
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp
}

func login(t *testing.T, e *env, ip, email, password string) int {
	t.Helper()
	return postFrom(t, e, "/api/v1/auth/login", ip, "", api.LoginRequest{Email: email, Password: password}).StatusCode
}

// Every spelling of one address is one account, so one limiter bucket.
func TestLoginLimiterCountsEverySpellingOfAnEmailAsOne(t *testing.T) {
	e := newEnv(t)
	secOwner(t, e)
	variants := []string{"owner@example.com", "owner@example.com ", "\towner@example.com", "OWNER@example.com", " Owner@Example.COM\n"}
	for i := 0; i < loginPerAddressAccount; i++ {
		if got := login(t, e, "198.51.100.1", variants[i%len(variants)], "wrong password here"); got != 401 {
			t.Fatalf("attempt %d: status %d, want 401", i, got)
		}
	}
	for _, v := range variants {
		if got := login(t, e, "198.51.100.1", v, "wrong password here"); got != 429 {
			t.Errorf("%q: status %d after the limit, want 429", v, got)
		}
	}
}

func TestLoginLimiterHoldsAcrossAddresses(t *testing.T) {
	e := newEnv(t)
	secOwner(t, e)
	for i := 0; i < loginPerAccount; i++ {
		if got := login(t, e, fmt.Sprintf("198.51.100.%d", i+1), "owner@example.com", "wrong password here"); got != 401 {
			t.Fatalf("attempt %d: status %d", i, got)
		}
	}
	if got := login(t, e, "203.0.113.9", "owner@example.com", "wrong password here"); got != 429 {
		t.Fatalf("a fresh address got %d, want 429: the account count must not depend on the address", got)
	}
	if got := login(t, e, "203.0.113.9", "someone-else@example.com", "wrong password here"); got != 401 {
		t.Fatalf("another account got %d, want 401", got)
	}
}

func TestLoginLimiterCountsAnIPv6Slash64AsOne(t *testing.T) {
	e := newEnv(t)
	secOwner(t, e)
	for i := 0; i < loginPerAddressAccount; i++ {
		if got := login(t, e, fmt.Sprintf("2001:db8:1:2::%x", i+1), "owner@example.com", "wrong password here"); got != 401 {
			t.Fatalf("attempt %d: status %d", i, got)
		}
	}
	if got := login(t, e, "2001:db8:1:2:ffff::1", "owner@example.com", "wrong password here"); got != 429 {
		t.Fatalf("same /64: status %d, want 429", got)
	}
	if got := login(t, e, "2001:db8:1:3::1", "owner@example.com", "wrong password here"); got != 401 {
		t.Fatalf("another /64: status %d, want 401", got)
	}
}

func TestFailedLoginIsAudited(t *testing.T) {
	e := newEnv(t)
	owner, _, _ := secOwner(t, e)
	login(t, e, "198.51.100.7", " Owner@example.com", "wrong password here")
	var out api.Page[api.AuditEntry]
	owner.do("GET", "/api/v1/audit", nil, &out, 200)
	found := false
	for _, a := range out.Items {
		found = found || (a.Action == "auth.login_failed" && a.Target == "owner@example.com" && a.IP == "198.51.100.7")
	}
	if !found {
		t.Fatalf("no entry for the failed login: %+v", out.Items)
	}
}

// A bad token must be refused before the 64 MiB hash, so it answers far faster
// than a hash takes.
func TestBadTokensAreRefusedBeforeHashing(t *testing.T) {
	e := newEnv(t)
	secOwner(t, e)
	start := time.Now()
	if _, err := auth.HashPassword("a long enough password"); err != nil {
		t.Fatal(err)
	}
	oneHash := time.Since(start)

	for path, in := range map[string]any{
		"/api/v1/auth/reset":     api.ResetPasswordRequest{Token: "not-a-real-token", Password: "a long enough password"},
		"/api/v1/invites/accept": api.AcceptInviteRequest{Token: "not-a-real-token", Email: "a@example.com", Name: "A", Password: "a long enough password"},
	} {
		start := time.Now()
		if got := postFrom(t, e, path, "", "", in).StatusCode; got != 401 {
			t.Errorf("%s: status %d, want 401", path, got)
		}
		if took := time.Since(start); took > oneHash/2 {
			t.Errorf("%s took %v and a hash takes %v, so it looks like it hashed", path, took, oneHash)
		}
	}
}

func TestPublicBodiesAreSmall(t *testing.T) {
	e := newEnv(t)
	secOwner(t, e)
	huge := strings.Repeat("x", maxPublicBody+1)
	if got := login(t, e, "", "a@example.com", huge); got != 422 {
		t.Fatalf("an oversized body: status %d, want 422", got)
	}
}

func TestPublicPostsRefuseAnotherOrigin(t *testing.T) {
	e := newEnv(t)
	secOwner(t, e)
	body := api.LoginRequest{Email: "owner@example.com", Password: "wrong password here"}
	for _, origin := range []string{"https://evil.example", "null"} {
		if got := postFrom(t, e, "/api/v1/auth/login", "", origin, body).StatusCode; got != 403 {
			t.Errorf("Origin %s: status %d, want 403", origin, got)
		}
	}
	if got := postFrom(t, e, "/api/v1/auth/login", "", e.srv.URL, body).StatusCode; got != 401 {
		t.Errorf("own origin: status %d, want 401", got)
	}
}

func TestConsoleHeaders(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/api/v1/setup")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers: %v", resp.Header)
	}
}

func TestSessionCookieUsesTheHostPrefixOnHTTPS(t *testing.T) {
	e := newEnv(t)
	tok, err := e.c.CreateSetupToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e.c.File().Insecure = false
	b, _ := json.Marshal(api.SetupRequest{Token: tok, Email: "owner@example.com", Name: "Owner", Password: "correct horse battery"})
	resp, err := http.Post(e.srv.URL+"/api/v1/setup", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	var ck *http.Cookie
	for _, c := range resp.Cookies() {
		ck = c
	}
	if ck == nil || ck.Name != "__Host-5050_session" || !ck.Secure || ck.Path != "/" || ck.Domain != "" {
		t.Fatalf("cookie: %+v", ck)
	}
	// The name without the prefix must no longer count.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: ck.Value})
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = r2.Body.Close()
	if r2.StatusCode != 401 {
		t.Fatalf("the old cookie name got %d, want 401", r2.StatusCode)
	}
}

// A TOTP code and a ticket each work once.
func TestTOTPCodeAndTicketAreSingleUse(t *testing.T) {
	e := newEnv(t)
	owner, _, _ := secOwner(t, e)
	var ts api.TOTPSetupResponse
	owner.do("POST", "/api/v1/me/2fa/setup", nil, &ts, 200)
	owner.do("POST", "/api/v1/me/2fa/enable", api.TOTPEnableRequest{Code: auth.TOTPCode(ts.Secret, time.Now())}, &api.TOTPEnableResponse{}, 200)

	first, second := e.client(), e.client()
	var a, b api.LoginResponse
	first.do("POST", "/api/v1/auth/login", api.LoginRequest{Email: "owner@example.com", Password: "correct horse battery"}, &a, 200)
	second.do("POST", "/api/v1/auth/login", api.LoginRequest{Email: "owner@example.com", Password: "correct horse battery"}, &b, 200)
	code := auth.TOTPCode(ts.Secret, time.Now())
	first.do("POST", "/api/v1/auth/login/totp", api.TOTPLoginRequest{Ticket: a.Ticket, Code: code}, nil, 200)
	second.do("POST", "/api/v1/auth/login/totp", api.TOTPLoginRequest{Ticket: b.Ticket, Code: code}, nil, 401)
	first.do("POST", "/api/v1/auth/login/totp", api.TOTPLoginRequest{Ticket: a.Ticket, Code: code}, nil, 401)
}
