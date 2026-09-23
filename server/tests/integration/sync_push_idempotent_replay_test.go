package integration

import (
	"database/sql"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

type pushReplayMutationWire struct {
	MutationID  string                     `json:"mutation_id"`
	EntityType  string                     `json:"entity_type"`
	EntityID    string                     `json:"entity_id"`
	BaseVersion int64                      `json:"base_version"`
	Fields      map[string]json.RawMessage `json:"fields"`
	Op          string                     `json:"op,omitempty"`
}

type pushReplayRequestWire struct {
	DeviceID  string                   `json:"device_id"`
	Mutations []pushReplayMutationWire `json:"mutations"`
}

type pushReplayAppliedEntry struct {
	MutationID string `json:"mutation_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Version    int64  `json:"version"`
}

type pushReplaySkippedEntry struct {
	MutationID string `json:"mutation_id"`
}

type pushReplayConflictEntry struct {
	MutationID string `json:"mutation_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	FieldName  string `json:"field_name"`
}

type pushReplayResponseWire struct {
	Applied      []pushReplayAppliedEntry  `json:"applied"`
	Skipped      []pushReplaySkippedEntry  `json:"skipped"`
	Conflicts    []pushReplayConflictEntry `json:"conflicts"`
	NewWatermark int64                     `json:"new_watermark"`
}

func pushReplayField(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func pushReplayNewID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return id.String()
}

func pushReplayCreateItemMutation(mutationID, entityID, name string) pushReplayMutationWire {
	return pushReplayMutationWire{
		MutationID: mutationID, EntityType: "item", EntityID: entityID, BaseVersion: 0,
		Fields: map[string]json.RawMessage{"name": pushReplayField(name)},
	}
}

func pushReplayMutationIDs(mutations []pushReplayMutationWire) []string {
	ids := make([]string, len(mutations))
	for i, m := range mutations {
		ids[i] = m.MutationID
	}
	return ids
}

func pushReplaySkippedIDs(entries []pushReplaySkippedEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.MutationID
	}
	return ids
}

func pushReplayAppliedIDs(entries []pushReplayAppliedEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.MutationID
	}
	return ids
}

func pushReplayAssertIDSet(t *testing.T, got, want []string, label string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("%s: got %v, want %v", label, gotSorted, wantSorted)
	}
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Fatalf("%s: got %v, want %v", label, gotSorted, wantSorted)
		}
	}
}

func pushReplayPush(t *testing.T, srv *liveServer, cookie string, body []byte) (*httptest.ResponseRecorder, pushReplayResponseWire) {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/sync/push", cookie, body)
	var resp pushReplayResponseWire
	if rec.Code == http.StatusOK {
		mustDecode(t, rec, &resp)
	}
	return rec, resp
}

func pushReplayPullBody(t *testing.T, srv *liveServer, cookie, deviceID string, since, limit int64) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/sync/pull", cookie, mustMarshal(t, syncPullRequestWire{DeviceID: deviceID, Since: since, Limit: limit}))
	if rec.Code != http.StatusOK {
		t.Fatalf("sync pull: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func pushReplayOpenDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func pushReplayCount(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func pushReplayCounts(t *testing.T, db *sql.DB, tables []string) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		counts[table] = pushReplayCount(t, db, table)
	}
	return counts
}

func pushReplayAssertCountsEqual(t *testing.T, got, want map[string]int64, context string) {
	t.Helper()
	for table, wantN := range want {
		if gotN := got[table]; gotN != wantN {
			t.Errorf("%s: table %s row count = %d, want %d (a replay must never duplicate a row)", context, table, gotN, wantN)
		}
	}
}

type pushReplayRowState struct {
	version, changeSeq, updatedAt int64
}

func pushReplaySnapshotRow(t *testing.T, db *sql.DB, table, id string) pushReplayRowState {
	t.Helper()
	var s pushReplayRowState
	err := db.QueryRow("SELECT version, change_seq, updated_at FROM "+table+" WHERE id = ?", id).Scan(&s.version, &s.changeSeq, &s.updatedAt)
	if err != nil {
		t.Fatalf("snapshot %s row %s: %v", table, id, err)
	}
	return s
}

