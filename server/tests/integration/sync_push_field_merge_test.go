package integration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func fieldMergeItemUpdateBody(name, description, locationID string, quantity, version int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"name": name, "description": description, "location_id": locationID,
		"quantity": quantity, "version": version,
	})
	return b
}

type fieldMergeConflictRow struct {
	entityType  string
	entityID    string
	fieldName   string
	serverValue sql.NullString
	losingValue sql.NullString
	ledgerID    string
}

func fieldMergeSelectConflicts(t *testing.T, db *sql.DB, groupID, entityID string) []fieldMergeConflictRow {
	t.Helper()
	rows, err := db.Query(
		`SELECT entity_type, entity_id, field_name, server_value_snapshot, losing_client_value_snapshot, mutation_id
		 FROM conflicts WHERE group_id = ? AND entity_id = ? ORDER BY detected_at, id`,
		groupID, entityID)
	if err != nil {
		t.Fatalf("query conflicts for entity %s: %v", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []fieldMergeConflictRow
	for rows.Next() {
		var r fieldMergeConflictRow
		if err := rows.Scan(&r.entityType, &r.entityID, &r.fieldName, &r.serverValue, &r.losingValue, &r.ledgerID); err != nil {
			t.Fatalf("scan conflicts row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate conflicts rows: %v", err)
	}
	return out
}

func fieldMergeConflictCount(t *testing.T, db *sql.DB, groupID string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM conflicts WHERE group_id = ?`, groupID).Scan(&n); err != nil {
		t.Fatalf("count conflicts for group %s: %v", groupID, err)
	}
	return n
}

func fieldMergeLedgerRowID(t *testing.T, db *sql.DB, groupID, mutationID string) string {
	t.Helper()
	var id string
	err := db.QueryRow(`SELECT id FROM mutations WHERE group_id = ? AND mutation_id = ?`, groupID, mutationID).Scan(&id)
	if err != nil {
		t.Fatalf("lookup ledger row for mutation_id %s: %v", mutationID, err)
	}
	return id
}

func fieldMergeFieldVersion(t *testing.T, db *sql.DB, groupID, entityType, entityID, fieldName string) (version int64, tracked bool) {
	t.Helper()
	err := db.QueryRow(
		`SELECT version FROM field_versions WHERE group_id = ? AND entity_type = ? AND entity_id = ? AND field_name = ?`,
		groupID, entityType, entityID, fieldName,
	).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false
	case err != nil:
		t.Fatalf("query field_versions %s/%s/%s: %v", entityType, entityID, fieldName, err)
	}
	return version, true
}

type fieldMergeItemRow struct {
	name, description            string
	quantity, version, changeSeq int64
}

func fieldMergeReadItem(t *testing.T, db *sql.DB, itemID string) fieldMergeItemRow {
	t.Helper()
	var r fieldMergeItemRow
	err := db.QueryRow(`SELECT name, description, quantity, version, change_seq FROM items WHERE id = ?`, itemID).
		Scan(&r.name, &r.description, &r.quantity, &r.version, &r.changeSeq)
	if err != nil {
		t.Fatalf("read item %s: %v", itemID, err)
	}
	return r
}

func fieldMergeGroupWatermark(t *testing.T, db *sql.DB, groupID string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT change_seq_counter FROM groups WHERE id = ?`, groupID).Scan(&n); err != nil {
		t.Fatalf("read group %s watermark: %v", groupID, err)
	}
	return n
}

type fieldMergePulledItemFields struct {
	Name        string
	Description string
	Quantity    int64
}

func fieldMergeFindItemChange(t *testing.T, page syncPullOKBody, itemID string) fieldMergePulledItemFields {
	t.Helper()
	for _, c := range page.Changes {
		if c.EntityType == "item" && c.ID == itemID {
			var f fieldMergePulledItemFields
			if err := json.Unmarshal(c.Data, &f); err != nil {
				t.Fatalf("decode pulled item %s data: %v; raw=%s", itemID, err, c.Data)
			}
			return f
		}
	}
	t.Fatalf("pull(since=0) has no item change for id=%s", itemID)
	return fieldMergePulledItemFields{}
}

func fieldMergeSinglePush(t *testing.T, srv *liveServer, cookie, deviceID string, m pushReplayMutationWire) pushReplayResponseWire {
	t.Helper()
	body := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: []pushReplayMutationWire{m}})
	rec, resp := pushReplayPush(t, srv, cookie, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("push %s: status = %d, want 200: %s", m.MutationID, rec.Code, rec.Body.String())
	}
	return resp
}

