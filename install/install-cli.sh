#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Installs the 5050 command line tool only (no server, no root needed).
#
#   curl -fsSL https://5050cloud.com/install-cli.sh | sh
#   curl -fsSL https://5050cloud.com/install-cli.sh | sh -s -- --version v0.1.0 --dir ~/bin
#
# It downloads 5050-cli_<version>_<os>_<arch>.tar.gz from a GitHub release,
# checks the minisign signature of checksums.txt and the SHA-256 of the
# archive, and puts the 5050 binary in /usr/local/bin when that is writable,
# else in ~/.local/bin. Every flag is also an environment variable
# FIFTY_<FLAG> (for example FIFTY_VERSION or FIFTY_DIR). Run with --help.

set -eu

# Release signing keys (minisign, base64), the same list as install/install.sh
# and internal/upgrade/release-keys.txt. e2e/scripts/release-key.sh writes it.
RELEASE_PUBKEYS="RWS5v2vaQNNARGmod7/0v3a8ZYZGhT95PWYNvc+dXNxe3/gWarQEiKiR"

DEFAULT_REPO="musakkka/5050cloud"

if [ -t 1 ]; then
	B=$(printf '\033[1m'); R=$(printf '\033[0m'); RED=$(printf '\033[31m'); YEL=$(printf '\033[33m')
else
	B=""; R=""; RED=""; YEL=""
fi
say() { printf '%s\n' "$*"; }
step() { printf '%s==>%s %s\n' "$B" "$R" "$*"; }
warn() { printf '%swarning:%s %s\n' "$YEL" "$R" "$*" >&2; }
die() { printf '%serror:%s %s\n' "$RED" "$R" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
	cat <<'EOF'
Install the 5050 command line tool.

  curl -fsSL https://5050cloud.com/install-cli.sh | sh [-s -- FLAGS]

Flags (each also an environment variable FIFTY_<FLAG>, for example FIFTY_VERSION):
  --version vX.Y.Z         release to install (default: latest)
  --dir PATH               where to put the binary (default /usr/local/bin when
                           writable, else ~/.local/bin)
  --repo OWNER/NAME        GitHub repository to download from (default musakkka/5050cloud)
  --release-base URL       download from URL/<version>/ instead of GitHub
  --release-pubkey KEY     minisign public key to check the release with
  --archive PATH           install from a local 5050-cli tarball (checksums.txt and
                           checksums.txt.minisig must sit next to it)
  --insecure-skip-verify   skip the signature check; the SHA-256 from checksums.txt
                           is still checked (test builds only)
  --railway-alias          also install the railway alias next to the binary
  --quiet                  print only errors and the installed path
  --help                   show this help
EOF
}

env_or() { v=""; eval "v=\${FIFTY_$1:-}"; printf '%s' "$v"; }

VERSION=$(env_or VERSION)
DIR=$(env_or DIR)
REPO=$(env_or REPO)
RELEASE_BASE=$(env_or RELEASE_BASE)
RELEASE_PUBKEY=$(env_or RELEASE_PUBKEY)
ARCHIVE=$(env_or ARCHIVE)
INSECURE=$(env_or INSECURE_SKIP_VERIFY)
RAILWAY_ALIAS=$(env_or RAILWAY_ALIAS)
QUIET=$(env_or QUIET)

need_arg() { if [ "$#" -lt 2 ] || [ -z "$2" ]; then die "$1 needs a value."; fi; }
while [ "$#" -gt 0 ]; do
	case "$1" in
	--version) need_arg "$@"; VERSION=$2; shift ;;
	--dir) need_arg "$@"; DIR=$2; shift ;;
	--repo) need_arg "$@"; REPO=$2; shift ;;
	--release-base) need_arg "$@"; RELEASE_BASE=$2; shift ;;
	--release-pubkey) need_arg "$@"; RELEASE_PUBKEY=$2; shift ;;
	--archive) need_arg "$@"; ARCHIVE=$2; shift ;;
	--insecure-skip-verify) INSECURE=1 ;;
	--railway-alias) RAILWAY_ALIAS=1 ;;
	--quiet | -q) QUIET=1 ;;
	--help | -h) usage; exit 0 ;;
	*) die "unknown flag $1. Run with --help for the list." ;;
	esac
	shift
done

