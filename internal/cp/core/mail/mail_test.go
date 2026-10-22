// SPDX-License-Identifier: AGPL-3.0-only

package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpServer is a minimal SMTP server that records one transaction.
type smtpServer struct {
	ln       net.Listener
	mu       sync.Mutex
	data     string
	auth     string
	rcpts    []string
	starttls bool
}

func newServer(t *testing.T, starttls bool) *smtpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpServer{ln: ln, starttls: starttls}
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *smtpServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *smtpServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *smtpServer) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	w := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	w("220 test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if s.starttls {
				w("250-test")
				w("250-STARTTLS")
			} else {
				w("250-test")
			}
			w("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.auth = strings.TrimSpace(line)
			s.mu.Unlock()
			w("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO"):
			s.mu.Lock()
			s.rcpts = append(s.rcpts, strings.TrimSpace(line))
			s.mu.Unlock()
			w("250 ok")
		case cmd == "DATA":
			w("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			w("250 queued")
		case cmd == "QUIT":
			w("221 bye")
			return
		default:
			w("500 unknown")
		}
	}
}

func TestSendNone(t *testing.T) {
	srv := newServer(t, false)
	s := SMTP{Host: "127.0.0.1", Port: srv.port(), TLSMode: "none", Username: "u", Password: "p", From: "5050cloud <noreply@example.com>"}
	err := s.Send(context.Background(), Message{To: []string{"ada@example.com"}, Subject: "Hi\nthere", Text: "line1\nline2"})
	if err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.Contains(srv.data, "Subject: Hi there") || !strings.Contains(srv.data, "line1\r\nline2") || !strings.Contains(srv.data, "From: 5050cloud <noreply@example.com>") {
		t.Fatalf("data = %q", srv.data)
	}
	if !strings.HasPrefix(srv.auth, "AUTH PLAIN") || len(srv.rcpts) != 1 {
		t.Fatalf("auth=%q rcpts=%v", srv.auth, srv.rcpts)
	}
}

func TestSendErrors(t *testing.T) {
	srv := newServer(t, false)
	base := SMTP{Host: "127.0.0.1", Port: srv.port(), TLSMode: "starttls", From: "a@example.com", Timeout: 2 * time.Second}
	if err := base.Send(context.Background(), Message{To: []string{"b@example.com"}}); err == nil || !strings.Contains(err.Error(), "does not offer STARTTLS") {
		t.Fatalf("starttls missing: %v", err)
	}
	for _, s := range []SMTP{
		{Port: 25, From: "a@example.com"},
		{Host: "h", Port: 0, From: "a@example.com"},
		{Host: "h", Port: 25, TLSMode: "ssl", From: "a@example.com"},
		{Host: "h", Port: 25, From: "nope"},
	} {
		if s.Validate() == nil {
			t.Errorf("%+v validated", s)
		}
	}
	ok := SMTP{Host: "127.0.0.1", Port: srv.port(), TLSMode: "none", From: "a@example.com"}
	if err := ok.Send(context.Background(), Message{}); err == nil {
		t.Fatal("no recipients accepted")
	}
	if err := ok.Send(context.Background(), Message{To: []string{"bad"}}); err == nil {
		t.Fatal("bad recipient accepted")
	}
	closed := SMTP{Host: "127.0.0.1", Port: 1, TLSMode: "none", From: "a@example.com", Timeout: time.Second}
	if err := closed.Send(context.Background(), Message{To: []string{"b@example.com"}}); err == nil {
		t.Fatal("a closed port must fail")
	}
	tlsS := SMTP{Host: "127.0.0.1", Port: srv.port(), TLSMode: "tls", From: "a@example.com", Timeout: time.Second}
	if err := tlsS.Send(context.Background(), Message{To: []string{"b@example.com"}}); err == nil {
		t.Fatal("implicit TLS against a plain server must fail")
	}
	if _, err := (plainAuth{}).Next(nil, true); err == nil {
		t.Fatal("challenge must fail")
	}
	_ = strconv.Itoa(0)
}

func TestFakeAndMessages(t *testing.T) {
	f := &Fake{}
	_ = f.Send(context.Background(), Message{To: []string{"A@example.com"}, Subject: "s1"})
	_ = f.Send(context.Background(), Message{To: []string{"a@example.com"}, Subject: "s2"})
	if m, ok := f.Last("a@example.com"); !ok || m.Subject != "s2" || len(f.Sent()) != 2 {
		t.Fatal("Last")
	}
	if _, ok := f.Last("x@example.com"); ok {
		t.Fatal("no message for x")
	}
	f.Err = errors.New("down")
	if f.Send(context.Background(), Message{}) == nil {
		t.Fatal("Err not returned")
	}
	for _, pair := range [][2]string{
		pair(ResetPassword("i", "https://l")), pair(Invite("i", "Ada", "member", "https://l", time.Now())),
		pair(Verify("i", "https://l")), pair(Notice("i", strings.Repeat("x", 200), "https://l")), pair(Test("i")),
	} {
		if pair[0] == "" || pair[1] == "" {
			t.Fatal("empty message")
		}
	}
	if s, _ := Notice("i", "short", ""); s != "[i] short" {
		t.Fatal(s)
	}
	if b := Render("bad", Message{To: []string{"x@example.com"}, Text: "t\n"}); !strings.Contains(string(b), "@5050cloud>") {
		t.Fatal("Message-ID host fallback")
	}
}

func pair(a, b string) [2]string { return [2]string{a, b} }
