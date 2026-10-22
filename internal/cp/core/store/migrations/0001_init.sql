-- SPDX-License-Identifier: AGPL-3.0-only
-- Core tables of docs/SPEC.md 6.1. Timestamps are Unix milliseconds,
-- booleans INTEGER 0/1, JSON columns TEXT. Columns ending in _enc hold
-- secretbox ciphertext with the additional data "<table>|<id>|<column>".

CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT '{}',
	secret_enc BLOB
);

CREATE TABLE users (
	id TEXT PRIMARY KEY,
	email TEXT NOT NULL UNIQUE COLLATE NOCASE,
	name TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL,
	email_verified_at INTEGER,
	totp_secret_enc BLOB,
	totp_enabled INTEGER NOT NULL DEFAULT 0,
	recovery_codes_hash TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL,
	disabled INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE password_resets (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER
);

CREATE TABLE email_verifications (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER
);

CREATE TABLE notification_prefs (
	user_id TEXT NOT NULL,
	event TEXT NOT NULL,
	email INTEGER NOT NULL,
	in_app INTEGER NOT NULL,
	PRIMARY KEY (user_id, event)
);

CREATE TABLE notifications (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	ts INTEGER NOT NULL,
	kind TEXT NOT NULL,
	text TEXT NOT NULL,
	ref TEXT NOT NULL DEFAULT '',
	link TEXT NOT NULL DEFAULT '',
	read_at INTEGER
);
CREATE INDEX notifications_user ON notifications (user_id, id DESC);

CREATE TABLE project_grants (
	project_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	role TEXT NOT NULL,
	PRIMARY KEY (project_id, user_id)
);

CREATE TABLE sessions (
	id_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	extended_at INTEGER NOT NULL DEFAULT 0,
	ip TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE api_tokens (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL,
	prefix TEXT NOT NULL,
	secret_hash TEXT NOT NULL UNIQUE,
	scope TEXT NOT NULL,
	environment_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_used INTEGER NOT NULL DEFAULT 0,
	expires_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE invites (
	id TEXT PRIMARY KEY,
	role TEXT NOT NULL,
	project_id TEXT,
	email TEXT,
	token_hash TEXT NOT NULL UNIQUE,
	max_uses INTEGER NOT NULL DEFAULT 1,
	uses INTEGER NOT NULL DEFAULT 0,
	expires_at INTEGER NOT NULL,
	accepted_by TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL,
	created_by TEXT NOT NULL DEFAULT ''
);

CREATE TABLE setup_tokens (
	token_hash TEXT PRIMARY KEY,
	expires_at INTEGER NOT NULL,
	used_at INTEGER
);

CREATE TABLE cli_auth (
	code TEXT PRIMARY KEY,
	poll_hash TEXT NOT NULL UNIQUE,
	user_id TEXT,
	status TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	public_read INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	last_opened_at INTEGER NOT NULL DEFAULT 0,
	last_activity_at INTEGER NOT NULL DEFAULT 0,
	deleted_at INTEGER
);

CREATE TABLE environments (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	name TEXT NOT NULL,
	egress TEXT NOT NULL DEFAULT 'allowed',
	egress_allow TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL,
	deleted_at INTEGER
);
CREATE UNIQUE INDEX environments_name ON environments (project_id, name) WHERE deleted_at IS NULL;

CREATE TABLE services (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX services_project ON services (project_id);

CREATE TABLE service_instances (
	service_id TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	rev INTEGER NOT NULL,
	config TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	deleted_at INTEGER,
	name TEXT GENERATED ALWAYS AS (json_extract(config, '$.name')) VIRTUAL,
	PRIMARY KEY (service_id, environment_id)
);
CREATE UNIQUE INDEX service_instances_name ON service_instances (environment_id, name) WHERE deleted_at IS NULL;

CREATE TABLE config_revisions (
	service_id TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	rev INTEGER NOT NULL,
	config TEXT NOT NULL,
	commit_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	PRIMARY KEY (service_id, environment_id, rev)
);

CREATE TABLE config_files (
	service_id TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	commit_sha TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL,
	format TEXT NOT NULL,
	overlay TEXT NOT NULL,
	lines TEXT NOT NULL DEFAULT '{}',
	seen_at INTEGER NOT NULL,
	PRIMARY KEY (service_id, environment_id)
);

CREATE TABLE name_aliases (
	environment_id TEXT NOT NULL,
	old_name TEXT NOT NULL,
	service_id TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	PRIMARY KEY (environment_id, old_name)
);

CREATE TABLE variables (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT,
	name TEXT NOT NULL,
	value_enc BLOB NOT NULL,
	is_reference INTEGER NOT NULL DEFAULT 0,
	reference_raw TEXT NOT NULL DEFAULT '',
	sealed INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX variables_name ON variables (environment_id, ifnull(service_id, ''), name);

CREATE TABLE changes (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	patch TEXT NOT NULL,
	tag TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE (environment_id, service_id, kind)
);

CREATE TABLE commits (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	summary TEXT NOT NULL DEFAULT '[]',
	deployments TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE domains (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT NOT NULL,
	domain TEXT NOT NULL UNIQUE COLLATE NOCASE,
	kind TEXT NOT NULL,
	port INTEGER NOT NULL DEFAULT 0,
	dns_ok INTEGER NOT NULL DEFAULT 0,
	cloudflare INTEGER NOT NULL DEFAULT 0,
	message TEXT NOT NULL DEFAULT '',
	checked_at INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX domains_service ON domains (environment_id, service_id);

CREATE TABLE tcp_proxies (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT NOT NULL,
	target_port INTEGER NOT NULL,
	host_port INTEGER NOT NULL UNIQUE,
	created_at INTEGER NOT NULL
);

-- One volume per service instance. The spec's "service_id UNIQUE" is
-- per environment, because a service exists in every environment.
CREATE TABLE volumes (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT NOT NULL,
	name TEXT NOT NULL,
	mount_path TEXT NOT NULL,
	host_id TEXT NOT NULL DEFAULT '',
	alert_at_pct INTEGER NOT NULL DEFAULT 80,
	created_at INTEGER NOT NULL,
	deleted_at INTEGER
);
CREATE UNIQUE INDEX volumes_service ON volumes (environment_id, service_id) WHERE deleted_at IS NULL;

CREATE TABLE registry_credentials (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	server TEXT NOT NULL,
	username TEXT NOT NULL,
	password_enc BLOB NOT NULL,
	created_at INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE monitors (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT NOT NULL,
	metric TEXT NOT NULL,
	threshold REAL NOT NULL,
	unit TEXT NOT NULL DEFAULT 'pct',
	for_minutes INTEGER NOT NULL,
	channels TEXT NOT NULL DEFAULT '{}',
	state TEXT NOT NULL DEFAULT 'ok',
	last_fired_at INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE imports (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	environment_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	report TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL
);

CREATE TABLE github_installations (
	id TEXT PRIMARY KEY,
	account TEXT NOT NULL,
	installation_id INTEGER NOT NULL UNIQUE,
	created_at INTEGER NOT NULL
);

-- Last commit seen by the public repo poller (9.14).
CREATE TABLE github_polls (
	service_id TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	sha TEXT NOT NULL,
	checked_at INTEGER NOT NULL,
	PRIMARY KEY (service_id, environment_id)
);

CREATE TABLE webhooks (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	url TEXT NOT NULL,
	events TEXT NOT NULL DEFAULT '[]',
	preset TEXT NOT NULL DEFAULT '',
	secret_enc BLOB NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL
);

-- webhook_id is empty for install alert deliveries, which carry url instead.
CREATE TABLE webhook_deliveries (
	id TEXT PRIMARY KEY,
	webhook_id TEXT NOT NULL,
	url TEXT NOT NULL DEFAULT '',
	event TEXT NOT NULL,
	payload TEXT NOT NULL,
	status TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	next_at INTEGER NOT NULL,
	response_code INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries (status, next_at);
CREATE INDEX webhook_deliveries_hook ON webhook_deliveries (webhook_id, id DESC);

CREATE TABLE audit_log (
	id TEXT PRIMARY KEY,
	ts INTEGER NOT NULL,
	actor TEXT NOT NULL,
	action TEXT NOT NULL,
	target TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	detail TEXT NOT NULL DEFAULT ''
);

CREATE TABLE activity (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	service_id TEXT NOT NULL DEFAULT '',
	ts INTEGER NOT NULL,
	kind TEXT NOT NULL,
	text TEXT NOT NULL,
	actor TEXT NOT NULL DEFAULT '',
	ref TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT ''
);
CREATE INDEX activity_env ON activity (environment_id, id DESC);

CREATE TABLE canvas_positions (
	environment_id TEXT PRIMARY KEY,
	positions TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE groups (
	id TEXT PRIMARY KEY,
	environment_id TEXT NOT NULL,
	name TEXT NOT NULL,
	icon TEXT NOT NULL DEFAULT '',
	parent TEXT NOT NULL DEFAULT '',
	services TEXT NOT NULL DEFAULT '[]'
);
