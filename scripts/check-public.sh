#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# check-public.sh: what must hold before anything in this tree is public
# (docs/SPEC.md 17.1 and 21). It checks the files git would publish (tracked
# plus untracked files that are not ignored), never reference/ or
# node_modules/.
#
#   DENYLIST=reference/denylist.txt  fixed strings that must never appear (default: that file when present)
#   STRICT_STUBS=1                   fail on any CONTRACT-STUB marker (release and V6); default: report them
#   GITLEAKS=path                    gitleaks binary (default: bin/tools/gitleaks, else on PATH)
#
# Written for bash 3.2 (macOS) and later.
set -euo pipefail

cd "$(dirname "$0")/.."
root=$(pwd)
failures=0
fail() {
	echo "FAIL  $*" >&2
	failures=$((failures + 1))
}
ok() { echo "ok    $*"; }

list=$(mktemp)
trap 'rm -f "$list"' EXIT
if git rev-parse --git-dir >/dev/null 2>&1; then
	git ls-files -z --cached --others --exclude-standard >"$list"
else
	find . -type f -not -path './.git/*' -not -path '*/node_modules/*' -not -path './reference/*' -print0 >"$list"
fi

# files PATTERN... prints NUL-separated paths matching any shell pattern,
# skipping files that no longer exist.
files() {
	local f p
	while IFS= read -r -d '' f; do
		f=${f#./}
		[ -f "$f" ] || continue
		for p in "$@"; do
			# shellcheck disable=SC2053 # glob match on purpose
			if [[ $f == $p ]]; then
				printf '%s\0' "$f"
				break
			fi
		done
	done <"$list"
}

# Generated, vendored and lock files are skipped by the text checks.
is_skipped() {
	case "$1" in
	*/package-lock.json | go.sum | */mockServiceWorker.js | *.png | *.jpg | *.ico | *.woff2 | *.gz | docs/research/*) return 0 ;;
	esac
	return 1
}

text_files() {
	local f
	while IFS= read -r -d '' f; do
		is_skipped "$f" || printf '%s\0' "$f"
	done < <(files '*')
}

# without PATH drops one path from a NUL-separated list on stdin.
without() {
	local f
	while IFS= read -r -d '' f; do
		[ "$f" = "$1" ] || printf '%s\0' "$f"
	done
}

# --- 1. secrets (gitleaks over the publishable files, and over history when there is any)
gitleaks=${GITLEAKS:-}
if [ -z "$gitleaks" ]; then
	if [ -x bin/tools/gitleaks ]; then gitleaks=bin/tools/gitleaks; else gitleaks=$(command -v gitleaks || true); fi
fi
if [ -z "$gitleaks" ]; then
	fail "gitleaks is not installed. Run make tools."
else
	stage=$(mktemp -d)
	(files '*' | xargs -0 -I{} rsync -R "{}" "$stage/") >/dev/null 2>&1 || true
	if "$gitleaks" dir --no-banner --redact --exit-code 1 --config .gitleaks.toml "$stage" >/dev/null 2>&1; then
		ok "gitleaks found no secrets in the working tree"
	else
		fail "gitleaks found a possible secret in the working tree. Run: $gitleaks dir --redact -v --config .gitleaks.toml ."
	fi
	if git rev-parse HEAD >/dev/null 2>&1; then
		if "$gitleaks" git --no-banner --redact --exit-code 1 --config .gitleaks.toml . >/dev/null 2>&1; then
			ok "gitleaks found no secrets in git history"
		else
			fail "gitleaks found a possible secret in git history. Run: $gitleaks git --redact -v --config .gitleaks.toml ."
		fi
	fi
	rm -rf "$stage"
fi

# --- 2. deny list of private strings
deny=${DENYLIST:-}
if [ -z "$deny" ] && [ -f reference/denylist.txt ]; then deny=reference/denylist.txt; fi
if [ -n "$deny" ]; then
	patterns=$(mktemp)
	grep -v '^[[:space:]]*#' "$deny" | sed '/^[[:space:]]*$/d' >"$patterns"
	hits=$(text_files | xargs -0 grep -n -i -F -f "$patterns" -- 2>/dev/null || true)
	rm -f "$patterns"
	if [ -n "$hits" ]; then
		fail "private strings from $deny appear in the tree:"
		echo "$hits" | cut -c1-160 >&2
	else
		ok "no string from $deny appears"
	fi
else
	echo "note  no deny list given (DENYLIST) and reference/denylist.txt is absent; skipped"
fi

# --- 3. private IP addresses (RFC 1918) outside the documented 10.128.0.0/9 pod range
ipre='\b(10\.([0-9]|[1-9][0-9]|1[01][0-9]|12[0-7])\.[0-9]{1,3}\.[0-9]{1,3}|172\.(1[6-9]|2[0-9]|3[01])\.[0-9]{1,3}\.[0-9]{1,3}|192\.168\.[0-9]{1,3}\.[0-9]{1,3})\b'
# Tests and their golden files use private ranges on purpose (firewall, IPAM
# and proxy cases), so only shipped code, docs and UI copy are checked.
hits=$(text_files | xargs -0 grep -n -E "$ipre" -- 2>/dev/null | grep -v 'check-public: allow' |
	grep -v -E '^[^:]*(_test\.go|\.test\.tsx?|/testdata/[^:]*):' || true)
if [ -n "$hits" ]; then
	fail "private IP addresses outside 10.128.0.0/9 (use 203.0.113.0/24 or 10.128.0.0/9 in examples; mark a deliberate one with 'check-public: allow'):"
	echo "$hits" | cut -c1-160 >&2
else
	ok "no private IP addresses outside the documented pod range"
fi

# --- 4. em dashes in docs, UI copy and comments
hits=$(text_files | xargs -0 grep -n $'\xe2\x80\x94' -- 2>/dev/null || true)
if [ -n "$hits" ]; then
	fail "em dashes (write a comma, colon or two sentences instead):"
	echo "$hits" | cut -c1-160 >&2
else
	ok "no em dashes"
fi

# --- 5. banned words (e2e/banned-words.txt, one per line, whole words, any case)
if [ -f e2e/banned-words.txt ]; then
	words=$(grep -v '^[[:space:]]*#' e2e/banned-words.txt | sed '/^[[:space:]]*$/d' | paste -sd'|' -)
	if [ -n "$words" ]; then
		hits=$(text_files | without e2e/banned-words.txt | xargs -0 grep -n -i -w -E "($words)" -- 2>/dev/null || true)
		if [ -n "$hits" ]; then
			fail "banned words from e2e/banned-words.txt:"
			echo "$hits" | cut -c1-160 >&2
		else
			ok "no banned words"
		fi
	fi
else
	fail "e2e/banned-words.txt is missing"
fi

# --- 6. SPDX headers on source files (within the first 5 lines)
missing=""
while IFS= read -r -d '' f; do
	is_skipped "$f" && continue
	if ! head -n 5 "$f" | grep -q 'SPDX-License-Identifier:'; then
		missing="$missing
$f"
	fi
done < <(files '*.go' '*.ts' '*.tsx' '*.js' '*.mjs' '*.cjs' '*.sh' '*.css')
if [ -n "$missing" ]; then
	fail "source files without an SPDX-License-Identifier line:$missing"
else
	ok "every source file has an SPDX line"
fi

# --- 7. CLI license boundary (docs/SPEC.md 5.2)
if [ -d cmd/5050 ]; then
	bad=$(go list -deps ./cmd/5050 | grep -E '/internal/(cp|agent|upgrade)|/web$' || true)
	if [ -n "$bad" ]; then
		fail "the MIT CLI imports AGPL packages:
$bad"
	else
		ok "the CLI imports nothing from internal/cp, internal/agent, internal/upgrade or web"
	fi
else
	echo "note  cmd/5050 does not exist yet; license boundary check skipped"
fi

# --- 8. contract stubs
stubs=$(files '*.go' '*.ts' '*.tsx' | xargs -0 grep -l 'CONTRACT-STUB' -- 2>/dev/null | grep -v '^scripts/' || true)
if [ -n "$stubs" ]; then
	if [ "${STRICT_STUBS:-0}" = 1 ]; then
		fail "CONTRACT-STUB markers remain (a release has none):
$stubs"
	else
		echo "note  $(echo "$stubs" | wc -l | tr -d ' ') files still carry CONTRACT-STUB (fails with STRICT_STUBS=1):"
		while IFS= read -r f; do echo "      $f"; done <<<"$stubs"
	fi
else
	ok "no CONTRACT-STUB markers"
fi

# --- 9. contract hashes
if sh scripts/contracts.sh check >/dev/null; then
	ok "contract hashes match docs/contracts.sha256"
else
	sh scripts/contracts.sh check || true
	fail "contract hashes"
fi

if [ "$failures" -gt 0 ]; then
	echo "check-public: $failures check(s) failed in $root." >&2
	exit 1
fi
echo "check-public: every check passed."
