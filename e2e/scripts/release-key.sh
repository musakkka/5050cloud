#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# Creates the release signing key (docs/SPEC.md 9.16 and 17.3) and embeds
# its public key in install/install.sh and internal/upgrade/release-keys.txt.
#
#   e2e/scripts/release-key.sh OUT_DIR          new current key
#   e2e/scripts/release-key.sh --next OUT_DIR   announce a next key
#
# The secret key lands in OUT_DIR/release.key (0600). Store it as the
# MINISIGN_KEY secret of the release workflow, then delete it from disk.
# OUT_DIR must be outside the repository.
set -eu
next=""
if [ "${1:-}" = --next ]; then next=1; shift; fi
out=${1:?usage: release-key.sh [--next] OUT_DIR}
root=$(cd "$(dirname "$0")/../.." && pwd)
case "$(cd "$(dirname "$out")" 2>/dev/null && pwd)/" in "$root"/*) echo "OUT_DIR must be outside the repository." >&2; exit 1 ;; esac
mkdir -p "$out"
umask 077
(cd "$root" && go run ./e2e/signtool keygen -p "$out/release.pub" -s "$out/release.key")
pub=$(sed -n 2p "$out/release.pub")
keys=$(grep -v '^#' "$root/internal/upgrade/release-keys.txt" | sed '/^$/d' | head -n 1)
if [ -n "$next" ]; then
	[ -n "$keys" ] || { echo "there is no current key yet; run without --next first." >&2; exit 1; }
	keys="$keys $pub"
else
	keys=$pub
fi
tmp=$(mktemp)
grep '^#' "$root/internal/upgrade/release-keys.txt" | grep -v 'No release key\|release-key.sh creates' >"$tmp" || true
for k in $keys; do echo "$k" >>"$tmp"; done
mv "$tmp" "$root/internal/upgrade/release-keys.txt"
chmod 0644 "$root/internal/upgrade/release-keys.txt"
sed "s|^RELEASE_PUBKEYS=.*|RELEASE_PUBKEYS=\"$keys\"|" "$root/install/install.sh" >"$root/install/install.sh.new"
mv "$root/install/install.sh.new" "$root/install/install.sh"
chmod 0755 "$root/install/install.sh"
echo "Public key: $pub"
echo "Secret key: $out/release.key. Store it as the MINISIGN_KEY secret (gh secret set MINISIGN_KEY < $out/release.key), then delete it."