if [ -n "$QUIET" ]; then
	step() { :; }
	say() { :; }
fi

REPO=${REPO:-$DEFAULT_REPO}
case "$REPO" in */*) ;; *) die "--repo takes OWNER/NAME, not $REPO." ;; esac
REPO_RELEASES="https://github.com/$REPO/releases"
if [ -n "$RELEASE_PUBKEY" ]; then
	RELEASE_PUBKEYS=$RELEASE_PUBKEY
fi
if [ -n "$RELEASE_BASE" ] && [ -z "$RELEASE_PUBKEY" ] && [ -z "$INSECURE" ]; then
	die "--release-base needs --release-pubkey (or --insecure-skip-verify for a test build)."
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# ------------------------------------------------------------------ download and verify

fetch() { # fetch URL FILE
	if have curl; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is needed to download releases."
	fi
}

latest_version() {
	if [ -n "$RELEASE_BASE" ]; then
		fetch "${RELEASE_BASE%/}/latest.txt" "$TMP/latest.txt" || die "cannot read $RELEASE_BASE/latest.txt."
	else
		fetch "$REPO_RELEASES/latest/download/latest.txt" "$TMP/latest.txt" ||
			die "cannot find the latest release of $REPO. Pass --version vX.Y.Z."
	fi
	tr -d ' \r\n' <"$TMP/latest.txt"
}

sha256_of() {
	if have sha256sum; then sha256sum "$1" | cut -d' ' -f1
	else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

b64d() { base64 -d 2>/dev/null || base64 -D; }
hexof() { od -An -tx1 | tr -d ' \n'; }

# minisign_verify FILE SIGFILE: checks FILE against every key in
# RELEASE_PUBKEYS. Uses minisign when installed, else OpenSSL 3 (Ed25519).
# Kept identical to the function of the same name in install/install.sh.
minisign_verify() {
	file=$1; sig=$2
	[ -n "$RELEASE_PUBKEYS" ] || die "this installer has no release signing key. Pass --release-pubkey KEY."
	for key in $RELEASE_PUBKEYS; do
		if have minisign; then
			if minisign -Vq -P "$key" -m "$file" -x "$sig" >/dev/null 2>&1; then return 0; fi
			continue
		fi
		have openssl || die "verifying the release signature needs minisign or OpenSSL 3. Install one of them."
		printf '%s' "$key" | b64d >"$TMP/pk.bin"
		sed -n 2p "$sig" | b64d >"$TMP/sig.bin"
		[ "$(wc -c <"$TMP/pk.bin" | tr -d ' ')" = 42 ] || die "the release public key is not a minisign key."
		[ "$(wc -c <"$TMP/sig.bin" | tr -d ' ')" = 74 ] || die "$sig is not a minisign signature."
		kid=$(dd if="$TMP/pk.bin" bs=1 skip=2 count=8 2>/dev/null | hexof)
		sid=$(dd if="$TMP/sig.bin" bs=1 skip=2 count=8 2>/dev/null | hexof)
		[ "$kid" = "$sid" ] || continue
		alg=$(dd if="$TMP/sig.bin" bs=1 count=2 2>/dev/null)
		printf '\060\052\060\005\006\003\053\145\160\003\041\000' >"$TMP/pk.der"
		dd if="$TMP/pk.bin" bs=1 skip=10 count=32 2>/dev/null >>"$TMP/pk.der"
		dd if="$TMP/sig.bin" bs=1 skip=10 count=64 2>/dev/null >"$TMP/sig.raw"
		openssl pkey -pubin -inform DER -in "$TMP/pk.der" -out "$TMP/pk.pem" 2>/dev/null ||
			die "OpenSSL cannot read an Ed25519 key here. Install minisign (apt install minisign or brew install minisign) and run again."
		case "$alg" in
		ED) openssl dgst -blake2b512 -binary "$file" >"$TMP/msg.bin" ;;
		Ed) cp "$file" "$TMP/msg.bin" ;;
		*) die "$sig uses an unknown signature algorithm." ;;
		esac
		if openssl pkeyutl -verify -pubin -inkey "$TMP/pk.pem" -rawin -in "$TMP/msg.bin" -sigfile "$TMP/sig.raw" >/dev/null 2>&1; then
			return 0
		fi
	done
	return 1
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "this installer supports Linux and macOS. On Windows, download the zip from $REPO_RELEASES." ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "this processor ($(uname -m)) is not supported; the CLI is built for amd64 and arm64." ;;
	esac
}

# get_archive: puts the CLI tarball in $TMP and checks it. Sets VERSION and FILE.
get_archive() {
	os=$(detect_os)
	arch=$(detect_arch)
	if [ -n "$ARCHIVE" ]; then
		[ -f "$ARCHIVE" ] || die "$ARCHIVE does not exist."
		dir=$(dirname "$ARCHIVE")
		name=$(basename "$ARCHIVE")
		cp "$ARCHIVE" "$TMP/$name"
		[ -f "$dir/checksums.txt" ] || die "checksums.txt must sit next to $ARCHIVE."
		cp "$dir/checksums.txt" "$TMP/"
		if [ -f "$dir/checksums.txt.minisig" ]; then cp "$dir/checksums.txt.minisig" "$TMP/"; fi
		[ -n "$VERSION" ] || VERSION=v$(printf '%s' "$name" | sed -n 's/^5050-cli_\([0-9][^_]*\)_.*/\1/p')
	else
		[ -n "$VERSION" ] || VERSION=$(latest_version)
		case "$VERSION" in v*) ;; *) VERSION=v$VERSION ;; esac
		base=${RELEASE_BASE:-$REPO_RELEASES/download}
		base=${base%/}/$VERSION
		name="5050-cli_${VERSION#v}_${os}_${arch}.tar.gz"
		step "Downloading the 5050 CLI $VERSION for $os/$arch"
		fetch "$base/checksums.txt" "$TMP/checksums.txt" || die "cannot download $base/checksums.txt. Does release $VERSION exist?"
		if [ -z "$INSECURE" ]; then
			fetch "$base/checksums.txt.minisig" "$TMP/checksums.txt.minisig" || die "cannot download the release signature."
		fi
		fetch "$base/$name" "$TMP/$name" || die "cannot download $base/$name."
	fi
	if [ -n "$INSECURE" ]; then
		warn "not checking the release signature (--insecure-skip-verify)."
	else
		[ -f "$TMP/checksums.txt.minisig" ] || die "checksums.txt.minisig is missing, so the release cannot be checked."
		minisign_verify "$TMP/checksums.txt" "$TMP/checksums.txt.minisig" ||
			die "the release signature does not match the release key, so nothing was installed."
	fi
	want=$(awk -v n="$name" '{f=$2; sub(/^\*/, "", f); if (f == n) print $1}' "$TMP/checksums.txt")
	[ -n "$want" ] || die "$name is not listed in checksums.txt."
	got=$(sha256_of "$TMP/$name")
	[ "$want" = "$got" ] || die "the SHA-256 of $name does not match checksums.txt, so nothing was installed."
	FILE=$TMP/$name
}

