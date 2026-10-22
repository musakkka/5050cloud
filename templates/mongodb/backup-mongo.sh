#!/bin/sh
# SPDX-License-Identifier: MIT
#
# backup-mongo: runs inside the MongoDB container. `mongodump --archive
# --gzip` streams straight into `restic backup --stdin`, so no dump lands on
# the volume. The password goes through a 0600 config file, never argv.
# Prints {"ok":true,"sizeBytes":N,"id":"<snapshot id>"} as its last line.
set -eu
. /opt/5050/bin/restic-env

user=${MONGO_INITDB_ROOT_USERNAME:-${MONGOUSER:-}}
pass=${MONGO_INITDB_ROOT_PASSWORD:-${MONGOPASSWORD:-}}
umask 077
conf=$(mktemp)
out=$(mktemp)
err=$(mktemp)
trap 'rm -f "$conf" "$out" "$err"' EXIT
printf 'password: "%s"\n' "$pass" >"$conf"

ensure_repo
set +e
mongodump --host 127.0.0.1 --port 27017 --username "$user" --authenticationDatabase admin \
	--config "$conf" --archive --gzip 2>"$err" |
	run_restic backup --json --host 5050 --tag 5050 --stdin --stdin-filename dump.archive.gz >"$out"
restic_status=$?
set -e
if grep -q 'Failed:' "$err"; then
	cat "$err" >&2
	echo "mongodump failed; the lines above say why." >&2
	exit 1
fi
cat "$err" >&2
if [ "$restic_status" -ne 0 ]; then
	tail -n 20 "$out" >&2
	echo "restic could not store the dump; the lines above say why." >&2
	exit 1
fi
id=$(snapshot_id "$out")
if [ -z "$id" ]; then
	echo "restic finished but did not report a snapshot id." >&2
	exit 1
fi
printf '{"ok":true,"sizeBytes":%s,"id":"%s"}\n' "$(snapshot_size "$out" || echo 0)" "$id"
