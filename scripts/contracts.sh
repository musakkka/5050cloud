#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# contracts.sh check|update
#
# The frozen contract files of docs/SPEC.md 5.3 and their SHA-256 in
# docs/contracts.sha256. `check` fails when a file changed without a new
# hash (V6). `update` rewrites the hash file; only the planner runs it, in
# the same change as the spec edit that allows the contract change.
set -eu

cd "$(dirname "$0")/.."

FILES="
pkg/api/types.go
pkg/api/routes.go
pkg/api/status.go
pkg/proto/proto.go
pkg/manifest/manifest.go
pkg/manifest/golden_test.go
pkg/manifest/testdata/template.yaml
pkg/manifest/testdata/project.yaml
pkg/manifest/testdata/railway.yaml
pkg/manifest/testdata/nixpacks-hints.yaml
pkg/version/version.go
internal/cp/cpapi/cpapi.go
internal/agent/agentapi/agentapi.go
internal/cp/secretbox/secretbox.go
internal/cp/orch/README.md
"

if command -v sha256sum >/dev/null 2>&1; then
	sha() { sha256sum "$@"; }
	shacheck() { sha256sum -c --quiet "$@"; }
else
	sha() { shasum -a 256 "$@"; }
	shacheck() { shasum -a 256 -c --quiet "$@"; }
fi

case "${1:-check}" in
update)
	# shellcheck disable=SC2086 # FILES is a word list on purpose
	sha $FILES >docs/contracts.sha256
	echo "Wrote docs/contracts.sha256 for $(wc -l <docs/contracts.sha256 | tr -d ' ') frozen files."
	;;
check)
	if [ ! -f docs/contracts.sha256 ]; then
		echo "docs/contracts.sha256 is missing. The planner creates it with scripts/contracts.sh update." >&2
		exit 1
	fi
	listed=$(awk '{print $2}' docs/contracts.sha256 | sort)
	# shellcheck disable=SC2086 # FILES is a word list on purpose
	expected=$(printf '%s\n' $FILES | sed '/^$/d' | sort)
	if [ "$listed" != "$expected" ]; then
		echo "docs/contracts.sha256 does not list exactly the frozen files of docs/SPEC.md 5.3." >&2
		exit 1
	fi
	if ! shacheck docs/contracts.sha256; then
		echo "A frozen contract file changed without a matching hash in docs/contracts.sha256. Contract changes go through the planner and docs/SPEC.md first (5.3)." >&2
		exit 1
	fi
	echo "Contract hashes match docs/contracts.sha256."
	;;
*)
	echo "Usage: scripts/contracts.sh check|update" >&2
	exit 2
	;;
esac
