// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// healthServer answers with the status in code, recording the request.
func healthServer(t *testing.T, code *atomic.Int32, seen chan<- *http.Request) (string, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			select {
			case seen <- r:
			default:
			}
		}
		if code.Load() == 302 {
			http.Redirect(w, r, "http://127.0.0.1:1/elsewhere", http.StatusFound)
			return
		}
		w.WriteHeader(int(code.Load()))
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	return host, p
}

func webReplica(ip string, port int, path string) proto.ReplicaSpec {
	s := worker("rep_w")
	s.IP, s.Port = ip, port
	s.Health = proto.HealthCheck{Path: path, Host: "api.example.com", IntervalSec: 1, TimeoutSec: 50, DeadlineSec: 40, GraceSec: 2}
	return s
}

func runChecker(t *testing.T, ta *testAgent, s proto.ReplicaSpec) (*replica, *collector, context.CancelFunc) {
	t.Helper()
	sess := ta.attach()
	c := collect(sess)
	r := ta.track(s)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go ta.checkHealth(ctx, r, engine.Container{ID: "cid", StartedAt: time.Now(), State: "running"})
	return r, c, cancel
}

func TestHealthHTTPPassSendsHeaders(t *testing.T) {
	ta := newTestAgent(t)
	var code atomic.Int32
	code.Store(204)
	seen := make(chan *http.Request, 8)
	ip, port := healthServer(t, &code, seen)
	_, c, _ := runChecker(t, ta, webReplica(ip, port, "/health"))
	waitFor(t, "healthy", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "healthy" })
	r := <-seen
	if r.Host != "api.example.com" || r.Header.Get("X-Forwarded-Proto") != "https" || r.Header.Get("User-Agent") != "5050cloud-healthcheck" || r.URL.Path != "/health" {
		t.Fatalf("probe request %s %v %s", r.Host, r.Header, r.URL.Path)
	}
}

func TestHealthRedirectPassesWithoutFollowing(t *testing.T) {
	ta := newTestAgent(t)
	var code atomic.Int32
	code.Store(302)
	ip, port := healthServer(t, &code, nil)
	_, c, _ := runChecker(t, ta, webReplica(ip, port, "health"))
	waitFor(t, "healthy", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "healthy" })
}

func TestHealthStatusFailureDiagnosis(t *testing.T) {
	ta := newTestAgent(t)
	var code atomic.Int32
	code.Store(404)
	ip, port := healthServer(t, &code, nil)
	_, c, _ := runChecker(t, ta, webReplica(ip, port, "/health"))
	waitFor(t, "unhealthy", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "unhealthy" })
	st, _ := c.lastStatus(t, "rep_w")
	if st.Diagnosis != "Health check /health returned 404. Check the path in Settings > Deploy." {
		t.Fatalf("diagnosis %q", st.Diagnosis)
	}
}