func TestFieldMergePushDifferentFieldsBothSurvive(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, groupID := registerAndLogin(t, srv, "fm1-owner", "fm1-owner-pw-1")

	item := createSyncItem(t, srv, cookie, "fm1-item")
	baseVersion := item.Version

	nameResp := fieldMergeSinglePush(t, srv, cookie, "fm1-dev-a", pushReplayMutationWire{
		MutationID: "fm1-name", EntityType: "item", EntityID: item.ID, BaseVersion: baseVersion,
		Fields: map[string]json.RawMessage{"name": pushReplayField("fm1-item-renamed")},
	})
	if len(nameResp.Applied) != 1 || len(nameResp.Conflicts) != 0 {
		t.Fatalf("name push: applied=%+v conflicts=%+v, want exactly one applied entry, no conflicts", nameResp.Applied, nameResp.Conflicts)
	}

	qtyResp := fieldMergeSinglePush(t, srv, cookie, "fm1-dev-b", pushReplayMutationWire{
		MutationID: "fm1-qty", EntityType: "item", EntityID: item.ID, BaseVersion: baseVersion,
		Fields: map[string]json.RawMessage{"quantity": pushReplayField(int64(42))},
	})
	if len(qtyResp.Applied) != 1 || len(qtyResp.Conflicts) != 0 {
		t.Fatalf("quantity push: applied=%+v conflicts=%+v, want exactly one applied entry, no conflicts", qtyResp.Applied, qtyResp.Conflicts)
	}

	db := pushReplayOpenDB(t, srv.dbPath)
	if n := fieldMergeConflictCount(t, db, groupID); n != 0 {
		t.Errorf("conflicts row count = %d, want 0 (different fields must never conflict)", n)
	}

	if v, tracked := fieldMergeFieldVersion(t, db, groupID, "item", item.ID, "name"); !tracked || v != nameResp.Applied[0].Version {
		t.Errorf("field_versions[name] = (%d, tracked=%v), want (%d, true)", v, tracked, nameResp.Applied[0].Version)
	}
	if v, tracked := fieldMergeFieldVersion(t, db, groupID, "item", item.ID, "quantity"); !tracked || v != qtyResp.Applied[0].Version {
		t.Errorf("field_versions[quantity] = (%d, tracked=%v), want (%d, true)", v, tracked, qtyResp.Applied[0].Version)
	}

	row := fieldMergeReadItem(t, db, item.ID)
	if row.name != "fm1-item-renamed" || row.quantity != 42 {
		t.Errorf("item row = %+v, want name=fm1-item-renamed quantity=42", row)
	}

	if got := fieldMergeGroupWatermark(t, db, groupID); got != qtyResp.NewWatermark {
		t.Errorf("group change_seq_counter = %d, want %d (the second push's own new_watermark)", got, qtyResp.NewWatermark)
	}

	page := srv.decodeSyncPullOK(t, cookie, "fm1-puller", 0, 500)
	pulled := fieldMergeFindItemChange(t, page, item.ID)
	if pulled.Name != "fm1-item-renamed" || pulled.Quantity != 42 {
		t.Errorf("pulled item = %+v, want name=fm1-item-renamed quantity=42", pulled)
	}
}

