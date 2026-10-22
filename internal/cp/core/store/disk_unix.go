// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package store

import "syscall"

func diskFree(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:unconvert // field types differ between Linux and macOS
}
