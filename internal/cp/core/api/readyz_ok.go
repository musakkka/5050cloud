// SPDX-License-Identifier: AGPL-3.0-only

//go:build !e2e_broken_readyz

package api

// brokenReadyz is set by the e2e_broken_readyz build tag, which makes
// /readyz fail so the upgrade rollback path can be tested (E29).
const brokenReadyz = false
