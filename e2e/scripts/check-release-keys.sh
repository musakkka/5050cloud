#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Fails when install/install.sh and internal/upgrade/release-keys.txt embed
# different release keys (docs/SPEC.md 9.16).
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
a=$(grep -v '^#' "$root/internal/upgrade/release-keys.txt" | sed '/^$/d' | tr '\n' ' ' | sed 's/ *$//')
b=$(sed -n 's/^RELEASE_PUBKEYS="\(.*\)"$/\1/p' "$root/install/install.sh")
c=$(sed -n 's/^RELEASE_PUBKEYS="\(.*\)"$/\1/p' "$root/install/install-cli.sh")
if [ "$a" != "$c" ]; then
	echo "release keys differ: release-keys.txt has '$a', install-cli.sh has '$c'." >&2
	exit 1
fi
if [ "$a" != "$b" ]; then
	echo "release keys differ: release-keys.txt has '$a', install.sh has '$b'." >&2
	exit 1
fi
if [ -z "$a" ] && [ "${REQUIRE_RELEASE_KEY:-}" = 1 ]; then
	echo "no release key is embedded; run e2e/scripts/release-key.sh first." >&2
	exit 1
fi
echo "release keys match${a:+: $a}"
