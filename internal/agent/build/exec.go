// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// cmdSpec is one child process. Args never carry a variable value: values
// travel in Env (docs/SPEC.md 8.2 step 7).
type cmdSpec struct {
	Path string
	Args []string
	Env  []string // the complete environment; nothing is inherited
	Dir  string
}

// runner runs a command and streams its combined output line by line.
type runner interface {
	Run(ctx context.Context, c cmdSpec, line func(string)) error
}

// exitError is a command that ran and failed.
type exitError struct {
	Cmd  string
	Code int
	Last string // last non-empty output line
}

func (e *exitError) Error() string {
	if e.Last != "" {
		return fmt.Sprintf("%s exited with code %d: %s", e.Cmd, e.Code, e.Last)
	}
	return fmt.Sprintf("%s exited with code %d", e.Cmd, e.Code)
}

// stopGrace is how long a cancelled command gets after SIGTERM before the
// whole process group is killed. Abort must finish within 5 s (WP4).
const stopGrace = 2 * time.Second

// execRunner runs real processes in their own process group, so cancelling
// a build also stops everything the command started.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, c cmdSpec, line func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Env = c.Env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	cmd.Dir = c.Dir
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return terminateGroup(cmd) }
	cmd.WaitDelay = stopGrace

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	var last string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		last = scanLines(pr, line)
	}()
	err := cmd.Start()
	if err != nil {
		_ = pw.Close()
		wg.Wait()
		return fail("The %s command could not be started on this host: %v.", baseName(c.Path), err)
	}
	waitErr := cmd.Wait()
	killGroup(cmd) // nothing the command started may outlive it
	_ = pw.Close()
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			return &exitError{Cmd: baseName(c.Path), Code: ee.ExitCode(), Last: last}
		}
		return fmt.Errorf("%s failed: %w", baseName(c.Path), waitErr)
	}
	return nil
}

// scanLines splits output on \n and \r (progress bars), drops empty lines
// and returns the last line seen.
func scanLines(r io.Reader, line func(string)) string {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	sc.Split(splitCRLF)
	last := ""
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), " \t")
		if l == "" {
			continue
		}
		last = l
		if line != nil {
			line(l)
		}
	}
	// Drain anything left after a scanner error (an over-long line) so the
	// child never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, r)
	return last
}

func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	for i, c := range data {
		if c == '\n' || c == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
