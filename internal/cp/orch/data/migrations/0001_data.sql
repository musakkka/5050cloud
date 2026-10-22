-- SPDX-License-Identifier: AGPL-3.0-only
-- Data services tables (WP2b, docs/SPEC.md 6.2). Applied once, forward only,
-- and recorded in o_data_migrations.

-- One row per cron slot. The UNIQUE constraint is what makes a slot run at
-- most once, even across a control plane restart at the boundary (9.9).
CREATE TABLE o_cron_runs (
	id             TEXT PRIMARY KEY,
	service_id     TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	deployment_id  TEXT NOT NULL DEFAULT '',
	status         TEXT NOT NULL,
	trigger        TEXT NOT NULL DEFAULT 'schedule',
	exit_code      INTEGER,
	message        TEXT NOT NULL DEFAULT '',
	output         TEXT NOT NULL DEFAULT '',
	scheduled_at   INTEGER NOT NULL,
	started_at     INTEGER NOT NULL DEFAULT 0,
	finished_at    INTEGER NOT NULL DEFAULT 0,
	job_id         TEXT NOT NULL DEFAULT '',
	UNIQUE (service_id, environment_id, scheduled_at)
);
CREATE INDEX o_cron_runs_by_service ON o_cron_runs (environment_id, service_id, scheduled_at DESC);
CREATE INDEX o_cron_runs_by_status ON o_cron_runs (status);

CREATE TABLE o_backups (
	id             TEXT PRIMARY KEY,
	service_id     TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	kind           TEXT NOT NULL,
	status         TEXT NOT NULL,
	size_bytes     INTEGER NOT NULL DEFAULT 0,
	trigger        TEXT NOT NULL,
	locked         INTEGER NOT NULL DEFAULT 0,
	store_id       TEXT NOT NULL DEFAULT '',
	error          TEXT NOT NULL DEFAULT '',
	started_at     INTEGER NOT NULL,
	finished_at    INTEGER NOT NULL DEFAULT 0,
	verified_at    INTEGER NOT NULL DEFAULT 0,
	deleted_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX o_backups_by_service ON o_backups (environment_id, service_id, started_at DESC);

CREATE TABLE o_backup_schedules (
	service_id     TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	schedule       TEXT NOT NULL,
	last_run_at    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (service_id, environment_id)
);

-- Key/value state of the data services (cron tick, last restore test month,
-- last registry garbage collection).
CREATE TABLE o_data_state (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- Per data service state: WAL guard, backup health notifications and work
-- that waits for the next successful deploy (a base backup after Promote or a
-- bucket change).
CREATE TABLE o_data_services (
	service_id            TEXT NOT NULL,
	environment_id        TEXT NOT NULL,
	wal_bytes             INTEGER NOT NULL DEFAULT 0,
	oldest_unarchived_sec INTEGER NOT NULL DEFAULT 0,
	wal_sampled_at        INTEGER NOT NULL DEFAULT 0,
	wal_failing_since     INTEGER NOT NULL DEFAULT 0,
	stale_notified_at     INTEGER NOT NULL DEFAULT 0,
	pending_base_backup   INTEGER NOT NULL DEFAULT 0,
	cron_since            INTEGER NOT NULL DEFAULT 0,
	first_seen_at         INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (service_id, environment_id)
);

-- Copies made by Restore and Upgrade to a new major version, until promoted.
CREATE TABLE o_data_restores (
	service_id            TEXT PRIMARY KEY,
	environment_id        TEXT NOT NULL,
	source_service_id     TEXT NOT NULL,
	source_environment_id TEXT NOT NULL,
	kind                  TEXT NOT NULL,
	backup_id             TEXT NOT NULL DEFAULT '',
	target_time           INTEGER NOT NULL DEFAULT 0,
	state                 TEXT NOT NULL,
	recovered_to          INTEGER NOT NULL DEFAULT 0,
	error                 TEXT NOT NULL DEFAULT '',
	created_at            INTEGER NOT NULL,
	updated_at            INTEGER NOT NULL
);
CREATE INDEX o_data_restores_by_source ON o_data_restores (source_environment_id, source_service_id);
