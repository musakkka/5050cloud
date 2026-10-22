#!/bin/sh
# SPDX-License-Identifier: MIT
#
# restore-mongo: starts a private mongod on the EMPTY volume mounted at
# /data/db, streams snapshot BACKUP_ID into `mongorestore --archive --gzip`
# (users and roles included), and shuts down cleanly. The new service then
# starts on the restored data with the same credentials.
#
#   restore-mongo            restore to a copy
#   restore-mongo --check    also run db.stats() (monthly restore test)
set -eu
. /opt/5050/bin/restic-env

if [ -z "${BACKUP_ID:-}" ]; then
	echo "No snapshot was given to restore (BACKUP_ID is empty)." >&2
	exit 1
fi
mkdir -p /data/db
if [ "${FIFTY_RESTORE_WIPE:-}" = "1" ]; then
	find /data/db -mindepth 1 -maxdepth 1 -exec rm -rf {} +
fi
if [ -n "$(ls -A /data/db 2>/dev/null | grep -v '^lost+found$' || true)" ]; then
	echo "The target volume is not empty, so the restore stopped to protect it." >&2
	exit 1
fi
chown -R mongodb:mongodb /data/db
port=27099
gosu mongodb mongod --dbpath /data/db --bind_ip 127.0.0.1 --port "$port" \
	--fork --logpath /tmp/5050-restore.log >&2
if ! run_restic dump "$BACKUP_ID" /dump.archive.gz |
	mongorestore --host 127.0.0.1 --port "$port" --archive --gzip --drop >&2; then
	tail -n 30 /tmp/5050-restore.log >&2
	echo "mongorestore could not load the dump." >&2
	exit 1
fi
if [ "${1:-}" = "--check" ]; then
	if ! mongosh --quiet --host 127.0.0.1 --port "$port" --eval 'const s = db.stats(); if (!s.ok) { quit(1) }' >&2; then
		echo "db.stats() failed on the restored copy." >&2
		exit 1
	fi
fi
gosu mongodb mongod --dbpath /data/db --shutdown >&2
echo '{"ok":true}'