# ------------------------------------------------------------------ install

get_archive
mkdir -p "$TMP/cli"
tar -xzf "$FILE" -C "$TMP/cli"
[ -f "$TMP/cli/5050" ] || die "the archive has no 5050 binary."

if [ -z "$DIR" ]; then
	if [ -w /usr/local/bin ]; then
		DIR=/usr/local/bin
	else
		DIR=$HOME/.local/bin
	fi
fi
mkdir -p "$DIR" || die "cannot create $DIR."
[ -w "$DIR" ] || die "$DIR is not writable. Pass --dir, or run with sudo."
cp "$TMP/cli/5050" "$DIR/5050.new" && chmod 0755 "$DIR/5050.new" && mv "$DIR/5050.new" "$DIR/5050"

if [ -n "$RAILWAY_ALIAS" ]; then
	if [ -e "$DIR/railway" ] && [ ! -L "$DIR/railway" ]; then
		warn "$DIR/railway exists and is not our alias; leaving it alone."
	else
		ln -sf 5050 "$DIR/railway"
		say "Installed the railway alias at $DIR/railway."
	fi
fi

if [ -n "$QUIET" ]; then
	printf '%s\n' "$DIR/5050"
else
	say "Installed the 5050 CLI $VERSION to $DIR/5050."
	case ":$PATH:" in *":$DIR:"*) ;; *) say "Add $DIR to your PATH to run it." ;; esac
	say "Next: 5050 login https://<your console>"
fi
