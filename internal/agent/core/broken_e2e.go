// SPDX-License-Identifier: AGPL-3.0-only

//go:build e2e_broken_agent

package core

// brokenBuild makes this build never connect to the control plane, so the
// self-upgrade revert of docs/SPEC.md 9.16 can be proven end to end (E29).
// Release builds never carry this tag.
const brokenBuild = true
