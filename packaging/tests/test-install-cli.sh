#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs install/install-cli.sh against a local fixture release.
#   FIXTURE=DIR tests/test-install-cli.sh      (DIR from tests/make-fixture.sh)
set -u
# shellcheck source=packaging/tests/lib.sh
. "$(dirname "$0")/lib.sh"
FIXTURE=${FIXTURE:?set FIXTURE to a directory made by tests/make-fixture.sh}
VER=$(cat "$FIXTURE/latest.txt")
KEY=$(cat "$FIXTURE/test.pub")
OTHER=$(cat "$FIXTURE/other.pub")
INST=$REPO/install/install-cli.sh

W=$(mktemp -d)
cleanup() { kill "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; rm -rf "$W"; }
trap cleanup EXIT
serve "$FIXTURE" || exit 1
BASE=http://127.0.0.1:$PORT

run() { # run NAME -- flags... ; installs into $W/NAME
	n=$1; shift
	env -u FIFTY_VERSION sh "$INST" --dir "$W/$n" "$@" >"$W/$n.log" 2>&1
}

check "signed release, latest version" run latest --release-base "$BASE" --release-pubkey "$KEY"
check "installed binary reports $VER" sh -c "'$W/latest/5050' version | grep -q '$VER'"

check "explicit version without v" run explicit --release-base "$BASE" --release-pubkey "$KEY" --version "${VER#v}"
check "explicit version installed" test -x "$W/explicit/5050"

check "key from FIFTY_RELEASE_PUBKEY" sh -c "FIFTY_RELEASE_PUBKEY='$KEY' FIFTY_RELEASE_BASE='$BASE' sh '$INST' --dir '$W/envkey' --quiet >/dev/null"
check "--quiet prints only the path" sh -c "FIFTY_RELEASE_PUBKEY='$KEY' sh '$INST' --release-base '$BASE' --dir '$W/quiet' --quiet | grep -qx '$W/quiet/5050'"

if run wrongkey --release-base "$BASE" --release-pubkey "$OTHER"; then bad "wrong key is refused"; else ok "wrong key is refused"; fi
check "wrong key installs nothing" test ! -e "$W/wrongkey/5050"

if run nokey --release-base "$BASE"; then bad "release base without a key is refused"; else ok "release base without a key is refused"; fi

check "insecure mode still installs" run insecure --release-base "$BASE" --insecure-skip-verify
check "insecure mode warns" grep -q "not checking the release signature" "$W/insecure.log"

# Tampered archive: copy the fixture and flip the archive for this machine.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
T=$W/tampered
mkdir -p "$T/$VER"
cp "$FIXTURE/$VER/"* "$T/$VER/"
cp "$FIXTURE/latest.txt" "$T/"
printf 'x' >>"$T/$VER/5050-cli_${VER#v}_${os}_${arch}.tar.gz"
if run tampered --archive "$T/$VER/5050-cli_${VER#v}_${os}_${arch}.tar.gz" --release-pubkey "$KEY"; then
	bad "tampered archive is refused"
else
	ok "tampered archive is refused"
fi
check "tampered archive message" grep -q "does not match checksums.txt" "$W/tampered.log"

check "local archive" run archive --archive "$FIXTURE/$VER/5050-cli_${VER#v}_${os}_${arch}.tar.gz" --release-pubkey "$KEY"
check "local archive version detected" grep -q "$VER" "$W/archive.log"

check "railway alias" run alias --release-base "$BASE" --release-pubkey "$KEY" --railway-alias
check "railway alias is a symlink to 5050" sh -c "[ \"\$(readlink '$W/alias/railway')\" = 5050 ]"

if sh "$INST" --bogus >/dev/null 2>&1; then bad "unknown flag is refused"; else ok "unknown flag is refused"; fi
check "--help" sh -c "sh '$INST' --help | grep -q -- '--railway-alias'"

if sh "$INST" --repo nonsense --dir "$W/x" >/dev/null 2>&1; then bad "--repo needs OWNER/NAME"; else ok "--repo needs OWNER/NAME"; fi

# OpenSSL path versus minisign path: hide minisign when it is installed.
if command -v openssl >/dev/null 2>&1 && openssl version | grep -q '^OpenSSL [3-9]'; then
	check "verifies with OpenSSL" run openssl --release-base "$BASE" --release-pubkey "$KEY"
fi

finish
