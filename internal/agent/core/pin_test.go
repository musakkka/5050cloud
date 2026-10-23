// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"testing"
	"time"
)

type pinCert struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newPinCert(t *testing.T, cn string, ca bool, parent *pinCert, dns string) *pinCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: ca,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ca {
		tpl.KeyUsage |= x509.KeyUsageCertSign
	}
	if dns != "" {
		tpl.DNSNames = []string{dns}
	}
	signer, signKey := tpl, key
	if parent != nil {
		signer, signKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, signer, &key.PublicKey, signKey)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &pinCert{cert: c, key: key}
}

func pinOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

func TestPinRejectsForgedLeafWithThePublicCA(t *testing.T) {
	ca := newPinCert(t, "real ca", true, nil, "")
	good := newPinCert(t, "console", false, ca, "console.test")
	evilCA := newPinCert(t, "evil ca", true, nil, "")
	forged := newPinCert(t, "console", false, evilCA, "console.test")
	want := pinOf(ca.cert)

	// The real chain verifies against the pinned CA.
	err := verifyPinned(tls.ConnectionState{PeerCertificates: []*x509.Certificate{good.cert, ca.cert}, ServerName: "console.test"}, want)
	if err != nil {
		t.Fatalf("the real chain was refused: %v", err)
	}
	// A forged leaf that only travels with the real CA certificate is refused.
	err = verifyPinned(tls.ConnectionState{PeerCertificates: []*x509.Certificate{forged.cert, ca.cert}, ServerName: "console.test"}, want)
	if err == nil {
		t.Fatal("a forged leaf next to the pinned CA certificate was accepted")
	}
	// The wrong host name is refused too.
	err = verifyPinned(tls.ConnectionState{PeerCertificates: []*x509.Certificate{good.cert, ca.cert}, ServerName: "other.test"}, want)
	if err == nil {
		t.Fatal("a leaf for another host name was accepted")
	}
	// Pinning the leaf itself still works, and nothing else matches.
	if err := verifyPinned(tls.ConnectionState{PeerCertificates: []*x509.Certificate{good.cert}}, pinOf(good.cert)); err != nil {
		t.Fatalf("the pinned leaf was refused: %v", err)
	}
	if err := verifyPinned(tls.ConnectionState{PeerCertificates: []*x509.Certificate{forged.cert}}, pinOf(good.cert)); err == nil {
		t.Fatal("an unpinned leaf was accepted")
	}
}
