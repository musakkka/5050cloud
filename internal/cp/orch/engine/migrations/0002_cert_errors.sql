-- The last certificate issuance error the ACME primary reported per domain,
-- shown in the console next to the domain (docs/SPEC.md 9.11).
CREATE TABLE o_cert_errors (
	domain     TEXT PRIMARY KEY,
	message    TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
