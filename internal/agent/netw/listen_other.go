// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package netw

import "net"

func defaultSockets() socketMaker { return &net.ListenConfig{} }
