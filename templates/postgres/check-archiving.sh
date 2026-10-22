#!/bin/sh
# SPDX-License-Identifier: MIT
#
# check-archiving: reports the health of continuous WAL archiving from
# pg_stat_archiver as one JSON line.
set -eu

PGHOST=/var/run/postgresql
PGUSER=${POSTGRES_USER:-postgres}
PGDATABASE=${POSTGRES_DB:-postgres}
export PGHOST PGUSER PGDATABASE

psql -X -At -v ON_ERROR_STOP=1 -c "SELECT json_build_object(
 'archivedCount', archived_count,
 'failedCount', failed_count,
 'lastArchivedAt', COALESCE(EXTRACT(EPOCH FROM last_archived_time)::bigint * 1000, 0),
 'lastFailedAt', COALESCE(EXTRACT(EPOCH FROM last_failed_time)::bigint * 1000, 0),
 'ok', last_failed_time IS NULL OR (last_archived_time IS NOT NULL AND last_archived_time > last_failed_time)
) FROM pg_stat_archiver"
