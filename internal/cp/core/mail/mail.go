// SPDX-License-Identifier: AGPL-3.0-only

// Package mail sends email over SMTP (docs/SPEC.md 9.15): password resets,
// invites, email verification and notifications. TLS modes are "starttls"
// (required, the default), "tls" (implicit TLS, usually port 465) and
// "none". Fake records messages for tests and the dev server.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/ids"
)

// Message is a plain-text email.
type Message struct {
	To      []string
	Subject string
	Text    string
}

// Sender sends messages.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP is a Sender for one SMTP server.
type SMTP struct {
	Host     string
	Port     int
	TLSMode  string // "starttls" | "tls" | "none"
	Username string
	Password string
	From     string
	// TLSConfig overrides certificate checks (tests).
	TLSConfig *tls.Config
	Timeout   time.Duration
}

// Validate checks the settings as one sentence.
func (s SMTP) Validate() error {
	switch {
	case s.Host == "":
		return errors.New("the SMTP host is empty")
	case s.Port < 1 || s.Port > 65535:
		return errors.New("the SMTP port must be between 1 and 65535")
	case s.TLSMode != "" && s.TLSMode != "starttls" && s.TLSMode != "tls" && s.TLSMode != "none":
		return errors.New("the TLS mode must be starttls, tls or none")
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Errorf("the from address %q is not an email address", s.From)
	}
	return nil
}

// Send delivers m.
func (s SMTP) Send(ctx context.Context, m Message) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if len(m.To) == 0 {
		return errors.New("the message has no recipients")
	}
	from, _ := mail.ParseAddress(s.From)
	for _, to := range m.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("%q is not an email address", to)
		}
		if strings.ContainsAny(to, "\r\n") {
			return fmt.Errorf("%q is not an email address", to)
		}
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	tcfg := s.TLSConfig
	if tcfg == nil {
		tcfg = &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	}
	d := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	if s.TLSMode == "tls" {
		conn, err = tls.DialWithDialer(d, "tcp", addr, tcfg)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("the server at %s did not greet as SMTP: %w", addr, err)
	}
	defer func() { _ = c.Close() }()
	if s.TLSMode == "" || s.TLSMode == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; choose the tls or none mode", addr)
		}
		if err := c.StartTLS(tcfg); err != nil {
			return fmt.Errorf("STARTTLS with %s failed: %w", addr, err)
		}
	}
	if s.Username != "" {
		if err := c.Auth(plainAuth{s.Username, s.Password}); err != nil {
			return fmt.Errorf("the SMTP server refused the sign-in: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("the SMTP server refused the sender: %w", err)
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("the SMTP server refused %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(Render(s.From, m)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("the SMTP server did not accept the message: %w", err)
	}
	return c.Quit()
}

// plainAuth is AUTH PLAIN without net/smtp's TLS-only rule, so "none"
// works with servers on a private network; STARTTLS and TLS modes are
// encrypted before it runs.
type plainAuth struct{ user, pass string }

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

// Render builds the RFC 5322 message.
func Render(from string, m Message) []byte {
	var b bytes.Buffer
	host := "5050cloud"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndexByte(a.Address, '@'); i >= 0 {
			host = a.Address[i+1:]
		}
	}
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from)
	h("To", strings.Join(m.To, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	h("Date", time.Now().UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+ids.Random(24, ids.LowerAlnum)+"@"+host+">")
	h("MIME-Version", "1.0")
	h("Content-Type", `text/plain; charset="utf-8"`)
	h("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	text := strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n")
	// Dot-stuffing is done by net/smtp's data writer.
	b.WriteString(text)
	if !strings.HasSuffix(text, "\r\n") {
		b.WriteString("\r\n")
	}
	return b.Bytes()
}

func oneLine(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

// Fake records messages.
type Fake struct {
	mu   sync.Mutex
	sent []Message
	Err  error
}

// Send records m or returns Err.
func (f *Fake) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.sent = append(f.sent, m)
	return nil
}

// Sent returns a copy of the recorded messages.
func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.sent...)
}

// Last returns the last message to addr, if any.
func (f *Fake) Last(addr string) (Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.sent) - 1; i >= 0; i-- {
		for _, to := range f.sent[i].To {
			if strings.EqualFold(to, addr) {
				return f.sent[i], true
			}
		}
	}
	return Message{}, false
}

// ---------------------------------------------------------------- messages

// ResetPassword is the forgot-password email.
func ResetPassword(install, link string) (subject, text string) {
	return "Reset your " + install + " password",
		"Someone asked to reset the password of your account on " + install + ".\n\n" +
			"Open this link within one hour to choose a new password. It works once:\n" + link + "\n\n" +
			"If you did not ask for this, ignore this email; your password stays the same.\n"
}

// Invite is the invite email.
func Invite(install, inviter, role, link string, expires time.Time) (subject, text string) {
	return "You are invited to " + install,
		inviter + " invited you to " + install + " as " + role + ".\n\n" +
			"Accept the invite here (valid until " + expires.UTC().Format("2 January 2006 15:04 UTC") + "):\n" + link + "\n"
}

// Verify is the email verification message.
func Verify(install, link string) (subject, text string) {
	return "Confirm your email for " + install,
		"Confirm that this address belongs to your account on " + install + ", so it can send you notifications:\n" + link + "\n"
}

// Notice is a notification email.
func Notice(install, text, link string) (subject, body string) {
	subj := text
	if len(subj) > 120 {
		subj = subj[:117] + "..."
	}
	body = text + "\n"
	if link != "" {
		body += "\nOpen it in the console: " + link + "\n"
	}
	body += "\nYou can change which emails you get in Account > Notifications on " + install + ".\n"
	return "[" + install + "] " + subj, body
}

// Test is the "Send test email" message.
func Test(install string) (subject, text string) {
	return "Test email from " + install, "This is a test email from " + install + ". Email is set up correctly.\n"
}
