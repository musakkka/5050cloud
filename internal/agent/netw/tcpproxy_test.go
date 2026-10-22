// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// echoServer answers each line with "<name>:<line>".
func echoServer(t *testing.T, name string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					if _, err := io.WriteString(c, name+":"+sc.Text()+"\n"); err != nil {
						return
					}
				}
			}()
		}
	}()
	return l.Addr().String()
}

func newTestProxy(t *testing.T) *tcpProxy {
	t.Helper()
	p := newTCPProxy(slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.bind = "127.0.0.1"
	// Map the 20000-29999 range to free ephemeral ports so tests never collide.
	ports := map[string]string{}
	p.listen = func(network, addr string) (net.Listener, error) {
		l, err := net.Listen(network, "127.0.0.1:0")
		if err == nil {
			ports[addr] = l.Addr().String()
		}
		return l, err
	}
	t.Cleanup(p.close)
	return p
}

func roundTrip(t *testing.T, addr, msg string) string {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(c, msg+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "ERR " + err.Error()
	}
	return strings.TrimSpace(line)
}

func TestTCPProxyRoundRobinAndFailover(t *testing.T) {
	p := newTestProxy(t)
	a, b := echoServer(t, "a"), echoServer(t, "b")
	dead := "127.0.0.1:1" // nothing listens there
	if err := p.apply([]proto.TCPProxy{{HostPort: 20001, Upstreams: []string{a, dead, b}}}); err != nil {
		t.Fatal(err)
	}
	addr := p.addr(20001).String()
	seen := map[string]int{}
	for i := range 9 {
		got := roundTrip(t, addr, "hi"+strconv.Itoa(i))
		seen[strings.SplitN(got, ":", 2)[0]]++
	}
	if seen["a"] == 0 || seen["b"] == 0 || len(seen) != 2 {
		t.Errorf("distribution = %v (the dead upstream must be skipped)", seen)
	}

	// Change upstreams in place: the same listener keeps its port.
	if err := p.apply([]proto.TCPProxy{{HostPort: 20001, Upstreams: []string{b}}}); err != nil {
		t.Fatal(err)
	}
	if p.addr(20001).String() != addr {
		t.Error("an upstream change reopened the listener")
	}
	for range 3 {
		if got := roundTrip(t, addr, "x"); got != "b:x" {
			t.Errorf("after change = %q", got)
		}
	}
	// No upstreams: connections are closed.
	if err := p.apply([]proto.TCPProxy{{HostPort: 20001}}); err != nil {
		t.Fatal(err)
	}
	if got := roundTrip(t, addr, "x"); !strings.HasPrefix(got, "ERR") {
		t.Errorf("no upstreams = %q", got)
	}
	// Removed port: listener closed.
	if err := p.apply(nil); err != nil {
		t.Fatal(err)
	}
	if p.addr(20001) != nil {
		t.Error("removed port still listening")
	}
	if _, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		t.Error("removed listener still accepts")
	}
}

func TestTCPProxyValidation(t *testing.T) {
	p := newTestProxy(t)
	err := p.apply([]proto.TCPProxy{
		{HostPort: 80, Upstreams: []string{"10.0.0.1:5432"}},
		{HostPort: 20002, Upstreams: []string{"no-port", "10.0.0.1:5432"}},
		{HostPort: 20002, Upstreams: []string{"10.0.0.2:5432"}},
	})
	if err == nil {
		t.Fatal("bad entries not reported")
	}
	for _, want := range []string{"port 80 is outside", "not host:port", "appears twice"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if p.addr(20002) == nil {
		t.Error("the valid part was not applied")
	}
	p.listen = func(string, string) (net.Listener, error) {
		return nil, &net.OpError{Op: "listen", Err: io.ErrClosedPipe}
	}
	if err := p.apply([]proto.TCPProxy{{HostPort: 20003, Upstreams: []string{"10.0.0.1:1"}}}); err == nil || !strings.Contains(err.Error(), "cannot listen on port 20003") {
		t.Errorf("listen failure = %v", err)
	}
}

func TestTCPProxyHalfCloseAndIdle(t *testing.T) {
	p := newTestProxy(t)
	p.idle = 300 * time.Millisecond
	// An upstream that reads everything, then answers with the byte count.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				n, _ := io.Copy(io.Discard, c)
				_, _ = io.WriteString(c, strconv.FormatInt(n, 10))
			}()
		}
	}()
	if err := p.apply([]proto.TCPProxy{{HostPort: 20004, Upstreams: []string{l.Addr().String()}}}); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", p.addr(20004).String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(c, strings.Repeat("x", 100000))
	_ = c.(*net.TCPConn).CloseWrite()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, _ := io.ReadAll(c)
	c.Close()
	if string(b) != "100000" {
		t.Errorf("half-close answer = %q", b)
	}

	// An idle connection is closed after the idle timeout.
	echo := echoServer(t, "e")
	_ = p.apply([]proto.TCPProxy{{HostPort: 20004, Upstreams: []string{echo}}})
	c, err = net.Dial("tcp", p.addr(20004).String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err = c.Read(make([]byte, 1))
	if err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("idle connection not closed in time: %v after %v", err, time.Since(start))
	}
}
