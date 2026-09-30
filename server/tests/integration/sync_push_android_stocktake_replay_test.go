package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const stocktakeGoldenDir = "../../../android-app/app/src/test/resources/golden"

func stocktakeReplayLoadGolden(name string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(stocktakeGoldenDir, name))
	if err != nil {
		return nil, fmt.Errorf("golden file %s: %w", name, err)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("golden file %s is empty", name)
	}
	return b, nil
}

func stocktakeReplayMustLoad(t *testing.T, name string) []byte {
	t.Helper()
	b, err := stocktakeReplayLoadGolden(name)
	if err != nil {
		t.Fatalf("the Android golden is required, not optional: %v", err)
	}
	return b
}

type stocktakeReplayItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LocationID  string `json:"location_id"`
	Quantity    int64  `json:"quantity"`
}

type stocktakeReplayEdge struct {
	ItemID  string `json:"item_id"`
	LabelID string `json:"label_id"`
}

type stocktakeReplayExpected struct {
	Items      []stocktakeReplayItem `json:"items"`
	ItemLabels []stocktakeReplayEdge `json:"item_labels"`
}

func stocktakeReplayLiveItems(t *testing.T, db *sql.DB) []stocktakeReplayItem {
	t.Helper()
	rows, err := db.Query("SELECT id, name, description, COALESCE(location_id, ''), quantity FROM items WHERE deleted_at IS NULL ORDER BY id")
	if err != nil {
		t.Fatalf("query items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []stocktakeReplayItem
	for rows.Next() {
		var it stocktakeReplayItem
		if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.LocationID, &it.Quantity); err != nil {
			t.Fatalf("scan item: %v", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("items rows: %v", err)
	}
	return out
}

func stocktakeReplayLiveEdges(t *testing.T, db *sql.DB) []stocktakeReplayEdge {
	t.Helper()
	rows, err := db.Query("SELECT item_id, label_id FROM item_labels WHERE deleted_at IS NULL ORDER BY item_id, label_id")
	if err != nil {
		t.Fatalf("query item_labels: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []stocktakeReplayEdge
	for rows.Next() {
		var e stocktakeReplayEdge
		if err := rows.Scan(&e.ItemID, &e.LabelID); err != nil {
			t.Fatalf("scan item_label: %v", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("item_labels rows: %v", err)
	}
	return out
}

func stocktakeReplayAssertNoConflicts(t *testing.T, srv *liveServer, cookie, when string) {
	t.Helper()
	rec := srv.do(t, http.MethodGet, "/api/v1/sync/conflicts?limit=200", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: GET /sync/conflicts: status = %d: %s", when, rec.Code, rec.Body.String())
	}
	var body struct {
		Conflicts  []json.RawMessage `json:"conflicts"`
		NextCursor *string           `json:"next_cursor"`
	}
	mustDecode(t, rec, &body)
	if len(body.Conflicts) != 0 || body.NextCursor != nil {
		t.Fatalf("%s: conflict log = %d entries (next_cursor %v), want empty: %s", when, len(body.Conflicts), body.NextCursor, rec.Body.String())
	}
}

func stocktakeReplayParse(t *testing.T, raw []byte, wantMutations int, label string) pushReplayRequestWire {
	t.Helper()
	var req pushReplayRequestWire
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("%s: decode golden: %v", label, err)
	}
	if req.DeviceID == "" || len(req.Mutations) != wantMutations {
		t.Fatalf("%s: golden has device_id=%q and %d mutations, want a device id and exactly %d", label, req.DeviceID, len(req.Mutations), wantMutations)
	}
	return req
}

func TestSyncPushAndroidStocktakeGoldenReplay(t *testing.T) {
	seedRaw := stocktakeReplayMustLoad(t, "stocktake-seed-push-request.json")
	pushRaw := stocktakeReplayMustLoad(t, "stocktake-push-request.json")
	expectedRaw := stocktakeReplayMustLoad(t, "stocktake-expected-state.json")

	seed := stocktakeReplayParse(t, seedRaw, 43, "seed")
	golden := stocktakeReplayParse(t, pushRaw, 50, "push")
	var expected stocktakeReplayExpected
	if err := json.Unmarshal(expectedRaw, &expected); err != nil {
		t.Fatalf("decode expected state: %v", err)
	}
	if len(expected.Items) != 30 || len(expected.ItemLabels) == 0 {
		t.Fatalf("expected-state golden has %d items and %d edges, want 30 items and some edges", len(expected.Items), len(expected.ItemLabels))
	}

	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "stocktake-owner", "stocktake-owner-pw-1")
	db := pushReplayOpenDB(t, srv.dbPath)

	recSeed, seeded := pushReplayPush(t, srv, cookie, seedRaw)
	if recSeed.Code != http.StatusOK {
		t.Fatalf("seed push: status = %d, want 200: %s", recSeed.Code, recSeed.Body.String())
	}
	pushReplayAssertIDSet(t, pushReplayAppliedIDs(seeded.Applied), pushReplayMutationIDs(seed.Mutations), "seed applied set")
	if len(seeded.Skipped) != 0 || len(seeded.Conflicts) != 0 {
		t.Fatalf("seed push skipped=%+v conflicts=%+v, want none", seeded.Skipped, seeded.Conflicts)
	}
	stocktakeReplayAssertNoConflicts(t, srv, cookie, "after seed")

	rec1, first := pushReplayPush(t, srv, cookie, pushRaw)
	if rec1.Code != http.StatusOK {
		t.Fatalf("golden push: status = %d, want 200: %s", rec1.Code, rec1.Body.String())
	}
	pushReplayAssertIDSet(t, pushReplayAppliedIDs(first.Applied), pushReplayMutationIDs(golden.Mutations), "golden applied set")
	if len(first.Applied) != 50 || len(first.Skipped) != 0 || len(first.Conflicts) != 0 {
		t.Fatalf("golden push applied=%d skipped=%d conflicts=%+v, want 50/0/none", len(first.Applied), len(first.Skipped), first.Conflicts)
	}
	stocktakeReplayAssertNoConflicts(t, srv, cookie, "after first push")

	tables := []string{"items", "locations", "labels", "item_labels", "stock_adjustments", "mutations"}
	afterFirst := pushReplayCounts(t, db, tables)
	want := map[string]int64{"items": 30, "locations": 4, "labels": 4, "stock_adjustments": 15, "mutations": 43 + 50}
	for table, n := range want {
		if afterFirst[table] != n {
			t.Errorf("after first push: table %s rows = %d, want %d", table, afterFirst[table], n)
		}
	}
	itemsAfterFirst := stocktakeReplayLiveItems(t, db)
	edgesAfterFirst := stocktakeReplayLiveEdges(t, db)
	pullAfterFirst := pushReplayPullBody(t, srv, cookie, golden.DeviceID, 0, 500)

	rec2, second := pushReplayPush(t, srv, cookie, pushRaw)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay push: status = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}
	if len(second.Applied) != 0 || len(second.Conflicts) != 0 {
		t.Errorf("replay applied=%+v conflicts=%+v, want none", second.Applied, second.Conflicts)
	}
	pushReplayAssertIDSet(t, pushReplaySkippedIDs(second.Skipped), pushReplayMutationIDs(golden.Mutations), "replay skipped set")
	if len(second.Skipped) != 50 {
		t.Errorf("replay skipped = %d, want 50", len(second.Skipped))
	}
	if second.NewWatermark != first.NewWatermark {
		t.Errorf("replay new_watermark = %d, want %d", second.NewWatermark, first.NewWatermark)
	}
	pushReplayAssertCountsEqual(t, pushReplayCounts(t, db, tables), afterFirst, "golden replay")

	gotItems := stocktakeReplayLiveItems(t, db)
	if fmt.Sprint(gotItems) != fmt.Sprint(itemsAfterFirst) {
		t.Errorf("items changed by the replay:\nbefore=%v\nafter=%v", itemsAfterFirst, gotItems)
	}
	sort.Slice(expected.Items, func(i, j int) bool { return expected.Items[i].ID < expected.Items[j].ID })
	if len(gotItems) != len(expected.Items) {
		t.Fatalf("live items = %d, want %d", len(gotItems), len(expected.Items))
	}
	for i := range gotItems {
		if gotItems[i] != expected.Items[i] {
			t.Errorf("item %s: got %+v, want %+v", expected.Items[i].ID, gotItems[i], expected.Items[i])
		}
	}

	gotEdges := stocktakeReplayLiveEdges(t, db)
	if fmt.Sprint(gotEdges) != fmt.Sprint(edgesAfterFirst) {
		t.Errorf("item_labels changed by the replay:\nbefore=%v\nafter=%v", edgesAfterFirst, gotEdges)
	}
	sort.Slice(expected.ItemLabels, func(i, j int) bool {
		a, b := expected.ItemLabels[i], expected.ItemLabels[j]
		return a.ItemID < b.ItemID || (a.ItemID == b.ItemID && a.LabelID < b.LabelID)
	})
	if fmt.Sprint(gotEdges) != fmt.Sprint(expected.ItemLabels) {
		t.Errorf("live item_labels:\n got=%v\nwant=%v", gotEdges, expected.ItemLabels)
	}
	seen := map[stocktakeReplayEdge]bool{}
	for _, e := range gotEdges {
		if seen[e] {
			t.Errorf("duplicate live item_label edge %+v", e)
		}
		seen[e] = true
	}

	if afterPull := pushReplayPullBody(t, srv, cookie, golden.DeviceID, 0, 500); afterPull != pullAfterFirst {
		t.Errorf("sync pull payload changed after the replay")
	}

	stocktakeReplayAssertNoConflicts(t, srv, cookie, "after replay")
}

func TestSyncPushAndroidStocktakeGoldenMissingFailsLoudly(t *testing.T) {
	if _, err := stocktakeReplayLoadGolden("stocktake-does-not-exist.json"); err == nil {
		t.Fatal("loading a missing golden returned no error")
	}
	for _, name := range []string{"stocktake-seed-push-request.json", "stocktake-push-request.json", "stocktake-expected-state.json"} {
		if _, err := stocktakeReplayLoadGolden(name); err != nil {
			t.Errorf("committed golden not loadable: %v", err)
		}
	}
}
