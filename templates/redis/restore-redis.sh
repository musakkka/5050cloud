#!/bin/sh
# SPDX-License-Identifier: MIT
#
# restore-redis: writes the RDB of snapshot BACKUP_ID into the EMPTY volume
# mounted at /data, loads it once with append-only off, then turns
# append-only on so Redis rebuilds the AOF from the data set, and shuts down
# cleanly. The new service starts with both files in place.
#
#   restore-redis            restore to a copy
#   restore-redis --check    also PING the loaded copy (monthly restore test)
set -eu
. /opt/5050/bin/restic-env

if [ -z "${BACKUP_ID:-}" ]; then
	echo "No snapshot was given to restore (BACKUP_ID is empty)." >&2
	exit 1
fi
if [ "${FIFTY_RESTORE_WIPE:-}" = "1" ]; then
	find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +
fi
if [ -n "$(ls -A /data 2>/dev/null | grep -v '^lost+found$' || true)" ]; then
	echo "The target volume is not empty, so the restore stopped to protect it." >&2
	exit 1
fi
run_restic dump "$BACKUP_ID" /dump.rdb >/data/dump.rdb
chown redis:redis /data /data/dump.rdb 2>/dev/null || true

port=6390
setpriv --reuid=redis --regid=redis --clear-groups redis-server --port "$port" --bind 127.0.0.1 --dir /data --dbfilename dump.rdb \
	--appendonly no --save '' --protected-mode yes --daemonize no >/tmp/5050-restore.log 2>&1 &
pid=$!
cli() { redis-cli -h 127.0.0.1 -p "$port" "$@"; }
i=0
until [ "$(cli PING 2>/dev/null)" = "PONG" ]; do
	if ! kill -0 "$pid" 2>/dev/null; then
		cat /tmp/5050-restore.log >&2
		echo "Redis could not load the restored RDB file." >&2
		exit 1
	fi
	i=$((i + 1))
	[ "$i" -lt 600 ] || { echo "Redis did not finish loading within 10 minutes." >&2; exit 1; }
	sleep 1
done
# The loading flag must be off before the AOF rewrite starts.
until cli INFO persistence | tr -d '\r' | grep -q '^loading:0$'; do sleep 1; done
cli CONFIG SET appendonly yes >/dev/null
until cli INFO persistence | tr -d '\r' | grep -q '^aof_rewrite_in_progress:0$' &&
	cli INFO persistence | tr -d '\r' | grep -q '^aof_rewrite_scheduled:0$'; do
	sleep 1
done
keys=$(cli DBSIZE)
if [ "${1:-}" = "--check" ] && [ "$(cli PING)" != "PONG" ]; then
	echo "The restored copy did not answer PING." >&2
	exit 1
fi
cli SHUTDOWN SAVE >/dev/null 2>&1 || true
wait "$pid" 2>/dev/null || true
chown -R redis:redis /data 2>/dev/null || true
printf '{"ok":true,"keys":%s}\n' "${keys:-0}"
