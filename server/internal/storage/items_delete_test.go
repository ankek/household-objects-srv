package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func itemsDeleteScope(t *testing.T) (scopeA, scopeB Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	seedItem(t, s, "groupA", "itemA2", 20)
	seedItem(t, s, "groupB", "itemB", 30)

	var err error
	scopeA, err = s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err = s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return scopeA, scopeB
}

func TestItemDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := itemsDeleteScope(t)
	now := time.Now().UnixMilli()

	if err := scopeA.Items().Delete(t.Context(), "itemA1", now); err != nil {
		t.Fatalf("Delete = %v, want success", err)
	}
	if _, err := scopeA.Items().Get(t.Context(), "itemA1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Items().Delete(t.Context(), "itemA1", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound (the row is already tombstoned)", err)
	}
	if _, err := scopeA.Items().Get(t.Context(), "itemA2"); err != nil {
		t.Fatalf("itemA2 after itemA1's Delete = %v, want it still readable", err)
	}
}

func TestItemDeleteRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := itemsDeleteScope(t)
	now := time.Now().UnixMilli()

	if err := scopeA.Items().Delete(t.Context(), "itemB", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA deleting groupB's item = %v, want ErrNotFound", err)
	}
	if _, err := scopeB.Items().Get(t.Context(), "itemB"); err != nil {
		t.Fatalf("groupB's own item after groupA's cross-tenant Delete = %v, wanted it still readable; the delete answered ErrNotFound but landed anyway", err)
	}
}

func TestItemDeleteCascadesToDetailBlocks(t *testing.T) {
	scopeA, _ := itemsDeleteScope(t)
	now := time.Now().UnixMilli()

	for _, itemID := range []string{"itemA1", "itemA2"} {
		if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-" + itemID, ItemID: itemID, Holder: "holder-" + itemID, Now: now}); err != nil {
			t.Fatalf("seed warranty on %s: %v", itemID, err)
		}
		if _, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{ID: "sale-" + itemID, ItemID: itemID, BuyerName: "buyer-" + itemID, Now: now}); err != nil {
			t.Fatalf("seed sale on %s: %v", itemID, err)
		}
		if _, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{ID: "purch-" + itemID, ItemID: itemID, Vendor: "vendor-" + itemID, Now: now}); err != nil {
			t.Fatalf("seed purchase on %s: %v", itemID, err)
		}
	}

	if err := scopeA.Items().Delete(t.Context(), "itemA1", now); err != nil {
		t.Fatalf("Delete itemA1: %v", err)
	}

	for _, tc := range []struct {
		name string
		get  func() error
	}{
		{"warranty", func() error { _, err := scopeA.Warranty().Get(t.Context(), "itemA1"); return err }},
		{"sale", func() error { _, err := scopeA.Sale().Get(t.Context(), "itemA1"); return err }},
		{"purchase", func() error { _, err := scopeA.Purchase().Get(t.Context(), "itemA1"); return err }},
	} {
		if err := tc.get(); !errors.Is(err, ErrNotFound) {
			t.Errorf("itemA1's %s after item Delete = %v, want ErrNotFound -- the cascade did not tombstone it", tc.name, err)
		}
	}

	for _, tc := range []struct {
		name string
		get  func() error
	}{
		{"warranty", func() error { _, err := scopeA.Warranty().Get(t.Context(), "itemA2"); return err }},
		{"sale", func() error { _, err := scopeA.Sale().Get(t.Context(), "itemA2"); return err }},
		{"purchase", func() error { _, err := scopeA.Purchase().Get(t.Context(), "itemA2"); return err }},
	} {
		if err := tc.get(); err != nil {
			t.Errorf("itemA2's %s after itemA1's Delete = %v, want it still readable -- the cascade's item_id predicate leaked into a sibling item", tc.name, err)
		}
	}
}

