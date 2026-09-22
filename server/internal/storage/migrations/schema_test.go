package migrations

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

var syncPrimitives = []struct {
	name string
	typ  string
}{
	{"id", "TEXT"},
	{"group_id", "TEXT"},
	{"updated_at", "INTEGER"},
	{"version", "INTEGER"},
	{"deleted_at", "INTEGER"},
	{"change_seq", "INTEGER"},
}

var nonDomainTables = map[string]string{
	"groups":           "tenant root; it has nothing to be scoped to but itself, so five primitives and no group_id",
	"mutations":        "sync infrastructure; giving the push ledger a version would mean versioning the version log",
	"conflicts":        "sync infrastructure; clients read it through a plain group-scoped list, never through /sync/pull",
	"items_fts":        "derived FTS5 index shadowing items; rebuildable from its sources, not a table of record",
	"goose_db_version": "goose's own migration bookkeeping, created by the runner rather than by migration 0001",
	"import_sessions":  "sync infrastructure, ephemeral and TTL'd (expires_at); never synced to Android, never conflict-resolved, never read through /sync/pull (migration 0003, T148a, A122.2)",
	"field_versions":   "sync infrastructure, not sync payload; the D4.2 per-field merge ledger (migration 0005, T165a) -- never synced to Android, never itself conflict-resolved, never read through /sync/pull, the identical mutations/conflicts exemption restated for a third table",
}

var compositePrimaryKeyTables = map[string][]string{
	"field_versions": {"group_id", "entity_type", "entity_id", "field_name"},
}

func TestDomainTablesCarrySixSyncPrimitives(t *testing.T) {
	db := newSchemaDB(t)

	domain := domainTables(t, db)
	if len(domain) == 0 {
		t.Fatal("enumerated zero domain tables; the introspection query is broken and every other assertion here is vacuous")
	}

	for _, table := range domain {
		cols := columns(t, db, table)
		for _, p := range syncPrimitives {
			col, ok := cols[p.name]
			if !ok {
				t.Errorf("%s: missing sync primitive %q (P-2 requires all six on every domain table)", table, p.name)
				continue
			}
			if col.typ != p.typ {
				t.Errorf("%s.%s: declared type %s, want %s", table, p.name, col.typ, p.typ)
			}
		}
		if col, ok := cols["group_id"]; ok && !col.notNull {
			t.Errorf("%s.group_id: nullable, want NOT NULL", table)
		}
		if col, ok := cols["deleted_at"]; ok && col.notNull {
			t.Errorf("%s.deleted_at: NOT NULL, want nullable so a live row can leave it unset", table)
		}
	}
}

func TestGroupsIsTheTenantRoot(t *testing.T) {
	db := newSchemaDB(t)
	cols := columns(t, db, "groups")

	for _, p := range syncPrimitives {
		if p.name == "group_id" {
			continue
		}
		if _, ok := cols[p.name]; !ok {
			t.Errorf("groups: missing sync primitive %q", p.name)
		}
	}
	if _, ok := cols["group_id"]; ok {
		t.Error("groups.group_id exists; the tenant root cannot be scoped to itself")
	}
	if _, ok := cols["change_seq_counter"]; !ok {
		t.Error("groups: missing change_seq_counter; nothing can allocate a per-group change_seq")
	}
}

func TestSyncInfrastructureTablesOmitVersionAndChangeSeq(t *testing.T) {
	db := newSchemaDB(t)

	for _, table := range []string{"mutations", "conflicts"} {
		cols := columns(t, db, table)
		for _, required := range []string{"id", "group_id", "updated_at"} {
			if _, ok := cols[required]; !ok {
				t.Errorf("%s: missing %q; tenant isolation and retention both need it", table, required)
			}
		}
		for _, forbidden := range []string{"version", "change_seq"} {
			if _, ok := cols[forbidden]; ok {
				t.Errorf("%s.%s exists; sync infrastructure is not sync payload (data-model.md scoping call)", table, forbidden)
			}
		}
	}
}

