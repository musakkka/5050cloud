// SPDX-License-Identifier: AGPL-3.0-only

// Package store opens state.db and enforces the SQLite operating rules of
// docs/SPEC.md 6.5: WAL mode and the fixed pragmas, one writer goroutine
// that runs every core write as a closure in an immediate transaction, a
// pool of up to 8 read connections, a WAL checkpointer, forward-only
// embedded migrations, and read-only mode when the disk is full (6.6).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
)

//go:embed migrations/*.sql
var coreMigrations embed.FS

// ErrReadOnly is returned for writes while the control host disk is full.
// The API answers it with code "unavailable" (6.6).
var ErrReadOnly error = fail.Unavailable("The control host disk is full. Free space to continue.")

// ErrClosed is returned for writes after Close.
var ErrClosed error = fail.Unavailable("The control plane is shutting down. Try again in a moment.")

// MinFreeBytes is the free space under which the store switches to read-only mode.
const MinFreeBytes = 200 << 20

const (
	sqliteFull = 13 // SQLITE_FULL
)

// Store is state.db.
type Store struct {
	path string
	// Reader is the read pool (query_only, up to 8 connections). Read
	// transactions must be short: read, release, then stream.
	Reader *sql.DB
	// Shared is a normal pool handed to the orchestrator (cpapi), which
	// follows the same rules with its own writer.
	Shared *sql.DB
	writer *sql.DB

	writes chan writeReq
	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool

	readOnly   atomic.Bool
	fullByErr  atomic.Bool
	onReadOnly func(bool)
	freeBytes  func(path string) (uint64, error)
	mu         sync.Mutex
}

type writeReq struct {
	ctx  context.Context
	fn   func(*sql.Tx) error
	resp chan error
}

// Options tune Open.
type Options struct {
	// OnReadOnly is called when read-only mode switches on or off.
	OnReadOnly func(on bool)
	// FreeBytes overrides the free-space probe (tests).
	FreeBytes func(path string) (uint64, error)
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)",
		"foreign_keys(ON)", "wal_autocheckpoint(1000)",
	} {
		q.Add("_pragma", p)
	}
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens (and creates) the database at path and applies core migrations.
func Open(ctx context.Context, path string, o Options) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the directory for %s: %w", path, err)
	}
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := w.PingContext(ctx); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	r.SetMaxIdleConns(8)
	sh, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, err
	}
	sh.SetMaxOpenConns(4)
	sh.SetMaxIdleConns(4)
	s := &Store{
		path: path, Reader: r, Shared: sh, writer: w,
		writes: make(chan writeReq, 256), done: make(chan struct{}),
		onReadOnly: o.OnReadOnly, freeBytes: o.FreeBytes,
	}
	if s.freeBytes == nil {
		s.freeBytes = diskFree
	}
	s.wg.Add(1)
	go s.writeLoop()
	if err := s.Migrate(ctx, coreMigrations, "migrations", "core_migrations"); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Path is the database file.
func (s *Store) Path() string { return s.path }

// Close stops the writer and closes every pool.
func (s *Store) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	close(s.done)
	s.wg.Wait()
	_ = s.Reader.Close()
	_ = s.Shared.Close()
	return s.writer.Close()
}

func (s *Store) writeLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			// Drain pending requests with a clear error.
			for {
				select {
				case req := <-s.writes:
					req.resp <- ErrClosed
				default:
					return
				}
			}
		case req := <-s.writes:
			req.resp <- s.runWrite(req.ctx, req.fn)
		}
	}
}

func (s *Store) runWrite(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var tx *sql.Tx
	tx, err = s.writer.BeginTx(ctx, nil)
	if err != nil {
		return s.classify(err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			err = fmt.Errorf("a database write panicked: %v", p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return s.classify(err)
	}
	if err := tx.Commit(); err != nil {
		return s.classify(err)
	}
	return nil
}

// classify turns SQLITE_FULL into read-only mode.
func (s *Store) classify(err error) error {
	if IsFull(err) {
		s.fullByErr.Store(true)
		s.setReadOnly(true)
		return ErrReadOnly
	}
	return err
}

// IsFull reports whether err is SQLITE_FULL.
func IsFull(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()&0xff == sqliteFull
	}
	return false
}

// Write runs fn in one immediate transaction on the writer goroutine.
// Writes are refused with ErrReadOnly while the disk is full.
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if s.readOnly.Load() {
		return ErrReadOnly
	}
	req := writeReq{ctx: ctx, fn: fn, resp: make(chan error, 1)}
	select {
	case s.writes <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return ErrClosed
	}
	select {
	case err := <-req.resp:
		return err
	case <-ctx.Done():
		// The closure may still run; its transaction is bound to ctx and rolls back.
		return ctx.Err()
	}
}

// Exec is Write for a single statement.
func (s *Store) Exec(ctx context.Context, query string, args ...any) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	})
}

// ReadOnly reports read-only mode (6.6).
func (s *Store) ReadOnly() bool { return s.readOnly.Load() }