func TestFieldMergePushSameFieldDiverges(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, groupID := registerAndLogin(t, srv, "fm2-owner", "fm2-owner-pw-1")

	item := createSyncItem(t, srv, cookie, "fm2-item")
	baseVersion := item.Version

	first := fieldMergeSinglePush(t, srv, cookie, "fm2-dev-a", pushReplayMutationWire{
		MutationID: "fm2-name-a", EntityType: "item", EntityID: item.ID, BaseVersion: baseVersion,
		Fields: map[string]json.RawMessage{"name": pushReplayField("fm2-name-A")},
	})
	if len(first.Applied) != 1 || len(first.Conflicts) != 0 {
		t.Fatalf("first push: applied=%+v conflicts=%+v, want exactly one applied entry, no conflicts", first.Applied, first.Conflicts)
	}

	second := fieldMergeSinglePush(t, srv, cookie, "fm2-dev-b", pushReplayMutationWire{
		MutationID: "fm2-name-b", EntityType: "item", EntityID: item.ID, BaseVersion: baseVersion,
		Fields: map[string]json.RawMessage{"name": pushReplayField("fm2-name-B")},
	})
	if len(second.Applied) != 0 {
		t.Errorf("second push applied = %+v, want none (the whole mutation's only field conflicted)", second.Applied)
	}
	if len(second.Conflicts) != 1 || second.Conflicts[0].MutationID != "fm2-name-b" || second.Conflicts[0].FieldName != "name" {
		t.Fatalf("second push conflicts = %+v, want exactly one fm2-name-b/name entry", second.Conflicts)
	}
	if second.NewWatermark != first.NewWatermark {
		t.Errorf("second push new_watermark = %d, want %d (unchanged -- it wrote nothing)", second.NewWatermark, first.NewWatermark)
	}

	db := pushReplayOpenDB(t, srv.dbPath)
	conflicts := fieldMergeSelectConflicts(t, db, groupID, item.ID)
	if len(conflicts) != 1 {
		t.Fatalf("persisted conflicts rows = %+v, want exactly 1", conflicts)
	}
	c := conflicts[0]
	if c.entityType != "item" || c.fieldName != "name" {
		t.Errorf("conflict row = %+v, want entity_type=item field_name=name", c)
	}
	wantLedgerID := fieldMergeLedgerRowID(t, db, groupID, "fm2-name-b")
	if c.ledgerID != wantLedgerID {
		t.Errorf("conflict row mutation_id = %q, want the LEDGER row id %q (not the wire mutation_id fm2-name-b)", c.ledgerID, wantLedgerID)
	}
	if !c.serverValue.Valid || c.serverValue.String != `"fm2-name-A"` {
		t.Errorf("conflict server_value_snapshot = %+v, want the JSON-encoded winning value \"fm2-name-A\"", c.serverValue)
	}
	if !c.losingValue.Valid || c.losingValue.String != `"fm2-name-B"` {
		t.Errorf("conflict losing_client_value_snapshot = %+v, want the JSON-encoded losing value \"fm2-name-B\"", c.losingValue)
	}

	row := fieldMergeReadItem(t, db, item.ID)
	if row.name != "fm2-name-A" {
		t.Errorf("item name = %q, want %q (the first, winning write) -- the losing value must never land on the row", row.name, "fm2-name-A")
	}

	page := srv.decodeSyncPullOK(t, cookie, "fm2-puller", 0, 500)
	pulled := fieldMergeFindItemChange(t, page, item.ID)
	if pulled.Name != "fm2-name-A" {
		t.Errorf("pulled item name = %q, want %q -- the losing client's value must never appear anywhere but the conflict row's own snapshot", pulled.Name, "fm2-name-A")
	}
}

