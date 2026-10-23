// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	_ "modernc.org/sqlite"

	"github.com/musakkka/5050cloud/internal/cp/orch/fakeconfig"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// TestJoinAndConnect runs the real orchestrator with fakeconfig and one
// in-memory agent speaking pkg/proto over a real WebSocket: join, hello,
// hello_ack, first desired state, and the host shows online.
func TestJoinAndConnect(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	defer db.Close()
	key, _ := secretbox.GenerateKey()
	box, _ := secretbox.New(key)
	cfg := fakeconfig.New()
	o, err := New(Options{DB: db, Config: cfg, DataDir: dir, Box: box, PublicURL: "http://127.0.0.1", LocalURL: "http://127.0.0.1", ServerVersion: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- o.Start(ctx) }()
	defer func() { cancel(); <-done }()

	srv := httptest.NewServer(http.StripPrefix("/api/agent/v1", o.AgentHandler()))
	defer srv.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if ok, _ := o.Ready(ctx); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hub never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	jt, err := o.CreateJoinToken(ctx, api.CreateJoinTokenRequest{Roles: []string{"app", "build", "ingress"}, Name: "one"})
	if err != nil {
		t.Fatal(err)
	}
	facts := proto.Facts{Version: "0.1.0", Arch: "amd64", OS: "ubuntu 24.04", CPUs: 2, MemBytes: 4 << 30, PublicIP: "203.0.113.7", WireGuard: true}
	body, _ := json.Marshal(proto.JoinRequest{Token: jt.Token, Hostname: "one", Facts: facts, MachineID: "m1", WGPublicKey: "wgkey1"})
	resp, err := http.Post(srv.URL+"/api/agent/v1/join", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var jr proto.JoinResponse
	if resp.StatusCode != http.StatusOK {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		t.Fatalf("join: %d %s", resp.StatusCode, b.String())
	}
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		t.Fatal(err)
	}
	if jr.HostID == "" || jr.HostToken == "" {
		t.Fatalf("join response incomplete: %+v", jr)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agent/v1/connect"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + jr.HostToken}}}) //nolint:bodyclose // the websocket library owns the response body
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(proto.MaxMessageBytes)
	hello, _ := json.Marshal(proto.Hello{HostID: jr.HostID, MachineID: "m1", WGPublicKey: "wgkey1", Facts: facts})
	env, _ := json.Marshal(proto.Envelope{T: proto.THello, ID: "h1", D: hello})
	if err := c.Write(ctx, websocket.MessageText, env); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
	defer rcancel()
	for !seen[proto.THelloAck] || !seen[proto.TDesired] {
		typ, b, err := c.Read(rctx)
		if err != nil {
			t.Fatalf("read: %v (seen %v)", err, seen)
		}
		if typ != websocket.MessageText {
			continue
		}
		var e proto.Envelope
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatal(err)
		}
		seen[e.T] = true
		if e.T == proto.TDesired {
			var ds proto.DesiredState
			if err := json.Unmarshal(e.D, &ds); err != nil {
				t.Fatal(err)
			}
			ap, _ := json.Marshal(proto.StateApplied{Generation: ds.Generation})
			out, _ := json.Marshal(proto.Envelope{T: proto.TStateApplied, Ref: e.ID, D: ap})
			_ = c.Write(ctx, websocket.MessageText, out)
		}
	}
	hosts, err := o.Hosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].ID != jr.HostID || hosts[0].State != "online" {
		t.Fatalf("hosts = %+v", hosts)
	}
	if _, n := o.Ready(ctx); n != 1 {
		t.Fatalf("connected agents = %d, want 1", n)
	}
}
