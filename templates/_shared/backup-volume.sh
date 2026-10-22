#!/bin/sh
# SPDX-License-Identifier: MIT
#
# backup-volume: backs up a volume mounted read-only at /data with restic
# (crash consistent: files are read while the service may be writing them).
# Prints {"ok":true,"sizeBytes":N,"id":"<snapshot id>"} as its last line.
set -eu
. /opt/5050/bin/restic-env

ensure_repo
out=$(mktemp)
trap 'rm -f "$out"' EXIT
if ! run_restic backup --json --host 5050 --tag 5050 /data >"$out"; then
	tail -n 20 "$out" >&2
	echo "restic could not back up the volume; the lines above say why." >&2
	exit 1
fi
id=$(snapshot_id "$out")
if [ -z "$id" ]; then
	echo "restic finished but did not report a snapshot id." >&2
	exit 1
fi
printf '{"ok":true,"sizeBytes":%s,"id":"%s"}\n' "$(snapshot_size "$out" || echo 0)" "$id"
