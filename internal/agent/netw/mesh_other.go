// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package netw

import (
	"fmt"
	"runtime"
)

func newMeshDriver() (meshDriver, error) {
	return nil, fmt.Errorf("the WireGuard mesh needs Linux, and this agent was built for %s", runtime.GOOS)
}
