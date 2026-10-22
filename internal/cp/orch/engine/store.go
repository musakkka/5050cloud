// SPDX-License-Identifier: AGPL-3.0-only

package engine

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

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// MigrationsTable records applied orchestrator migrations (docs/SPEC.md 6).
// Rows are keyed by file name, so WP2b can apply its own files to the same
// table under distinct names.
const MigrationsTable = "orch_migrations"

// Migrate applies the embedded orchestrator migrations in order, each in its
// own transaction. Applied files are never re-run (forward only).
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+MigrationsTable+` (
		name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create %s: %w", MigrationsTable, err)
	}
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, path := range names {
		name := strings.TrimPrefix(path, "migrations/")
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+MigrationsTable+` WHERE name = ?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile(path)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("orchestrator migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+MigrationsTable+` (name, applied_at) VALUES (?, ?)`, name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// OpenDB opens a SQLite database with the pragmas of docs/SPEC.md 6.5. 5050d
// gets its handle from core; tests and tools use this.
func OpenDB(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)&_pragma=wal_autocheckpoint(1000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	return db, nil
}

// Store is the orchestrator's single writer for state.db: every write runs
// on one goroutine, in a short transaction (docs/SPEC.md 6.5).
type Store struct {
	DB *sql.DB

	reqs chan writeReq
	stop chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

type writeReq struct {
	ctx  context.Context
	fn   func(*sql.Tx) error
	done chan error
}

// NewStore starts the writer.
func NewStore(db *sql.DB) *Store {
	s := &Store{DB: db, reqs: make(chan writeReq, 256), stop: make(chan struct{})}
	s.wg.Add(1)
	go s.loop()
	return s
}

// Close stops the writer after pending writes.
func (s *Store) Close() {
	s.once.Do(func() { close(s.stop) })
	s.wg.Wait()
}

func (s *Store) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case r := <-s.reqs:
			r.done <- s.run(r)
		}
	}
}

func (s *Store) run(r writeReq) (err error) {
	if r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	tx, err := s.DB.BeginTx(context.WithoutCancel(r.ctx), nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			err = fmt.Errorf("write panicked: %v", p)
		}
	}()
	if err := r.fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ErrClosed is returned after Close.
var ErrClosed = errors.New("the orchestrator store is closed")

// Write runs fn in a write transaction on the writer goroutine.
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	r := writeReq{ctx: ctx, fn: fn, done: make(chan error, 1)}
	select {
	case s.reqs <- r:
	case <-s.stop:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-r.done:
		return err
	case <-s.stop:
		return ErrClosed
	}
}
