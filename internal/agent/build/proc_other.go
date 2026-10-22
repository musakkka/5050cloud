// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package build

import "os/exec"

// The agent runs on Linux only; these keep the package compiling elsewhere.

func setProcessGroup(*exec.Cmd) {}

func terminateGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func killGroup(*exec.Cmd) {}
