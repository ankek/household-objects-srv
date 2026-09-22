package storage

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func stockAdjustmentScope(t *testing.T) (scopeA, scopeB Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupA", "itemA2", 11)
	seedItem(t, s, "groupB", "itemB", 20)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err = s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return scopeA, scopeB
}

func TestStockAdjustmentCreateAppliesDeltaToItemQuantityAtomically(t *testing.T) {
	scopeA, _ := stockAdjustmentScope(t)

	created, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
		ID: "sa-1", ItemID: "itemA", Delta: 5, Reason: "restock", Note: "found more in storage", Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
	}
	if created.Delta != 5 || created.Reason != "restock" || created.Note != "found more in storage" {
		t.Errorf("created = %+v, want {Delta:5 Reason:restock Note:\"found more in storage\"}", created)
	}
	if created.ResultingQuantity != 5 {
		t.Errorf("ResultingQuantity = %d, want 5 (0 starting quantity + delta 5)", created.ResultingQuantity)
	}

	item, err := scopeA.Items().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Items().Get(itemA): %v", err)
	}
	if item.Quantity != 5 {
		t.Errorf("item.Quantity = %d, want 5 -- the adjustment's delta was not applied to the item", item.Quantity)
	}
	if item.Quantity != created.ResultingQuantity {
		t.Errorf("item.Quantity (%d) and the adjustment's own ResultingQuantity snapshot (%d) disagree -- FR-018's whole atomicity guarantee is that these can never diverge", item.Quantity, created.ResultingQuantity)
	}
	if item.Version != 2 {
		t.Errorf("item.Version = %d, want 2 -- the item row genuinely changed, so its optimistic-concurrency version must advance", item.Version)
	}
	if item.ChangeSeq != created.ChangeSeq {
		t.Errorf("item.ChangeSeq (%d) and the adjustment's own ChangeSeq (%d) disagree -- db.AllocChangeSeq is documented to be called ONCE per transaction and stamp every row written in it, so a sync puller must never observe one without the other", item.ChangeSeq, created.ChangeSeq)
	}
}

func TestStockAdjustmentCreateAccumulatesAcrossMultipleAdjustments(t *testing.T) {
	scopeA, _ := stockAdjustmentScope(t)

	steps := []struct {
		id    string
		delta int64
		want  int64
	}{
		{"sa-1", 10, 10},
		{"sa-2", -3, 7},
		{"sa-3", 0, 7},
		{"sa-4", 100, 107},
	}
	for _, step := range steps {
		created, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
			ID: step.id, ItemID: "itemA", Delta: step.delta, Now: time.Now().UnixMilli(),
		})
		if err != nil {
			t.Fatalf("Create(%s, delta=%d): %v", step.id, step.delta, err)
		}
		if created.ResultingQuantity != step.want {
			t.Fatalf("Create(%s, delta=%d).ResultingQuantity = %d, want %d", step.id, step.delta, created.ResultingQuantity, step.want)
		}
	}

	item, err := scopeA.Items().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Items().Get(itemA): %v", err)
	}
	if item.Quantity != 107 {
		t.Errorf("final item.Quantity = %d, want 107", item.Quantity)
	}
}

func TestStockAdjustmentListReturnsNewestFirst(t *testing.T) {
	scopeA, _ := stockAdjustmentScope(t)
	base := time.Now().UnixMilli()

	for i, id := range []string{"sa-1", "sa-2", "sa-3"} {
		if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
			ID: id, ItemID: "itemA", Delta: 1, Now: base + int64(i)*1000,
		}); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}

	rows, err := scopeA.StockAdjustments().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("List returned %d rows, want 3", len(rows))
	}
	wantOrder := []string{"sa-3", "sa-2", "sa-1"}
	for i, row := range rows {
		if row.ID != wantOrder[i] {
			t.Errorf("rows[%d].ID = %q, want %q -- List must return newest-first", i, row.ID, wantOrder[i])
		}
	}
}

func TestStockAdjustmentListReturnsEmptySliceNotNilForALiveItemWithNoHistory(t *testing.T) {
	scopeA, _ := stockAdjustmentScope(t)
	rows, err := scopeA.StockAdjustments().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if rows == nil {
		t.Error("List returned nil, want a non-nil empty slice")
	}
	if len(rows) != 0 {
		t.Errorf("List returned %d rows, want 0", len(rows))
	}
}

