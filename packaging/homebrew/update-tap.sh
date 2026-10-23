#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Writes the Homebrew formula for a CLI release into a tap checkout.
#
#   packaging/homebrew/update-tap.sh --version v0.1.0 --tap ../homebrew-tap
#   packaging/homebrew/update-tap.sh --version v0.1.0 --checksums dist/checksums.txt \
#       --tap ../homebrew-tap --commit --push
#
# It reads the SHA-256 of the four 5050-cli_<version>_<os>_<arch>.tar.gz
# archives from the release's checksums.txt (a local file, or downloaded
# from the release), fills packaging/homebrew/fifty-cli.rb.in and writes
# TAP/Formula/fifty-cli.rb plus the alias TAP/Aliases/5050. With --commit it
# commits in the tap, with --push it also pushes.

set -eu

here=$(cd "$(dirname "$0")" && pwd)
TEMPLATE=$here/fifty-cli.rb.in
FORMULA=fifty-cli
ALIAS=5050

VERSION=""
REPO=musakkka/5050cloud
CHECKSUMS=""
URL_BASE=""
TAP=""
COMMIT=""
PUSH=""
PUBKEY=""

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }
usage() {
	cat <<'EOF'
Usage: update-tap.sh --version vX.Y.Z --tap DIR [flags]

  --version vX.Y.Z      CLI release (required)
  --tap DIR             checkout of the tap repository (required unless --stdout)
  --stdout              print the formula instead of writing it
  --repo OWNER/NAME     repository whose releases hold the archives (default musakkka/5050cloud)
  --checksums FILE      use this checksums.txt instead of downloading it
  --url-base URL        base of the download URLs in the formula
                        (default https://github.com/OWNER/NAME/releases/download)
  --pubkey KEY          check checksums.txt.minisig with this minisign key (needs minisign)
  --commit              git commit the change in the tap
  --push                git push after committing (implies --commit)
EOF
}

STDOUT=""
need_arg() { if [ "$#" -lt 2 ] || [ -z "$2" ]; then die "$1 needs a value."; fi; }
while [ "$#" -gt 0 ]; do
	case "$1" in
	--version) need_arg "$@"; VERSION=$2; shift ;;
	--tap) need_arg "$@"; TAP=$2; shift ;;
	--stdout) STDOUT=1 ;;
	--repo) need_arg "$@"; REPO=$2; shift ;;
	--checksums) need_arg "$@"; CHECKSUMS=$2; shift ;;
	--url-base) need_arg "$@"; URL_BASE=$2; shift ;;
	--pubkey) need_arg "$@"; PUBKEY=$2; shift ;;
	--commit) COMMIT=1 ;;
	--push) COMMIT=1; PUSH=1 ;;
	--help | -h) usage; exit 0 ;;
	*) die "unknown flag $1 (see --help)." ;;
	esac
	shift
done

[ -n "$VERSION" ] || die "--version is required."
case "$VERSION" in v*) ;; *) VERSION=v$VERSION ;; esac
V=${VERSION#v}
printf '%s' "$V" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$' || die "$VERSION is not a vX.Y.Z version."
case "$REPO" in */*) ;; *) die "--repo takes OWNER/NAME." ;; esac
[ -n "$STDOUT" ] || [ -n "$TAP" ] || die "--tap DIR is required (or --stdout)."
URL_BASE=${URL_BASE:-https://github.com/$REPO/releases/download}
URL_BASE=${URL_BASE%/}
[ -f "$TEMPLATE" ] || die "$TEMPLATE is missing."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

fetch() {
	if have curl; then curl -fsSL --retry 3 -o "$2" "$1"
	elif have wget; then wget -q -O "$2" "$1"
	else die "curl or wget is needed."; fi
}

if [ -z "$CHECKSUMS" ]; then
	CHECKSUMS=$TMP/checksums.txt
	fetch "$URL_BASE/$VERSION/checksums.txt" "$CHECKSUMS" || die "cannot download $URL_BASE/$VERSION/checksums.txt."
	if [ -n "$PUBKEY" ]; then
		fetch "$URL_BASE/$VERSION/checksums.txt.minisig" "$CHECKSUMS.minisig" || die "cannot download the signature."
	fi
fi
[ -f "$CHECKSUMS" ] || die "$CHECKSUMS does not exist."
if [ -n "$PUBKEY" ]; then
	have minisign || die "--pubkey needs minisign on PATH."
	[ -f "$CHECKSUMS.minisig" ] || die "$CHECKSUMS.minisig is missing."
	minisign -Vq -P "$PUBKEY" -m "$CHECKSUMS" -x "$CHECKSUMS.minisig" >/dev/null || die "the checksums signature does not verify."
fi

sum_of() { # sum_of OS ARCH
	n="5050-cli_${V}_$1_$2.tar.gz"
	s=$(awk -v n="$n" '{f=$2; sub(/^\*/, "", f); if (f == n) print $1}' "$CHECKSUMS")
	printf '%s' "$s" | grep -Eq '^[0-9a-f]{64}$' || die "$n has no SHA-256 in $CHECKSUMS."
	printf '%s' "$s"
}
DA=$(sum_of darwin arm64)
DI=$(sum_of darwin amd64)
LA=$(sum_of linux arm64)
LI=$(sum_of linux amd64)

esc() { printf '%s' "$1" | sed 's/[|&\\]/\\&/g'; }
sed -e "s|@VERSION@|$(esc "$V")|g" \
	-e "s|@URL_BASE@|$(esc "$URL_BASE")|g" \
	-e "s|@SHA256_DARWIN_ARM64@|$DA|g" \
	-e "s|@SHA256_DARWIN_AMD64@|$DI|g" \
	-e "s|@SHA256_LINUX_ARM64@|$LA|g" \
	-e "s|@SHA256_LINUX_AMD64@|$LI|g" \
	"$TEMPLATE" >"$TMP/$FORMULA.rb"
if grep -q '@[A-Z0-9_]*@' "$TMP/$FORMULA.rb"; then
	die "the template has a placeholder this script does not fill."
fi

if [ -n "$STDOUT" ]; then
	cat "$TMP/$FORMULA.rb"
	exit 0
fi

[ -d "$TAP" ] || die "$TAP is not a directory."
mkdir -p "$TAP/Formula" "$TAP/Aliases"
cp "$TMP/$FORMULA.rb" "$TAP/Formula/$FORMULA.rb"
if [ ! -L "$TAP/Aliases/$ALIAS" ]; then
	rm -f "$TAP/Aliases/$ALIAS"
	ln -s "../Formula/$FORMULA.rb" "$TAP/Aliases/$ALIAS"
fi
echo "Wrote $TAP/Formula/$FORMULA.rb for $VERSION."

if [ -n "$COMMIT" ]; then
	git -C "$TAP" add "Formula/$FORMULA.rb" "Aliases/$ALIAS"
	if git -C "$TAP" diff --cached --quiet; then
		echo "The tap already has $VERSION; nothing to commit."
		exit 0
	fi
	git -C "$TAP" commit -q -m "$FORMULA $V"
	echo "Committed $FORMULA $V."
	if [ -n "$PUSH" ]; then
		git -C "$TAP" push -q
		echo "Pushed the tap."
	fi
fi
