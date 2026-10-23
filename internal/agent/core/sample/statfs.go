// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package sample

import "syscall"

// Statfs returns used and total bytes of the filesystem holding path.
func Statfs(path string) (used, total int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := int64(st.Bsize)         //nolint:unconvert // the field is int32 on some platforms
	total = int64(st.Blocks) * bs //nolint:unconvert // the field type differs by platform
	// Use what an unprivileged writer would see: total minus available.
	used = total - int64(st.Bavail)*bs //nolint:unconvert // the field type differs by platform
	return used, total, nil
}

// SameDevice reports whether two paths are on the same filesystem.
func SameDevice(a, b string) bool {
	var sa, sb syscall.Stat_t
	if syscall.Stat(a, &sa) != nil || syscall.Stat(b, &sb) != nil {
		return false
	}
	return sa.Dev == sb.Dev
}
