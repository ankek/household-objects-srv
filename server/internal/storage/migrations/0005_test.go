package migrations

import (
	"database/sql"
	"errors"
	"github.com/pressly/goose/v3"
	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"net/url"
	"path/filepath"
	"testing"
)

func TestMigration0005AppliesCleanlyFromAnEmptyDatabase(t *testing.T) {
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

	cols := columns(t, db, "field_versions")
	want := map[string]string{
		"group_id":    "TEXT",
		"entity_type": "TEXT",
		"entity_id":   "TEXT",
		"field_name":  "TEXT",
		"version":     "INTEGER",
		"updated_at":  "INTEGER",
	}
	for name, typ := range want {
		c, ok := cols[name]
		if !ok {
			t.Errorf("field_versions.%s does not exist after Apply", name)
			continue
		}
		if c.typ != typ {
			t.Errorf("field_versions.%s type = %s, want %s", name, c.typ, typ)
		}
		if !c.notNull {
			t.Errorf("field_versions.%s is nullable, want NOT NULL", name)
		}
	}
	for _, forbidden := range []string{"id", "deleted_at", "change_seq"} {
		if _, ok := cols[forbidden]; ok {
			t.Errorf("field_versions.%s exists; this table is sync infrastructure (nonDomainTables) with a composite primary key (compositePrimaryKeyTables) and should not carry it", forbidden)
		}
	}
}

func TestMigration0005AppliesOntoExistingDataWithoutTouchingIt(t *testing.T) {
	db, provider := openMigrationsDB(t)

	if _, err := provider.UpTo(t.Context(), 4); err != nil {
		t.Fatalf("UpTo(4): %v", err)
	}

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('group-a', 'A', 1000, 1000)`,
	); err != nil {
		t.Fatalf("seed group at version 4: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at, version, change_seq)
		 VALUES ('item-a', 'group-a', 'Drill', 'ABC1', 1000, 1000, 3, 7)`,
	); err != nil {
		t.Fatalf("seed item at version 4: %v", err)
	}

	if _, err := provider.UpTo(t.Context(), 5); err != nil {
		t.Fatalf("UpTo(5): %v", err)
	}

	var name string
	var itemVersion, changeSeq int64
	if err := db.QueryRowContext(t.Context(),
		`SELECT name, version, change_seq FROM items WHERE id = 'item-a'`,
	).Scan(&name, &itemVersion, &changeSeq); err != nil {
		t.Fatalf("read back item seeded before migration 0005: %v", err)
	}
	if name != "Drill" || itemVersion != 3 || changeSeq != 7 {
		t.Errorf("item-a after migration 0005 = {name:%q version:%d change_seq:%d}, want unchanged {Drill 3 7}", name, itemVersion, changeSeq)
	}

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO field_versions (group_id, entity_type, entity_id, field_name, version, updated_at)
		 VALUES ('group-a', 'item', 'item-a', 'name', 3, 1000)`,
	); err != nil {
		t.Fatalf("insert field_versions row after migration 0005: %v", err)
	}
}

func TestMigration0005DownRevertsCleanly(t *testing.T) {
	db, provider := openMigrationsDB(t)

	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('group-a', 'A', 1000, 1000)`,
	); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO field_versions (group_id, entity_type, entity_id, field_name, version, updated_at)
		 VALUES ('group-a', 'item', 'item-a', 'name', 1, 1000)`,
	); err != nil {
		t.Fatalf("seed field_versions row: %v", err)
	}

	if _, err := provider.Down(t.Context()); err != nil {
		t.Fatalf("Down: %v", err)
	}

	version, err := provider.GetDBVersion(t.Context())
	if err != nil {
		t.Fatalf("GetDBVersion after Down: %v", err)
	}
	if version != 4 {
		t.Fatalf("schema version after reverting migration 0005 = %d, want 4", version)
	}

	var exists int
	if err := db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = 'field_versions'`,
	).Scan(&exists); err != nil {
		t.Fatalf("check field_versions absence: %v", err)
	}
	if exists != 0 {
		t.Error("field_versions still exists after Down; the Down migration did not drop it")
	}

	var groupName string
	if err := db.QueryRowContext(t.Context(),
		`SELECT name FROM groups WHERE id = 'group-a'`,
	).Scan(&groupName); err != nil {
		t.Fatalf("groups survives Down: %v", err)
	}
	if groupName != "A" {
		t.Errorf("groups.name after Down = %q, want unchanged %q", groupName, "A")
	}
}

func TestFieldVersionsPrimaryKeyRejectsDuplicateTuple(t *testing.T) {
	db, provider := openMigrationsDB(t)
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('group-a', 'A', 1000, 1000)`,
	); err != nil {
		t.Fatalf("seed group: %v", err)
	}

	insert := `INSERT INTO field_versions (group_id, entity_type, entity_id, field_name, version, updated_at)
	           VALUES ('group-a', 'item', 'item-a', 'name', ?, 1000)`
	if _, err := db.ExecContext(t.Context(), insert, 3); err != nil {
		t.Fatalf("first insert for the tuple: %v", err)
	}

	_, err := db.ExecContext(t.Context(), insert, 4)
	if err == nil {
		t.Fatal("second insert for the identical (group_id, entity_type, entity_id, field_name) tuple succeeded; want the primary key to refuse it")
	}
	var sqliteErr *sqlitedriver.Error
	if !errors.As(err, &sqliteErr) {
		t.Fatalf("duplicate-tuple insert failed with %T (%v), want a SQLite constraint violation", err, err)
	}
	if sqliteErr.Code() != sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY {
		t.Errorf("duplicate-tuple insert failed with SQLite code %d (%v), want %d SQLITE_CONSTRAINT_PRIMARYKEY",
			sqliteErr.Code(), err, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY)
	}
}

func TestFieldVersionsForeignKeyRejectsUnknownGroup(t *testing.T) {
	db, provider := openMigrationsDB(t)
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up: %v", err)
	}

	_, err := db.ExecContext(t.Context(),
		`INSERT INTO field_versions (group_id, entity_type, entity_id, field_name, version, updated_at)
		 VALUES ('no-such-group', 'item', 'item-a', 'name', 1, 1000)`,
	)
	if err == nil {
		t.Fatal("insert referencing an unknown group_id succeeded; want the foreign key to refuse it")
	}
	var sqliteErr *sqlitedriver.Error
	if !errors.As(err, &sqliteErr) {
		t.Fatalf("unknown-group insert failed with %T (%v), want a SQLite foreign-key violation", err, err)
	}
	if sqliteErr.Code() != sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
		t.Errorf("unknown-group insert failed with SQLite code %d (%v), want %d SQLITE_CONSTRAINT_FOREIGNKEY",
			sqliteErr.Code(), err, sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY)
	}
}

func openMigrationsDB(t *testing.T) (*sql.DB, *goose.Provider) {
	t.Helper()

	q := url.Values{}
	q.Add("_pragma", "foreign_keys(ON)")
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "field_versions.db")+"?"+q.Encode())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read back foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatal("foreign_keys is off; the foreign-key assertions below would be vacuous")
	}

	provider, err := goose.NewProvider(dialect, db, FS)
	if err != nil {
		t.Fatalf("build goose provider: %v", err)
	}
	return db, provider
}
