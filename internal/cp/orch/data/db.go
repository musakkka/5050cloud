// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// store wraps state.db for the data tables. Writes go through one mutex so
// this package never runs two write transactions at once (docs/SPEC.md 6.5);
// reads are plain queries that hold no transaction while they wait.
type store struct {
	db *sql.DB
	mu sync.Mutex
}

// migrate applies every embedded migration not yet recorded in
// o_data_migrations, in file name order, each in its own transaction.
func (st *store) migrate(ctx context.Context) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, err := st.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS o_data_migrations (
		name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create the data migrations table: %w", err)
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM o_data_migrations WHERE name = ?`, name).Scan(&n); err != nil {
			return fmt.Errorf("read the data migrations table: %w", err)
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := st.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply data migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO o_data_migrations (name, applied_at) VALUES (?, ?)`, name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit data migration %s: %w", name, err)
		}
	}
	return nil
}

// exec runs one write statement under the write lock.
func (st *store) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.db.ExecContext(ctx, q, args...)
}

// stateGet reads a key of o_data_state ("" when missing).
func (st *store) stateGet(ctx context.Context, key string) (string, error) {
	var v string
	err := st.db.QueryRowContext(ctx, `SELECT value FROM o_data_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (st *store) stateSet(ctx context.Context, key, value string) error {
	_, err := st.exec(ctx, `INSERT INTO o_data_state (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ensureServiceRow makes sure o_data_services has a row for the service.
func (st *store) ensureServiceRow(ctx context.Context, envID, serviceID string, nowMs int64) error {
	_, err := st.exec(ctx, `INSERT INTO o_data_services (service_id, environment_id, first_seen_at) VALUES (?, ?, ?)
		ON CONFLICT DO NOTHING`, serviceID, envID, nowMs)
	return err
}

// tableExists reports whether another package's table is present (registry
// retention reads WP2a's o_images and o_deployments when they exist).
func (st *store) tableExists(ctx context.Context, name string) bool {
	var n int
	err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	return err == nil && n > 0
}

// isUniqueViolation reports a UNIQUE constraint failure from modernc.org/sqlite.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