func pushReplayMutationLedgerCount(t *testing.T, db *sql.DB, groupID string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow("SELECT COUNT(*) FROM mutations WHERE group_id = ?", groupID).Scan(&n); err != nil {
		t.Fatalf("count mutations for group %s: %v", groupID, err)
	}
	return n
}

func TestSyncPushIdempotentReplayMixedBatch(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "replay-mixed-owner", "replay-mixed-owner-pw-1")
	const deviceID = "replay-mixed-device"

	baseItem := createSyncItem(t, srv, cookie, "replay-mixed-base-item")

	locRec := srv.do(t, http.MethodPost, "/api/v1/locations", cookie, locationCreateBody("replay-mixed-base-location", ""))
	if locRec.Code != http.StatusCreated {
		t.Fatalf("create base location: status = %d: %s", locRec.Code, locRec.Body.String())
	}
	var baseLocation locationResponse
	mustDecode(t, locRec, &baseLocation)

	labelRec := srv.do(t, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody("replay-mixed-base-label", "#336699"))
	if labelRec.Code != http.StatusCreated {
		t.Fatalf("create base label: status = %d: %s", labelRec.Code, labelRec.Body.String())
	}
	var baseLabel labelResponse
	mustDecode(t, labelRec, &baseLabel)

	warrantyID := pushReplayNewID(t)
	itemLabelID := pushReplayNewID(t)
	stockID := pushReplayNewID(t)

	mutations := []pushReplayMutationWire{
		{
			MutationID: "mix-warranty-create", EntityType: "warranty_block", EntityID: warrantyID, BaseVersion: 0,
			Fields: map[string]json.RawMessage{
				"item_id":     pushReplayField(baseItem.ID),
				"holder":      pushReplayField("Acme Corp"),
				"provider":    pushReplayField("Acme Warranty Co"),
				"starts_on":   pushReplayField("2024-01-01"),
				"expires_on":  pushReplayField("2026-01-01"),
				"is_lifetime": pushReplayField(false),
				"notes":       pushReplayField(""),
			},
		},
		{
			MutationID: "mix-itemlabel-create", EntityType: "item_label", EntityID: itemLabelID, BaseVersion: 0,
			Fields: map[string]json.RawMessage{
				"item_id":  pushReplayField(baseItem.ID),
				"label_id": pushReplayField(baseLabel.ID),
			},
		},
		{
			MutationID: "mix-item-update", EntityType: "item", EntityID: baseItem.ID, BaseVersion: baseItem.Version,
			Fields: map[string]json.RawMessage{"name": pushReplayField("replay-mixed-base-item-renamed")},
		},
		{
			MutationID: "mix-stock-create", EntityType: "stock_adjustment", EntityID: stockID, BaseVersion: 0,
			Fields: map[string]json.RawMessage{
				"item_id": pushReplayField(baseItem.ID),
				"delta":   pushReplayField(int64(5)),
				"reason":  pushReplayField("restock"),
				"note":    pushReplayField("replay-mixed initial stock"),
			},
		},
		{
			MutationID: "mix-location-update", EntityType: "location", EntityID: baseLocation.ID, BaseVersion: baseLocation.Version,
			Fields: map[string]json.RawMessage{"name": pushReplayField("replay-mixed-base-location-renamed")},
		},
	}

	body := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: mutations})

	rec1, first := pushReplayPush(t, srv, cookie, body)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first push: status = %d, want 200: %s", rec1.Code, rec1.Body.String())
	}
	if len(first.Applied) != len(mutations) {
		t.Fatalf("first push applied = %+v, want %d entries", first.Applied, len(mutations))
	}
	if len(first.Skipped) != 0 {
		t.Fatalf("first push skipped = %+v, want none", first.Skipped)
	}
	if len(first.Conflicts) != 0 {
		t.Fatalf("first push conflicts = %+v, want none", first.Conflicts)
	}

	db := pushReplayOpenDB(t, srv.dbPath)
	tables := []string{"items", "item_warranty", "item_labels", "stock_adjustments", "locations", "mutations"}
	beforeCounts := pushReplayCounts(t, db, tables)
	beforeItem := pushReplaySnapshotRow(t, db, "items", baseItem.ID)
	beforeLocation := pushReplaySnapshotRow(t, db, "locations", baseLocation.ID)
	beforeWarranty := pushReplaySnapshotRow(t, db, "item_warranty", warrantyID)
	beforeItemLabel := pushReplaySnapshotRow(t, db, "item_labels", itemLabelID)
	beforeStock := pushReplaySnapshotRow(t, db, "stock_adjustments", stockID)

	if beforeItem.version != baseItem.Version+2 {
		t.Fatalf("item version after the first push = %d, want %d (one applied update, plus the stock-adjustment's own implicit bump)", beforeItem.version, baseItem.Version+2)
	}

	beforePull := pushReplayPullBody(t, srv, cookie, deviceID, 0, 500)

	rec2, second := pushReplayPush(t, srv, cookie, body)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay push: status = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}
	if len(second.Applied) != 0 {
		t.Errorf("replay applied = %+v, want none", second.Applied)
	}
	if len(second.Conflicts) != 0 {
		t.Errorf("replay conflicts = %+v, want none", second.Conflicts)
	}
	pushReplayAssertIDSet(t, pushReplaySkippedIDs(second.Skipped), pushReplayMutationIDs(mutations), "replay skipped set")
	if second.NewWatermark != first.NewWatermark {
		t.Errorf("replay new_watermark = %d, want %d (unchanged -- no change_seq allocation on a replay)", second.NewWatermark, first.NewWatermark)
	}

	afterCounts := pushReplayCounts(t, db, tables)
	pushReplayAssertCountsEqual(t, afterCounts, beforeCounts, "mixed batch replay")

	afterItem := pushReplaySnapshotRow(t, db, "items", baseItem.ID)
	afterLocation := pushReplaySnapshotRow(t, db, "locations", baseLocation.ID)
	afterWarranty := pushReplaySnapshotRow(t, db, "item_warranty", warrantyID)
	afterItemLabel := pushReplaySnapshotRow(t, db, "item_labels", itemLabelID)
	afterStock := pushReplaySnapshotRow(t, db, "stock_adjustments", stockID)

	for _, row := range []struct {
		name          string
		before, after pushReplayRowState
	}{
		{"items", beforeItem, afterItem},
		{"locations", beforeLocation, afterLocation},
		{"item_warranty", beforeWarranty, afterWarranty},
		{"item_labels", beforeItemLabel, afterItemLabel},
		{"stock_adjustments", beforeStock, afterStock},
	} {
		if row.before != row.after {
			t.Errorf("%s row state changed by replay: before=%+v after=%+v", row.name, row.before, row.after)
		}
	}

	if afterItem.version != beforeItem.version {
		t.Errorf("item version double-bumped by replay: before=%d after=%d", beforeItem.version, afterItem.version)
	}

	afterPull := pushReplayPullBody(t, srv, cookie, deviceID, 0, 500)
	if afterPull != beforePull {
		t.Errorf("sync pull payload changed after an idempotent replay:\nbefore=%s\nafter=%s", beforePull, afterPull)
	}
}

