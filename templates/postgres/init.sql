-- SPDX-License-Identifier: MIT
-- Runs once, on the first start of an empty volume (docker-entrypoint-initdb.d).
-- Re-runnable: it only creates what is missing.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
