#!/bin/sh
# SPDX-License-Identifier: MIT
#
# backup-redis: runs inside the Redis container. It asks for a BGSAVE, waits
# until LASTSAVE changes and no background save is running, copies the
# completed dump.rdb to a temporary file (never the live AOF directory) and
# backs that file up with restic. Prints {"ok":true,"sizeBytes":N,"id":"..."}.
set -eu
. /opt/5050/bin/restic-env

REDISCLI_AUTH=${REDIS_PASSWORD:-${REDISPASSWORD:-}}
export REDISCLI_AUTH
cli() { redis-cli -h 127.0.0.1 -p 6379 --no-auth-warning "$@"; }

before=$(cli LASTSAVE)
reply=$(cli BGSAVE)
case "$reply" in
*"Background saving started"* | *"already in progress"* | *"scheduled"*) ;;
*)
	echo "Redis refused BGSAVE: $reply" >&2
	exit 1
	;;
esac

i=0
while :; do
	now=$(cli LASTSAVE)
	info=$(cli INFO persistence | tr -d '\r')
	running=$(printf '%s\n' "$info" | sed -n 's/^rdb_bgsave_in_progress:\([0-9]\)$/\1/p')
	if [ "$now" != "$before" ] && [ "$running" = "0" ]; then
		break
	fi
	i=$((i + 1))
	if [ "$i" -gt 3600 ]; then
		echo "The background save did not finish within an hour." >&2
		exit 1
	fi
	sleep 1
done
status=$(printf '%s\n' "$info" | sed -n 's/^rdb_last_bgsave_status:\(.*\)$/\1/p')
if [ "$status" != "ok" ]; then
	echo "The background save failed (rdb_last_bgsave_status is $status)." >&2
	exit 1
fi

dir=$(cli CONFIG GET dir | sed -n 2p)
file=$(cli CONFIG GET dbfilename | sed -n 2p)
tmp=$(mktemp /tmp/5050-dump.XXXXXX)
out=$(mktemp)
trap 'rm -f "$tmp" "$out"' EXIT
cp "$dir/$file" "$tmp"

ensure_repo
if ! run_restic backup --json --host 5050 --tag 5050 --stdin --stdin-filename dump.rdb <"$tmp" >"$out"; then
	tail -n 20 "$out" >&2
	echo "restic could not store the RDB file; the lines above say why." >&2
	exit 1
fi
id=$(snapshot_id "$out")
if [ -z "$id" ]; then
	echo "restic finished but did not report a snapshot id." >&2
	exit 1
fi
printf '{"ok":true,"sizeBytes":%s,"id":"%s"}\n' "$(wc -c <"$tmp" | tr -d ' ')" "$id"
