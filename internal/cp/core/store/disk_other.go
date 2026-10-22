// SPDX-License-Identifier: AGPL-3.0-only

//go:build !unix

package store

import "math"

// diskFree is not measured on non-unix systems (development only).
func diskFree(string) (uint64, error) { return math.MaxUint64, nil }
