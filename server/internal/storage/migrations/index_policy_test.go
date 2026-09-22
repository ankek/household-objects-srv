package migrations

import (
	"database/sql"
	"strings"
	"testing"
)

var a21Exceptions = map[string]string{
	"ux_users_username":           "login resolves the user before their group is known",
	"ux_sessions_token_hash":      "bearer-token lookup happens pre-auth",
	"ux_device_tokens_token_hash": "bearer-token lookup happens pre-auth",
	"ux_invites_token_hash":       "invite redemption is unauthenticated",
	"ux_attachments_storage_path": "the filesystem orphan reconciler walks paths, not tenants",
}

func TestEveryIndexLeadsWithGroupID(t *testing.T) {
	db := newSchemaDB(t)

	indexes := explicitIndexes(t, db)
	if len(indexes) == 0 {
		t.Fatal("enumerated zero explicit indexes; the introspection query is broken and this test asserts nothing")
	}

	checked := 0
	for _, idx := range indexes {
		if !idx.tableHasGroupID {
			continue
		}
		if _, waived := a21Exceptions[idx.name]; waived {
			continue
		}
		checked++
		if idx.firstColumn != "group_id" {
			t.Errorf("index %s on %s leads with %q, not group_id, and is not in a21Exceptions (A21)",
				idx.name, idx.table, idx.firstColumn)
		}
	}
	if checked == 0 {
		t.Fatalf("every one of the %d indexes was skipped; the rule was never applied to anything", len(indexes))
	}
}

func TestEveryA21ExceptionIsStillNeeded(t *testing.T) {
	db := newSchemaDB(t)

	present := make(map[string]indexLead)
	for _, idx := range explicitIndexes(t, db) {
		present[idx.name] = idx
	}

	for name, reason := range a21Exceptions {
		idx, ok := present[name]
		if !ok {
			t.Errorf("a21Exceptions declares %q (%s) but no such index exists", name, reason)
			continue
		}
		if idx.firstColumn == "group_id" {
			t.Errorf("a21Exceptions declares %q (%s) but it already leads with group_id; retire the waiver", name, reason)
		}
	}
}

var hotReadPaths = []struct {
	table string
	query string
	index string
}{
	{"item_identifications", `SELECT * FROM item_identifications WHERE group_id = ? AND item_id = ?`, "ix_item_identifications_group_item"},
	{"item_labels", `SELECT * FROM item_labels WHERE group_id = ? AND item_id = ?`, "ix_item_labels_group_item"},
	{"stock_adjustments", `SELECT * FROM stock_adjustments WHERE group_id = ? AND item_id = ?`, "ix_stock_adjustments_group_item"},
	{"attachments", `SELECT * FROM attachments WHERE group_id = ? AND item_id = ?`, "ix_attachments_group_item"},
	{"items", `SELECT * FROM items WHERE group_id = ? AND change_seq > ? ORDER BY change_seq`, "ix_items_group_change_seq"},
}

func TestHotReadPathsSearchAGroupIDLeadingIndex(t *testing.T) {
	db := newSchemaDB(t)

	for _, path := range hotReadPaths {
		t.Run(path.table, func(t *testing.T) {
			plan := queryPlan(t, db, path.query, "group-1", "item-1")
			joined := strings.Join(plan, " | ")

			if !strings.Contains(joined, "USING INDEX "+path.index) {
				t.Errorf("%s: planner did not use %s; plan was: %s", path.table, path.index, joined)
			}
			for _, row := range plan {
				if strings.Contains(row, "SCAN ") {
					t.Errorf("%s: planner chose a table scan: %s", path.table, row)
				}
				if strings.Contains(row, "TEMP B-TREE") {
					t.Errorf("%s: planner sorted in a temp b-tree instead of reading the index in order: %s", path.table, row)
				}
			}
		})
	}
}

type indexLead struct {
	name            string
	table           string
	firstColumn     string
	tableHasGroupID bool
}

func explicitIndexes(t *testing.T, db *sql.DB) []indexLead {
	t.Helper()
	rows, err := db.Query(`
		SELECT s.name, s.tbl_name, i.name,
		       EXISTS (SELECT 1 FROM pragma_table_info(s.tbl_name) WHERE name = 'group_id')
		FROM sqlite_schema AS s
		JOIN pragma_index_info(s.name) AS i
		WHERE s.type = 'index'
		  AND s.sql IS NOT NULL
		  AND i.seqno = 0
		ORDER BY s.tbl_name, s.name`)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer rows.Close()

	var indexes []indexLead
	for rows.Next() {
		var idx indexLead
		if err := rows.Scan(&idx.name, &idx.table, &idx.firstColumn, &idx.tableHasGroupID); err != nil {
			t.Fatalf("scan index row: %v", err)
		}
		indexes = append(indexes, idx)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	return indexes
}

func queryPlan(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain %s: %v", query, err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan row for %s: %v", query, err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain %s: %v", query, err)
	}
	if len(plan) == 0 {
		t.Fatalf("explain %s: empty plan", query)
	}
	return plan
}
