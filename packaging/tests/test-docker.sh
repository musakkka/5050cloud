#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Builds packaging/docker/Dockerfile.cli against the fixture release (served
# to the build over host.docker.internal) and runs the image.
#   FIXTURE=DIR tests/test-docker.sh
set -u
# shellcheck source=packaging/tests/lib.sh
. "$(dirname "$0")/lib.sh"
FIXTURE=${FIXTURE:?set FIXTURE to a directory made by tests/make-fixture.sh}
VER=$(cat "$FIXTURE/latest.txt")
KEY=$(cat "$FIXTURE/test.pub")
IMAGE=fifty-cli-test:local

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
	echo "skip docker tests (no Docker)"
	exit 0
fi
cleanup() { kill "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; docker rmi -f "$IMAGE" >/dev/null 2>&1; }
trap cleanup EXIT
serve "$FIXTURE" || exit 1

build() {
	docker build -q -f "$REPO/packaging/docker/Dockerfile.cli" \
		--add-host host.docker.internal:host-gateway \
		--build-arg RELEASE_BASE="http://host.docker.internal:$PORT" \
		--build-arg "$@" -t "$IMAGE" "$REPO" >"$ROOT/.docker.log" 2>&1
}
check "image builds from the signed release" build RELEASE_PUBKEY="$KEY"
[ "$FAIL" -eq 0 ] || cat "$ROOT/.docker.log"
check "image runs 5050 version" sh -c "docker run --rm '$IMAGE' version | grep -q '$VER'"
check "image runs as a non-root user" sh -c "[ \"\$(docker run --rm --entrypoint id '$IMAGE' -u)\" = 10001 ]"
check "railway alias in the image" sh -c "docker run --rm --entrypoint railway '$IMAGE' --help >/dev/null 2>&1"
check "default command prints help" sh -c "docker run --rm '$IMAGE' | grep -q 'Usage'"
check "image has no curl" sh -c "! docker run --rm --entrypoint sh '$IMAGE' -c 'command -v curl'"
if build RELEASE_PUBKEY="$(cat "$FIXTURE/other.pub")"; then bad "build with the wrong key fails"; else ok "build with the wrong key fails"; fi
rm -f "$ROOT/.docker.log"

finish
