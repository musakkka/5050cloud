#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# VM end-to-end suite (docs/SPEC.md 18.2). Runs over SSH against a fresh
# Ubuntu 24.04 VM and writes e2e/out/<ts>/report.json.
#
#   e2e/run.sh --host ubuntu@IP [--host2 ubuntu@IP2] [--dist DIR]
#              [--release-pubkey KEY] [--acme-ca staging|production] [--suite single|all]
#
# The release under test comes from DIR (default dist/, the output of
# `make release-snapshot`). It is copied to the VM and served there over
# HTTP on 127.0.0.1, so the installer runs through --release-base and checks
# the minisign signature and SHA-256 exactly as for a real release.
#
# Implemented now: E1 (fresh install), E21 (idempotent installer), E24
# (uninstall). Every other E test is listed in the report as "not_run" with
# the reason; README.md in this directory tracks them.

set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
HOST=""; HOST2=""; DIST=$root/dist; PUBKEY=""; ACME_CA=staging; SUITE=single
while [ "$#" -gt 0 ]; do
	case "$1" in
	--host) HOST=$2; shift ;;
	--host2) HOST2=$2; shift ;;
	--dist) DIST=$2; shift ;;
	--release-pubkey) PUBKEY=$2; shift ;;
	--acme-ca) ACME_CA=$2; shift ;;
	--suite) SUITE=$2; shift ;;
	-h | --help) sed -n 4,17p "$0"; exit 0 ;;
	*) echo "unknown flag $1" >&2; exit 2 ;;
	esac
	shift
done
[ -n "$HOST" ] || { echo "--host user@ip is required" >&2; exit 2; }
[ -n "$PUBKEY" ] || PUBKEY=$(sed -n 2p "$root/e2e/keys/test.pub")
[ -f "$DIST/checksums.txt" ] || { echo "$DIST/checksums.txt is missing; run make release-snapshot first." >&2; exit 2; }
: "${HOST2:=}"

