// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// iptables reads and atomically replaces rules. The exec implementation
// calls iptables-save and iptables-restore, whichever backend (legacy or
// nft) the host selected, so our rules sit next to Docker's.
type iptables interface {
	Save(ctx context.Context) (string, error)
	Restore(ctx context.Context, input string) error
}

type execIPTables struct {
	// prefix runs the commands through another program, for example
	// ["ip", "netns", "exec", "h1"] in the two-namespace test.
	prefix []string
	save   string
	store  string
}

func newExecIPTables(prefix []string) *execIPTables {
	return &execIPTables{prefix: prefix, save: "iptables-save", store: "iptables-restore"}
}

func (e *execIPTables) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	argv := append(append(append([]string{}, e.prefix...), name), args...)
	return exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- fixed program names, no user input
}

func (e *execIPTables) Save(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	c := e.cmd(ctx, e.save)
	c.Stdout, c.Stderr = &out, &stderr
	if err := c.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s is not installed; install the iptables package (Docker needs it too)", e.save)
		}
		return "", fmt.Errorf("reading the current firewall rules with %s failed: %s", e.save, firstLine(stderr.String(), err))
	}
	return out.String(), nil
}

func (e *execIPTables) Restore(ctx context.Context, input string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	c := e.cmd(ctx, e.store, "--noflush", "--wait=10")
	c.Stdin = strings.NewReader(input)
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%s is not installed; install the iptables package (Docker needs it too)", e.store)
		}
		return fmt.Errorf("applying the firewall rules failed, so the previous rules are still in place: %s", firstLine(stderr.String(), err))
	}
	return nil
}

func firstLine(stderr string, err error) string {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return err.Error()
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		// iptables-restore names the failing line on the last line; keep both.
		return strings.TrimSpace(s[:i]) + " (" + strings.TrimSpace(s[strings.LastIndexByte(s, '\n')+1:]) + ")"
	}
	return s
}