func TestHealthTCPAndSteadyState(t *testing.T) {
	ta := newTestAgent(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	r, c, _ := runChecker(t, ta, webReplica("127.0.0.1", addr.Port, ""))
	waitFor(t, "healthy", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "healthy" })
	// Three failures in a row after it was healthy report unhealthy, the next pass healthy again.
	ln.Close()
	waitFor(t, "unhealthy after 3 steady failures", func() bool { st, _ := c.lastStatus(t, "rep_w"); return st.State == "unhealthy" })
	st, _ := c.lastStatus(t, "rep_w")
	if !strings.Contains(st.Diagnosis, "Nothing answered on port") {
		t.Fatalf("diagnosis %q", st.Diagnosis)
	}
	ta.mu.Lock()
	if !r.localUnhealthy {
		t.Error("local view not updated")
	}
	ta.mu.Unlock()
	ln2, err := net.Listen("tcp", addr.String())
	if err != nil {
		t.Skip("port was taken again:", err)
	}
	defer ln2.Close()
	go func() {
		for {
			c, err := ln2.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	waitFor(t, "healthy again", func() bool { st, _ := c.lastStatus(t, "rep_w"); return st.State == "healthy" })
}

func TestHealthDeadlineTCP(t *testing.T) {
	ta := newTestAgent(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	_, c, _ := runChecker(t, ta, webReplica("127.0.0.1", port, ""))
	waitFor(t, "unhealthy at deadline", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "unhealthy" })
	st, _ := c.lastStatus(t, "rep_w")
	if st.Diagnosis != "Nothing answered on port "+strconv.Itoa(port)+" within 40 s. Make sure the app starts and listens on PORT." {
		t.Fatalf("diagnosis %q", st.Diagnosis)
	}
}

func TestHealthGraceRequiresSecondPass(t *testing.T) {
	ta := newTestAgent(t)
	var code atomic.Int32
	code.Store(200)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pass once, then fail: the probe after the grace must catch it.
		if hits.Add(1) == 1 {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	_, c, _ := runChecker(t, ta, webReplica(host, p, "/h"))
	waitFor(t, "unhealthy", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "unhealthy" })
	if st, _ := c.lastStatus(t, "rep_w"); !strings.Contains(st.Diagnosis, "returned 500") {
		t.Fatalf("diagnosis %q", st.Diagnosis)
	}
	for _, e := range c.of(proto.TReplicaStatus) {
		if decodeAs[proto.ReplicaStatus](t, e).State == "healthy" {
			t.Fatal("reported healthy without passing after the grace")
		}
	}
}

func TestHealthExecProbe(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	id, _ := ta.eng.Create(context.Background(), engine.Spec{Name: "5050-rep_w", Image: testImage})
	_ = ta.eng.Start(context.Background(), id)
	var exit atomic.Int32
	exit.Store(1)
	ta.eng.ExecFn = func(string, engine.ExecSpec, []byte) (string, int) { return "", int(exit.Load()) }
	s := worker("rep_w")
	s.Health = proto.HealthCheck{Exec: []string{"pg_isready"}, IntervalSec: 1, DeadlineSec: 1000, GraceSec: 1, TimeoutSec: 100}
	sess := ta.attach()
	c := collect(sess)
	r := ta.track(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ta.checkHealth(ctx, r, engine.Container{ID: id, StartedAt: time.Now(), State: "running"})
	time.Sleep(30 * time.Millisecond)
	if st, ok := c.lastStatus(t, "rep_w"); ok && st.State == "healthy" {
		t.Fatal("healthy while the exec probe fails")
	}
	exit.Store(0)
	waitFor(t, "healthy", func() bool { st, ok := c.lastStatus(t, "rep_w"); return ok && st.State == "healthy" })
}

func TestHealthDiagnosisCatalog(t *testing.T) {
	s := proto.ReplicaSpec{Port: 8080, Health: proto.HealthCheck{Path: "/health"}}
	cases := []struct {
		res       probeResult
		listen    []int
		loopback  []int
		want      string
		deadlineS int
	}{
		{probeResult{}, []int{3000}, nil, "Your app listens on 3000 but the target port is 8080. Set PORT=8080 in your app or change the target port to 3000.", 300},
		{probeResult{}, nil, []int{8080}, "Your app listens on 127.0.0.1:8080, which is unreachable from outside the container. Listen on 0.0.0.0.", 300},
		{probeResult{}, nil, []int{3000}, "Your app listens on 127.0.0.1:3000, which is unreachable from outside the container. Listen on 0.0.0.0.", 300},
		{probeResult{status: 404}, []int{8080}, nil, "Health check /health returned 404. Check the path in Settings > Deploy.", 300},
		{probeResult{kind: "tcp", err: errors.New("refused")}, nil, nil, "Nothing answered on port 8080 within 300 s. Make sure the app starts and listens on PORT.", 300},
		{probeResult{kind: "exec", err: errors.New("x")}, []int{8080}, nil, "The health command pg_isready did not succeed within 60 s.", 60},
	}
	for i, c := range cases {
		sp := s
		if c.res.kind == "exec" {
			sp.Health.Exec = []string{"pg_isready"}
		}
		if got := HealthDiagnosis(sp, c.res, c.listen, c.loopback, c.deadlineS); got != c.want {
			t.Errorf("%d: %q\nwant %q", i, got, c.want)
		}
	}
}

func TestProbeAddrFallsBackToContainerIP(t *testing.T) {
	s := proto.ReplicaSpec{Port: 80, Network: "n"}
	if got := probeAddr(s, engine.Container{IPs: map[string]string{"n": "10.0.0.5"}}); got != "10.0.0.5:80" {
		t.Fatal(got)
	}
	if got := probeAddr(s, engine.Container{IPs: map[string]string{"other": "10.0.0.6"}}); got != "10.0.0.6:80" {
		t.Fatal(got)
	}
}
