#!/bin/sh
# SPDX-License-Identifier: MIT
#
# restore-postgres: restores a WAL-G base backup into an EMPTY data directory
# (a new volume), replays archived WAL up to RESTORE_TARGET_TIME when it is set
# (point in time) or to the end of the archive, promotes, and stops the server
# cleanly so the new service starts on a finished copy. The source database is
# never touched.
#
#   restore-postgres                 restore to a copy
#   restore-postgres --check CMD...  also run CMD against the restored copy
#                                    (monthly restore test) before stopping
#
# Environment: BACKUP_ID (WAL-G backup name, default LATEST), RESTORE_TARGET_TIME
# ("2006-01-02 15:04:05.000+00", optional), the WALG_* and AWS_* settings of the
# SOURCE database, PGDATA. Prints {"ok":true,"recoveredTo":"..."} last.
set -eu

check=0
if [ "${1:-}" = "--check" ]; then
	check=1
	shift
fi
PGDATA=${PGDATA:-/var/lib/postgresql/data/pgdata}
POSTGRES_USER=${POSTGRES_USER:-postgres}
export PGDATA

if [ -z "${WALG_S3_PREFIX:-}" ]; then
	echo "Backups are not configured, so there is nothing to restore from." >&2
	exit 1
fi
# FIFTY_RESTORE_WIPE is set only for the new volume of a copy that started
# once empty before the restore (the control plane could not place the
# volume first); it never reaches a job that mounts a source volume.
if [ "${FIFTY_RESTORE_WIPE:-}" = "1" ]; then
	rm -rf "$PGDATA"
fi
mkdir -p "$PGDATA"
if [ -n "$(ls -A "$PGDATA" 2>/dev/null)" ]; then
	echo "The target data directory $PGDATA is not empty, so the restore stopped to protect it." >&2
	exit 1
fi

echo "Fetching base backup ${BACKUP_ID:-LATEST}." >&2
/opt/5050/bin/wal-g backup-fetch "$PGDATA" "${BACKUP_ID:-LATEST}" >&2

auto="$PGDATA/postgresql.auto.conf"
saved=$(mktemp)
if [ -f "$auto" ]; then cp "$auto" "$saved"; fi
{
	echo "restore_command = '/opt/5050/bin/wal-g wal-fetch %f %p'"
	echo "recovery_target_action = 'promote'"
	echo "recovery_target_timeline = 'latest'"
	if [ -n "${RESTORE_TARGET_TIME:-}" ]; then
		echo "recovery_target_time = '${RESTORE_TARGET_TIME}'"
	fi
} >>"$auto"
touch "$PGDATA/recovery.signal"
chown -R postgres:postgres "$PGDATA"
chmod 700 "$PGDATA"
mkdir -p /var/run/postgresql
chown postgres:postgres /var/run/postgresql

log=/tmp/5050-restore.log
echo "Replaying archived WAL." >&2
gosu postgres postgres -D "$PGDATA" -c config_file=/etc/postgresql/5050.conf \
	-c listen_addresses='' -c archive_mode=off -c hot_standby=off \
	-c shared_buffers=128MB -c effective_cache_size=256MB >"$log" 2>&1 &
pid=$!

while :; do
	if ! kill -0 "$pid" 2>/dev/null; then
		tail -n 30 "$log" >&2
		if grep -q 'recovery ended before configured recovery target was reached' "$log"; then
			echo "The archive ends before the requested time, so the copy cannot reach it. Pick an earlier time." >&2
		else
			echo "Postgres stopped during recovery; the log lines above say why." >&2
		fi
		exit 1
	fi
	if gosu postgres pg_isready -q -h /var/run/postgresql; then
		state=$(gosu postgres psql -X -At -h /var/run/postgresql -U "$POSTGRES_USER" -d postgres -c 'SELECT pg_is_in_recovery()' 2>/dev/null || true)
		if [ "$state" = "f" ]; then
			break
		fi
	fi
	sleep 1
done
cat "$log" >&2

recovered=$(sed -n 's/.*recovery stopping before .* time \([0-9][0-9-]* [0-9:.]*[+-][0-9]*\).*/\1/p' "$log" | tail -n 1)
if [ -z "$recovered" ]; then
	recovered=$(sed -n 's/.*last completed transaction was at log time \([0-9][0-9-]* [0-9:.]*[+-][0-9]*\).*/\1/p' "$log" | tail -n 1)
fi

status=0
if [ "$check" = 1 ] && [ "$#" -gt 0 ]; then
	echo "Running the restore check." >&2
	if ! PGHOST=/var/run/postgresql PGUSER="$POSTGRES_USER" PGDATABASE=postgres gosu postgres "$@" >&2; then
		echo "The restore check failed against the restored copy." >&2
		status=1
	fi
fi

gosu postgres pg_ctl -D "$PGDATA" stop -m fast -w >&2
# Drop the recovery settings so the copy never reads the source's archive again.
cp "$saved" "$auto"
chown postgres:postgres "$auto"
rm -f "$saved"
if [ "$status" -ne 0 ]; then
	exit "$status"
fi
printf '{"ok":true,"recoveredTo":"%s"}\n' "$recovered"