version=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$DIST/metadata.json" 2>/dev/null | head -n1)
[ -n "$version" ] || { echo "cannot read the version from $DIST/metadata.json" >&2; exit 2; }
tag=v${version#v}

ts=$(date -u +%Y%m%dT%H%M%SZ)
out=$root/e2e/out/$ts
mkdir -p "$out"
results=$out/results.jsonl
: >"$results"
failed=0

SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15"
# shellcheck disable=SC2086,SC2029
on() { ssh $SSH_OPTS "$HOST" "$@"; }
now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
jstr() { printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'; }

record() { # record ID pass|fail|not_run START_MS EVIDENCE
	end=$(now_ms)
	dur=0
	[ "$3" = not_run ] || dur=$((end - $3))
	printf '{"id":"%s","status":"%s","durationMs":%s,"evidence":%s}\n' "$1" "$2" "$dur" "$(jstr "$4")" >>"$results"
	printf '%-4s %-8s %s\n' "$1" "$2" "$(printf '%s' "$4" | head -n1 | cut -c1-120)"
	[ "$2" != fail ] || failed=1
}

echo "Release $tag from $DIST, VM $HOST, suite $SUITE"

# ---------------------------------------------------------------- serve the release on the VM

on "rm -rf /tmp/5050-rel && mkdir -p /tmp/5050-rel/$tag"
# shellcheck disable=SC2086
scp $SSH_OPTS -q "$DIST"/checksums.txt "$DIST"/checksums.txt.minisig "$DIST"/5050_*_linux_*.tar.gz "$HOST:/tmp/5050-rel/$tag/"
# shellcheck disable=SC2086
scp $SSH_OPTS -q "$root/install/install.sh" "$HOST:/tmp/5050-rel/$tag/install.sh"
on "echo $tag > /tmp/5050-rel/latest.txt; cd /tmp/5050-rel && (nohup python3 -m http.server 8099 --bind 127.0.0.1 >/dev/null 2>&1 &) ; sleep 1"
BASE=http://127.0.0.1:8099
INSTALL="curl -fsSL $BASE/$tag/install.sh | sudo sh -s -- --release-base $BASE --release-pubkey $PUBKEY --acme-ca $ACME_CA"

# shellcheck disable=SC2016
vm_os=$(on '. /etc/os-release; echo "$PRETTY_NAME"' || echo unknown)
# shellcheck disable=SC2016
vm_sku=${E2E_VM_SKU:-$(on 'echo "$(nproc) vCPU, $(free -m | awk "/^Mem:/ {print \$2}") MB RAM"' || echo unknown)}

# ---------------------------------------------------------------- E1 fresh install

start=$(now_ms)
if on "$INSTALL" >"$out/E1.log" 2>&1; then rc=0; else rc=$?; fi
dur=$(($(now_ms) - start))
setup=$(sed -n 's/^ *\(https:\/\/[^ ]*\/setup?token=[^ ]*\).*/\1/p' "$out/E1.log" | head -n1)
console=$(printf '%s' "$setup" | sed -n 's|https://\([^/]*\)/.*|\1|p')
why=""
[ "$rc" = 0 ] || why="installer exited $rc"
[ -n "$why" ] || [ "$dur" -lt 180000 ] || why="took ${dur} ms (limit 180000)"
[ -n "$why" ] || [ -n "$setup" ] || why="no setup URL printed"
[ -n "$why" ] || ! grep -q '5050R-' "$out/E1.log" || why="the recovery key was printed without a TTY"
if [ -z "$why" ]; then
	mode=$(on "sudo stat -c %a /etc/5050/recovery-key.txt" 2>/dev/null || echo missing)
	[ "$mode" = 600 ] || why="recovery key file mode is $mode, want 600"
fi
if [ -z "$why" ]; then
	if [ "$ACME_CA" = staging ]; then
		curl -fsSL -o "$out/stg-root.pem" https://letsencrypt.org/certs/staging/letsencrypt-stg-root-x1.pem
		curl -fsS --max-time 10 --cacert "$out/stg-root.pem" "https://$console/readyz" >"$out/E1.readyz" 2>&1 || why="console certificate did not verify against the staging root"
	else
		curl -fsS --max-time 10 "https://$console/readyz" >"$out/E1.readyz" 2>&1 || why="console certificate did not verify"
	fi
fi
if [ -z "$why" ]; then record E1 pass "$start" "installed in ${dur} ms; console https://$console verified"
else record E1 fail "$start" "$why (log: $out/E1.log)"; fi

# ---------------------------------------------------------------- E21 idempotent installer

start=$(now_ms)
starts='sudo docker ps -q | xargs -r sudo docker inspect -f "{{.Name}} {{.State.StartedAt}}" | sort'
before=$(on "$starts" || true)
if on "$INSTALL" >"$out/E21.log" 2>&1; then rc=0; else rc=$?; fi
after=$(on "$starts" || true)
if [ "$rc" != 0 ]; then record E21 fail "$start" "second install exited $rc (log: $out/E21.log)"
elif [ "$before" != "$after" ]; then record E21 fail "$start" "containers restarted: before=[$before] after=[$after]"
else record E21 pass "$start" "re-run exit 0; $(printf '%s\n' "$before" | grep -c . || true) containers kept their start times"; fi

# ---------------------------------------------------------------- E24 uninstall

start=$(now_ms)
why=""
on "curl -fsSL $BASE/$tag/install.sh | sudo sh -s -- --uninstall" >"$out/E24.log" 2>&1 || why="--uninstall failed"
if [ -z "$why" ]; then
	left=$(on 'ls /etc/systemd/system/5050d.service /etc/systemd/system/5050-agent.service /usr/local/bin/5050d /opt/5050 2>/dev/null' || true)
	[ -z "$left" ] || why="left behind: $left"
fi
if [ -z "$why" ]; then
	on "curl -fsSL $BASE/$tag/install.sh | sudo sh -s -- --uninstall --purge --yes" >>"$out/E24.log" 2>&1 || why="--uninstall --purge failed"
fi
if [ -z "$why" ]; then
	cont=$(on "sudo docker ps -a --format '{{.Names}}' | grep '^5050-' || true")
	[ -z "$cont" ] || why="5050 containers left after purge: $cont"
fi
if [ -z "$why" ]; then record E24 pass "$start" "units and binaries removed; purge left no 5050 containers"
else record E24 fail "$start" "$why (log: $out/E24.log)"; fi

# ---------------------------------------------------------------- not yet automated

i=2
while [ "$i" -le 40 ]; do
	case "$i" in 21 | 24) ;; *) record "E$i" not_run 0 "not automated in this harness yet; see e2e/README.md" ;; esac
	i=$((i + 1))
done

on "pkill -f 'http.server 8099' || true; rm -rf /tmp/5050-rel" >/dev/null 2>&1 || true

{
	printf '{"version":"%s","suite":"%s","vmSku":%s,"os":%s,"acmeCa":"%s","at":"%s","host2":%s,"tests":[' \
		"$tag" "$SUITE" "$(jstr "$vm_sku")" "$(jstr "$vm_os")" "$ACME_CA" "$ts" "$(jstr "$HOST2")"
	paste -sd, "$results"
	printf ']}\n'
} >"$out/report.json"
rm -f "$results"
echo "Report: $out/report.json"
exit "$failed"
