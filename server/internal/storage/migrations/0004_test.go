package migrations

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestMigration0004AppliesCleanlyFromAnEmptyDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})

	version, err := Apply(t.Context(), db)
	if err != nil {
		t.Fatalf("Apply against an empty database: %v", err)
	}
	if version != 5 {
		t.Fatalf("schema version after Apply = %d, want 5 (0001 + 0002 + 0003 + 0004 + 0005 -- if this file is failing because a SIXTH migration landed, update the want, not the test's premise)", version)
	}

	cols := columns(t, db, "groups")
	col, ok := cols["tombstone_low_watermark"]
	if !ok {
		t.Fatal("groups.tombstone_low_watermark does not exist after Apply")
	}
	if col.typ != "INTEGER" || !col.notNull {
		t.Errorf("groups.tombstone_low_watermark = %+v, want INTEGER NOT NULL", col)
	}

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('g-empty-defaults', 'g', 1, 1)`,
	); err != nil {
		t.Fatalf("insert a group naming no tombstone_low_watermark: %v", err)
	}
	var watermark int64
	if err := db.QueryRowContext(t.Context(),
		`SELECT tombstone_low_watermark FROM groups WHERE id = 'g-empty-defaults'`,
	).Scan(&watermark); err != nil {
		t.Fatalf("read back default: %v", err)
	}
	if watermark != 0 {
		t.Errorf("default tombstone_low_watermark = %d, want 0", watermark)
	}

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at, tombstone_low_watermark) VALUES ('g-advanced', 'g', 1, 1, 42)`,
	); err != nil {
		t.Fatalf("insert a group with an explicit tombstone_low_watermark: %v", err)
	}
	var advanced int64
	if err := db.QueryRowContext(t.Context(),
		`SELECT tombstone_low_watermark FROM groups WHERE id = 'g-advanced'`,
	).Scan(&advanced); err != nil {
		t.Fatalf("read back explicit value: %v", err)
	}
	if advanced != 42 {
		t.Errorf("explicit tombstone_low_watermark = %d, want 42", advanced)
	}
}
