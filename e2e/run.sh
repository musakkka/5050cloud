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
# Implemented now: E1 (fresh install), E21 (idempotent installer), E25 (custom
# domain through nip.io), E24 (uninstall). Every other E test is listed in the report as "not_run" with
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

# ---------------------------------------------------------------- E25 custom domain (nip.io)

# The install's base domain is <dashed ip>.sslip.io, so a name under nip.io is
# another registered domain: it takes the custom path for real (public DNS,
# the DNS check, ACME HTTP-01, the certificate push). E2E_CUSTOM_DOMAIN swaps
# in a real domain; the bad-address and Cloudflare steps then do not apply.
# Needs the console E1 installed, so it runs before the uninstall.

jget() { python3 -c 'import json,sys
d = json.load(sys.stdin)
for k in sys.argv[1].split("."):
	d = d[int(k)] if isinstance(d, list) else d.get(k)
print("" if d is None else d)' "$1"; }
# api METHOD PATH [JSON]: call the console API from the runner with the E25
# session, verifying the console certificate like E1 does. The body of the
# answer is followed by its HTTP status on the last line.
api() {
	cacert=""
	[ "$ACME_CA" != staging ] || cacert="--cacert $out/stg-root.pem"
	if [ -n "${3:-}" ]; then
		# shellcheck disable=SC2086
		curl -sS --max-time 30 $cacert -w '\n%{http_code}' -b "$out/E25.jar" -c "$out/E25.jar" -X "$1" -H 'Content-Type: application/json' -d "$3" "https://$console/api/v1$2"
	else
		# shellcheck disable=SC2086
		curl -sS --max-time 30 $cacert -w '\n%{http_code}' -b "$out/E25.jar" -c "$out/E25.jar" -X "$1" "https://$console/api/v1$2"
	fi
}
body() { sed '$d'; }
code() { tail -n 1; }
rand4() { od -An -N2 -tx1 /dev/urandom | tr -d ' \n'; }

start=$(now_ms)
why=""
token=${setup##*token=}
if [ -z "$setup" ] || [ -z "$token" ]; then
	record E25 not_run 0 "E1 printed no setup URL, so there is no console to add a domain to"
else
	IP=${HOST#*@}
	suffix=$(rand4)
	name=${E2E_CUSTOM_DOMAIN:-e25-$suffix.$IP.nip.io}
	bad=e25bad-$suffix.192.0.2.1.nip.io
	cf=e25cf-$suffix.104.16.0.1.nip.io
	t2=0; t6=0
	api POST /setup "{\"token\":\"$token\",\"email\":\"e2e@example.com\",\"name\":\"E2E\",\"password\":\"correct horse battery\"}" >"$out/E25.setup" 2>&1 || true
	[ "$(code <"$out/E25.setup")" = 200 ] || why="setup failed: $(cat "$out/E25.setup")"
	if [ -z "$why" ]; then
		api POST /projects '{"name":"e25"}' >"$out/E25.project" 2>&1 || true
		eid=$(body <"$out/E25.project" | jget environments.0.id 2>/dev/null || true)
		[ -n "$eid" ] || why="no project: $(cat "$out/E25.project")"
	fi
	if [ -z "$why" ]; then
		api POST "/environments/$eid/services" '{"name":"whoami","deployNow":true,"source":{"type":"image","image":"traefik/whoami"}}' >"$out/E25.service" 2>&1 || true
		sid=$(body <"$out/E25.service" | jget services.0.id 2>/dev/null || true)
		[ -n "$sid" ] || why="no service: $(cat "$out/E25.service")"
	fi
	if [ -z "$why" ]; then
		# 1. add the name
		api POST "/environments/$eid/services/$sid/domains" "{\"kind\":\"custom\",\"domain\":\"$name\",\"port\":80}" >"$out/E25.add" 2>&1 || true
		did=$(body <"$out/E25.add" | jget id 2>/dev/null || true)
		st=$(body <"$out/E25.add" | jget status 2>/dev/null || true)
		has_a=$(body <"$out/E25.add" | python3 -c 'import json,sys; print(any(r["type"]=="A" and r["value"]==sys.argv[1] for r in json.load(sys.stdin).get("dns") or []))' "$IP" 2>/dev/null || echo False)
		[ -n "$did" ] || why="domain not created: $(cat "$out/E25.add")"
		[ -n "$why" ] || { [ "$st" = pending_dns ] || [ "$st" = issuing ]; } || why="new domain status is $st"
		[ -n "$why" ] || [ -n "${E2E_CUSTOM_DOMAIN:-}" ] || [ "$has_a" = True ] || why="the DNS records lack an A record for $IP"
	fi
	if [ -z "$why" ]; then
		# 2. wait for Active
		s2=$(now_ms); n=0; st=""; exp=0
		while [ "$n" -lt 36 ]; do
			api POST "/domains/$did/check" >"$out/E25.status" 2>&1 || true
			st=$(body <"$out/E25.status" | jget status 2>/dev/null || true)
			[ "$st" != active ] || break
			[ "$st" != error ] || break
			n=$((n + 1)); sleep 5
		done
		t2=$(($(now_ms) - s2))
		exp=$(body <"$out/E25.status" | jget certExpiresAt 2>/dev/null || echo 0)
		[ "$st" = active ] || why="the domain is $st after ${t2} ms: $(body <"$out/E25.status" | jget message 2>/dev/null)"
		[ -n "$why" ] || [ "${exp:-0}" -gt $(( $(now_ms) + 60 * 86400000 )) ] || why="certificate expiry $exp is less than 60 days away"
	fi
	if [ -z "$why" ]; then
		# 3. a verified request
		cacert=""
		[ "$ACME_CA" != staging ] || cacert="--cacert $out/stg-root.pem"
		n=0; got=""
		while [ "$n" -lt 12 ]; do
			# shellcheck disable=SC2086
			got=$(curl -fsS --max-time 10 $cacert --resolve "$name:443:$IP" "https://$name/" 2>/dev/null || true)
			case "$got" in *Hostname*) break ;; esac
			n=$((n + 1)); sleep 5
		done
		case "$got" in *Hostname*) ;; *) why="https://$name/ did not return the whoami page" ;; esac
	fi
	if [ -z "$why" ] && [ -z "${E2E_CUSTOM_DOMAIN:-}" ]; then
		# 4. an address that is not ours stays pending, and no certificate is requested
		api POST "/environments/$eid/services/$sid/domains" "{\"kind\":\"custom\",\"domain\":\"$bad\",\"port\":80}" >"$out/E25.bad" 2>&1 || true
		bid=$(body <"$out/E25.bad" | jget id 2>/dev/null || true)
		api POST "/domains/$bid/check" >"$out/E25.bad" 2>&1 || true
		[ "$(body <"$out/E25.bad" | jget status 2>/dev/null)" = pending_dns ] || why="$bad is not pending_dns"
		case "$(body <"$out/E25.bad" | jget message 2>/dev/null)" in *192.0.2.1*) ;; *) [ -n "$why" ] || why="the message for $bad does not name 192.0.2.1" ;; esac
		orders=$(on "sudo docker logs \$(sudo docker ps -q --filter name=caddy | head -n1) 2>&1 | grep -c 'obtaining certificate.*$bad'" 2>/dev/null || true)
		[ "${orders:-0}" = 0 ] || why="Caddy placed ${orders} order(s) for $bad"
		# 5. a Cloudflare address
		if [ -z "$why" ]; then
			api POST "/environments/$eid/services/$sid/domains" "{\"kind\":\"custom\",\"domain\":\"$cf\",\"port\":80}" >"$out/E25.cf" 2>&1 || true
			cid=$(body <"$out/E25.cf" | jget id 2>/dev/null || true)
			api POST "/domains/$cid/check" >"$out/E25.cf" 2>&1 || true
			[ "$(body <"$out/E25.cf" | jget cloudflare 2>/dev/null)" = True ] || why="$cf was not flagged as Cloudflare"
			case "$(body <"$out/E25.cf" | jget message 2>/dev/null)" in *Cloudflare*) ;; *) [ -n "$why" ] || why="no Cloudflare message for $cf" ;; esac
		fi
	fi
	if [ -z "$why" ]; then
		# 6. retry
		s6=$(now_ms)
		api POST "/domains/$did/retry-certificate" >"$out/E25.retry" 2>&1 || true
		[ "$(code <"$out/E25.retry")" = 200 ] || why="retry failed: $(cat "$out/E25.retry")"
		n=0; st=""
		while [ -z "$why" ] && [ "$n" -lt 12 ]; do
			api POST "/domains/$did/check" >"$out/E25.status" 2>&1 || true
			st=$(body <"$out/E25.status" | jget status 2>/dev/null || true)
			[ "$st" != active ] || break
			n=$((n + 1)); sleep 5
		done
		t6=$(($(now_ms) - s6))
		[ -n "$why" ] || [ "$st" = active ] || why="after a retry the domain is $st"
	fi
	if [ -z "$why" ]; then
		# 7. delete: the name gets the styled 404 page within 10 s
		api DELETE "/domains/$did" >/dev/null 2>&1 || true
		n=0; status404=""
		while [ "$n" -lt 5 ]; do
			sleep 2
			status404=$(curl -sS --max-time 10 -k -o /dev/null -w '%{http_code}' --resolve "$name:443:$IP" "https://$name/" 2>/dev/null || true)
			[ "$status404" != 404 ] || break
			n=$((n + 1))
		done
		[ "$status404" = 404 ] || why="after delete https://$name/ answered ${status404:-nothing}, want 404"
	fi
	if [ -z "$why" ]; then
		# 8. the console's own name is not available to a service
		api POST "/environments/$eid/services/$sid/domains" "{\"kind\":\"custom\",\"domain\":\"$console\",\"port\":80}" >"$out/E25.console" 2>&1 || true
		[ "$(code <"$out/E25.console")" = 422 ] || why="the console name was accepted as a custom domain: $(cat "$out/E25.console")"
	fi
	rm -f "$out/E25.jar"
	if [ -z "$why" ]; then record E25 pass "$start" "custom domain $name active in ${t2} ms, retried in ${t6} ms, deleted, console name refused"
	else record E25 fail "$start" "$why (files: $out/E25.*)"; fi
fi

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
	case "$i" in 21 | 24 | 25) ;; *) record "E$i" not_run 0 "not automated in this harness yet; see e2e/README.md" ;; esac
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
