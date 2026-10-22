// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package core

import (
	"os"
	"syscall"
)

// fallocate reserves size bytes for f without writing them.
func fallocate(f *os.File, size int64) error {
	return syscall.Fallocate(int(f.Fd()), 0, 0, size)
}
