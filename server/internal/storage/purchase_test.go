package storage

import (
	"errors"
	"testing"
	"time"
)

func purchaseScope(t *testing.T) (Scope, Scope) {
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

func seedPurchase(t *testing.T, scope Scope, id, itemID string) Purchase {
	t.Helper()
	created, err := scope.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID:     id,
		ItemID: itemID,
		Now:    time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("seed purchase %q on item %q: %v", id, itemID, err)
	}
	return created
}

func TestPurchaseCreateSucceedsWithNothingButAnItem(t *testing.T) {
	scopeA, _ := purchaseScope(t)

	created, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID:     "purchase-1",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create(item only) = %v, want success (FR-014: the block is opt-in and no field of it is required)", err)
	}
	if created.Vendor != "" || created.OrderReference != "" || created.Notes != "" {
		t.Errorf("got {vendor:%q order_reference:%q notes:%q}, want all \"\" (schema defaults)", created.Vendor, created.OrderReference, created.Notes)
	}
	if created.PurchasedOn.Valid {
		t.Errorf("got purchased_on:%+v, want NULL", created.PurchasedOn)
	}
	if created.PurchasePriceMinor != 0 {
		t.Errorf("PurchasePriceMinor = %d, want 0 (schema default)", created.PurchasePriceMinor)
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

func TestPurchaseCreateStoresEveryFR014Field(t *testing.T) {
	scopeA, _ := purchaseScope(t)

	created, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID:                 "purchase-1",
		ItemID:             "itemA",
		Vendor:             "Acme Hardware",
		PurchasedOn:        "2026-08-28",
		PurchasePriceMinor: 4599,
		OrderReference:     "ORD-12345",
		Notes:              "picked up in store",
		Now:                time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create(all fields) = %v, want success", err)
	}
	if created.Vendor != "Acme Hardware" {
		t.Errorf("Vendor = %q, want %q", created.Vendor, "Acme Hardware")
	}
	if !created.PurchasedOn.Valid || created.PurchasedOn.String != "2026-08-28" {
		t.Errorf("PurchasedOn = %+v, want a valid %q", created.PurchasedOn, "2026-08-28")
	}
	if created.PurchasePriceMinor != 4599 {
		t.Errorf("PurchasePriceMinor = %d, want 4599 (A96.1: the column and the wire carry the same integer minor units)", created.PurchasePriceMinor)
	}
	if created.OrderReference != "ORD-12345" {
		t.Errorf("OrderReference = %q, want %q", created.OrderReference, "ORD-12345")
	}
	if created.Notes != "picked up in store" {
		t.Errorf("Notes = %q, want %q", created.Notes, "picked up in store")
	}
}

func TestPurchaseCreateRejectsASecondBlock(t *testing.T) {
	scopeA, _ := purchaseScope(t)
	seedPurchase(t, scopeA, "purchase-1", "itemA")

	_, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID:     "purchase-2",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrPurchaseExists) {
		t.Fatalf("second Create on the same item = %v, want ErrPurchaseExists (1:0..1, FR-014)", err)
	}
}

func TestPurchaseCreateAgainAfterDeleteSucceeds(t *testing.T) {
	scopeA, _ := purchaseScope(t)
	seedPurchase(t, scopeA, "purchase-1", "itemA")

	if err := scopeA.Purchase().Delete(t.Context(), "itemA", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID:     "purchase-2",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("Create after Delete = %v, want success (ux_item_purchase_group_item is partial on deleted_at IS NULL)", err)
	}
}

func TestPurchaseCreateRejectsAnItemFromAnotherGroup(t *testing.T) {
	scopeA, _ := purchaseScope(t)

	_, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID:     "purchase-x",
		ItemID: "itemB",
		Now:    time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Create on another group's item = %v, want ErrNotFound (never a foreign-key error, which would confirm the item exists)", err)
	}
}

func TestPurchaseGetRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := purchaseScope(t)
	seedPurchase(t, scopeB, "purchase-b", "itemB")

	if _, err := scopeA.Purchase().Get(t.Context(), "itemB"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA reading groupB's block = %v, want ErrNotFound", err)
	}
	if _, err := scopeB.Purchase().Get(t.Context(), "itemB"); err != nil {
		t.Fatalf("groupB reading its OWN block = %v, want success; the cross-group assertion above would be vacuous", err)
	}
}

