// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs a host command and returns its combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if len(out) > 400 {
			out = out[len(out)-400:]
		}
		return buf.Bytes(), fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return buf.Bytes(), nil
}
