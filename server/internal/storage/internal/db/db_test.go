package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

const testReadConns = 3

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.Context(), Config{
		Path:      filepath.Join(t.TempDir(), "db", "hho.db"),
		ReadConns: testReadConns,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return store
}

func seedGroup(t *testing.T, s *Store, id string) {
	t.Helper()
	err := s.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO groups (id, name, created_at, updated_at) VALUES (?, ?, 1, 1)`, id, id)
		return err
	})
	if err != nil {
		t.Fatalf("seed group: %v", err)
	}
}

func TestPragmasHoldOnEveryPooledConnection(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	conns := make([]*sql.Conn, 0, testReadConns)
	for i := 0; i < testReadConns; i++ {
		conn, err := store.Reader().Conn(ctx)
		if err != nil {
			t.Fatalf("acquire read connection %d: %v", i, err)
		}
		defer conn.Close()
		conns = append(conns, conn)
	}
	writeConn, err := store.Writer().Conn(ctx)
	if err != nil {
		t.Fatalf("acquire write connection: %v", err)
	}
	defer writeConn.Close()
	conns = append(conns, writeConn)

	for i, conn := range conns {
		if err := assertPragmas(ctx, conn); err != nil {
			t.Errorf("connection %d: %v", i, err)
		}
	}
}

func TestOpenAppliesMigrations(t *testing.T) {
	store := newTestStore(t)

	if store.SchemaVersion() <= 0 {
		t.Fatalf("SchemaVersion() = %d, want an applied migration", store.SchemaVersion())
	}
	var tables int
	err := store.Reader().QueryRowContext(t.Context(),
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'items'`).Scan(&tables)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tables != 1 {
		t.Errorf("items table present = %d, want 1", tables)
	}
}

func TestForeignKeysRejectOrphanSatellite(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	err := store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_identifications (id, group_id, item_id, kind, value, created_at, updated_at)
			 VALUES ('d1', 'g1', 'no-such-item', 'serial', 'SN-1', 1, 1)`)
		return err
	})
	if err == nil {
		t.Fatal("orphan satellite insert succeeded; foreign keys are not enforced")
	}
}

func TestTxCommitsOnSuccess(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	if err := store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at)
			 VALUES ('i1', 'g1', 'Drill', 'AAA1', 1, 1)`)
		return err
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}

	if got := countItems(t, store); got != 1 {
		t.Errorf("items after commit = %d, want 1", got)
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")
	sentinel := errors.New("caller failed")

	err := store.Tx(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at)
			 VALUES ('i1', 'g1', 'Drill', 'AAA1', 1, 1)`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Tx error = %v, want %v", err, sentinel)
	}
	if got := countItems(t, store); got != 0 {
		t.Errorf("items after rollback = %d, want 0", got)
	}
}

func TestTxRollsBackOnPanic(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Error("panic did not propagate through Tx")
			}
		}()
		_ = store.Tx(t.Context(), func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(t.Context(),
				`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at)
				 VALUES ('i1', 'g1', 'Drill', 'AAA1', 1, 1)`); err != nil {
				t.Errorf("insert: %v", err)
			}
			panic("boom")
		})
	}()

	if got := countItems(t, store); got != 0 {
		t.Errorf("items after panic = %d, want 0", got)
	}
	if err := store.Tx(t.Context(), func(tx *sql.Tx) error { return nil }); err != nil {
		t.Fatalf("write pool unusable after panic: %v", err)
	}
}

func TestImportTxAllowsSatellitesBeforeParent(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	insert := func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_identifications (id, group_id, item_id, kind, value, created_at, updated_at)
			 VALUES ('d1', 'g1', 'i1', 'serial', 'SN-1', 1, 1)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at)
			 VALUES ('i1', 'g1', 'Drill', 'AAA1', 1, 1)`)
		return err
	}

	if err := store.Tx(t.Context(), insert); err == nil {
		t.Fatal("satellites-first insert succeeded outside ImportTx; deferral would be redundant")
	}
	if err := store.ImportTx(t.Context(), insert); err != nil {
		t.Fatalf("ImportTx: %v", err)
	}
	if got := countItems(t, store); got != 1 {
		t.Errorf("items after import = %d, want 1", got)
	}
}

func TestImportTxStillEnforcesForeignKeysAtCommit(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	err := store.ImportTx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_identifications (id, group_id, item_id, kind, value, created_at, updated_at)
			 VALUES ('d1', 'g1', 'never-inserted', 'serial', 'SN-1', 1, 1)`)
		return err
	})
	if err == nil {
		t.Fatal("import with a permanently missing parent committed; deferral disabled enforcement")
	}
}

func countItems(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.Reader().QueryRowContext(t.Context(), `SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return n
}
