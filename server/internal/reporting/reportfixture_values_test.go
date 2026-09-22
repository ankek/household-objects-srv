package reporting

import (
	"sort"
	"testing"
)

const (
	expectedUnassignedItemCount       = 13
	expectedUnassignedTotalValueMinor = 13755
	expectedHouseItemCount            = 2
	expectedHouseTotalValueMinor      = 1500
	expectedGarageItemCount           = 1
	expectedGarageTotalValueMinor     = 0
	expectedShelfItemCount            = 1
	expectedShelfTotalValueMinor      = 2000
)

const (
	expectedFragileItemCount        = 1
	expectedFragileTotalValueMinor  = 300
	expectedValuableItemCount       = 1
	expectedValuableTotalValueMinor = 300
)

const (
	expectedWarrantyDefaultWindowRowCount = 2
	expectedWarrantyTodayDaysRemaining    = 0
	expectedWarrantyPlusNDaysRemaining    = 5
	expectedWarrantyWidenedWindowDays     = fixtureWithinDays + 1
	expectedWarrantyPlusNPlus1DaysRemain  = fixtureWithinDays + 1
	expectedWarrantyZeroWindowRowCount    = 1
)

const expectedPurchaseRowCount = 3

const (
	expectedHouseTotalItemCount  = 4
	expectedGarageTotalItemCount = 2
	expectedShelfTotalItemCount  = 1
)

func TestD24FixtureValuationByLocation(t *testing.T) {
	s := newFixtureStorage(t)
	fx := seedD24Fixture(t, s, "grpA")
	poisonLoc, _, _ := seedD24PoisonGroup(t, s, "grpB")

	rows, err := Valuation(t.Context(), fx.reports, GroupByLocation)
	if err != nil {
		t.Fatalf("Valuation(location): %v", err)
	}

	byKey := map[string]ValuationRow{}
	for _, r := range rows {
		byKey[r.GroupKey] = r
	}

	unassigned, ok := byKey[""]
	if !ok {
		t.Fatalf("no Unassigned row; rows = %+v", rows)
	}
	if unassigned.GroupLabel != unassignedLocationLabel {
		t.Errorf("Unassigned row GroupLabel = %q, want %q", unassigned.GroupLabel, unassignedLocationLabel)
	}
	if unassigned.ItemCount != expectedUnassignedItemCount || unassigned.TotalValueMinor != expectedUnassignedTotalValueMinor {
		t.Errorf("Unassigned row = %+v, want ItemCount %d, TotalValueMinor %d", unassigned, expectedUnassignedItemCount, expectedUnassignedTotalValueMinor)
	}

	house, ok := byKey[fx.houseID]
	if !ok {
		t.Fatalf("no House row; rows = %+v", rows)
	}
	if house.ItemCount != expectedHouseItemCount || house.TotalValueMinor != expectedHouseTotalValueMinor {
		t.Errorf("House row = %+v, want ItemCount %d, TotalValueMinor %d", house, expectedHouseItemCount, expectedHouseTotalValueMinor)
	}

	garage, ok := byKey[fx.garageID]
	if !ok {
		t.Fatalf("no Garage row; rows = %+v", rows)
	}
	if garage.ItemCount != expectedGarageItemCount || garage.TotalValueMinor != expectedGarageTotalValueMinor {
		t.Errorf("Garage row = %+v, want ItemCount %d, TotalValueMinor %d", garage, expectedGarageItemCount, expectedGarageTotalValueMinor)
	}

	shelf, ok := byKey[fx.shelfID]
	if !ok {
		t.Fatalf("no Shelf row; rows = %+v", rows)
	}
	if shelf.ItemCount != expectedShelfItemCount || shelf.TotalValueMinor != expectedShelfTotalValueMinor {
		t.Errorf("Shelf row = %+v, want ItemCount %d, TotalValueMinor %d", shelf, expectedShelfItemCount, expectedShelfTotalValueMinor)
	}

	if len(rows) != 4 {
		t.Errorf("Valuation(location) returned %d rows, want 4 (Unassigned, House, Garage, Shelf); rows = %+v", len(rows), rows)
	}
	if _, leaked := byKey[poisonLoc]; leaked {
		t.Errorf("Valuation(location) for group A contains group B's own location %q -- cross-tenant leak", poisonLoc)
	}
}

