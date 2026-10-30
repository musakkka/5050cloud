// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/musakkka/5050cloud/internal/cp/orch/fakeconfig"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func envelope(t *testing.T, typ string, v any) proto.Envelope {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return proto.Envelope{T: typ, D: b}
}

// A host may only report on work it was given: a leaked build or job id must not
// let another host set an image digest, and only the ACME primary sends certs.
func TestHostMessagesAreCheckedAgainstTheSender(t *testing.T) {
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

	ch := make(chan buildOutcome, 1)
	o.mu.Lock()
	o.buildWait["dep_1"] = ch
	o.mu.Unlock()
	o.HostMessage(ctx, "host_evil", envelope(t, proto.TBuildResult, proto.BuildResult{BuildID: "dep_1"}))
	select {
	case <-ch:
		t.Error("a build result from another host was delivered")
	default:
	}

	w := o.jobs.add("job_1", "host_real", nil)
	defer o.jobs.remove("job_1")
	o.HostMessage(ctx, "host_evil", envelope(t, proto.TJobResult, proto.JobResult{JobID: "job_1"}))
	select {
	case <-w.done:
		t.Error("a job result from another host was delivered")
	default:
	}
	o.HostMessage(ctx, "host_real", envelope(t, proto.TJobResult, proto.JobResult{JobID: "job_1"}))
	select {
	case <-w.done:
	default:
		t.Error("the job's own host was not heard")
	}

	const d = "console.example.com"
	o.HostMessage(ctx, "host_evil", envelope(t, proto.TCertIssued, proto.CertIssued{CertBundle: proto.CertBundle{Domain: d, CertPEM: "c", KeyPEM: "k", NotAfter: 4102444800000}}))
	if st, _, _, err := o.DomainCertStatus(ctx, d); err != nil || st != "issuing" {
		t.Errorf("a certificate from a host that is not the primary was stored: %q %v", st, err)
	}
}
