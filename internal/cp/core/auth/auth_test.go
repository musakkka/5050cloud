// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPasswords(t *testing.T) {
	if err := CheckPasswordPolicy("password", "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := CheckPasswordPolicy("password", strings.Repeat("x", MaxPasswordLen+1)); err == nil {
		t.Fatal("huge password accepted")
	}
	if err := CheckPasswordPolicy("password", "correct horse"); err != nil {
		t.Fatal(err)
	}
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("hash = %s", h)
	}
	if !checkPassword(h, "correct horse") || checkPassword(h, "wrong horse") {
		t.Fatal("CheckPassword")
	}
	h2, _ := HashPassword("correct horse")
	if h == h2 {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$a$b", "$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=0,t=3,p=2$c2FsdA$aGFzaA", "$argon2id$v=19$m=65536,t=3,p=2$!!$aGFzaA", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!!",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$", "$argon2id$vx$m=65536,t=3,p=2$c2FsdA$aGFzaA", "$argon2id$v=19$mx$c2FsdA$aGFzaA"} {
		if checkPassword(bad, "x") {
			t.Fatalf("malformed hash %q accepted", bad)
		}
	}
	if err := DummyCheck("x"); err != nil {
		t.Fatal(err)
	}
}

func TestTokens(t *testing.T) {
	a, b := NewToken(32), NewToken(32)
	if a == b || len(a) != 43 {
		t.Fatalf("tokens %q %q", a, b)
	}
	if HashToken("x") != "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881" {
		t.Fatal("HashToken is not hex SHA-256")
	}
	if !Equal("a", "a") || Equal("a", "b") {
		t.Fatal("Equal")
	}
	s, p := NewAPIToken()
	if !regexp.MustCompile(`^5050_[A-Za-z0-9]{40}$`).MatchString(s) || p != s[:8] || !LooksLikeAPIToken(s) || LooksLikeAPIToken("5050_x") {
		t.Fatalf("api token %q %q", s, p)
	}
	if !regexp.MustCompile(`^[A-Z]{4}-[2-9]{4}$`).MatchString(NewCLICode()) {
		t.Fatal("cli code shape")
	}
	if !regexp.MustCompile(`^([A-Z2-9]{4}-){5}[A-Z2-9]{4}$`).MatchString(NewSetupToken()) {
		t.Fatal("setup token shape")
	}
	if NormalizeCode(" wxyz-1234 ") != "WXYZ-1234" {
		t.Fatal("NormalizeCode")
	}
}

func TestTOTP(t *testing.T) {
	secret, url, err := NewTOTP("5050cloud (test)", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "otpauth://totp/") || !strings.Contains(url, "secret="+secret) {
		t.Fatalf("url = %s", url)
	}
	now := time.Now()
	code := TOTPCode(secret, now)
	if !ValidateTOTP(secret, code, now) || !ValidateTOTP(secret, code[:3]+" "+code[3:], now) {
		t.Fatal("current code rejected")
	}
	if !ValidateTOTP(secret, TOTPCode(secret, now.Add(-30*time.Second)), now) {
		t.Fatal("one step of skew must pass")
	}
	if ValidateTOTP(secret, TOTPCode(secret, now.Add(-5*time.Minute)), now) || ValidateTOTP(secret, "12345", now) || ValidateTOTP("!!", "123456", now) {
		t.Fatal("bad codes accepted")
	}
	codes, hashes := NewRecoveryCodes()
	if len(codes) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatal("ten codes")
	}
	rest, ok := UseRecoveryCode(hashes, strings.ToUpper(codes[3]))
	if !ok || len(rest) != 9 {
		t.Fatal("recovery code not consumed")
	}
	if _, ok := UseRecoveryCode(rest, codes[3]); ok {
		t.Fatal("recovery code reused")
	}
	if len(hashes) != 10 {
		t.Fatal("input slice must not be modified")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatal("within limit refused")
		}
	}
	if l.Allow("ip") {
		t.Fatal("over limit allowed")
	}
	if !l.Allow("other") {
		t.Fatal("keys are independent")
	}
	now = now.Add(61 * time.Second)
	if !l.Allow("ip") {
		t.Fatal("window did not slide")
	}
	l.Reset("ip")
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatal("reset did not clear")
		}
	}
	// sweep runs every 1000 calls
	for i := 0; i < 1000; i++ {
		l.Allow("k" + string(rune(i)))
	}
	now = now.Add(time.Hour)
	for i := 0; i < 1000; i++ {
		l.Allow("j" + string(rune(i)))
	}
	l.mu.Lock()
	n := len(l.hits)
	l.mu.Unlock()
	if n > 1100 {
		t.Fatalf("sweep did not drop old keys: %d", n)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if ClientIP(r) != "203.0.113.9" || IsLocal(r) {
		t.Fatal("loopback with XFF")
	}
	r.Header.Set("X-Forwarded-For", "garbage")
	if ClientIP(r) != "127.0.0.1" {
		t.Fatal("bad XFF falls back")
	}
	r.Header.Del("X-Forwarded-For")
	if !IsLocal(r) {
		t.Fatal("plain loopback is local")
	}
	r.RemoteAddr = "198.51.100.4:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if ClientIP(r) != "198.51.100.4" || IsLocal(r) {
		t.Fatal("XFF from a non-loopback peer must be ignored")
	}
	r.RemoteAddr = "pipe"
	if ClientIP(r) != "pipe" || IsLocal(r) {
		t.Fatal("unparseable remote")
	}
	r.RemoteAddr = "[::1]:80"
	r.Header.Del("X-Forwarded-For")
	if !IsLocal(r) || ClientIP(r) != "::1" {
		t.Fatal("ipv6 loopback")
	}
}

func TestLimitKey(t *testing.T) {
	for in, want := range map[string]string{
		"198.51.100.7":              "198.51.100.7",
		"::ffff:198.51.100.7":       "::ffff:198.51.100.7",
		"2001:db8:1:2:3:4:5:6":      "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1%eth0": "2001:db8:1:2::/64",
		"not an address":            "not an address",
	} {
		if got := LimitKey(in); got != want {
			t.Errorf("LimitKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashingIsCappedAndRefusesWhenTheQueueIsFull(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if cap(hashSlots) > 4 {
		t.Fatalf("%d concurrent hashes is too much memory", cap(hashSlots))
	}
	for i := 0; i < cap(hashQueue); i++ {
		hashQueue <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(hashQueue); i++ {
			<-hashQueue
		}
	}()
	if _, err := VerifyPassword(h, "correct horse"); err != ErrBusy {
		t.Fatalf("VerifyPassword: %v, want ErrBusy", err)
	}
	if _, err := HashPassword("correct horse"); err != ErrBusy {
		t.Fatalf("HashPassword: %v, want ErrBusy", err)
	}
	if err := DummyCheck("x"); err != ErrBusy {
		t.Fatalf("DummyCheck: %v, want ErrBusy", err)
	}
}
