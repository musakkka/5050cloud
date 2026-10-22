// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func allocate(f *os.File, n int64) error { return unix.Fallocate(int(f.Fd()), 0, 0, n) }
