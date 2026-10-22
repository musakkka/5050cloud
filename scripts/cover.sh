#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# cover.sh: run the unit tests with coverage and fail when a package from
# the table in docs/SPEC.md 18.1 is under its floor. Packages that do not
# exist yet are skipped with a note, so the target works on the stub tree.
set -eu

cd "$(dirname "$0")/.."
mkdir -p bin/cover

FLOORS="
internal/cp/core/vars 85
internal/cp/core/changes 80
internal/cp/core/api 75
internal/cp/core/auth 85
internal/cp/core/github 80
internal/cp/core/imports 80
pkg/configfile 90
pkg/compose 85
internal/cp/orch/engine 80
internal/cp/orch/sched 85
internal/cp/orch/hub 75
internal/cp/orch/data 75
internal/agent/core 75
pkg/logfilter 90
internal/agent/build 75
internal/agent/netw 75
internal/upgrade 80
internal/cli 70
"

fail=0
echo "$FLOORS" | while read -r pkg floor; do
	[ -n "$pkg" ] || continue
	if ! ls "$pkg"/*_test.go >/dev/null 2>&1; then
		echo "skip  $pkg (no tests yet; floor $floor%)"
		continue
	fi
	out="bin/cover/$(echo "$pkg" | tr / _).out"
	go test -count=1 -coverprofile="$out" "./$pkg/..." >/dev/null
	pct=$(go tool cover -func="$out" | awk '/^total:/ {sub("%", "", $3); print $3}')
	if awk -v p="$pct" -v f="$floor" 'BEGIN { exit !(p + 0 < f + 0) }'; then
		echo "FAIL  $pkg $pct% (floor $floor%)"
		echo 1 >bin/cover/failed
	else
		echo "ok    $pkg $pct% (floor $floor%)"
	fi
done
if [ -f bin/cover/failed ]; then
	rm -f bin/cover/failed
	fail=1
fi
if [ "$fail" -ne 0 ]; then
	echo "Coverage is under the floor for at least one package (docs/SPEC.md 18.1)." >&2
	exit 1
fi
echo "Coverage floors hold for every package that has tests."