func TestD24FixtureValuationByLabel(t *testing.T) {
	s := newFixtureStorage(t)
	fx := seedD24Fixture(t, s, "grpA")
	_, poisonLabel, _ := seedD24PoisonGroup(t, s, "grpB")

	rows, err := Valuation(t.Context(), fx.reports, GroupByLabel)
	if err != nil {
		t.Fatalf("Valuation(label): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Valuation(label) returned %d rows, want 2 (Fragile, Valuable); rows = %+v", len(rows), rows)
	}

	byKey := map[string]ValuationRow{}
	for _, r := range rows {
		byKey[r.GroupKey] = r
	}

	fragile, ok := byKey[fx.labelFragileID]
	if !ok {
		t.Fatalf("no Fragile row; rows = %+v", rows)
	}
	if fragile.ItemCount != expectedFragileItemCount || fragile.TotalValueMinor != expectedFragileTotalValueMinor {
		t.Errorf("Fragile row = %+v, want ItemCount %d, TotalValueMinor %d", fragile, expectedFragileItemCount, expectedFragileTotalValueMinor)
	}

	valuable, ok := byKey[fx.labelValuableID]
	if !ok {
		t.Fatalf("no Valuable row; rows = %+v", rows)
	}
	if valuable.ItemCount != expectedValuableItemCount || valuable.TotalValueMinor != expectedValuableTotalValueMinor {
		t.Errorf("Valuable row = %+v, want ItemCount %d, TotalValueMinor %d", valuable, expectedValuableItemCount, expectedValuableTotalValueMinor)
	}

	if _, leaked := byKey[poisonLabel]; leaked {
		t.Errorf("Valuation(label) for group A contains group B's own label %q -- cross-tenant leak", poisonLabel)
	}
}

func TestD24FixtureWarrantyExpiring(t *testing.T) {
	s := newFixtureStorage(t)
	fx := seedD24Fixture(t, s, "grpA")
	_, _, poisonItem := seedD24PoisonGroup(t, s, "grpB")

	t.Run("default window (within_days=fixtureWithinDays)", func(t *testing.T) {
		rows, err := WarrantyExpiring(t.Context(), fx.reports, fixtureToday, fixtureWithinDays)
		if err != nil {
			t.Fatalf("WarrantyExpiring: %v", err)
		}
		if len(rows) != expectedWarrantyDefaultWindowRowCount {
			t.Fatalf("WarrantyExpiring(within_days=%d) returned %d rows, want %d; rows = %+v", fixtureWithinDays, len(rows), expectedWarrantyDefaultWindowRowCount, rows)
		}
		if rows[0].ItemID != fx.warrantyTodayID || rows[0].DaysRemaining != expectedWarrantyTodayDaysRemaining {
			t.Errorf("rows[0] = %+v, want warrantyToday at %d days remaining", rows[0], expectedWarrantyTodayDaysRemaining)
		}
		if rows[1].ItemID != fx.warrantyPlusNID || rows[1].DaysRemaining != expectedWarrantyPlusNDaysRemaining {
			t.Errorf("rows[1] = %+v, want warrantyPlusN at %d days remaining", rows[1], expectedWarrantyPlusNDaysRemaining)
		}
		for _, r := range rows {
			if r.ItemID == poisonItem {
				t.Errorf("WarrantyExpiring for group A contains group B's own item %q -- cross-tenant leak", poisonItem)
			}
		}
	})

	t.Run("widened window admits the one-day-past-edge warranty", func(t *testing.T) {
		rows, err := WarrantyExpiring(t.Context(), fx.reports, fixtureToday, expectedWarrantyWidenedWindowDays)
		if err != nil {
			t.Fatalf("WarrantyExpiring: %v", err)
		}
		var found *WarrantyExpiringRow
		for i := range rows {
			if rows[i].ItemID == fx.warrantyPlusNPlus1 {
				found = &rows[i]
			}
		}
		if found == nil {
			t.Fatalf("WarrantyExpiring(within_days=%d) did not include warrantyPlusNPlus1; rows = %+v", expectedWarrantyWidenedWindowDays, rows)
		}
		if found.DaysRemaining != expectedWarrantyPlusNPlus1DaysRemain {
			t.Errorf("warrantyPlusNPlus1 DaysRemaining = %d, want %d", found.DaysRemaining, expectedWarrantyPlusNPlus1DaysRemain)
		}
	})

	t.Run("within_days=0 means today only", func(t *testing.T) {
		rows, err := WarrantyExpiring(t.Context(), fx.reports, fixtureToday, 0)
		if err != nil {
			t.Fatalf("WarrantyExpiring: %v", err)
		}
		if len(rows) != expectedWarrantyZeroWindowRowCount {
			t.Fatalf("WarrantyExpiring(within_days=0) returned %d rows, want %d; rows = %+v", len(rows), expectedWarrantyZeroWindowRowCount, rows)
		}
		if rows[0].ItemID != fx.warrantyTodayID {
			t.Errorf("rows[0] = %+v, want warrantyToday", rows[0])
		}
	})

	t.Run("already-expired and lifetime warranties never appear", func(t *testing.T) {
		rows, err := WarrantyExpiring(t.Context(), fx.reports, fixtureToday, 3650)
		if err != nil {
			t.Fatalf("WarrantyExpiring: %v", err)
		}
		for _, r := range rows {
			if r.ItemID == fx.warrantyExpiredID {
				t.Errorf("WarrantyExpiring included the already-expired warranty: %+v", r)
			}
			if r.ItemID == fx.warrantyLifetimeID {
				t.Errorf("WarrantyExpiring included the lifetime warranty: %+v", r)
			}
			if r.ItemID == fx.softDeletedID {
				t.Errorf("WarrantyExpiring included the soft-deleted item's warranty: %+v", r)
			}
		}
	})
}

