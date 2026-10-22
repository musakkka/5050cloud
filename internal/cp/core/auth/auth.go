// SPDX-License-Identifier: AGPL-3.0-only

// Package auth holds the credential primitives of docs/SPEC.md 9.15:
// argon2id password hashes, random tokens stored as SHA-256, API token and
// CLI code formats, TOTP with recovery codes, a sliding-window rate
// limiter, and the client IP rule for requests that came through Caddy.
// Persistence lives in core; this package has no database.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
)

// Password policy and argon2id parameters (m=64 MiB, t=3, p=2).
const (
	MinPasswordLen = 10
	MaxPasswordLen = 1024
	argonTime      = 3
	argonMemory    = 64 * 1024
	argonThreads   = 2
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// CheckPasswordPolicy returns a validation error for a weak password.
func CheckPasswordPolicy(field, pw string) error {
	if len(pw) < MinPasswordLen {
		return fail.Validation(field, "The password must be at least %d characters long.", MinPasswordLen)
	}
	if len(pw) > MaxPasswordLen {
		return fail.Validation(field, "The password must be at most %d characters long.", MaxPasswordLen)
	}
	return nil
}

// HashPassword returns a PHC-style argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword reports whether pw matches hash, in constant time.
func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || m > 1<<22 || t == 0 || t > 16 || p == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// DummyCheck spends the same time as CheckPassword, for unknown emails.
func DummyCheck(pw string) {
	dummyOnce.Do(func() { dummyHash, _ = HashPassword("not-a-real-password") })
	CheckPassword(dummyHash, pw)
}

// NewToken returns n random bytes, base64url without padding.
func NewToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("auth: the system random source failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is the stored form of a token: hex SHA-256.
func HashToken(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Equal compares two secrets in constant time.
func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// APITokenPrefix starts every API token.
const APITokenPrefix = "5050_"

// NewAPIToken returns "5050_" + 40 random characters and its display prefix.
func NewAPIToken() (secret, prefix string) {
	secret = APITokenPrefix + ids.Random(40, ids.Alnum)
	return secret, secret[:8]
}

// LooksLikeAPIToken reports whether s has the API token shape.
func LooksLikeAPIToken(s string) bool {
	return strings.HasPrefix(s, APITokenPrefix) && len(s) == len(APITokenPrefix)+40
}

// NewCLICode returns a code people compare by eye, e.g. "WXYZ-1234".
func NewCLICode() string {
	return ids.Random(4, "ABCDEFGHJKLMNPQRSTUVWXYZ") + "-" + ids.Random(4, "23456789")
}

// NewSetupToken returns a setup token people paste, e.g. "Q2X7-KL9P-...".
func NewSetupToken() string {
	var parts []string
	for i := 0; i < 6; i++ {
		parts = append(parts, ids.Random(4, ids.CodeChars))
	}
	return strings.Join(parts, "-")
}

// NormalizeCode upper-cases and trims a typed code.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ---------------------------------------------------------------- TOTP

// NewTOTP returns a new secret and its otpauth:// URL for a QR code.
func NewTOTP(issuer, account string) (secret, url string, err error) {
	k, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: account, Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return "", "", err
	}
	return k.Secret(), k.URL(), nil
}

// ValidateTOTP checks a 6-digit code at now, allowing one step of skew.
func ValidateTOTP(secret, code string, now time.Time) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return false
	}
	ok, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	return err == nil && ok
}

// TOTPCode returns the current code (tests and the dev server).
func TOTPCode(secret string, now time.Time) string {
	c, _ := totp.GenerateCode(secret, now)
	return c
}

// RecoveryCodeCount is how many recovery codes are issued.
const RecoveryCodeCount = 10

// NewRecoveryCodes returns 10 codes (shown once) and their hashes (stored).
func NewRecoveryCodes() (codes, hashes []string) {
	for i := 0; i < RecoveryCodeCount; i++ {
		c := strings.ToLower(ids.Random(5, ids.CodeChars) + "-" + ids.Random(5, ids.CodeChars))
		codes = append(codes, c)
		hashes = append(hashes, HashToken(c))
	}
	return codes, hashes
}

// UseRecoveryCode consumes code from hashes; it returns the remaining hashes.
func UseRecoveryCode(hashes []string, code string) ([]string, bool) {
	h := HashToken(strings.ToLower(strings.TrimSpace(code)))
	for i, x := range hashes {
		if Equal(x, h) {
			rest := append(append([]string(nil), hashes[:i]...), hashes[i+1:]...)
			return rest, true
		}
	}
	return hashes, false
}

// ---------------------------------------------------------------- rate limits

// Limiter allows at most Max events per key within Window.
type Limiter struct {
	Max    int
	Window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
	now    func() time.Time
	calls  int
}

// NewLimiter returns a limiter of max events per window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, hits: map[string][]time.Time{}, now: time.Now}
}

// Allow records an event for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cut := now.Add(-l.Window)
	h := l.hits[key]
	i := 0
	for i < len(h) && h[i].Before(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= l.Max {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	l.calls++
	if l.calls%1000 == 0 {
		l.sweep(cut)
	}
	return true
}

// Reset forgets key (after a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

func (l *Limiter) sweep(cut time.Time) {
	for k, h := range l.hits {
		if len(h) == 0 || h[len(h)-1].Before(cut) {
			delete(l.hits, k)
		}
	}
}

// ---------------------------------------------------------------- client IP

// ClientIP returns the client address. X-Forwarded-For is trusted only
// when the request came from loopback (Caddy on the same host); Caddy
// itself trusts forwarded headers only from the configured trusted
// proxies, so its first entry is the client.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if p := net.ParseIP(first); p != nil {
				return p.String()
			}
		}
	}
	if ip == nil {
		return host
	}
	return ip.String()
}

// IsLocal reports whether the request came straight from loopback, with no
// forwarding proxy in between (used for /readyz details).
func IsLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-For") == ""
}
