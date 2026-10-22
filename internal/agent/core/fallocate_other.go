// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package core

import (
	"errors"
	"os"
)

// fallocate is Linux only; elsewhere the caller writes zeros.
func fallocate(*os.File, int64) error { return errors.New("fallocate is not available") }