func TestFieldMergePushStaleAfterRESTEdit(t *testing.T) {
	t.Run("SameField", func(t *testing.T) {
		srv := newLiveServer(t, false)
		cookie, groupID := registerAndLogin(t, srv, "fm3a-owner", "fm3a-owner-pw-1")

		item := createSyncItem(t, srv, cookie, "fm3a-item")
		staleBaseVersion := item.Version

		restRec := srv.do(t, http.MethodPut, "/api/v1/items/"+item.ID, cookie,
			fieldMergeItemUpdateBody("fm3a-rest-name", "", "", 0, item.Version))
		if restRec.Code != http.StatusOK {
			t.Fatalf("REST PUT item: status = %d: %s", restRec.Code, restRec.Body.String())
		}

		push := fieldMergeSinglePush(t, srv, cookie, "fm3a-dev", pushReplayMutationWire{
			MutationID: "fm3a-push-name", EntityType: "item", EntityID: item.ID, BaseVersion: staleBaseVersion,
			Fields: map[string]json.RawMessage{"name": pushReplayField("fm3a-push-name")},
		})
		if len(push.Applied) != 0 {
			t.Errorf("stale same-field push applied = %+v, want none", push.Applied)
		}
		if len(push.Conflicts) != 1 || push.Conflicts[0].FieldName != "name" {
			t.Fatalf("stale same-field push conflicts = %+v, want exactly one name entry", push.Conflicts)
		}

		db := pushReplayOpenDB(t, srv.dbPath)
		conflicts := fieldMergeSelectConflicts(t, db, groupID, item.ID)
		if len(conflicts) != 1 || !conflicts[0].serverValue.Valid || conflicts[0].serverValue.String != `"fm3a-rest-name"` {
			t.Fatalf("conflicts = %+v, want one row snapshotting the REST value \"fm3a-rest-name\"", conflicts)
		}

		row := fieldMergeReadItem(t, db, item.ID)
		if row.name != "fm3a-rest-name" {
			t.Errorf("item name = %q, want %q (the REST value wins over the stale push)", row.name, "fm3a-rest-name")
		}

		page := srv.decodeSyncPullOK(t, cookie, "fm3a-puller", 0, 500)
		pulled := fieldMergeFindItemChange(t, page, item.ID)
		if pulled.Name != "fm3a-rest-name" {
			t.Errorf("pulled item name = %q, want %q", pulled.Name, "fm3a-rest-name")
		}
	})

	t.Run("DifferentField", func(t *testing.T) {
		srv := newLiveServer(t, false)
		cookie, groupID := registerAndLogin(t, srv, "fm3b-owner", "fm3b-owner-pw-1")

		item := createSyncItem(t, srv, cookie, "fm3b-item")
		staleBaseVersion := item.Version

		restRec := srv.do(t, http.MethodPut, "/api/v1/items/"+item.ID, cookie,
			fieldMergeItemUpdateBody("fm3b-rest-name", "", "", 0, item.Version))
		if restRec.Code != http.StatusOK {
			t.Fatalf("REST PUT item: status = %d: %s", restRec.Code, restRec.Body.String())
		}

		push := fieldMergeSinglePush(t, srv, cookie, "fm3b-dev", pushReplayMutationWire{
			MutationID: "fm3b-push-desc", EntityType: "item", EntityID: item.ID, BaseVersion: staleBaseVersion,
			Fields: map[string]json.RawMessage{"description": pushReplayField("fm3b-push-desc")},
		})
		if len(push.Conflicts) != 0 {
			t.Fatalf("stale different-field push conflicts = %+v, want none (A153: a stale push touching a different field still merges)", push.Conflicts)
		}
		if len(push.Applied) != 1 {
			t.Fatalf("stale different-field push applied = %+v, want exactly one entry", push.Applied)
		}

		db := pushReplayOpenDB(t, srv.dbPath)
		if n := fieldMergeConflictCount(t, db, groupID); n != 0 {
			t.Errorf("conflicts row count = %d, want 0", n)
		}

		row := fieldMergeReadItem(t, db, item.ID)
		if row.name != "fm3b-rest-name" || row.description != "fm3b-push-desc" {
			t.Errorf("item row = %+v, want name=fm3b-rest-name (REST) description=fm3b-push-desc (push) -- both must survive", row)
		}

		page := srv.decodeSyncPullOK(t, cookie, "fm3b-puller", 0, 500)
		pulled := fieldMergeFindItemChange(t, page, item.ID)
		if pulled.Name != "fm3b-rest-name" || pulled.Description != "fm3b-push-desc" {
			t.Errorf("pulled item = %+v, want name=fm3b-rest-name description=fm3b-push-desc", pulled)
		}
	})
}