func TestItemDeleteCascadesToIdentificationsAndCustomFields(t *testing.T) {
	scopeA, _ := itemsDeleteScope(t)
	now := time.Now().UnixMilli()

	identIDs := map[string][]string{
		"itemA1": {"id-a1-1", "id-a1-2", "id-a1-3"},
		"itemA2": {"id-a2-1", "id-a2-2"},
	}
	cfIDs := map[string][]string{
		"itemA1": {"cf-a1-1", "cf-a1-2"},
		"itemA2": {"cf-a2-1"},
	}
	for itemID, ids := range identIDs {
		for _, id := range ids {
			if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
				ID: id, ItemID: itemID, Kind: "serial", Value: id, Now: now,
			}); err != nil {
				t.Fatalf("seed identification %s on %s: %v", id, itemID, err)
			}
		}
	}
	for itemID, ids := range cfIDs {
		for _, id := range ids {
			val := id
			if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
				ID: id, ItemID: itemID, Name: "Colour", FieldType: "text", TextValue: &val, Now: now,
			}); err != nil {
				t.Fatalf("seed custom field %s on %s: %v", id, itemID, err)
			}
		}
	}

	if err := scopeA.Items().Delete(t.Context(), "itemA1", now); err != nil {
		t.Fatalf("Delete itemA1: %v", err)
	}

	for _, id := range identIDs["itemA1"] {
		if _, err := scopeA.Identifications().Get(t.Context(), "itemA1", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("itemA1's identification %s after item Delete = %v, want ErrNotFound -- the cascade did not tombstone it", id, err)
		}
	}
	for _, id := range cfIDs["itemA1"] {
		if _, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA1", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("itemA1's custom field %s after item Delete = %v, want ErrNotFound -- the cascade did not tombstone it", id, err)
		}
	}

	for _, id := range identIDs["itemA2"] {
		if _, err := scopeA.Identifications().Get(t.Context(), "itemA2", id); err != nil {
			t.Errorf("itemA2's identification %s after itemA1's Delete = %v, want it still readable", id, err)
		}
	}
	for _, id := range cfIDs["itemA2"] {
		if _, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA2", id); err != nil {
			t.Errorf("itemA2's custom field %s after itemA1's Delete = %v, want it still readable", id, err)
		}
	}
	rows, err := scopeA.Identifications().List(t.Context(), "itemA2")
	if err != nil {
		t.Fatalf("List itemA2 identifications: %v", err)
	}
	if len(rows) != len(identIDs["itemA2"]) {
		t.Errorf("itemA2 identification count after itemA1's Delete = %d, want %d unchanged", len(rows), len(identIDs["itemA2"]))
	}
}

func TestItemDeleteDoesNotCascadeToStockAdjustments(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	now := time.Now().UnixMilli()

	if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
		ID: "sa-1", ItemID: "itemA1", Delta: 5, Reason: "restock", Now: now,
	}); err != nil {
		t.Fatalf("seed stock adjustment: %v", err)
	}

	if err := scopeA.Items().Delete(t.Context(), "itemA1", now); err != nil {
		t.Fatalf("Delete itemA1: %v", err)
	}

	var deletedAt sql.NullInt64
	if err := s.store.Reader().QueryRowContext(t.Context(),
		`SELECT deleted_at FROM stock_adjustments WHERE group_id = ? AND id = ?`, "groupA", "sa-1",
	).Scan(&deletedAt); err != nil {
		t.Fatalf("read back stock adjustment row: %v", err)
	}
	if deletedAt.Valid {
		t.Errorf("stock adjustment sa-1's deleted_at = %v after item Delete, want NULL (unchanged) -- FR-018's append-only history must not be cascaded", deletedAt)
	}
}

func TestItemDeleteFreesShortCodeForReuse(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "item-old", 10)
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	now := time.Now().UnixMilli()

	if err := scopeA.Items().Delete(t.Context(), "item-old", now); err != nil {
		t.Fatalf("Delete item-old: %v", err)
	}

	created, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-new", Name: "A Replacement Lamp", ShortCode: "item-old", Now: now,
	})
	if err != nil {
		t.Fatalf("Create with a tombstoned item's short_code = %v, want success -- the partial unique index should have freed the slot", err)
	}
	if created.ShortCode != "item-old" {
		t.Errorf("created.ShortCode = %q, want %q", created.ShortCode, "item-old")
	}
}

