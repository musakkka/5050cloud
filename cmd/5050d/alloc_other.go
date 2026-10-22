// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package main

import (
	"errors"
	"os"
)

func allocate(*os.File, int64) error { return errors.New("fallocate is linux only") }
