// SPDX-License-Identifier: AGPL-3.0-only

package github

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Git runs git commands for public repositories. Bin is "git" by default.
type Git struct {
	Bin string
}

func (g Git) bin() string {
	if g.Bin == "" {
		return "git"
	}
	return g.Bin
}

func (g Git) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.bin(), args...)
	cmd.Dir = dir
	// Never prompt for credentials: a private repo without an app must fail fast.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s failed: %s", args[0], firstLineOf(msg))
	}
	return out, nil
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ErrNoBranch is returned when a branch does not exist.
var ErrNoBranch = errors.New("branch not found")

// LsRemote returns the commit of branch ("" = the default branch, also returned).
func (g Git) LsRemote(ctx context.Context, url, branch string) (sha, resolved string, err error) {
	if branch == "" {
		out, err := g.run(ctx, "", "ls-remote", "--symref", url, "HEAD")
		if err != nil {
			return "", "", err
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) == 3 && f[0] == "ref:" && f[2] == "HEAD" {
				resolved = strings.TrimPrefix(f[1], "refs/heads/")
			} else if len(f) == 2 && f[1] == "HEAD" {
				sha = f[0]
			}
		}
		if sha == "" {
			return "", "", ErrNoBranch
		}
		return sha, resolved, nil
	}
	out, err := g.run(ctx, "", "ls-remote", "--heads", url, "refs/heads/"+branch)
	if err != nil {
		return "", "", err
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return "", "", ErrNoBranch
	}
	return f[0], branch, nil
}

// Heads lists branch names of a remote.
func (g Git) Heads(ctx context.Context, url string) ([]string, error) {
	out, err := g.run(ctx, "", "ls-remote", "--heads", url)
	if err != nil {
		return nil, err
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.HasPrefix(f[1], "refs/heads/") {
			names = append(names, strings.TrimPrefix(f[1], "refs/heads/"))
		}
	}
	return names, nil
}

// ShallowClone clones branch into a new temp directory (blobs over 1 MB
// are skipped) and returns it; the caller removes it.
func (g Git) ShallowClone(ctx context.Context, url, branch string) (string, error) {
	dir, err := os.MkdirTemp("", "5050-scan-")
	if err != nil {
		return "", err
	}
	args := []string{"clone", "--depth", "1", "--filter=blob:limit=1m", "--single-branch"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, url, filepath.Join(dir, "repo"))
	if _, err := g.run(ctx, "", args...); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return filepath.Join(dir, "repo"), nil
}
