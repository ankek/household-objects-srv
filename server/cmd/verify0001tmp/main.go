package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/migrations"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var fails int

func check(name string, cond bool, detail string) {
	status := "PASS"
	if !cond {
		status = "FAIL"
		fails++
	}
	fmt.Printf("  [%s] %-58s %s\n", status, name, detail)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func open(path string) *sql.DB {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	must(err)
	db.SetMaxOpenConns(1)
	return db
}

var sixPrimitiveTables = []string{
	"users", "sessions", "device_tokens", "invites",
	"locations", "labels", "custom_field_defs",
	"items", "item_labels", "item_identifications", "item_custom_fields",
	"item_warranty", "item_purchase", "item_sale", "stock_adjustments", "attachments",
}

func cols(db *sql.DB, table string) map[string]string {
	out := map[string]string{}
	rows, err := db.Query(fmt.Sprintf("SELECT name, type, \"notnull\" FROM pragma_table_info(%q)", table))
	must(err)
	defer rows.Close()
	for rows.Next() {
		var n, t string
		var nn int
		must(rows.Scan(&n, &t, &nn))
		out[n] = fmt.Sprintf("%s notnull=%d", t, nn)
	}
	return out
}

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "hho-verify")
	must(err)
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "hho.db")

	db := open(path)
	defer db.Close()

	fmt.Println("== 1. goose applies migration 0001 ==")
	start := time.Now()
	v, err := migrations.Apply(ctx, db)
	must(err)
	fmt.Printf("  applied in %s, schema version = %d\n", time.Since(start).Round(time.Millisecond), v)
	check("goose reports version 1", v == 1, fmt.Sprintf("version=%d", v))

	v2, err := migrations.Apply(ctx, db)
	must(err)
	check("re-apply is a no-op", v2 == 1, fmt.Sprintf("version=%d", v2))

	fmt.Println("\n== 2. object inventory ==")
	var nTables, nIdx, nTrig int
	must(db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'items_fts%' AND name<>'goose_db_version'`).Scan(&nTables))
	must(db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL`).Scan(&nIdx))
	must(db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='trigger'`).Scan(&nTrig))
	fmt.Printf("  tables=%d  explicit indexes=%d  triggers=%d\n", nTables, nIdx, nTrig)
	check("19 base tables + items_fts", nTables == 19, fmt.Sprintf("got %d", nTables))
	check("15 FTS triggers", nTrig == 15, fmt.Sprintf("got %d", nTrig))

	fmt.Println("\n== 3. P-2 six sync primitives on every domain table ==")
	prims := []string{"id", "group_id", "updated_at", "version", "deleted_at", "change_seq"}
	for _, t := range sixPrimitiveTables {
		c := cols(db, t)
		missing := []string{}
		for _, p := range prims {
			if _, ok := c[p]; !ok {
				missing = append(missing, p)
			}
		}
		check(t, len(missing) == 0, fmt.Sprintf("%d cols, missing=%v", len(c), missing))
	}
	g := cols(db, "groups")
	_, hasSelfGroup := g["group_id"]
	check("groups: five primitives, no self group_id",
		!hasSelfGroup && g["id"] != "" && g["updated_at"] != "" && g["version"] != "" && g["change_seq"] != "" && func() bool { _, ok := g["deleted_at"]; return ok }(),
		fmt.Sprintf("group_id present=%v, change_seq_counter present=%v", hasSelfGroup, g["change_seq_counter"] != ""))
	for _, t := range []string{"mutations", "conflicts"} {
		c := cols(db, t)
		_, hasVersion := c["version"]
		_, hasSeq := c["change_seq"]
		_, hasGroup := c["group_id"]
		check(t+": id+group_id, no version/change_seq", hasGroup && !hasVersion && !hasSeq,
			fmt.Sprintf("group_id=%v version=%v change_seq=%v", hasGroup, hasVersion, hasSeq))
	}

	fmt.Println("\n== 4. no autoincrement PK anywhere (P-2 veto) ==")
	var ai int
	must(db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND sql LIKE '%AUTOINCREMENT%' AND name<>'goose_db_version'`).Scan(&ai))
	check("zero AUTOINCREMENT on our tables", ai == 0, fmt.Sprintf("got %d", ai))
	rowsPK, err := db.Query(`SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'items_fts%' AND name<>'goose_db_version'`)
	must(err)
	var ourTables []string
	for rowsPK.Next() {
		var n string
		must(rowsPK.Scan(&n))
		ourTables = append(ourTables, n)
	}
	rowsPK.Close()
	badPK := []string{}
	for _, t := range ourTables {
		var pkName, pkType string
		must(db.QueryRow(fmt.Sprintf(`SELECT name, type FROM pragma_table_info(%q) WHERE pk=1`, t)).Scan(&pkName, &pkType))
		if pkName != "id" || pkType != "TEXT" {
			badPK = append(badPK, fmt.Sprintf("%s(%s %s)", t, pkName, pkType))
		}
	}
	check("every PK is a TEXT `id` (UUIDv7), never an integer", len(badPK) == 0, fmt.Sprintf("offenders=%v", badPK))

	fmt.Println("\n== 5. every table is STRICT ==")
	rows, err := db.Query(`SELECT name, sql FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'items_fts%' AND name<>'goose_db_version'`)
	must(err)
	nonStrict := []string{}
	for rows.Next() {
		var n, s string
		must(rows.Scan(&n, &s))
		if !strings.Contains(strings.ToUpper(s), "STRICT") {
			nonStrict = append(nonStrict, n)
		}
	}
	rows.Close()
	check("all base tables STRICT", len(nonStrict) == 0, fmt.Sprintf("non-strict=%v", nonStrict))

	fmt.Println("\n== 6. A21: every index leads with group_id, or is a declared exception ==")
	exceptions := map[string]string{
		"ux_users_username":           "pre-auth login lookup",
		"ux_sessions_token_hash":      "pre-auth bearer token",
		"ux_device_tokens_token_hash": "pre-auth bearer token",
		"ux_invites_token_hash":       "unauthenticated redemption",
		"ux_attachments_storage_path": "filesystem orphan reconciler",
	}
	rows, err = db.Query(`SELECT name, tbl_name FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL ORDER BY tbl_name, name`)
	must(err)
	type ix struct{ name, table string }
	var all []ix
	for rows.Next() {
		var n, t string
		must(rows.Scan(&n, &t))
		all = append(all, ix{n, t})
	}
	rows.Close()
	bad := []string{}
	for _, i := range all {
		var first string
		must(db.QueryRow(fmt.Sprintf("SELECT name FROM pragma_index_info(%q) WHERE seqno=0", i.name)).Scan(&first))
		if first == "group_id" {
			continue
		}
		if _, ok := exceptions[i.name]; ok {
			continue
		}
		if i.table == "groups" {
			continue
		}
		bad = append(bad, i.name+"("+first+")")
	}
	fmt.Printf("  %d explicit indexes, %d declared A21 exceptions\n", len(all), len(exceptions))
	check("no undeclared non-group_id-leading index", len(bad) == 0, fmt.Sprintf("offenders=%v", bad))

	fmt.Println("\n== 7. CHECK constraints are enforced by modernc.org/sqlite ==")
	must(exec(db, `INSERT INTO groups(id,name,created_at,updated_at) VALUES('G1','Home',1,1)`))
	must(exec(db, `INSERT INTO users(id,group_id,username,password_hash,created_at,updated_at) VALUES('U1','G1','alice','h',1,1)`))
	must(exec(db, `INSERT INTO items(id,group_id,name,description,short_code,created_at,updated_at) VALUES('I1','G1','Drill','cordless hammer drill','ABC1',1,1)`))
	err = exec(db, `INSERT INTO item_identifications(id,group_id,item_id,kind,value,created_at,updated_at) VALUES('D0','G1','I1','nonsense','x',1,1)`)
	check("bad enum rejected by CHECK", err != nil, errText(err))
	err = exec(db, `INSERT INTO groups(id,name,registration_enabled,created_at,updated_at) VALUES('GX','x',7,1,1)`)
	check("bad boolean rejected by CHECK", err != nil, errText(err))

	fmt.Println("\n== 8. STRICT rejects a type mismatch ==")
	err = exec(db, `INSERT INTO items(id,group_id,name,description,short_code,quantity,created_at,updated_at) VALUES('IX','G1','n','d','ZZZ9','not-a-number',1,1)`)
	check("TEXT into INTEGER rejected", err != nil, errText(err))

	fmt.Println("\n== 9. P-3: composite FK blocks a cross-tenant satellite row ==")
	must(exec(db, `INSERT INTO groups(id,name,created_at,updated_at) VALUES('G2','Other',1,1)`))
	err = exec(db, `INSERT INTO item_identifications(id,group_id,item_id,kind,value,created_at,updated_at) VALUES('D1','G2','I1','serial','SN-1',1,1)`)
	check("group B cannot attach to group A's item", err != nil, errText(err))
	must(exec(db, `INSERT INTO labels(id,group_id,name,created_at,updated_at) VALUES('L2','G2','Theirs',1,1)`))
	err = exec(db, `INSERT INTO item_labels(id,group_id,item_id,label_id,created_at,updated_at) VALUES('IL1','G1','I1','L2',1,1)`)
	check("item cannot be tagged with another group's label", err != nil, errText(err))
	err = exec(db, `INSERT INTO locations(id,group_id,name,parent_id,created_at,updated_at) VALUES('LOC2','G2','Shed','LOCX',1,1)`)
	check("location cannot be parented outside its group", err != nil, errText(err))

	fmt.Println("\n== 10. tombstones do not block re-create (partial unique indexes) ==")
	must(exec(db, `INSERT INTO labels(id,group_id,name,created_at,updated_at) VALUES('L1','G1','Tools',1,1)`))
	must(exec(db, `INSERT INTO item_labels(id,group_id,item_id,label_id,created_at,updated_at) VALUES('IL2','G1','I1','L1',1,1)`))
	must(exec(db, `UPDATE item_labels SET deleted_at=2 WHERE id='IL2'`))
	err = exec(db, `INSERT INTO item_labels(id,group_id,item_id,label_id,created_at,updated_at) VALUES('IL3','G1','I1','L1',1,1)`)
	check("re-tag after tombstone succeeds", err == nil, errText(err))
	err = exec(db, `INSERT INTO item_labels(id,group_id,item_id,label_id,created_at,updated_at) VALUES('IL4','G1','I1','L1',1,1)`)
	check("duplicate LIVE link still rejected", err != nil, errText(err))
	must(exec(db, `UPDATE items SET deleted_at=2 WHERE id='I1'`))
	err = exec(db, `INSERT INTO items(id,group_id,name,description,short_code,created_at,updated_at) VALUES('I2','G1','Drill2','d','ABC1',1,1)`)
	check("short_code reusable after tombstone", err == nil, errText(err))
	err = exec(db, `UPDATE items SET deleted_at=NULL WHERE id='I1'`)
	check("resurrecting a tombstone whose key was reused is REJECTED", err != nil, errText(err))
	must(exec(db, `DELETE FROM items WHERE id='I2'`))
	must(exec(db, `UPDATE items SET deleted_at=NULL WHERE id='I1'`))
	must(exec(db, `DELETE FROM item_labels WHERE id='IL3'`))

	fmt.Println("\n== 11. FTS5 index behaviour ==")
	must(exec(db, `INSERT INTO item_identifications(id,group_id,item_id,kind,value,created_at,updated_at) VALUES('D2','G1','I1','serial','SN-77-XY',1,1)`))
	must(exec(db, `INSERT INTO item_purchase(id,group_id,item_id,vendor,notes,created_at,updated_at) VALUES('P1','G1','I1','Acme','bought at the flohmarkt',1,1)`))
	check("match on name", ftsCount(db, "Drill") == 1, "")
	check("match on description", ftsCount(db, "hammer") == 1, "")
	check("match on identifier (satellite-fed)", ftsCount(db, "SN") == 1, "")
	check("match on detail-block notes", ftsCount(db, "flohmarkt") == 1, "")
	check("diacritic folding (flohmarkt)", ftsCount(db, "flohmarkt") == 1, "")
	must(exec(db, `UPDATE items SET name='Impact Driver', updated_at=3 WHERE id='I1'`))
	check("rename reindexes", ftsCount(db, "Impact") == 1 && ftsCount(db, "Driver") == 1, "")
	check("identifier survives item update", ftsCount(db, "SN") == 1, "")
	must(exec(db, `UPDATE items SET deleted_at=9 WHERE id='I1'`))
	check("soft delete drops item from index", ftsCount(db, "Impact") == 0, "")
	must(exec(db, `UPDATE items SET deleted_at=NULL WHERE id='I1'`))
	check("undelete restores index row", ftsCount(db, "Impact") == 1 && ftsCount(db, "SN") == 1, "")
	must(exec(db, `UPDATE item_identifications SET deleted_at=9 WHERE id='D2'`))
	check("soft-deleted identifier leaves index", ftsCount(db, "SN") == 0, "")

	fmt.Println("\n== 12. hot-path write does NOT touch the FTS index ==")
	before := ftsWrites(db)
	must(exec(db, `UPDATE items SET quantity=5, version=version+1, updated_at=99, change_seq=7 WHERE id='I1'`))
	after := ftsWrites(db)
	check("stock/version/change_seq update is FTS-free", before == after, fmt.Sprintf("fts segment rows %d -> %d", before, after))

	fmt.Println("\n== 13. A21 in the planner: read_detail uses group_id-leading indexes ==")
	for _, q := range []string{
		`SELECT * FROM item_identifications WHERE group_id='G1' AND item_id='I1'`,
		`SELECT * FROM item_labels WHERE group_id='G1' AND item_id='I1'`,
		`SELECT * FROM stock_adjustments WHERE group_id='G1' AND item_id='I1'`,
		`SELECT * FROM attachments WHERE group_id='G1' AND item_id='I1'`,
		`SELECT * FROM items WHERE group_id='G1' AND change_seq > 0 ORDER BY change_seq`,
	} {
		plan := explain(db, q)
		check(truncate(q, 56), strings.Contains(plan, "USING INDEX ix_") || strings.Contains(plan, "USING INDEX ux_"), plan)
	}

	fmt.Println("\n== 14. change_seq allocation shape (design for T021) ==")
	var seq int64
	must(db.QueryRow(`UPDATE groups SET change_seq_counter = change_seq_counter + 1 WHERE id='G1' RETURNING change_seq_counter`).Scan(&seq))
	check("UPDATE ... RETURNING allocates a per-group seq", seq == 1, fmt.Sprintf("seq=%d", seq))

	fmt.Println("\n== 15. bulk-import fast path needs defer_foreign_keys ==")
	err = exec(db, `INSERT INTO item_identifications(id,group_id,item_id,kind,value,created_at,updated_at) VALUES('DZ','G1','NOPE','serial','x',1,1)`)
	check("satellite before parent rejected without defer", err != nil, errText(err))
	tx, err := db.Begin()
	must(err)
	_, err = tx.Exec(`PRAGMA defer_foreign_keys = ON`)
	must(err)
	_, err = tx.Exec(`INSERT INTO item_identifications(id,group_id,item_id,kind,value,created_at,updated_at) VALUES('DZ','G1','IZ','serial','zz',1,1)`)
	must(err)
	_, err = tx.Exec(`INSERT INTO items(id,group_id,name,description,short_code,created_at,updated_at) VALUES('IZ','G1','Zed','z','ZED1',1,1)`)
	must(err)
	err = tx.Commit()
	check("satellites-before-parent works with defer_foreign_keys", err == nil, errText(err))
	check("parent insert picked up the pre-inserted identifier", ftsCount(db, "zz") == 1, "")

	fmt.Println("\n== 16. VACUUM INTO preserves the items.rowid <-> items_fts.rowid link ==")
	out := filepath.Join(dir, "snap.db")
	must(exec(db, `VACUUM INTO '`+out+`'`))
	db2 := open(out)
	var mism int
	must(db2.QueryRow(`SELECT count(*) FROM items_fts f JOIN items i ON i.rowid=f.rowid WHERE i.name <> f.name`).Scan(&mism))
	var joined int
	must(db2.QueryRow(`SELECT count(*) FROM items_fts f JOIN items i ON i.rowid=f.rowid`).Scan(&joined))
	check("snapshot search index still points at the right items", mism == 0 && joined > 0, fmt.Sprintf("joined=%d mismatched=%d", joined, mism))
	db2.Close()

	fmt.Println("\n== 17. FTS write-cost measurement (A22) ==")
	measure(ctx, dir)

	fmt.Println("\n== 18. Down migration drops everything ==")
	downDir, _ := os.MkdirTemp("", "hho-down")
	defer os.RemoveAll(downDir)
	d3 := open(filepath.Join(downDir, "d.db"))
	_, err = migrations.Apply(ctx, d3)
	must(err)
	must(downTo(ctx, d3))
	var left int
	must(d3.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name<>'goose_db_version'`).Scan(&left))
	check("Down leaves no tables behind", left == 0, fmt.Sprintf("remaining=%d", left))
	d3.Close()

	fmt.Printf("\n==== %d failing checks ====\n", fails)
	if fails > 0 {
		os.Exit(1)
	}
}

func downTo(ctx context.Context, db *sql.DB) error {
	p, err := gooseProvider(db)
	if err != nil {
		return err
	}
	_, err = p.DownTo(ctx, 0)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func errText(err error) string {
	if err == nil {
		return "no error"
	}
	return truncate(err.Error(), 70)
}

func exec(db *sql.DB, q string) error {
	_, err := db.Exec(q)
	return err
}

func ftsCount(db *sql.DB, term string) int {
	var n int
	err := db.QueryRow(`SELECT count(*) FROM items_fts f JOIN items i ON i.rowid=f.rowid WHERE items_fts MATCH ? AND i.group_id='G1'`, term).Scan(&n)
	if err != nil {
		fmt.Println("   fts query error:", err)
		return -1
	}
	return n
}

func ftsWrites(db *sql.DB) int {
	var n int
	must(db.QueryRow(`SELECT count(*) FROM items_fts_data`).Scan(&n))
	return n
}

func explain(db *sql.DB, q string) string {
	rows, err := db.Query("EXPLAIN QUERY PLAN " + q)
	must(err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, b, c int
		var d string
		must(rows.Scan(&a, &b, &c, &d))
		out = append(out, d)
	}
	return strings.Join(out, " | ")
}
