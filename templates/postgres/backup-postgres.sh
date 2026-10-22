#!/bin/sh
# SPDX-License-Identifier: MIT
#
# backup-postgres: takes a WAL-G base backup inside the running Postgres
# container and prints {"ok":true,"sizeBytes":N,"id":"<backup name>"} as its
# last line (manifest.BackupRecipe). Runs as the postgres user.
set -eu

if [ -z "${WALG_S3_PREFIX:-}" ]; then
	echo "Backups are not configured: set a bucket in Settings > Backups first." >&2
	exit 1
fi
PGDATA=${PGDATA:-/var/lib/postgresql/data/pgdata}
PGHOST=/var/run/postgresql
PGUSER=${POSTGRES_USER:-postgres}
PGDATABASE=${POSTGRES_DB:-postgres}
export PGDATA PGHOST PGUSER PGDATABASE

log=$(mktemp)
trap 'rm -f "$log"' EXIT
if ! /opt/5050/bin/wal-g backup-push "$PGDATA" >"$log" 2>&1; then
	cat "$log" >&2
	echo "WAL-G could not take the base backup; the lines above say why." >&2
	exit 1
fi
cat "$log" >&2
name=$(sed -n 's/.*Wrote backup with name \([A-Za-z0-9_]*\).*/\1/p' "$log" | tail -n 1)
if [ -z "$name" ]; then
	echo "WAL-G finished but did not report a backup name." >&2
	exit 1
fi
size=$(/opt/5050/bin/wal-g backup-list --detail --json 2>/dev/null |
	sed -n "s/.*\"backup_name\":\"$name\"[^}]*\"compressed_size\":\([0-9][0-9]*\).*/\1/p" | head -n 1)
printf '{"ok":true,"sizeBytes":%s,"id":"%s"}\n' "${size:-0}" "$name"
