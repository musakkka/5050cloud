#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs ShellCheck, actionlint and hadolint over the packaging files. Uses the
# tools from PATH when present, else their pinned Docker images. Also checks
# the public writing rules (no em dashes, no forbidden words, denylist when given).
#   DENYLIST=path/to/denylist.txt packaging/tests/lint.sh
set -u
# shellcheck source=packaging/tests/lib.sh
. "$(dirname "$0")/lib.sh"

SHELLCHECK_IMAGE=koalaman/shellcheck:v0.10.0
ACTIONLINT_IMAGE=rhysd/actionlint:1.7.7
HADOLINT_IMAGE=hadolint/hadolint:v2.12.0-debian

tool() { # tool NAME IMAGE ARGS...: runs NAME from PATH or IMAGE with REPO mounted at /src
	name=$1; image=$2; shift 2
	if command -v "$name" >/dev/null 2>&1; then
		(cd "$REPO" && "$name" "$@")
	elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
		docker run --rm -v "$REPO:/src:ro" -w /src --entrypoint "$name" "$image" "$@"
	else
		echo "skip $name (not installed and no Docker)"
		return 0
	fi
}

check "shellcheck (POSIX sh)" tool shellcheck "$SHELLCHECK_IMAGE" -x -s sh \
	install/install-cli.sh packaging/homebrew/update-tap.sh \
	packaging/tests/make-fixture.sh packaging/tests/test-install-cli.sh packaging/tests/test-action.sh packaging/tests/test-tap.sh \
	packaging/tests/test-docker.sh packaging/tests/test-tap-linux.sh packaging/tests/lint.sh packaging/tests/run.sh packaging/tests/lib.sh
check "shellcheck (bash)" tool shellcheck "$SHELLCHECK_IMAGE" -s bash packaging/action/run.sh

# actionlint checks workflows. Lay out a throwaway repository where the
# example workflow uses the action from ./ so its inputs are checked too.
L=$ROOT/.lint
rm -rf "$L"
mkdir -p "$L/.github/workflows"
cp "$REPO/action.yml" "$L/action.yml"
cp "$REPO/.github/workflows/cli-dist.yml" "$L/.github/workflows/"
sed 's|uses: musakkka/5050cloud@v0.1.0|uses: ./|' "$REPO/packaging/action/example-deploy.yml" >"$L/.github/workflows/example-deploy.yml"
check "actionlint" tool actionlint "$ACTIONLINT_IMAGE" packaging/.lint/.github/workflows/cli-dist.yml packaging/.lint/.github/workflows/example-deploy.yml
check "action.yml parses with the expected inputs" python3 -I -c '
import sys, re
s = open(sys.argv[1]).read()
for k in ["token", "url", "project", "environment", "service", "path", "detach", "cli-version"]:
    assert re.search(r"^  " + re.escape(k) + r":$", s, re.M), k
assert "using: composite" in s
' "$REPO/action.yml"
rm -rf "$L"

check "hadolint" tool hadolint "$HADOLINT_IMAGE" packaging/docker/Dockerfile.cli

# Writing rules for public files.
emdash=$(printf '\342\200\224')
check "no em dashes" sh -c "! grep -rn '$emdash' '$REPO/install/install-cli.sh' '$REPO/action.yml' '$REPO/packaging'"
check "no forbidden word" sh -c "! grep -rniw 'b[e]ts' '$REPO/install/install-cli.sh' '$REPO/action.yml' '$REPO/packaging'"
if [ -n "${DENYLIST:-}" ] && [ -f "$DENYLIST" ]; then
	DENY=$(mktemp)
	grep -v '^#' "$DENYLIST" | sed '/^$/d' >"$DENY"
	check "denylist" sh -c "! grep -rIniF --exclude-dir=.fixture -f '$DENY' '$REPO/install/install-cli.sh' '$REPO/action.yml' '$REPO/packaging'"
	rm -f "$DENY"
fi

finish