func TestItemDeleteSharesOneChangeSeqAcrossTheCascade(t *testing.T) {
	scopeA, _ := itemsDeleteScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA1", Holder: "alice", Now: now}); err != nil {
		t.Fatalf("seed warranty: %v", err)
	}
	if _, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{ID: "sale-1", ItemID: "itemA1", BuyerName: "bob", Now: now}); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	if _, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{ID: "purch-1", ItemID: "itemA1", Vendor: "acme", Now: now}); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}
	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{ID: "id-1", ItemID: "itemA1", Kind: "serial", Value: "sn-1", Now: now}); err != nil {
		t.Fatalf("seed identification: %v", err)
	}
	val := "red"
	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{ID: "cf-1", ItemID: "itemA1", Name: "Colour", FieldType: "text", TextValue: &val, Now: now}); err != nil {
		t.Fatalf("seed custom field: %v", err)
	}

	if err := scopeA.Items().Delete(t.Context(), "itemA1", now); err != nil {
		t.Fatalf("Delete itemA1: %v", err)
	}

	s := newTestStorageFromScope(t, scopeA)
	itemSeq := rawChangeSeq(t, s, "items", "id", "itemA1")
	if itemSeq <= 0 {
		t.Fatalf("item change_seq = %d, want a positive allocated sequence", itemSeq)
	}
	for _, tc := range []struct {
		table, idCol, id string
	}{
		{"item_warranty", "item_id", "itemA1"},
		{"item_sale", "item_id", "itemA1"},
		{"item_purchase", "item_id", "itemA1"},
		{"item_identifications", "id", "id-1"},
		{"item_custom_fields", "id", "cf-1"},
	} {
		if got := rawChangeSeq(t, s, tc.table, tc.idCol, tc.id); got != itemSeq {
			t.Errorf("%s's change_seq = %d, want %d (the SAME value the item itself was stamped with in this cascade)", tc.table, got, itemSeq)
		}
	}
}

func TestItemDeleteDropsTheItemFromFTS(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "item-searchable", 10)
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	now := time.Now().UnixMilli()

	countFTSRows := func() int {
		var n int
		if err := s.store.Reader().QueryRowContext(t.Context(),
			`SELECT count(*) FROM items_fts WHERE rowid = (SELECT rowid FROM items WHERE id = ?)`,
			"item-searchable",
		).Scan(&n); err != nil {
			t.Fatalf("count items_fts rows: %v", err)
		}
		return n
	}

	if n := countFTSRows(); n != 1 {
		t.Fatalf("items_fts row count before Delete = %d, want 1 -- items_fts_ai's own INSERT trigger did not fire on seedItem's INSERT (this fixture is broken, not the code under test)", n)
	}

	if err := scopeA.Items().Delete(t.Context(), "item-searchable", now); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := countFTSRows(); n != 0 {
		t.Errorf("items_fts row count after Delete = %d, want 0 -- a tombstoned item is still present in the search index (FR-021 leak)", n)
	}
}

func newTestStorageFromScope(t *testing.T, s Scope) *Storage {
	t.Helper()
	gs, ok := s.(groupScope)
	if !ok {
		t.Fatalf("scope is a %T, want groupScope", s)
	}
	return &Storage{store: gs.binding.store}
}

func rawChangeSeq(t *testing.T, s *Storage, table, idCol, id string) int64 {
	t.Helper()
	var seq int64
	q := "SELECT change_seq FROM " + table + " WHERE group_id = 'groupA' AND " + idCol + " = ?"
	if err := s.store.Reader().QueryRowContext(t.Context(), q, id).Scan(&seq); err != nil {
		t.Fatalf("read change_seq from %s where %s = %q: %v", table, idCol, id, err)
	}
	return seq
}