func TestFieldMergePushSameBatchStockAdjustmentThenItemUpdate(t *testing.T) {
	t.Run("NameOnlyBothApply", func(t *testing.T) {
		srv := newLiveServer(t, false)
		cookie, groupID := registerAndLogin(t, srv, "fm4a-owner", "fm4a-owner-pw-1")

		item := createSyncItem(t, srv, cookie, "fm4a-item")
		staleBaseVersion := item.Version

		stockID := pushReplayNewID(t)
		batch := []pushReplayMutationWire{
			{
				MutationID: "fm4a-stock", EntityType: "stock_adjustment", EntityID: stockID, BaseVersion: 0,
				Fields: map[string]json.RawMessage{
					"item_id": pushReplayField(item.ID), "delta": pushReplayField(int64(7)),
					"reason": pushReplayField("restock"), "note": pushReplayField("fm4a"),
				},
			},
			{
				MutationID: "fm4a-item-name", EntityType: "item", EntityID: item.ID, BaseVersion: staleBaseVersion,
				Fields: map[string]json.RawMessage{"name": pushReplayField("fm4a-renamed")},
			},
		}
		body := mustMarshal(t, pushReplayRequestWire{DeviceID: "fm4a-dev", Mutations: batch})
		rec, resp := pushReplayPush(t, srv, cookie, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("batch push: status = %d: %s", rec.Code, rec.Body.String())
		}
		if len(resp.Applied) != 2 {
			t.Fatalf("batch applied = %+v, want both mutations applied", resp.Applied)
		}
		if len(resp.Conflicts) != 0 {
			t.Fatalf("batch conflicts = %+v, want none", resp.Conflicts)
		}

		db := pushReplayOpenDB(t, srv.dbPath)
		if n := fieldMergeConflictCount(t, db, groupID); n != 0 {
			t.Errorf("conflicts row count = %d, want 0", n)
		}
		row := fieldMergeReadItem(t, db, item.ID)
		if row.name != "fm4a-renamed" || row.quantity != 7 {
			t.Errorf("item row = %+v, want name=fm4a-renamed quantity=7 (stock delta applied, name applied)", row)
		}
		if got := fieldMergeGroupWatermark(t, db, groupID); got != resp.NewWatermark {
			t.Errorf("group change_seq_counter = %d, want %d", got, resp.NewWatermark)
		}

		page := srv.decodeSyncPullOK(t, cookie, "fm4a-puller", 0, 500)
		pulled := fieldMergeFindItemChange(t, page, item.ID)
		if pulled.Name != "fm4a-renamed" || pulled.Quantity != 7 {
			t.Errorf("pulled item = %+v, want name=fm4a-renamed quantity=7", pulled)
		}
	})

	t.Run("NameAndQuantityConflictsOnQuantity", func(t *testing.T) {
		srv := newLiveServer(t, false)
		cookie, groupID := registerAndLogin(t, srv, "fm4b-owner", "fm4b-owner-pw-1")

		item := createSyncItem(t, srv, cookie, "fm4b-item")
		staleBaseVersion := item.Version

		stockID := pushReplayNewID(t)
		batch := []pushReplayMutationWire{
			{
				MutationID: "fm4b-stock", EntityType: "stock_adjustment", EntityID: stockID, BaseVersion: 0,
				Fields: map[string]json.RawMessage{
					"item_id": pushReplayField(item.ID), "delta": pushReplayField(int64(3)),
					"reason": pushReplayField("restock"), "note": pushReplayField("fm4b"),
				},
			},
			{
				MutationID: "fm4b-item-update", EntityType: "item", EntityID: item.ID, BaseVersion: staleBaseVersion,
				Fields: map[string]json.RawMessage{
					"name":     pushReplayField("fm4b-renamed"),
					"quantity": pushReplayField(int64(999)),
				},
			},
		}
		body := mustMarshal(t, pushReplayRequestWire{DeviceID: "fm4b-dev", Mutations: batch})
		rec, resp := pushReplayPush(t, srv, cookie, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("batch push: status = %d: %s", rec.Code, rec.Body.String())
		}
		if len(resp.Applied) != 2 {
			t.Fatalf("batch applied = %+v, want both mutation_ids present (stock create + the partially-applied item update)", resp.Applied)
		}
		if len(resp.Conflicts) != 1 || resp.Conflicts[0].MutationID != "fm4b-item-update" || resp.Conflicts[0].FieldName != "quantity" {
			t.Fatalf("batch conflicts = %+v, want exactly one fm4b-item-update/quantity entry", resp.Conflicts)
		}

		db := pushReplayOpenDB(t, srv.dbPath)
		conflicts := fieldMergeSelectConflicts(t, db, groupID, item.ID)
		if len(conflicts) != 1 || conflicts[0].fieldName != "quantity" {
			t.Fatalf("persisted conflicts = %+v, want exactly one quantity row", conflicts)
		}
		if !conflicts[0].serverValue.Valid || conflicts[0].serverValue.String != "3" {
			t.Errorf("conflict server_value_snapshot = %+v, want the stock adjustment's own resulting quantity, JSON-encoded (\"3\")", conflicts[0].serverValue)
		}
		if !conflicts[0].losingValue.Valid || conflicts[0].losingValue.String != "999" {
			t.Errorf("conflict losing_client_value_snapshot = %+v, want the push's own losing quantity, JSON-encoded (\"999\")", conflicts[0].losingValue)
		}

		row := fieldMergeReadItem(t, db, item.ID)
		if row.name != "fm4b-renamed" {
			t.Errorf("item name = %q, want %q (name still applied)", row.name, "fm4b-renamed")
		}
		if row.quantity != 3 {
			t.Errorf("item quantity = %d, want 3 (the stock adjustment's own value, never overwritten by the conflicting push)", row.quantity)
		}

		page := srv.decodeSyncPullOK(t, cookie, "fm4b-puller", 0, 500)
		pulled := fieldMergeFindItemChange(t, page, item.ID)
		if pulled.Name != "fm4b-renamed" || pulled.Quantity != 3 {
			t.Errorf("pulled item = %+v, want name=fm4b-renamed quantity=3", pulled)
		}
	})
}
