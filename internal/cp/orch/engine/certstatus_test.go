// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/internal/cp/orch/fakeconfig"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// The ACME primary's cert_status reports reach DomainCertStatus: an error
// shows with its sentence, an issued certificate clears it, and forgetting a
// domain drops the certificate.
func TestCertStatusErrorsAreKeptAndCleared(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	defer db.Close()
	key, _ := secretbox.GenerateKey()
	box, _ := secretbox.New(key)
	o, err := New(Options{DB: db, Config: fakeconfig.New(), DataDir: dir, Box: box, PublicURL: "http://127.0.0.1", LocalURL: "http://127.0.0.1", ServerVersion: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	status := func(domain string) (string, int64, string) {
		t.Helper()
		st, na, msg, err := o.DomainCertStatus(ctx, domain)
		if err != nil {
			t.Fatal(err)
		}
		return st, na, msg
	}
	const d = "shop.example.com"
	if st, _, _ := status(d); st != "issuing" {
		t.Fatalf("before any report: %q", st)
	}
	o.storeCertStatus(ctx, proto.CertStatus{Domains: map[string]proto.CertDomainState{d: {Status: "error", Error: "The certificate for shop.example.com could not be issued yet: boom"}}})
	if st, _, msg := status(d); st != "error" || !strings.Contains(msg, "boom") {
		t.Fatalf("after an error report: %q %q", st, msg)
	}
	if err := o.storeCert(ctx, proto.CertBundle{Domain: d, CertPEM: "c", KeyPEM: "k", NotAfter: 4102444800000}); err != nil {
		t.Fatal(err)
	}
	if st, na, msg := status(d); st != "active" || na != 4102444800000 || msg != "" {
		t.Fatalf("after the certificate: %q %d %q", st, na, msg)
	}
	if err := o.ForgetCertificate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := status(d); st != "issuing" {
		t.Fatalf("after forgetting: %q", st)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM o_certificates`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("certificate rows left: %d %v", n, err)
	}
}
