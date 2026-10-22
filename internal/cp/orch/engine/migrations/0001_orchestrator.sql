-- SPDX-License-Identifier: AGPL-3.0-only
-- Orchestrator tables owned by WP2a (docs/SPEC.md 6.2). Forward only.

CREATE TABLE o_hosts (
	id                 TEXT PRIMARY KEY,
	name               TEXT NOT NULL,
	roles              TEXT NOT NULL DEFAULT '["app"]',
	state              TEXT NOT NULL,
	token_hash         TEXT,
	prev_token_hash    TEXT,
	prev_valid_until   INTEGER NOT NULL DEFAULT 0,
	pending_token_hash TEXT,
	token_rotated_at   INTEGER NOT NULL DEFAULT 0,
	machine_id         TEXT NOT NULL DEFAULT '',
	facts              TEXT NOT NULL DEFAULT '{}',
	mesh_index         INTEGER UNIQUE,
	mesh_pubkey        TEXT NOT NULL DEFAULT '',
	public_ip          TEXT NOT NULL DEFAULT '',
	last_seen          INTEGER NOT NULL DEFAULT 0,
	disconnected_at    INTEGER NOT NULL DEFAULT 0,
	drain              INTEGER NOT NULL DEFAULT 0,
	reserve_mb         INTEGER NOT NULL DEFAULT 0,
	version            TEXT NOT NULL DEFAULT '',
	created_at         INTEGER NOT NULL
);
CREATE UNIQUE INDEX o_hosts_token ON o_hosts (token_hash);
CREATE INDEX o_hosts_prev_token ON o_hosts (prev_token_hash);
CREATE INDEX o_hosts_pending_token ON o_hosts (pending_token_hash);

CREATE TABLE o_host_conflicts (
	host_id    TEXT NOT NULL,
	machine_id TEXT NOT NULL,
	wg_pubkey  TEXT NOT NULL,
	ip         TEXT NOT NULL DEFAULT '',
	seen_at    INTEGER NOT NULL,
	PRIMARY KEY (host_id, machine_id, wg_pubkey)
);

CREATE TABLE o_unknown_replicas (
	id           TEXT PRIMARY KEY,
	host_id      TEXT NOT NULL,
	container_id TEXT NOT NULL,
	replica_id   TEXT NOT NULL DEFAULT '',
	labels       TEXT NOT NULL DEFAULT '{}',
	image        TEXT NOT NULL DEFAULT '',
	reported_at  INTEGER NOT NULL,
	UNIQUE (host_id, container_id)
);