func TestSyncPushIdempotentReplayRetryAfterPartialDelivery(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "replay-retry-owner", "replay-retry-owner-pw-1")
	const deviceID = "replay-retry-device"

	id1, id2, id3, id4 := pushReplayNewID(t), pushReplayNewID(t), pushReplayNewID(t), pushReplayNewID(t)
	m1 := pushReplayCreateItemMutation("retry-m1", id1, "replay-retry-item-1")
	m2 := pushReplayCreateItemMutation("retry-m2", id2, "replay-retry-item-2")
	m3 := pushReplayCreateItemMutation("retry-m3", id3, "replay-retry-item-3")
	m4 := pushReplayCreateItemMutation("retry-m4", id4, "replay-retry-item-4")

	firstBody := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: []pushReplayMutationWire{m1, m2}})
	rec1, first := pushReplayPush(t, srv, cookie, firstBody)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first (partial-delivery) push: status = %d: %s", rec1.Code, rec1.Body.String())
	}
	pushReplayAssertIDSet(t, pushReplayAppliedIDs(first.Applied), []string{"retry-m1", "retry-m2"}, "first push applied set")

	retryBody := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: []pushReplayMutationWire{m1, m2, m3, m4}})
	rec2, second := pushReplayPush(t, srv, cookie, retryBody)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry push: status = %d: %s", rec2.Code, rec2.Body.String())
	}
	pushReplayAssertIDSet(t, pushReplaySkippedIDs(second.Skipped), []string{"retry-m1", "retry-m2"}, "retry skipped set")
	pushReplayAssertIDSet(t, pushReplayAppliedIDs(second.Applied), []string{"retry-m3", "retry-m4"}, "retry applied set")
	if len(second.Conflicts) != 0 {
		t.Errorf("retry conflicts = %+v, want none", second.Conflicts)
	}

	db := pushReplayOpenDB(t, srv.dbPath)
	if n := pushReplayCount(t, db, "items"); n != 4 {
		t.Errorf("items row count = %d, want exactly 4 (no duplicate of the replayed m1/m2)", n)
	}
	for i, id := range []string{id1, id2, id3, id4} {
		if n := pushReplaySnapshotItemCount(t, db, id); n != 1 {
			t.Errorf("items row count for m%d's id = %d, want exactly 1", i+1, n)
		}
	}
}