func TestNoAutoincrementAnywhere(t *testing.T) {
	db := newSchemaDB(t)

	for _, table := range ourTables(t, db) {
		var ddl string
		if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type IN ('table', 'view') AND name = ?`, table).Scan(&ddl); err != nil {
			t.Fatalf("read DDL for %s: %v", table, err)
		}
		if strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
			t.Errorf("%s: declares AUTOINCREMENT; P-2 vetoes server-allocated integer keys", table)
		}
	}
}

func TestEveryPrimaryKeyIsATextID(t *testing.T) {
	db := newSchemaDB(t)

	for _, table := range baseTables(t, db) {
		var pk []column
		for _, col := range columns(t, db, table) {
			if col.pk > 0 {
				pk = append(pk, col)
			}
		}
		sort.Slice(pk, func(i, j int) bool { return pk[i].pk < pk[j].pk })

		if want, ok := compositePrimaryKeyTables[table]; ok {
			var got []string
			for _, col := range pk {
				got = append(got, col.name)
				if col.typ != "TEXT" {
					t.Errorf("%s: composite primary key column %s is %s, want TEXT", table, col.name, col.typ)
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s: composite primary key is %v, want %v (compositePrimaryKeyTables)", table, got, want)
			}
			continue
		}

		if len(pk) != 1 {
			t.Errorf("%s: primary key has %d columns %s, want the single TEXT id (or an entry in compositePrimaryKeyTables)", table, len(pk), columnNames(pk))
			continue
		}
		if pk[0].name != "id" || pk[0].typ != "TEXT" {
			t.Errorf("%s: primary key is %s %s, want id TEXT (client-generated UUIDv7)", table, pk[0].name, pk[0].typ)
		}
	}
}

func TestEveryCompositePrimaryKeyExceptionIsStillNeeded(t *testing.T) {
	db := newSchemaDB(t)

	present := make(map[string]bool)
	for _, table := range baseTables(t, db) {
		present[table] = true
	}

	for table := range compositePrimaryKeyTables {
		if !present[table] {
			t.Errorf("compositePrimaryKeyTables declares %q but no such table exists", table)
		}
	}
}

func TestEveryBaseTableIsStrict(t *testing.T) {
	db := newSchemaDB(t)

	for _, table := range baseTables(t, db) {
		var strict int
		if err := db.QueryRow(`SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&strict); err != nil {
			t.Fatalf("read STRICT flag for %s: %v", table, err)
		}
		if strict != 1 {
			t.Errorf("%s: not STRICT", table)
		}
	}
}

func TestEveryDeclaredExceptionExists(t *testing.T) {
	db := newSchemaDB(t)

	present := make(map[string]bool)
	for _, table := range ourTables(t, db) {
		present[table] = true
	}
	present["goose_db_version"] = true

	for table, reason := range nonDomainTables {
		if !present[table] {
			t.Errorf("nonDomainTables declares %q (%s) but no such table exists", table, reason)
		}
	}
}

type column struct {
	name    string
	typ     string
	notNull bool
	pk      int
}

func columnNames(cols []column) string {
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.name)
	}
	return "[" + strings.Join(names, " ") + "]"
}

func newSchemaDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "schema.db"))
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
		t.Fatalf("apply migrations: %v", err)
	}
	if version <= 0 {
		t.Fatalf("schema version = %d, want an applied migration", version)
	}
	return db
}

func ourTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
		SELECT name FROM pragma_table_list
		WHERE schema = 'main'
		  AND type IN ('table', 'virtual')
		  AND name NOT LIKE 'sqlite_%'   -- SQLite's own reserved namespace, not ours
		  AND name <> 'goose_db_version'
		ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	return tables
}

func baseTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	for _, table := range ourTables(t, db) {
		var typ string
		if err := db.QueryRow(`SELECT type FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&typ); err != nil {
			t.Fatalf("read type for %s: %v", table, err)
		}
		if typ == "table" {
			out = append(out, table)
		}
	}
	return out
}

func domainTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	for _, table := range ourTables(t, db) {
		if _, exempt := nonDomainTables[table]; exempt {
			continue
		}
		out = append(out, table)
	}
	return out
}

func columns(t *testing.T, db *sql.DB, table string) map[string]column {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf(`SELECT name, type, "notnull", pk FROM pragma_table_info(%q)`, table))
	if err != nil {
		t.Fatalf("read columns of %s: %v", table, err)
	}
	defer rows.Close()

	cols := make(map[string]column)
	for rows.Next() {
		var c column
		var notNull int
		if err := rows.Scan(&c.name, &c.typ, &notNull, &c.pk); err != nil {
			t.Fatalf("scan column of %s: %v", table, err)
		}
		c.notNull = notNull != 0
		cols[c.name] = c
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read columns of %s: %v", table, err)
	}
	if len(cols) == 0 {
		t.Fatalf("%s: no columns; the table does not exist", table)
	}
	return cols
}