func TestPurchaseGetOnAnItemWithNoBlockIsNotFound(t *testing.T) {
	scopeA, _ := purchaseScope(t)

	if _, err := scopeA.Purchase().Get(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on an item with no block = %v, want ErrNotFound", err)
	}
}

func TestPurchaseUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := purchaseScope(t)
	created := seedPurchase(t, scopeA, "purchase-1", "itemA")

	updated, err := scopeA.Purchase().Update(t.Context(), UpdatePurchaseParams{
		ItemID:             "itemA",
		Vendor:             "Acme Hardware",
		PurchasedOn:        "2026-08-28",
		PurchasePriceMinor: 4599,
		OrderReference:     "ORD-12345",
		Notes:              "picked up in store",
		ExpectedVersion:    created.Version,
		Now:                time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Update = %v, want success", err)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("Version = %d, want %d (FR-090: every write bumps the version)", updated.Version, created.Version+1)
	}
	if updated.Vendor != "Acme Hardware" || updated.PurchasePriceMinor != 4599 {
		t.Errorf("got {vendor:%q purchase_price_minor:%d}, want {%q 4599}", updated.Vendor, updated.PurchasePriceMinor, "Acme Hardware")
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestPurchaseUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := purchaseScope(t)
	created := seedPurchase(t, scopeA, "purchase-1", "itemA")

	if _, err := scopeA.Purchase().Update(t.Context(), UpdatePurchaseParams{
		ItemID:          "itemA",
		Vendor:          "first writer",
		ExpectedVersion: created.Version,
		Now:             time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err := scopeA.Purchase().Update(t.Context(), UpdatePurchaseParams{
		ItemID:          "itemA",
		Vendor:          "second writer, stale",
		ExpectedVersion: created.Version,
		Now:             time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale Update = %v, want ErrVersionMismatch (FR-090)", err)
	}
}

func TestPurchaseUpdateRejectsAnotherGroupsBlock(t *testing.T) {
	scopeA, scopeB := purchaseScope(t)
	seedPurchase(t, scopeB, "purchase-b", "itemB")

	_, err := scopeA.Purchase().Update(t.Context(), UpdatePurchaseParams{
		ItemID:          "itemB",
		Vendor:          "pwned by groupA",
		ExpectedVersion: 1,
		Now:             time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA updating groupB's block = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the block exists)", err)
	}
	stillB, err := scopeB.Purchase().Get(t.Context(), "itemB")
	if err != nil {
		t.Fatalf("groupB re-reading its own block: %v", err)
	}
	if stillB.Vendor == "pwned by groupA" || stillB.Version != 1 {
		t.Fatalf("groupB's block is now {vendor:%q version:%d}; groupA's cross-tenant Update answered ErrNotFound but landed anyway", stillB.Vendor, stillB.Version)
	}
}

func TestPurchaseDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := purchaseScope(t)
	seedPurchase(t, scopeA, "purchase-1", "itemA")

	if err := scopeA.Purchase().Delete(t.Context(), "itemA", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete = %v, want success", err)
	}
	if _, err := scopeA.Purchase().Get(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Purchase().Delete(t.Context(), "itemA", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound (the row is already tombstoned)", err)
	}
}

func TestPurchaseDeleteRejectsAnotherGroupsBlock(t *testing.T) {
	scopeA, scopeB := purchaseScope(t)
	seedPurchase(t, scopeB, "purchase-b", "itemB")

	if err := scopeA.Purchase().Delete(t.Context(), "itemB", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA deleting groupB's block = %v, want ErrNotFound", err)
	}
	if _, err := scopeB.Purchase().Get(t.Context(), "itemB"); err != nil {
		t.Fatalf("groupB's own block after groupA's cross-tenant Delete = %v, wanted it still readable; the delete answered ErrNotFound but landed anyway", err)
	}
}

func TestPurchaseRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := purchaseScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{ItemID: "itemA", Now: now}); err == nil {
		t.Error("Create with no ID = nil, want an error")
	}
	if _, err := scopeA.Purchase().Create(t.Context(), CreatePurchaseParams{ID: "purchase-1", Now: now}); err == nil {
		t.Error("Create with no ItemID = nil, want an error")
	}
	if _, err := scopeA.Purchase().Update(t.Context(), UpdatePurchaseParams{ExpectedVersion: 1, Now: now}); err == nil {
		t.Error("Update with no ItemID = nil, want an error")
	}
}