func pushReplaySnapshotItemCount(t *testing.T, db *sql.DB, id string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow("SELECT COUNT(*) FROM items WHERE id = ?", id).Scan(&n); err != nil {
		t.Fatalf("count items id=%s: %v", id, err)
	}
	return n
}

func TestSyncPushIdempotentReplayPoisonPillConflictSkipsWithoutReconflict(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, groupID := registerAndLogin(t, srv, "replay-poison-owner", "replay-poison-owner-pw-1")
	const deviceID = "replay-poison-device"

	existingLabelRec := srv.do(t, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody("replay-poison-taken-name", "#112233"))
	if existingLabelRec.Code != http.StatusCreated {
		t.Fatalf("create existing label: status = %d: %s", existingLabelRec.Code, existingLabelRec.Body.String())
	}

	itemID := pushReplayNewID(t)
	conflictLabelID := pushReplayNewID(t)

	m1 := pushReplayCreateItemMutation("poison-m1", itemID, "replay-poison-item")
	m2 := pushReplayMutationWire{
		MutationID: "poison-m2", EntityType: "label", EntityID: conflictLabelID, BaseVersion: 0,
		Fields: map[string]json.RawMessage{
			"name":  pushReplayField("replay-poison-taken-name"),
			"color": pushReplayField("#445566"),
		},
	}

	body := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: []pushReplayMutationWire{m1, m2}})

	rec1, first := pushReplayPush(t, srv, cookie, body)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first push: status = %d: %s", rec1.Code, rec1.Body.String())
	}
	pushReplayAssertIDSet(t, pushReplayAppliedIDs(first.Applied), []string{"poison-m1"}, "first push applied set")
	if len(first.Conflicts) != 1 || first.Conflicts[0].MutationID != "poison-m2" || first.Conflicts[0].FieldName != "name" {
		t.Fatalf("first push conflicts = %+v, want exactly one poison-m2/name entry", first.Conflicts)
	}
	if len(first.Skipped) != 0 {
		t.Fatalf("first push skipped = %+v, want none", first.Skipped)
	}

	db := pushReplayOpenDB(t, srv.dbPath)
	ledgerBefore := pushReplayMutationLedgerCount(t, db, groupID)
	if ledgerBefore != 2 {
		t.Fatalf("mutation ledger rows after the first push = %d, want 2 (one applied, one conflict -- both ledgered)", ledgerBefore)
	}
	labelsBefore := pushReplayCount(t, db, "labels")

	rec2, second := pushReplayPush(t, srv, cookie, body)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay push: status = %d, want 200 (never an error): %s", rec2.Code, rec2.Body.String())
	}
	if len(second.Applied) != 0 {
		t.Errorf("replay applied = %+v, want none", second.Applied)
	}
	if len(second.Conflicts) != 0 {
		t.Errorf("replay conflicts = %+v, want none -- a recorded conflict must replay as skipped, never re-conflicted", second.Conflicts)
	}
	pushReplayAssertIDSet(t, pushReplaySkippedIDs(second.Skipped), []string{"poison-m1", "poison-m2"}, "replay skipped set")

	ledgerAfter := pushReplayMutationLedgerCount(t, db, groupID)
	if ledgerAfter != ledgerBefore {
		t.Errorf("mutation ledger row count changed by replay: before=%d after=%d", ledgerBefore, ledgerAfter)
	}
	labelsAfter := pushReplayCount(t, db, "labels")
	if labelsAfter != labelsBefore {
		t.Errorf("labels row count changed by replay: before=%d after=%d (poison-m2 must never create a row, on either push)", labelsBefore, labelsAfter)
	}
}

