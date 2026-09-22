package migrations

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestMigration0002AppliesCleanlyFromAnEmptyDatabase(t *testing.T) {
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
		t.Fatalf("schema version after Apply = %d, want 5 (0001 + 0002 + 0003 + 0004 + 0005 -- if this file is failing because a SIXTH migration landed, update the want again, not the test's premise)", version)
	}

	cols := columns(t, db, "groups")
	for _, name := range []string{"warranty_visible", "sale_visible", "purchase_visible"} {
		c, ok := cols[name]
		if !ok {
			t.Fatalf("groups.%s does not exist after Apply", name)
		}
		if c.typ != "INTEGER" || !c.notNull {
			t.Errorf("groups.%s = %+v, want INTEGER NOT NULL", name, c)
		}
	}

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('g-empty-defaults', 'g', 1, 1)`,
	); err != nil {
		t.Fatalf("insert a group naming none of the three visibility columns: %v", err)
	}
	var warranty, sale, purchase int64
	if err := db.QueryRowContext(t.Context(),
		`SELECT warranty_visible, sale_visible, purchase_visible FROM groups WHERE id = 'g-empty-defaults'`,
	).Scan(&warranty, &sale, &purchase); err != nil {
		t.Fatalf("read back defaults: %v", err)
	}
	if warranty != 0 || sale != 0 || purchase != 0 {
		t.Errorf("defaults = {warranty:%d sale:%d purchase:%d}, want all 0 (hidden)", warranty, sale, purchase)
	}

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at, warranty_visible) VALUES ('g-bad-check', 'g', 1, 1, 2)`,
	); err == nil {
		t.Error("insert with warranty_visible=2 succeeded, want the CHECK (warranty_visible IN (0, 1)) constraint to refuse it")
	}
}