func TestD24FixturePurchasesInRange(t *testing.T) {
	s := newFixtureStorage(t)
	fx := seedD24Fixture(t, s, "grpA")
	_, _, poisonItem := seedD24PoisonGroup(t, s, "grpB")

	rows, err := PurchasesInRange(t.Context(), fx.reports, fixtureRangeFrom, fixtureRangeTo)
	if err != nil {
		t.Fatalf("PurchasesInRange: %v", err)
	}
	if len(rows) != expectedPurchaseRowCount {
		t.Fatalf("PurchasesInRange returned %d rows, want %d; rows = %+v", len(rows), expectedPurchaseRowCount, rows)
	}

	if rows[0].ItemID != fx.purchaseOnFromID || rows[0].PurchasedOn != fixtureRangeFrom || rows[0].PurchasePriceMinor != 111 {
		t.Errorf("rows[0] = %+v, want purchaseOnFrom at %s, price 111", rows[0], fixtureRangeFrom)
	}
	if rows[1].ItemID != fx.purchaseZeroID || rows[1].PurchasedOn != "2026-03-05" || rows[1].PurchasePriceMinor != 0 {
		t.Errorf("rows[1] = %+v, want purchaseZero at 2026-03-05, price 0", rows[1])
	}
	if rows[2].ItemID != fx.purchaseOnToID || rows[2].PurchasedOn != fixtureRangeTo || rows[2].PurchasePriceMinor != 222 {
		t.Errorf("rows[2] = %+v, want purchaseOnTo at %s, price 222", rows[2], fixtureRangeTo)
	}

	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ItemID)
	}
	sort.Strings(ids)
	for _, excluded := range []string{fx.purchaseBeforeFrom, fx.purchaseAfterToID, fx.purchaseNullDateID, fx.deletedPurchaseID, fx.softDeletedID, poisonItem} {
		for _, id := range ids {
			if id == excluded {
				t.Errorf("PurchasesInRange unexpectedly included %q", excluded)
			}
		}
	}
}

func TestD24FixtureItemCountByLocation(t *testing.T) {
	s := newFixtureStorage(t)
	fx := seedD24Fixture(t, s, "grpA")
	poisonLoc, _, _ := seedD24PoisonGroup(t, s, "grpB")

	rows, err := ItemCountByLocation(t.Context(), fx.locations)
	if err != nil {
		t.Fatalf("ItemCountByLocation: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ItemCountByLocation returned %d rows, want 3 (House, Garage, Shelf); rows = %+v", len(rows), rows)
	}

	if rows[0].LocationID != fx.houseID || rows[0].ItemCount != expectedHouseTotalItemCount {
		t.Errorf("rows[0] = %+v, want House at rolled-up count %d", rows[0], expectedHouseTotalItemCount)
	}
	if rows[1].LocationID != fx.garageID || rows[1].ItemCount != expectedGarageTotalItemCount {
		t.Errorf("rows[1] = %+v, want Garage at rolled-up count %d", rows[1], expectedGarageTotalItemCount)
	}
	if rows[2].LocationID != fx.shelfID || rows[2].ItemCount != expectedShelfTotalItemCount {
		t.Errorf("rows[2] = %+v, want Shelf at rolled-up count %d", rows[2], expectedShelfTotalItemCount)
	}

	for _, r := range rows {
		if r.LocationID == poisonLoc {
			t.Errorf("ItemCountByLocation for group A contains group B's own location %q -- cross-tenant leak", poisonLoc)
		}
	}
}