func TestSyncPushIdempotentReplayStructuralAbortThenRetry(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "replay-abort-owner", "replay-abort-owner-pw-1")
	const deviceID = "replay-abort-device"

	id1, id3 := pushReplayNewID(t), pushReplayNewID(t)
	m1 := pushReplayCreateItemMutation("abort-m1", id1, "replay-abort-item-1")
	m2 := pushReplayMutationWire{
		MutationID: "abort-m2", EntityType: "attachment", EntityID: pushReplayNewID(t), BaseVersion: 0,
		Fields: map[string]json.RawMessage{},
	}
	m3 := pushReplayCreateItemMutation("abort-m3", id3, "replay-abort-item-3")

	firstBody := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: []pushReplayMutationWire{m1, m2, m3}})
	rec1 := srv.do(t, http.MethodPost, "/api/v1/sync/push", cookie, firstBody)
	if rec1.Code != http.StatusBadRequest {
		t.Fatalf("structurally invalid batch: status = %d, want 400: %s", rec1.Code, rec1.Body.String())
	}
	prob := decodeProblem(t, rec1)
	if !strings.Contains(prob.Detail, "mutations[1]") || !strings.Contains(prob.Detail, "abort-m2") {
		t.Errorf("400 detail = %q, want it to name mutations[1] and mutation_id=abort-m2", prob.Detail)
	}

	db := pushReplayOpenDB(t, srv.dbPath)
	if n := pushReplayCount(t, db, "items"); n != 1 {
		t.Fatalf("items row count after the aborted batch = %d, want exactly 1 (m1 already committed)", n)
	}

	retryBody := mustMarshal(t, pushReplayRequestWire{DeviceID: deviceID, Mutations: []pushReplayMutationWire{m1, m3}})
	rec2, second := pushReplayPush(t, srv, cookie, retryBody)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry push: status = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}
	pushReplayAssertIDSet(t, pushReplaySkippedIDs(second.Skipped), []string{"abort-m1"}, "retry skipped set")
	pushReplayAssertIDSet(t, pushReplayAppliedIDs(second.Applied), []string{"abort-m3"}, "retry applied set")
	if len(second.Conflicts) != 0 {
		t.Errorf("retry conflicts = %+v, want none", second.Conflicts)
	}

	if n := pushReplayCount(t, db, "items"); n != 2 {
		t.Errorf("items row count after retry = %d, want exactly 2 (no duplicate of m1)", n)
	}
	if n := pushReplaySnapshotItemCount(t, db, id1); n != 1 {
		t.Errorf("items row count for m1's id = %d, want exactly 1 (no duplicate)", n)
	}
}
