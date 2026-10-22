#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Signs dist/checksums.txt for goreleaser. Tagged releases sign with the
# MINISIGN_KEY secret (written to MINISIGN_KEY_FILE by release.yml, since
# the key spans two lines); snapshots with the test key in e2e/keys.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
file=${1:?usage: sign-checksums.sh FILE}
if [ -n "${MINISIGN_KEY_FILE:-}" ]; then
	go run "$root/e2e/signtool" sign -s "$MINISIGN_KEY_FILE" -m "$file" -x "$file.minisig"
elif [ -n "${MINISIGN_KEY:-}" ]; then
	go run "$root/e2e/signtool" sign -env MINISIGN_KEY -m "$file" -x "$file.minisig"
elif [ "${GITHUB_REF_TYPE:-}" = tag ]; then
	echo "MINISIGN_KEY is not set; a tagged release is never signed with the test key." >&2
	exit 1
else
	go run "$root/e2e/signtool" sign -s "$root/e2e/keys/test.key" -m "$file" -x "$file.minisig"
fi