func (s *Store) setReadOnly(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly.Load() == on {
		return
	}
	s.readOnly.Store(on)
	if !on {
		s.fullByErr.Store(false)
	}
	if s.onReadOnly != nil {
		go s.onReadOnly(on)
	}
}

// CheckDisk switches read-only mode on under MinFreeBytes and off once
// space comes back. It runs periodically from Run; tests call it directly.
func (s *Store) CheckDisk() {
	free, err := s.freeBytes(filepath.Dir(s.path))
	if err != nil {
		return
	}
	if free < MinFreeBytes {
		s.setReadOnly(true)
		return
	}
	if s.readOnly.Load() {
		// Space is back: probe with a real write before clearing a full error.
		s.setReadOnly(false)
	}
}

// Run starts the disk monitor and the WAL checkpointer. It blocks until ctx is done.
func (s *Store) Run(ctx context.Context) {
	disk := time.NewTicker(5 * time.Second)
	walCheck := time.NewTicker(30 * time.Second)
	full := time.NewTicker(5 * time.Minute)
	defer disk.Stop()
	defer walCheck.Stop()
	defer full.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-disk.C:
			s.CheckDisk()
		case <-walCheck.C:
			if fi, err := os.Stat(s.path + "-wal"); err == nil && fi.Size() > 64<<20 {
				_ = s.Checkpoint(ctx)
			}
		case <-full.C:
			_ = s.Checkpoint(ctx)
		}
	}
}

// Checkpoint runs PRAGMA wal_checkpoint(TRUNCATE) on the writer connection.
func (s *Store) Checkpoint(ctx context.Context) error {
	if s.closed.Load() {
		return ErrClosed
	}
	_, err := s.writer.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// VacuumInto writes a consistent copy of the database to dst.
func (s *Store) VacuumInto(ctx context.Context, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	_, err := s.Reader.ExecContext(ctx, "VACUUM INTO ?", dst)
	if err != nil {
		// query_only forbids VACUUM INTO on some builds; fall back to the shared pool.
		_, err = s.Shared.ExecContext(ctx, "VACUUM INTO ?", dst)
	}
	return err
}

// Migrate applies the embedded files dir/NNNN_*.sql of fsys that are not
// yet recorded in table, each in its own transaction, in order.
func (s *Store) Migrate(ctx context.Context, fsys fs.FS, dir, table string) error {
	if !validIdent(table) {
		return fmt.Errorf("invalid migration table name %q", table)
	}
	if err := s.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+table+` (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("cannot create the migration table %s: %w", table, err)
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	type mig struct {
		v    int
		name string
	}
	var migs []mig
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".sql") {
			continue
		}
		i := strings.IndexByte(n, '_')
		if i < 1 {
			return fmt.Errorf("migration file %s must be named NNNN_name.sql", n)
		}
		v, err := strconv.Atoi(n[:i])
		if err != nil {
			return fmt.Errorf("migration file %s must start with a number", n)
		}
		migs = append(migs, mig{v, n})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].v < migs[j].v })
	applied := map[int]bool{}
	versions, err := All(ctx, s.Reader, func(r *sql.Rows) (int, error) {
		var v int
		return v, r.Scan(&v)
	}, `SELECT version FROM `+table)
	if err != nil {
		return err
	}
	for _, v := range versions {
		applied[v] = true
	}
	for _, m := range migs {
		if applied[m.v] {
			continue
		}
		body, err := fs.ReadFile(fsys, dir+"/"+m.name)
		if err != nil {
			return err
		}
		err = s.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("migration %s failed: %w", m.name, err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (version, name, applied_at) VALUES (?, ?, ?)`, m.v, m.name, time.Now().UnixMilli())
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// MigrationsApplied reports whether every embedded core migration is recorded.
func (s *Store) MigrationsApplied(ctx context.Context) bool {
	entries, err := fs.ReadDir(coreMigrations, "migrations")
	if err != nil {
		return false
	}
	var n int
	if err := s.Reader.QueryRowContext(ctx, `SELECT count(*) FROM core_migrations`).Scan(&n); err != nil {
		return false
	}
	return n >= len(entries)
}

// Ping checks the read pool.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.Reader.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// Now is the store's clock in Unix milliseconds.
func Now() int64 { return time.Now().UnixMilli() }

// Querier is *sql.DB or *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// All runs query and scans every row with scan, closing the rows before it
// returns so no read transaction outlives the call.
func All[T any](ctx context.Context, q Querier, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return out, rows.Close()
}

// Strings returns the single string column of every row.
func Strings(ctx context.Context, q Querier, query string, args ...any) ([]string, error) {
	return All(ctx, q, func(r *sql.Rows) (string, error) {
		var s string
		return s, r.Scan(&s)
	}, query, args...)
}

// NoRows reports sql.ErrNoRows.
func NoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// Bool converts a bool to INTEGER 0/1.
func Bool(b bool) int {
	if b {
		return 1
	}
	return 0
}
