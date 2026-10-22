// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func open(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustScan(t *testing.T, row *sql.Row, dst ...any) {
	t.Helper()
	if err := row.Scan(dst...); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAppliesPragmasAndMigrations(t *testing.T) {
	s := open(t, Options{})
	ctx := context.Background()
	var mode string
	if err := s.Reader.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	var fk, busy int
	mustScan(t, s.Reader.QueryRowContext(ctx, "PRAGMA foreign_keys"), &fk)
	mustScan(t, s.Reader.QueryRowContext(ctx, "PRAGMA busy_timeout"), &busy)
	if fk != 1 || busy != 5000 {
		t.Fatalf("foreign_keys=%d busy_timeout=%d", fk, busy)
	}
	if !s.MigrationsApplied(ctx) {
		t.Fatal("migrations not applied")
	}
	// Re-applying is a no-op.
	if err := s.Migrate(ctx, coreMigrations, "migrations", "core_migrations"); err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	// Readers are query only.
	if _, err := s.Reader.ExecContext(ctx, "INSERT INTO settings (key) VALUES ('x')"); err == nil {
		t.Fatal("the read pool accepted a write")
	}
}

func TestWriteSerialisesAndRollsBack(t *testing.T) {
	s := open(t, Options{})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES (?, '{}')", "k"+string(rune('a'+i%26))+string(rune('a'+i/26)))
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	var n int
	mustScan(t, s.Reader.QueryRowContext(ctx, "SELECT count(*) FROM settings"), &n)
	if n != 50 {
		t.Fatalf("got %d rows", n)
	}
	boom := errors.New("boom")
	err := s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO settings (key) VALUES ('rolled')"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	mustScan(t, s.Reader.QueryRowContext(ctx, "SELECT count(*) FROM settings WHERE key = 'rolled'"), &n)
	if n != 0 {
		t.Fatal("failed write was not rolled back")
	}
	err = s.Write(ctx, func(tx *sql.Tx) error { panic("oops") })
	if err == nil {
		t.Fatal("a panicking write must fail")
	}
	if err := s.Exec(ctx, "INSERT INTO settings (key) VALUES ('exec')"); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyMode(t *testing.T) {
	var free atomic.Int64
	free.Store(1 << 40)
	changes := make(chan bool, 4)
	s := open(t, Options{
		FreeBytes:  func(string) (uint64, error) { return uint64(free.Load()), nil },
		OnReadOnly: func(on bool) { changes <- on },
	})
	ctx := context.Background()
	free.Store(100 << 20)
	s.CheckDisk()
	if !s.ReadOnly() {
		t.Fatal("expected read-only under 200 MB")
	}
	if err := s.Exec(ctx, "INSERT INTO settings (key) VALUES ('a')"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v", err)
	}
	if got := <-changes; !got {
		t.Fatal("callback did not report read-only on")
	}
	free.Store(1 << 40)
	s.CheckDisk()
	if s.ReadOnly() {
		t.Fatal("expected read-only to clear")
	}
	select {
	case got := <-changes:
		if got {
			t.Fatal("callback did not report read-only off")
		}
	case <-time.After(time.Second):
		t.Fatal("no callback")
	}
	if err := s.Exec(ctx, "INSERT INTO settings (key) VALUES ('a')"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateErrorsAndVacuum(t *testing.T) {
	s := open(t, Options{})
	ctx := context.Background()
	bad := fstest.MapFS{"m/x.sql": {Data: []byte("SELECT 1")}}
	if err := s.Migrate(ctx, bad, "m", "t_migrations"); err == nil {
		t.Fatal("a badly named migration must fail")
	}
	if err := s.Migrate(ctx, bad, "m", "Bad-Name"); err == nil {
		t.Fatal("a bad table name must fail")
	}
	good := fstest.MapFS{
		"m/0001_a.sql": {Data: []byte("CREATE TABLE o_a (x INTEGER)")},
		"m/0002_b.sql": {Data: []byte("CREATE TABLE o_b (x INTEGER)")},
	}
	if err := s.Migrate(ctx, good, "m", "orch_migrations"); err != nil {
		t.Fatal(err)
	}
	broken := fstest.MapFS{"m/0003_c.sql": {Data: []byte("CREATE TABLE")}}
	if err := s.Migrate(ctx, broken, "m", "orch_migrations"); err == nil {
		t.Fatal("broken SQL must fail")
	}
	dst := filepath.Join(t.TempDir(), "copy.db")
	if err := s.VacuumInto(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.VacuumInto(ctx, dst); err != nil {
		t.Fatal("vacuum over an existing file:", err)
	}
	if err := s.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := s.Exec(ctx, "SELECT 1"); !errors.Is(err, ErrClosed) {
		t.Fatalf("after close: %v", err)
	}
	if IsFull(errors.New("x")) {
		t.Fatal("IsFull on a plain error")
	}
}
