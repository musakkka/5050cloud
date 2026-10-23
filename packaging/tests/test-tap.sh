#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Renders the Homebrew formula from a fixture release with
# packaging/homebrew/update-tap.sh, then (when brew is installed) runs
# `brew style` and `brew audit` on it and installs it from a throwaway local
# tap served over HTTP. The tap is removed at the end.
#   FIXTURE=DIR tests/test-tap.sh
# The brew steps run only with BREW_HOST=1: `brew style` and `brew audit`
# turn on Homebrew developer mode and can tap homebrew/core on this machine.
# tests/test-tap-linux.sh runs the same steps in a container instead.
set -u
# shellcheck source=packaging/tests/lib.sh
. "$(dirname "$0")/lib.sh"
FIXTURE=${FIXTURE:?set FIXTURE to a directory made by tests/make-fixture.sh}
VER=$(cat "$FIXTURE/latest.txt")
UPD=$REPO/packaging/homebrew/update-tap.sh
TAPNAME=fiftytest/tap

W=$(mktemp -d)
cleanup() {
	kill "$SERVER_PID" 2>/dev/null
	wait "$SERVER_PID" 2>/dev/null
	if [ "${BREW_HOST:-}" = 1 ] && command -v brew >/dev/null 2>&1; then
		brew uninstall --force fifty-cli >/dev/null 2>&1
		brew untap "$TAPNAME" >/dev/null 2>&1
		if [ "${was_dev:-1}" = 0 ]; then brew developer off >/dev/null 2>&1; fi
	fi
	rm -rf "$W"
}
trap cleanup EXIT
serve "$FIXTURE" || exit 1
BASE=http://127.0.0.1:$PORT

if sh "$UPD" --tap "$W" >/dev/null 2>&1; then bad "--version is required"; else ok "--version is required"; fi
if sh "$UPD" --version banana --stdout >/dev/null 2>&1; then bad "bad version refused"; else ok "bad version refused"; fi

check "render to stdout from local checksums" sh -c "sh '$UPD' --version '$VER' --checksums '$FIXTURE/$VER/checksums.txt' --stdout >'$W/stdout.rb'"
check "no placeholders left" sh -c "! grep -q '@[A-Z0-9_]*@' '$W/stdout.rb'"
check "default URL is the GitHub release" grep -q 'url "https://github.com/musakkka/5050cloud/releases/download/v#{version}/5050-cli_#{version}_darwin_arm64.tar.gz"' "$W/stdout.rb"
check "version filled" grep -q "version \"${VER#v}\"" "$W/stdout.rb"
for t in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
	sum=$(awk -v n="5050-cli_${VER#v}_$t.tar.gz" '$2 == n {print $1}' "$FIXTURE/$VER/checksums.txt")
	check "sha256 for $t" grep -q "sha256 \"$sum\"" "$W/stdout.rb"
done
check "--repo changes the URL" sh -c "sh '$UPD' --version '$VER' --checksums '$FIXTURE/$VER/checksums.txt' --repo someone/fork --stdout | grep -q 'github.com/someone/fork/releases'"

grep -v linux_arm64 "$FIXTURE/$VER/checksums.txt" >"$W/partial.txt"
if sh "$UPD" --version "$VER" --checksums "$W/partial.txt" --stdout >/dev/null 2>&1; then
	bad "missing archive checksum is refused"
else
	ok "missing archive checksum is refused"
fi

# A tap checkout with git, written and committed by the script.
TAP=$W/homebrew-tap
mkdir -p "$TAP"
git -C "$TAP" init -q
git -C "$TAP" config user.name test
git -C "$TAP" config user.email test@example.com
git -C "$TAP" config commit.gpgsign false
check "write tap and commit (download checksums)" sh "$UPD" --version "$VER" --url-base "$BASE" --tap "$TAP" --commit
check "formula file" test -f "$TAP/Formula/fifty-cli.rb"
check "alias 5050" sh -c "[ \"\$(readlink '$TAP/Aliases/5050')\" = ../Formula/fifty-cli.rb ]"
check "commit made" sh -c "git -C '$TAP' log -1 --format=%s | grep -qx 'fifty-cli ${VER#v}'"
check "second run is a no-op" sh -c "sh '$UPD' --version '$VER' --url-base '$BASE' --tap '$TAP' --commit | grep -q 'nothing to commit'"

if [ "${BREW_HOST:-}" != 1 ] || ! command -v brew >/dev/null 2>&1; then
	echo "skip host brew style, audit and install (set BREW_HOST=1; test-tap-linux.sh covers them)"
	finish; exit $?
fi
was_dev=$(brew developer 2>/dev/null | grep -c 'is enabled')
check "ruby syntax" sh -c "ruby -c '$TAP/Formula/fifty-cli.rb' >/dev/null 2>&1"

# Keep Homebrew's trust list for the throwaway tap inside the work directory.
export XDG_CONFIG_HOME="$W/xdg"
mkdir -p "$XDG_CONFIG_HOME"
brew untap "$TAPNAME" >/dev/null 2>&1
check "brew tap local" sh -c "HOMEBREW_NO_AUTO_UPDATE=1 brew tap '$TAPNAME' '$TAP' >'$W/tap.log' 2>&1"
# Homebrew 7 refuses formulae from taps nobody trusted yet.
if brew help trust >/dev/null 2>&1; then
	check "brew trust the test tap" sh -c "brew trust --tap '$TAPNAME' >/dev/null 2>&1"
fi
check "brew style" sh -c "HOMEBREW_NO_AUTO_UPDATE=1 brew style '$TAPNAME/fifty-cli' >'$W/style.log' 2>&1 || { cat '$W/style.log'; false; }"
# --strict --online would also fetch the GitHub URLs, which do not exist yet.
if HOMEBREW_NO_AUTO_UPDATE=1 brew audit --formula "$TAPNAME/fifty-cli" >"$W/audit.log" 2>&1; then
	ok "brew audit"
elif grep -q "Command Line Tools" "$W/audit.log"; then
	echo "skip brew audit (this machine's Command Line Tools are out of date)"
else
	cat "$W/audit.log"; bad "brew audit"
fi

if [ "${BREW_INSTALL:-1}" = 1 ]; then
	if HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 brew install "$TAPNAME/5050" >"$W/install.log" 2>&1; then
		ok "brew install through the 5050 alias"
		check "brew test" sh -c "HOMEBREW_NO_AUTO_UPDATE=1 brew test '$TAPNAME/fifty-cli' >'$W/test.log' 2>&1 || { tail -30 '$W/test.log'; false; }"
		check "installed 5050 runs" sh -c "\"\$(brew --prefix)/opt/fifty-cli/bin/5050\" version | grep -q '$VER'"
		check "zsh completion installed" sh -c "test -f \"\$(brew --prefix)/opt/fifty-cli/share/zsh/site-functions/_5050\""
	elif grep -q "Command Line Tools" "$W/install.log"; then
		echo "skip brew install (this machine's Command Line Tools are out of date; tests/test-tap-linux.sh covers it)"
	else
		tail -30 "$W/install.log"; bad "brew install through the 5050 alias"
	fi
fi

finish
