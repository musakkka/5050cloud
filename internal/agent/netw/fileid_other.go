// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package netw

import (
	"fmt"
	"os"
)

func fileID(fi os.FileInfo) string { return fmt.Sprintf("m%d", fi.ModTime().UnixNano()) }