CREATE TABLE o_join_tokens (
	id              TEXT PRIMARY KEY,
	token_hash      TEXT NOT NULL UNIQUE,
	roles           TEXT NOT NULL,
	name            TEXT NOT NULL DEFAULT '',
	expires_at      INTEGER NOT NULL,
	used_at         INTEGER NOT NULL DEFAULT 0,
	replace_host_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE o_subnets (
	host_id        TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	slot           INTEGER NOT NULL CHECK (slot BETWEEN 1 AND 254),
	PRIMARY KEY (host_id, environment_id),
	UNIQUE (host_id, slot)
);

CREATE TABLE o_ips (
	ip             TEXT PRIMARY KEY,
	host_id        TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	replica_id     TEXT NOT NULL
);
CREATE INDEX o_ips_owner ON o_ips (replica_id);
CREATE INDEX o_ips_host_env ON o_ips (host_id, environment_id);

CREATE TABLE o_deployments (
	id                 TEXT PRIMARY KEY,
	environment_id     TEXT NOT NULL,
	service_id         TEXT NOT NULL,
	project_id         TEXT NOT NULL DEFAULT '',
	status             TEXT NOT NULL,
	trigger            TEXT NOT NULL DEFAULT '',
	actor              TEXT NOT NULL DEFAULT '',
	message            TEXT NOT NULL DEFAULT '',
	config_rev         INTEGER NOT NULL DEFAULT 0,
	config             TEXT NOT NULL DEFAULT '{}',
	commit_info        TEXT NOT NULL DEFAULT '',
	image              TEXT NOT NULL DEFAULT '',
	digest             TEXT NOT NULL DEFAULT '',
	rollback_of        TEXT NOT NULL DEFAULT '',
	diagnosis          TEXT NOT NULL DEFAULT '',
	reason             TEXT NOT NULL DEFAULT '',
	steps              TEXT NOT NULL DEFAULT '[]',
	env_enc            BLOB,
	is_node            INTEGER NOT NULL DEFAULT 0,
	start_cmd_detected TEXT NOT NULL DEFAULT '',
	upload_path        TEXT NOT NULL DEFAULT '',
	rebuild            INTEGER NOT NULL DEFAULT 0,
	source_map         TEXT NOT NULL DEFAULT '{}',
	config_file        TEXT NOT NULL DEFAULT '',
	notices            TEXT NOT NULL DEFAULT '[]',
	build_host         TEXT NOT NULL DEFAULT '',
	ref                TEXT NOT NULL DEFAULT '',
	plan_commit        TEXT NOT NULL DEFAULT '',
	wait_for           TEXT NOT NULL DEFAULT '[]',
	created_at         INTEGER NOT NULL,
	started_at         INTEGER NOT NULL DEFAULT 0,
	finished_at        INTEGER NOT NULL DEFAULT 0,
	active             INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX o_deployments_instance ON o_deployments (environment_id, service_id, created_at);
CREATE INDEX o_deployments_status ON o_deployments (status);

CREATE TABLE o_replicas (
	id              TEXT PRIMARY KEY,
	deployment_id   TEXT NOT NULL,
	service_id      TEXT NOT NULL,
	environment_id  TEXT NOT NULL,
	host_id         TEXT NOT NULL,
	ip              TEXT NOT NULL DEFAULT '',
	state           TEXT NOT NULL DEFAULT 'pending',
	container_id    TEXT NOT NULL DEFAULT '',
	restarts        INTEGER NOT NULL DEFAULT 0,
	exit_code       INTEGER,
	oom             INTEGER NOT NULL DEFAULT 0,
	diagnosis       TEXT NOT NULL DEFAULT '',
	listening_ports TEXT NOT NULL DEFAULT '[]',
	loopback_only   TEXT NOT NULL DEFAULT '[]',
	started_at      INTEGER NOT NULL DEFAULT 0,
	updated_at      INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	desired         TEXT NOT NULL DEFAULT 'running',
	serving         INTEGER NOT NULL DEFAULT 0,
	draining        INTEGER NOT NULL DEFAULT 0,
	adopted         INTEGER NOT NULL DEFAULT 0,
	removed_at      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX o_replicas_deployment ON o_replicas (deployment_id);
CREATE INDEX o_replicas_host ON o_replicas (host_id);
CREATE INDEX o_replicas_instance ON o_replicas (environment_id, service_id);

CREATE TABLE o_images (
	deployment_id TEXT PRIMARY KEY,
	service_id    TEXT NOT NULL,
	ref           TEXT NOT NULL,
	digest        TEXT NOT NULL DEFAULT '',
	created_at    INTEGER NOT NULL,
	deleted_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX o_images_service ON o_images (service_id, created_at);

CREATE TABLE o_state (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE o_host_logs (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	host_id        TEXT NOT NULL,
	ts             INTEGER NOT NULL,
	line_id        TEXT NOT NULL DEFAULT '',
	service_id     TEXT NOT NULL DEFAULT '',
	deployment_id  TEXT NOT NULL DEFAULT '',
	replica_id     TEXT NOT NULL DEFAULT '',
	stream         TEXT NOT NULL DEFAULT '',
	level          TEXT NOT NULL DEFAULT '',
	line           TEXT NOT NULL,
	attrs          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX o_host_logs_host_ts ON o_host_logs (host_id, ts);

CREATE TABLE o_tcp_ports (
	host_port    INTEGER PRIMARY KEY,
	tcp_proxy_id TEXT NOT NULL
);

CREATE TABLE o_certificates (
	domain     TEXT PRIMARY KEY,
	cert_enc   BLOB NOT NULL,
	not_after  INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	retry_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE o_volume_hosts (
	volume_id      TEXT PRIMARY KEY,
	service_id     TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	host_id        TEXT NOT NULL,
	created_at     INTEGER NOT NULL,
	trashed_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX o_volume_hosts_instance ON o_volume_hosts (environment_id, service_id);
