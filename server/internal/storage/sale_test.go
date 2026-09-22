package storage

import (
	"errors"
	"testing"
	"time"
)

func saleScope(t *testing.T) (Scope, Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupB", "itemB", 20)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err := s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return scopeA, scopeB
}

func seedSale(t *testing.T, scope Scope, id, itemID string) Sale {
	t.Helper()
	created, err := scope.Sale().Create(t.Context(), CreateSaleParams{
		ID:     id,
		ItemID: itemID,
		Now:    time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("seed sale %q on item %q: %v", id, itemID, err)
	}
	return created
}

func TestSaleCreateSucceedsWithNothingButAnItem(t *testing.T) {
	scopeA, _ := saleScope(t)

	created, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{
		ID:     "sale-1",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create(item only) = %v, want success (FR-013: the block is opt-in and no field of it is required)", err)
	}
	if created.BuyerName != "" || created.Notes != "" {
		t.Errorf("got {buyer_name:%q notes:%q}, want both \"\" (schema defaults)", created.BuyerName, created.Notes)
	}
	if created.SoldOn.Valid {
		t.Errorf("got sold_on:%+v, want NULL", created.SoldOn)
	}
	if created.SalePriceMinor != 0 {
		t.Errorf("SalePriceMinor = %d, want 0 (schema default)", created.SalePriceMinor)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
	}
	if created.ItemID != "itemA" {
		t.Errorf("ItemID = %q, want %q", created.ItemID, "itemA")
	}
}

func TestSaleCreateStoresEveryFR013Field(t *testing.T) {
	scopeA, _ := saleScope(t)

	created, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{
		ID:             "sale-1",
		ItemID:         "itemA",
		BuyerName:      "Dana Buyer",
		SoldOn:         "2026-08-28",
		SalePriceMinor: 4599,
		Notes:          "collected in person",
		Now:            time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create(all fields) = %v, want success", err)
	}
	if created.BuyerName != "Dana Buyer" {
		t.Errorf("BuyerName = %q, want %q", created.BuyerName, "Dana Buyer")
	}
	if !created.SoldOn.Valid || created.SoldOn.String != "2026-08-28" {
		t.Errorf("SoldOn = %+v, want a valid %q", created.SoldOn, "2026-08-28")
	}
	if created.SalePriceMinor != 4599 {
		t.Errorf("SalePriceMinor = %d, want 4599 (A96.1: the column and the wire carry the same integer minor units)", created.SalePriceMinor)
	}
	if created.Notes != "collected in person" {
		t.Errorf("Notes = %q, want %q", created.Notes, "collected in person")
	}
}

func TestSaleCreateRejectsASecondBlock(t *testing.T) {
	scopeA, _ := saleScope(t)
	seedSale(t, scopeA, "sale-1", "itemA")

	_, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{
		ID:     "sale-2",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrSaleExists) {
		t.Fatalf("second Create on the same item = %v, want ErrSaleExists (1:0..1, FR-013)", err)
	}
}

func TestSaleCreateAgainAfterDeleteSucceeds(t *testing.T) {
	scopeA, _ := saleScope(t)
	seedSale(t, scopeA, "sale-1", "itemA")

	if err := scopeA.Sale().Delete(t.Context(), "itemA", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{
		ID:     "sale-2",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("Create after Delete = %v, want success (ux_item_sale_group_item is partial on deleted_at IS NULL)", err)
	}
}

func TestSaleCreateRejectsAnItemFromAnotherGroup(t *testing.T) {
	scopeA, _ := saleScope(t)

	_, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{
		ID:     "sale-x",
		ItemID: "itemB",
		Now:    time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Create on another group's item = %v, want ErrNotFound (never a foreign-key error, which would confirm the item exists)", err)
	}
}

func TestSaleGetRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := saleScope(t)
	seedSale(t, scopeB, "sale-b", "itemB")

	if _, err := scopeA.Sale().Get(t.Context(), "itemB"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA reading groupB's block = %v, want ErrNotFound", err)
	}
	if _, err := scopeB.Sale().Get(t.Context(), "itemB"); err != nil {
		t.Fatalf("groupB reading its OWN block = %v, want success; the cross-group assertion above would be vacuous", err)
	}
}

func TestSaleGetOnAnItemWithNoBlockIsNotFound(t *testing.T) {
	scopeA, _ := saleScope(t)

	if _, err := scopeA.Sale().Get(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on an item with no block = %v, want ErrNotFound", err)
	}
}

func TestSaleUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := saleScope(t)
	created := seedSale(t, scopeA, "sale-1", "itemA")

	updated, err := scopeA.Sale().Update(t.Context(), UpdateSaleParams{
		ItemID:          "itemA",
		BuyerName:       "Dana Buyer",
		SoldOn:          "2026-08-28",
		SalePriceMinor:  4599,
		Notes:           "collected in person",
		ExpectedVersion: created.Version,
		Now:             time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Update = %v, want success", err)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("Version = %d, want %d (FR-090: every write bumps the version)", updated.Version, created.Version+1)
	}
	if updated.BuyerName != "Dana Buyer" || updated.SalePriceMinor != 4599 {
		t.Errorf("got {buyer_name:%q sale_price_minor:%d}, want {%q 4599}", updated.BuyerName, updated.SalePriceMinor, "Dana Buyer")
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestSaleUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := saleScope(t)
	created := seedSale(t, scopeA, "sale-1", "itemA")

	if _, err := scopeA.Sale().Update(t.Context(), UpdateSaleParams{
		ItemID:          "itemA",
		BuyerName:       "first writer",
		ExpectedVersion: created.Version,
		Now:             time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err := scopeA.Sale().Update(t.Context(), UpdateSaleParams{
		ItemID:          "itemA",
		BuyerName:       "second writer, stale",
		ExpectedVersion: created.Version,
		Now:             time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale Update = %v, want ErrVersionMismatch (FR-090)", err)
	}
}

func TestSaleUpdateRejectsAnotherGroupsBlock(t *testing.T) {
	scopeA, scopeB := saleScope(t)
	seedSale(t, scopeB, "sale-b", "itemB")

	_, err := scopeA.Sale().Update(t.Context(), UpdateSaleParams{
		ItemID:          "itemB",
		BuyerName:       "pwned by groupA",
		ExpectedVersion: 1,
		Now:             time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA updating groupB's block = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the block exists)", err)
	}
	stillB, err := scopeB.Sale().Get(t.Context(), "itemB")
	if err != nil {
		t.Fatalf("groupB re-reading its own block: %v", err)
	}
	if stillB.BuyerName == "pwned by groupA" || stillB.Version != 1 {
		t.Fatalf("groupB's block is now {buyer_name:%q version:%d}; groupA's cross-tenant Update answered ErrNotFound but landed anyway", stillB.BuyerName, stillB.Version)
	}
}

func TestSaleDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := saleScope(t)
	seedSale(t, scopeA, "sale-1", "itemA")

	if err := scopeA.Sale().Delete(t.Context(), "itemA", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete = %v, want success", err)
	}
	if _, err := scopeA.Sale().Get(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Sale().Delete(t.Context(), "itemA", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound (the row is already tombstoned)", err)
	}
}

func TestSaleDeleteRejectsAnotherGroupsBlock(t *testing.T) {
	scopeA, scopeB := saleScope(t)
	seedSale(t, scopeB, "sale-b", "itemB")

	if err := scopeA.Sale().Delete(t.Context(), "itemB", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA deleting groupB's block = %v, want ErrNotFound", err)
	}
	if _, err := scopeB.Sale().Get(t.Context(), "itemB"); err != nil {
		t.Fatalf("groupB's own block after groupA's cross-tenant Delete = %v, wanted it still readable; the delete answered ErrNotFound but landed anyway", err)
	}
}

func TestSaleRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := saleScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{ItemID: "itemA", Now: now}); err == nil {
		t.Error("Create with no ID = nil, want an error")
	}
	if _, err := scopeA.Sale().Create(t.Context(), CreateSaleParams{ID: "sale-1", Now: now}); err == nil {
		t.Error("Create with no ItemID = nil, want an error")
	}
	if _, err := scopeA.Sale().Update(t.Context(), UpdateSaleParams{ExpectedVersion: 1, Now: now}); err == nil {
		t.Error("Update with no ItemID = nil, want an error")
	}
}
