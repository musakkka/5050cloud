#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs every packaging test. Needs a 5050cloud checkout to build the
# fixture release from (FIFTY_SRC, default the repository root, one level up from this
# directory), or a ready fixture in FIXTURE.
#   tests/run.sh                 everything
#   tests/run.sh lint action     only some suites (lint, install, action, tap, tap-linux, docker)
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC=${FIFTY_SRC:-$(cd "$ROOT/.." && pwd)}
suites=${*:-lint install action tap tap-linux docker}

if [ -z "${FIXTURE:-}" ]; then
	FIXTURE=$ROOT/.fixture
	if [ ! -f "$FIXTURE/latest.txt" ]; then
		sh "$ROOT/tests/make-fixture.sh" "$SRC" "$FIXTURE" || exit 1
	fi
fi
export FIXTURE

rc=0
for s in $suites; do
	case "$s" in
	lint) f=lint.sh ;;
	install) f=test-install-cli.sh ;;
	action) f=test-action.sh ;;
	tap) f=test-tap.sh ;;
	tap-linux) f=test-tap-linux.sh ;;
	docker) f=test-docker.sh ;;
	*) echo "unknown suite $s" >&2; exit 2 ;;
	esac
	echo "== $s"
	sh "$ROOT/tests/$f" || rc=1
done
exit "$rc"
