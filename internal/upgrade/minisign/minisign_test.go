// SPDX-License-Identifier: AGPL-3.0-only

package minisign

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The vectors in testdata were made with minisign 0.11 (Ubuntu 24.04).
func TestVerifiesRealMinisignSignatures(t *testing.T) {
	pk, err := ParsePublicKey(string(read(t, "minisign-0.11-test-only.pub")))
	if err != nil {
		t.Fatal(err)
	}
	if pk.ID.String() != "D1559235E3A2A7B8" {
		t.Fatalf("key id %s", pk.ID)
	}
	msg := read(t, "message.txt")
	for _, f := range []string{"message.txt.ed.minisig", "message.txt.legacy.minisig"} {
		_, sig, err := Verify([]PublicKey{pk}, msg, read(t, f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if f == "message.txt.ed.minisig" && (sig.Algorithm != "ED" || sig.TrustedComment != "timestamp:0 file:message.txt") {
			t.Fatalf("%s: %+v", f, sig)
		}
		if f == "message.txt.legacy.minisig" && sig.Algorithm != "Ed" {
			t.Fatalf("legacy algorithm %q", sig.Algorithm)
		}
		if _, _, err := Verify([]PublicKey{pk}, append(bytes.Clone(msg), 'x'), read(t, f)); err == nil {
			t.Fatalf("%s: a changed message verified", f)
		}
	}
}

func TestReadsRealUnencryptedSecretKey(t *testing.T) {
	k, err := ParsePrivateKey(string(read(t, "minisign-0.11-test-only.key")))
	if err != nil {
		t.Fatal(err)
	}
	pk, _ := ParsePublicKey(string(read(t, "minisign-0.11-test-only.pub")))
	if k.Public().String() != pk.String() {
		t.Fatal("the secret key does not match its public key")
	}
	sig, err := Sign(k, []byte("abc"), "c")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Verify([]PublicKey{pk}, []byte("abc"), []byte(sig)); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripAndTamper(t *testing.T) {
	pub, priv, err := GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := GenerateKey(nil)
	pk2, err := ParsePublicKey(pub.File())
	if err != nil || pk2.String() != pub.String() {
		t.Fatalf("public key round trip: %v", err)
	}
	k2, err := ParsePrivateKey(priv.File())
	if err != nil || !bytes.Equal(k2.Key, priv.Key) || k2.ID != priv.ID {
		t.Fatalf("secret key round trip: %v", err)
	}
	msg := []byte("deadbeef  5050_0.1.0_linux_amd64.tar.gz\n")
	sig, err := Sign(priv, msg, "timestamp:1 version:v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	got, s, err := Verify([]PublicKey{other, pub}, msg, []byte(sig))
	if err != nil || got.ID != pub.ID || s.TrustedComment != "timestamp:1 version:v0.1.0" {
		t.Fatalf("verify: %v %+v", err, s)
	}
	if _, _, err := Verify([]PublicKey{other}, msg, []byte(sig)); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown key: %v", err)
	}
	tampered := strings.Replace(sig, "version:v0.1.0", "version:v9.9.9", 1)
	if _, _, err := Verify([]PublicKey{pub}, msg, []byte(tampered)); err == nil || !strings.Contains(err.Error(), "trusted comment") {
		t.Fatalf("tampered comment: %v", err)
	}
	if _, err := Sign(priv, msg, "two\nlines"); err == nil {
		t.Fatal("multi-line trusted comment accepted")
	}
	if pub.Fingerprint() == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{"", "!!!", "untrusted comment: x", "RWQ="}
	for _, b := range bad {
		if _, err := ParsePublicKey(b); err == nil {
			t.Errorf("public key %q accepted", b)
		}
		if _, err := ParsePrivateKey(b); err == nil {
			t.Errorf("secret key %q accepted", b)
		}
	}
	sigs := []string{
		"",
		"a\nb\nc\nd",
		"untrusted comment: x\n!!\ntrusted comment: y\nzz",
		"untrusted comment: x\n" + strings.Repeat("A", 100) + "\ntrusted comment: y\nzz",
		strings.Repeat("x", maxSigBytes+1),
	}
	for _, s := range sigs {
		if _, err := ParseSignature([]byte(s)); err == nil {
			t.Errorf("signature %q accepted", s)
		}
	}
	_, priv, _ := GenerateKey(nil)
	good, _ := Sign(priv, []byte("m"), "t")
	lines := strings.Split(good, "\n")
	noTrusted := strings.Join([]string{lines[0], lines[1], "comment: t", lines[3]}, "\n")
	if _, err := ParseSignature([]byte(noTrusted)); err == nil {
		t.Error("signature without trusted comment accepted")
	}
	badGlobal := strings.Join([]string{lines[0], lines[1], lines[2], "AAAA"}, "\n")
	if _, err := ParseSignature([]byte(badGlobal)); err == nil {
		t.Error("short global signature accepted")
	}
	// A damaged checksum is refused.
	f := priv.File()
	raw := strings.Split(strings.TrimSpace(f), "\n")[1]
	flipped := raw[:len(raw)-6] + "AAAAB="
	if _, err := ParsePrivateKey(flipped); err == nil {
		t.Error("damaged secret key accepted")
	}
}
