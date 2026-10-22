// SPDX-License-Identifier: AGPL-3.0-only

//go:build deps

// Package deps pins the modules named in docs/SPEC.md 3.2 and 5.4 before
// any work package imports them, so `go mod tidy` keeps them in go.mod at the
// versions the planner chose. It is never compiled into a binary: the `deps`
// build tag is not set by any target. Remove an import here once real code
// imports the same package.
package deps

import (
	_ "filippo.io/age"
	_ "github.com/BurntSushi/toml"
	_ "github.com/coder/websocket"
	_ "github.com/docker/docker/client"
	_ "github.com/google/go-containerregistry/pkg/crane"
	_ "github.com/miekg/dns"
	_ "github.com/pquerna/otp/totp"
	_ "github.com/robfig/cron/v3"
	_ "github.com/spf13/cobra"
	_ "github.com/vishvananda/netlink"
	_ "golang.org/x/crypto/argon2"
	_ "golang.zx2c4.com/wireguard/wgctrl"
	_ "modernc.org/sqlite"
	_ "sigs.k8s.io/yaml"
)
