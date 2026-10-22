// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package netw

import (
	"fmt"
	"os"
	"syscall"
)

// fileID identifies a file across renames: device and inode.
func fileID(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d-%d", st.Dev, st.Ino)
	}
	return fmt.Sprintf("m%d", fi.ModTime().UnixNano())
}
