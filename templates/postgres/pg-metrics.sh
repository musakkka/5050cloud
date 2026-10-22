#!/bin/sh
# SPDX-License-Identifier: MIT
#
# pg-metrics: prints one JSON object shaped like proto.DBSample (connections by
# state, cache hit ratio, pg_wal size, age of the oldest segment waiting to be
# archived, top queries from pg_stat_statements). The agent runs it every 60 s
# (docs/SPEC.md 9.10, 9.13).
set -eu

PGHOST=/var/run/postgresql
PGUSER=${POSTGRES_USER:-postgres}
PGDATABASE=${POSTGRES_DB:-postgres}
export PGHOST PGUSER PGDATABASE

base="'connections', COALESCE((SELECT json_object_agg(s, c) FROM (SELECT COALESCE(state, 'other') AS s, count(*) AS c FROM pg_stat_activity WHERE backend_type = 'client backend' GROUP BY 1) x), '{}'::json),
 'cacheHitRatio', COALESCE((SELECT sum(blks_hit)::float8 / NULLIF(sum(blks_hit) + sum(blks_read), 0) FROM pg_stat_database), 0),
 'walBytes', COALESCE((SELECT sum(size) FROM pg_ls_waldir()), 0),
 'oldestUnarchivedSec', COALESCE((SELECT EXTRACT(EPOCH FROM now() - min(modification))::bigint FROM pg_ls_archive_statusdir() WHERE name LIKE '%.ready'), 0)"
top="'topQueries', COALESCE((SELECT json_agg(q) FROM (SELECT left(query, 2000) AS query, calls, round(total_exec_time::numeric, 3) AS \"totalMs\", round(mean_exec_time::numeric, 3) AS \"meanMs\" FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 10) q), '[]'::json)"

if ! psql -X -At -v ON_ERROR_STOP=1 -c "SELECT json_build_object($base, $top)" 2>/dev/null; then
	# pg_stat_statements is not installed in this database: report the rest.
	psql -X -At -v ON_ERROR_STOP=1 -c "SELECT json_build_object($base)"
fi
