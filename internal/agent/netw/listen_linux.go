// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package netw

import (
	"net"
	"syscall"
)

// defaultSockets binds with IP_FREEBIND, so the DNS server can listen on an
// environment gateway before Docker has created its bridge (at boot from the
// cache) and starts answering the moment the address appears.
func defaultSockets() socketMaker {
	return &net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_FREEBIND, 1)
		}); err != nil {
			return err
		}
		return serr
	}}
}
