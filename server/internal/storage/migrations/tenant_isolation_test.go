package migrations

import (
	"database/sql"
	"errors"
	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"net/url"
	"path/filepath"
	"testing"
)

var crossTenantWrites = []struct {
	name        string
	crossTenant string
	sameTenant  string
}{
	{
		name:        "satellite attached to another group's item",
		crossTenant: `INSERT INTO item_identifications (id, group_id, item_id, kind, value, created_at, updated_at) VALUES ('ident-x', 'group-b', 'item-a', 'serial', 'SN-1', 1, 1)`,
		sameTenant:  `INSERT INTO item_identifications (id, group_id, item_id, kind, value, created_at, updated_at) VALUES ('ident-ok', 'group-a', 'item-a', 'serial', 'SN-1', 1, 1)`,
	},
	{
		name:        "item tagged with another group's label",
		crossTenant: `INSERT INTO item_labels (id, group_id, item_id, label_id, created_at, updated_at) VALUES ('link-x', 'group-a', 'item-a', 'label-b', 1, 1)`,
		sameTenant:  `INSERT INTO item_labels (id, group_id, item_id, label_id, created_at, updated_at) VALUES ('link-ok', 'group-a', 'item-a', 'label-a', 1, 1)`,
	},
	{
		name:        "location parented outside its group",
		crossTenant: `INSERT INTO locations (id, group_id, name, parent_id, created_at, updated_at) VALUES ('loc-x', 'group-b', 'Shed', 'loc-a', 1, 1)`,
		sameTenant:  `INSERT INTO locations (id, group_id, name, parent_id, created_at, updated_at) VALUES ('loc-ok', 'group-a', 'Shed', 'loc-a', 1, 1)`,
	},
}

func TestCompositeForeignKeysRejectCrossTenantRows(t *testing.T) {
	for _, tc := range crossTenantWrites {
		t.Run(tc.name, func(t *testing.T) {
			db := newTenantDB(t)

			if _, err := db.Exec(tc.sameTenant); err != nil {
				t.Fatalf("same-tenant write was rejected, so the cross-tenant result proves nothing about tenancy: %v", err)
			}

			_, err := db.Exec(tc.crossTenant)
			if err == nil {
				t.Fatal("cross-tenant write was accepted; P-3 isolation is not enforced by the schema")
			}
			var sqliteErr *sqlitedriver.Error
			if !errors.As(err, &sqliteErr) {
				t.Fatalf("cross-tenant write failed with %T (%v), want a SQLite foreign-key violation", err, err)
			}
			if sqliteErr.Code() != sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
				t.Errorf("cross-tenant write failed with SQLite code %d (%v), want %d SQLITE_CONSTRAINT_FOREIGNKEY — it was rejected, but not by the composite key",
					sqliteErr.Code(), err, sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY)
			}
		})
	}
}

func newTenantDB(t *testing.T) *sql.DB {
	t.Helper()

	q := url.Values{}
	q.Add("_pragma", "foreign_keys(ON)")
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "tenants.db")+"?"+q.Encode())
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
		t.Fatal("foreign_keys is off; every cross-tenant insert below would be accepted for the wrong reason")
	}

	if _, err := Apply(t.Context(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	for _, stmt := range []string{
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('group-a', 'A', 1, 1)`,
		`INSERT INTO groups (id, name, created_at, updated_at) VALUES ('group-b', 'B', 1, 1)`,
		`INSERT INTO locations (id, group_id, name, created_at, updated_at) VALUES ('loc-a', 'group-a', 'Garage', 1, 1)`,
		`INSERT INTO labels (id, group_id, name, created_at, updated_at) VALUES ('label-a', 'group-a', 'Tools', 1, 1)`,
		`INSERT INTO labels (id, group_id, name, created_at, updated_at) VALUES ('label-b', 'group-b', 'Theirs', 1, 1)`,
		`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at) VALUES ('item-a', 'group-a', 'Drill', 'ABC1', 1, 1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed tenants: %v (%s)", err, stmt)
		}
	}
	return db
}
