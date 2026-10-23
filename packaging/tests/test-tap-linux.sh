#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Installs the formula with Homebrew on Linux inside the homebrew/brew
# image: renders a tap for the fixture release, taps it, then runs
# `brew style`, `brew audit`, `brew install musakkka/tap/5050` (the alias)
# and `brew test`. Nothing on this machine's Homebrew is touched.
#   FIXTURE=DIR tests/test-tap-linux.sh
set -u
# shellcheck source=packaging/tests/lib.sh
. "$(dirname "$0")/lib.sh"
FIXTURE=${FIXTURE:?set FIXTURE to a directory made by tests/make-fixture.sh}
VER=$(cat "$FIXTURE/latest.txt")
BREW_IMAGE=homebrew/brew:4.6.20

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
	echo "skip Linux Homebrew tests (no Docker)"
	exit 0
fi
W=$(mktemp -d)
cleanup() { kill "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; rm -rf "$W"; }
trap cleanup EXIT
serve "$FIXTURE" || exit 1

TAP=$W/homebrew-tap
mkdir -p "$TAP"
git -C "$TAP" init -q
git -C "$TAP" config user.name test
git -C "$TAP" config user.email test@example.com
git -C "$TAP" config commit.gpgsign false
check "render the tap" sh -c "sh '$REPO/packaging/homebrew/update-tap.sh' --version '$VER' --checksums '$FIXTURE/$VER/checksums.txt' --url-base 'http://host.docker.internal:$PORT' --tap '$TAP' --commit >/dev/null"

cat >"$W/in-container.sh" <<'EOF'
set -eu
export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1
cp -R /tap /home/linuxbrew/tap
git config --global --add safe.directory '*'
brew tap fiftytest/tap /home/linuxbrew/tap
if brew help trust >/dev/null 2>&1; then brew trust --tap fiftytest/tap >/dev/null; fi
echo "== style"; brew style fiftytest/tap/fifty-cli
echo "== audit"; brew audit --strict --formula fiftytest/tap/fifty-cli
echo "== install"; brew install fiftytest/tap/5050
echo "== test"; brew test fiftytest/tap/fifty-cli
echo "== run"; 5050 version
test -f "$(brew --prefix)/share/zsh/site-functions/_5050"
test -f "$(brew --prefix)/etc/bash_completion.d/5050"
echo "== done"
EOF
docker run --rm --add-host host.docker.internal:host-gateway \
	-v "$TAP:/tap:ro" -v "$W/in-container.sh:/run.sh:ro" "$BREW_IMAGE" bash /run.sh >"$W/brew.log" 2>&1
rc=$?; [ -n "${VERBOSE:-}" ] && cat "$W/brew.log"
if [ "$rc" -eq 0 ]; then ok "brew style, audit, install, test on Linux"; else tail -40 "$W/brew.log"; bad "brew style, audit, install, test on Linux"; fi
check "installed CLI reports $VER" grep -q "5050 $VER" "$W/brew.log"

finish
