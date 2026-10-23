# SPDX-License-Identifier: MIT
# Shared helpers for the packaging tests. Sourced, not run.
# shellcheck disable=SC2034 # REPO and SERVER_PID are used by the scripts that source this.

ROOT=$(cd "$(dirname "$0")/.." && pwd)
REPO=$(cd "$ROOT/.." && pwd)
PASS=0
FAIL=0

ok() { PASS=$((PASS + 1)); printf 'ok   %s\n' "$*"; }
bad() { FAIL=$((FAIL + 1)); printf 'FAIL %s\n' "$*"; }
check() { # check "name" cmd...
	name=$1; shift
	if "$@"; then ok "$name"; else bad "$name"; fi
}
finish() {
	printf '%s: %d passed, %d failed\n' "$(basename "$0")" "$PASS" "$FAIL"
	[ "$FAIL" -eq 0 ]
}

# serve DIR: starts a local HTTP server for DIR, sets PORT and SERVER_PID.
serve() {
	PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
	python3 -I -m http.server "$PORT" --bind 127.0.0.1 --directory "$1" >/dev/null 2>&1 &
	SERVER_PID=$!
	i=0
	while ! curl -fs "http://127.0.0.1:$PORT/" >/dev/null 2>&1; do
		i=$((i + 1)); [ "$i" -lt 50 ] || { echo "server did not start" >&2; return 1; }
		sleep 0.1
	done
}
