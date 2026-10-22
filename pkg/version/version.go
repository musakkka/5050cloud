// SPDX-License-Identifier: MIT

// Package version holds build metadata set with -ldflags at release time:
//
//	-X github.com/musakkka/5050cloud/pkg/version.Version=v0.1.0
//	-X github.com/musakkka/5050cloud/pkg/version.Commit=<sha>
//	-X github.com/musakkka/5050cloud/pkg/version.Date=<rfc3339>
package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