func TestStockAdjustmentListExcludesOtherItemsRowsInTheSameGroup(t *testing.T) {
	scopeA, _ := stockAdjustmentScope(t)

	for i, id := range []string{"a-sa-1", "a-sa-2", "a-sa-3"} {
		if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
			ID: id, ItemID: "itemA", Delta: 1, Now: time.Now().UnixMilli() + int64(i),
		}); err != nil {
			t.Fatalf("Create(%s) on itemA: %v", id, err)
		}
	}
	for i, id := range []string{"a2-sa-1", "a2-sa-2"} {
		if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
			ID: id, ItemID: "itemA2", Delta: 1, Now: time.Now().UnixMilli() + int64(i),
		}); err != nil {
			t.Fatalf("Create(%s) on itemA2: %v", id, err)
		}
	}

	rows, err := scopeA.StockAdjustments().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List(itemA): %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("List(itemA) returned %d rows, want exactly 3 -- itemA2's rows leaked in if this is 5: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row.ItemID != "itemA" {
			t.Errorf("List(itemA) returned a row belonging to item %q, want only itemA's own rows: %+v", row.ItemID, row)
		}
	}

	rowsA2, err := scopeA.StockAdjustments().List(t.Context(), "itemA2")
	if err != nil {
		t.Fatalf("List(itemA2): %v", err)
	}
	if len(rowsA2) != 2 {
		t.Fatalf("List(itemA2) returned %d rows, want exactly 2: %+v", len(rowsA2), rowsA2)
	}
}

func TestStockAdjustmentCreateRejectsAnUnknownOrForeignItem(t *testing.T) {
	scopeA, scopeB := stockAdjustmentScope(t)

	if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
		ID: "sa-1", ItemID: "does-not-exist", Delta: 1, Now: time.Now().UnixMilli(),
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Create against an unknown item = %v, want ErrNotFound", err)
	}

	if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
		ID: "sa-2", ItemID: "itemB", Delta: 1, Now: time.Now().UnixMilli(),
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Create against another group's item = %v, want ErrNotFound", err)
	}

	itemB, err := scopeB.Items().Get(t.Context(), "itemB")
	if err != nil {
		t.Fatalf("scopeB Items().Get(itemB): %v", err)
	}
	if itemB.Quantity != 0 || itemB.Version != 1 {
		t.Errorf("itemB is now {quantity:%d version:%d}, want {quantity:0 version:1} -- a rejected cross-tenant create moved it anyway", itemB.Quantity, itemB.Version)
	}
	rows, err := scopeB.StockAdjustments().List(t.Context(), "itemB")
	if err != nil {
		t.Fatalf("scopeB List(itemB): %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("itemB has %d stock-adjustment rows, want 0 -- a rejected cross-tenant create left a row behind anyway: %+v", len(rows), rows)
	}
}

func TestStockAdjustmentListRejectsAnUnknownOrForeignItem(t *testing.T) {
	scopeA, _ := stockAdjustmentScope(t)

	if _, err := scopeA.StockAdjustments().List(t.Context(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("List against an unknown item = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.StockAdjustments().List(t.Context(), "itemB"); !errors.Is(err, ErrNotFound) {
		t.Errorf("List against another group's item = %v, want ErrNotFound", err)
	}
}

func TestStockAdjustmentCreateUnderConcurrencyNeverLosesAnUpdate(t *testing.T) {
	const writers = 16
	scopeA, _ := stockAdjustmentScope(t)

	errs := make([]error, writers)
	resultingQuantities := make([]int64, writers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			created, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
				ID: fmt.Sprintf("sa-%d", i), ItemID: "itemA", Delta: 1, Now: time.Now().UnixMilli(),
			})
			errs[i] = err
			resultingQuantities[i] = created.ResultingQuantity
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[int64]int, writers)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
		if prev, dup := seen[resultingQuantities[i]]; dup {
			t.Fatalf("writers %d and %d both computed ResultingQuantity %d -- a lost update: both must have read the item's quantity before either write landed", prev, i, resultingQuantities[i])
		}
		seen[resultingQuantities[i]] = i
	}
	for want := int64(1); want <= writers; want++ {
		if _, ok := seen[want]; !ok {
			t.Errorf("no writer computed ResultingQuantity %d -- a gap in 1..%d means an update was lost", want, writers)
		}
	}

	item, err := scopeA.Items().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Items().Get(itemA): %v", err)
	}
	if item.Quantity != writers {
		t.Errorf("final item.Quantity = %d, want %d", item.Quantity, writers)
	}

	rows, err := scopeA.StockAdjustments().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List(itemA): %v", err)
	}
	if len(rows) != writers {
		t.Errorf("List(itemA) returned %d rows, want %d -- one per writer", len(rows), writers)
	}
}
