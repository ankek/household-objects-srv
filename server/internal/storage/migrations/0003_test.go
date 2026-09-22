package migrations

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestMigration0003AppliesCleanlyFromAnEmptyDatabase(t *testing.T) {
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

	cols := columns(t, db, "items")
	col, ok := cols["external_ref"]
	if !ok {
		t.Fatal("items.external_ref does not exist after Apply")
	}
	if col.typ != "TEXT" || col.notNull {
		t.Errorf("items.external_ref = %+v, want TEXT, nullable", col)
	}

	sessCols := columns(t, db, "import_sessions")
	for name, wantType := range map[string]string{
		"id":         "TEXT",
		"group_id":   "TEXT",
		"source":     "TEXT",
		"status":     "TEXT",
		"created_at": "INTEGER",
		"expires_at": "INTEGER",
	} {
		c, ok := sessCols[name]
		if !ok {
			t.Errorf("import_sessions.%s does not exist after Apply", name)
			continue
		}
		if c.typ != wantType {
			t.Errorf("import_sessions.%s type = %s, want %s", name, c.typ, wantType)
		}
	}
	for _, forbidden := range []string{"version", "deleted_at", "change_seq", "updated_at"} {
		if _, ok := sessCols[forbidden]; ok {
			t.Errorf("import_sessions.%s exists; this table is declared sync infrastructure (nonDomainTables) and should not carry it", forbidden)
		}
	}

	seedGroups(t, db, "group-a", "group-b")

	insertItem(t, db, "item-a1", "group-a", "AAA1", "homebox:key-1")
	insertItem(t, db, "item-b1", "group-b", "BBB1", "homebox:key-1")

	if err := tryInsertItem(db, "item-a2", "group-a", "AAA2", "homebox:key-1"); err == nil {
		t.Error("inserting a second item with the same (group_id, external_ref) succeeded; want the partial unique index to refuse it")
	}

	if err := tryInsertItem(db, "item-a3", "group-a", "AAA3", ""); err != nil {
		t.Errorf("insert item with external_ref NULL: %v", err)
	}
	if err := tryInsertItem(db, "item-a4", "group-a", "AAA4", ""); err != nil {
		t.Errorf("insert a second item with external_ref NULL in the same group: %v", err)
	}
}

func seedGroups(t *testing.T, db *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := db.Exec(
			`INSERT INTO groups (id, name, created_at, updated_at) VALUES (?, ?, 1, 1)`, id, id,
		); err != nil {
			t.Fatalf("seed group %s: %v", id, err)
		}
	}
}

func insertItem(t *testing.T, db *sql.DB, id, groupID, shortCode, externalRef string) {
	t.Helper()
	if err := tryInsertItem(db, id, groupID, shortCode, externalRef); err != nil {
		t.Fatalf("insert item %s: %v", id, err)
	}
}

func tryInsertItem(db *sql.DB, id, groupID, shortCode, externalRef string) error {
	var ref any
	if externalRef != "" {
		ref = externalRef
	}
	_, err := db.Exec(
		`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at, external_ref)
		 VALUES (?, ?, 'Item', ?, 1, 1, ?)`,
		id, groupID, shortCode, ref,
	)
	return err
}
