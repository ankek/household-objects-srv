package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func ftsRowFor(t *testing.T, s *Storage, itemID string) (row struct{ Name, Description, Identifier, Notes string }, ok bool) {
	t.Helper()
	err := s.store.Reader().QueryRowContext(t.Context(),
		`SELECT f.name, f.description, f.identifier, f.notes
		   FROM items_fts f JOIN items i ON i.rowid = f.rowid
		  WHERE i.id = ?`, itemID,
	).Scan(&row.Name, &row.Description, &row.Identifier, &row.Notes)
	switch {
	case err == sql.ErrNoRows:
		return row, false
	case err != nil:
		t.Fatalf("read items_fts row for %q: %v", itemID, err)
	}
	return row, true
}

func countFTSRowsFor(t *testing.T, s *Storage, itemID string) int {
	t.Helper()
	var n int
	if err := s.store.Reader().QueryRowContext(t.Context(),
		`SELECT count(*) FROM items_fts WHERE rowid = (SELECT rowid FROM items WHERE id = ?)`,
		itemID,
	).Scan(&n); err != nil {
		t.Fatalf("count items_fts rows for %q: %v", itemID, err)
	}
	return n
}

func tombstoneItemRaw(t *testing.T, s *Storage, itemID string, at int64) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`UPDATE items SET deleted_at = ?, updated_at = ? WHERE id = ?`, at, at, itemID)
		return err
	}); err != nil {
		t.Fatalf("tombstone item %q: %v", itemID, err)
	}
}

func undeleteItemRaw(t *testing.T, s *Storage, itemID string, at int64) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`UPDATE items SET deleted_at = NULL, updated_at = ? WHERE id = ?`, at, itemID)
		return err
	}); err != nil {
		t.Fatalf("undelete item %q: %v", itemID, err)
	}
}

func seedPurchaseNotes(t *testing.T, s *Storage, groupID, itemID, notes string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_purchase (id, group_id, item_id, notes, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, 1)`,
			"prc-"+itemID, groupID, itemID, notes)
		return err
	}); err != nil {
		t.Fatalf("seed purchase notes for %q: %v", itemID, err)
	}
}

func seedSaleNotes(t *testing.T, s *Storage, groupID, itemID, notes string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO item_sale (id, group_id, item_id, notes, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, 1)`,
			"sle-"+itemID, groupID, itemID, notes)
		return err
	}); err != nil {
		t.Fatalf("seed sale notes for %q: %v", itemID, err)
	}
}

func TestUndeleteRestoresItemToFTSWithAllFourColumns(t *testing.T) {
	f := newFilterFixture(t)
	s, scopeA := f.storage, f.scopeA
	seedFilterItem(t, s, "groupA", "itm-drill", "Cordless drill", "eighteen volt hammer", "", 30)
	seedIdentification(t, s, "groupA", "itm-drill", "SNXYZ")
	seedWarrantyNotes(t, s, "groupA", "itm-drill", "purchased at Hardwareshop")
	seedPurchaseNotes(t, s, "groupA", "itm-drill", "boughtFromVendorX")
	seedSaleNotes(t, s, "groupA", "itm-drill", "soldToBuyerY")

	if n := countFTSRowsFor(t, s, "itm-drill"); n != 1 {
		t.Fatalf("items_fts row count before tombstone = %d, want 1 -- fixture is broken, not the code under test", n)
	}

	tombstoneItemRaw(t, s, "itm-drill", 99)
	if _, ok := ftsRowFor(t, s, "itm-drill"); ok {
		t.Fatalf("items_fts still has a row for %q after tombstoning it -- fixture setup did not exercise items_fts_au's tombstone half", "itm-drill")
	}

	undeleteItemRaw(t, s, "itm-drill", 100)

	if n := countFTSRowsFor(t, s, "itm-drill"); n != 1 {
		t.Fatalf("items_fts row count after undelete = %d, want exactly 1 (not dropped, not duplicated)", n)
	}

	for name, term := range map[string]string{
		"name":                 "Cordless",
		"description":          "hammer",
		"identification value": "SNXYZ",
		"notes":                "Hardwareshop",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: term}, wholePage)
			if err != nil {
				t.Fatalf("search %q after undelete: %v", term, err)
			}
			assertIDs(t, got, "itm-drill")
		})
	}

	row, ok := ftsRowFor(t, s, "itm-drill")
	if !ok {
		t.Fatalf("items_fts has no row for itm-drill after undelete")
	}
	for _, want := range []string{"Hardwareshop", "boughtFromVendorX", "soldToBuyerY"} {
		if !strings.Contains(row.Notes, want) {
			t.Errorf("items_fts.notes after undelete = %q, want it to contain %q (all three note sources)", row.Notes, want)
		}
	}
}

func TestUndeleteRepopulatesIdentifierFromCurrentSatelliteStateNotStale(t *testing.T) {
	f := newFilterFixture(t)
	s, scopeA := f.storage, f.scopeA
	seedFilterItem(t, s, "groupA", "itm-drill", "Cordless drill", "", "", 30)

	tombstoneItemRaw(t, s, "itm-drill", 99)
	seedIdentification(t, s, "groupA", "itm-drill", "ADDEDWHILEDEAD")

	undeleteItemRaw(t, s, "itm-drill", 100)

	got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "ADDEDWHILEDEAD"}, wholePage)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertIDs(t, got, "itm-drill")
}

func TestNameDescriptionUpdateRefreshesFTSIndex(t *testing.T) {
	f := newFilterFixture(t)
	scopeA := f.scopeA
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless Drill", "eighteen volt", "", 30)
	current, err := scopeA.Items().Get(t.Context(), "itm-drill")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "itm-drill", Name: "Impact Wrench", Description: "twelve volt",
		ExpectedVersion: current.Version, Now: 40,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	old, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "Cordless"}, wholePage)
	if err != nil {
		t.Fatalf("search old name: %v", err)
	}
	assertIDs(t, old)

	got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "Wrench"}, wholePage)
	if err != nil {
		t.Fatalf("search new name: %v", err)
	}
	assertIDs(t, got, "itm-drill")
}

func TestHotPathWritesDoNotDisturbFTSIndex(t *testing.T) {
	t.Run("stock adjustment (quantity/version/change_seq only)", func(t *testing.T) {
		f := newFilterFixture(t)
		s, scopeA := f.storage, f.scopeA
		seedFilterItem(t, s, "groupA", "itm-drill", "Cordless Drill", "eighteen volt", "", 30)
		seedIdentification(t, s, "groupA", "itm-drill", "SNQTY")
		before, ok := ftsRowFor(t, s, "itm-drill")
		if !ok {
			t.Fatalf("no items_fts row before the stock adjustment -- fixture is broken")
		}

		if _, err := scopeA.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
			ID: "sa-1", ItemID: "itm-drill", Delta: 5, Reason: "restock", Now: 40,
		}); err != nil {
			t.Fatalf("stock adjustment: %v", err)
		}

		if n := countFTSRowsFor(t, s, "itm-drill"); n != 1 {
			t.Fatalf("items_fts row count after a pure quantity write = %d, want 1 (neither dropped nor duplicated)", n)
		}
		after, ok := ftsRowFor(t, s, "itm-drill")
		if !ok {
			t.Fatalf("items_fts row for itm-drill vanished after a stock adjustment -- FR-021 leak on a write that never touches name/description/deleted_at")
		}
		if after != before {
			t.Errorf("items_fts row changed after a pure quantity write:\nbefore: %+v\nafter:  %+v", before, after)
		}
		got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "Cordless"}, wholePage)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		assertIDs(t, got, "itm-drill")
	})

	t.Run("whole-row update resending the same name/description", func(t *testing.T) {
		f := newFilterFixture(t)
		s, scopeA := f.storage, f.scopeA
		seedFilterItem(t, s, "groupA", "itm-drill", "Cordless Drill", "eighteen volt", "", 30)
		current, err := scopeA.Items().Get(t.Context(), "itm-drill")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		before, ok := ftsRowFor(t, s, "itm-drill")
		if !ok {
			t.Fatalf("no items_fts row before the update -- fixture is broken")
		}

		if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
			ItemID: "itm-drill", Name: current.Name, Description: current.Description,
			Quantity: current.Quantity + 3, ExpectedVersion: current.Version, Now: 40,
		}); err != nil {
			t.Fatalf("Update: %v", err)
		}

		if n := countFTSRowsFor(t, s, "itm-drill"); n != 1 {
			t.Fatalf("items_fts row count after an unchanged-name/description update = %d, want 1", n)
		}
		after, ok := ftsRowFor(t, s, "itm-drill")
		if !ok {
			t.Fatalf("items_fts row for itm-drill vanished after an update that resent its own unchanged name/description")
		}
		if after != before {
			t.Errorf("items_fts row changed after an unchanged-name/description update:\nbefore: %+v\nafter:  %+v", before, after)
		}
	})
}

func TestIdentificationDeleteRemovesOnlyItsOwnValueFromFTS(t *testing.T) {
	f := newFilterFixture(t)
	scopeA := f.scopeA
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless Drill", "", "", 30)
	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "idn-serial", ItemID: "itm-drill", Kind: "serial", Value: "SNTOKEEP", Now: 1,
	}); err != nil {
		t.Fatalf("create identification to keep: %v", err)
	}
	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "idn-barcode", ItemID: "itm-drill", Kind: "barcode", Value: "SNTODELETE", Now: 1,
	}); err != nil {
		t.Fatalf("create identification to delete: %v", err)
	}

	for name, term := range map[string]string{"kept": "SNTOKEEP", "deleted-before": "SNTODELETE"} {
		got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: term}, wholePage)
		if err != nil {
			t.Fatalf("search %s: %v", name, err)
		}
		assertIDs(t, got, "itm-drill")
	}

	if err := scopeA.Identifications().Delete(t.Context(), "itm-drill", "idn-barcode", 40); err != nil {
		t.Fatalf("delete identification: %v", err)
	}

	gone, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "SNTODELETE"}, wholePage)
	if err != nil {
		t.Fatalf("search deleted value: %v", err)
	}
	assertIDs(t, gone)

	stillThere, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "SNTOKEEP"}, wholePage)
	if err != nil {
		t.Fatalf("search kept value: %v", err)
	}
	assertIDs(t, stillThere, "itm-drill")

	byName, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "Cordless"}, wholePage)
	if err != nil {
		t.Fatalf("search by name: %v", err)
	}
	assertIDs(t, byName, "itm-drill")
}

func TestSatelliteNotesEditDoesNotClobberTheOtherTwoSources(t *testing.T) {
	f := newFilterFixture(t)
	scopeA := f.scopeA
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless Drill", "", "", 30)
	seedWarrantyNotes(t, f.storage, "groupA", "itm-drill", "OLDWARRANTYNOTE")
	seedPurchaseNotes(t, f.storage, "groupA", "itm-drill", "PURCHASENOTEUNTOUCHED")
	seedSaleNotes(t, f.storage, "groupA", "itm-drill", "SALENOTEUNTOUCHED")

	if _, err := scopeA.Warranty().Update(t.Context(), UpdateWarrantyParams{
		ItemID: "itm-drill", Notes: "NEWWARRANTYNOTE", ExpectedVersion: 1, Now: 40,
	}); err != nil {
		t.Fatalf("update warranty notes: %v", err)
	}

	for name, tc := range map[string]struct {
		term string
		want []string
	}{
		"old warranty text is gone":        {"OLDWARRANTYNOTE", nil},
		"new warranty text is findable":    {"NEWWARRANTYNOTE", []string{"itm-drill"}},
		"purchase text survives untouched": {"PURCHASENOTEUNTOUCHED", []string{"itm-drill"}},
		"sale text survives untouched":     {"SALENOTEUNTOUCHED", []string{"itm-drill"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: tc.term}, wholePage)
			if err != nil {
				t.Fatalf("search %q: %v", tc.term, err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestSatelliteBlockDeleteRemovesOnlyItsOwnNotesFromFTS(t *testing.T) {
	f := newFilterFixture(t)
	scopeA := f.scopeA
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless Drill", "", "", 30)
	seedWarrantyNotes(t, f.storage, "groupA", "itm-drill", "WARRANTYNOTEGOESAWAY")
	seedPurchaseNotes(t, f.storage, "groupA", "itm-drill", "PURCHASENOTESTAYS")
	seedSaleNotes(t, f.storage, "groupA", "itm-drill", "SALENOTESTAYS")

	if err := scopeA.Warranty().Delete(t.Context(), "itm-drill", 40); err != nil {
		t.Fatalf("delete warranty: %v", err)
	}

	for name, tc := range map[string]struct {
		term string
		want []string
	}{
		"deleted warranty text is gone":  {"WARRANTYNOTEGOESAWAY", nil},
		"purchase text stays":            {"PURCHASENOTESTAYS", []string{"itm-drill"}},
		"sale text stays":                {"SALENOTESTAYS", []string{"itm-drill"}},
		"item is still findable by name": {"Cordless", []string{"itm-drill"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: tc.term}, wholePage)
			if err != nil {
				t.Fatalf("search %q: %v", tc.term, err)
			}
			assertIDs(t, got, tc.want...)
		})
	}
}

func TestSearchComposesWithPagination(t *testing.T) {
	f := newFilterFixture(t)
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("itm-drill-%02d", i)
		seedFilterItem(t, f.storage, "groupA", id, "Cordless Drill", "", "", 500)
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("itm-kettle-%02d", i)
		seedFilterItem(t, f.storage, "groupA", id, "Kettle", "", "", 500)
	}

	list := func(p Page) ([]Item, error) {
		return f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{Query: "drill"}, p)
	}
	oneShot, err := list(wholePage)
	if err != nil {
		t.Fatalf("one-shot search: %v", err)
	}
	if len(oneShot) != 7 {
		t.Fatalf("one-shot search matched %d rows, want 7 -- fixture is broken", len(oneShot))
	}
	assembled := pageThroughAll(t, list, 2)
	assertNoDropsOrRepeats(t, assembled, idsOf(oneShot))
}

func TestSearchComposesWithSort(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-z", "Cordless Drill Zulu", "", "", 300)
	seedFilterItem(t, f.storage, "groupA", "itm-a", "Cordless Drill Alpha", "", "", 100)
	seedFilterItem(t, f.storage, "groupA", "itm-m", "Cordless Drill Mike", "", "", 200)

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		Query: "drill", Sort: ItemSort{Field: ItemSortName},
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if want, gotIDs := []string{"itm-a", "itm-m", "itm-z"}, idListOrdered(got); strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("search+sort order = %v, want %v", gotIDs, want)
	}
}

func TestSearchComposesWithEachFilterKind(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "")
	seedLabelRow(t, f.storage, "groupA", "lbl-fragile", "fragile")

	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-target", "Cordless drill", "loc-garage", 5000, 5000)
	seedItemLabel(t, f.storage, "groupA", "itm-target", "lbl-fragile")
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-target", ItemID: "itm-target", Name: "Colour", FieldType: "text", TextValue: strPtr("Red")})
	seedWarranty(t, f.storage, "groupA", warrantySeed{ID: "wty-target", ItemID: "itm-target", IsLifetime: true})

	seedFilterItemWithTimestamps(t, f.storage, "groupA", "itm-decoy", "Cordless drill", "", 1000, 1000)

	for name, filter := range map[string]ItemFilter{
		"search + location":     {Query: "drill", LocationIDs: []string{"loc-garage"}},
		"search + label":        {Query: "drill", LabelIDs: []string{"lbl-fragile"}},
		"search + custom field": {Query: "drill", CustomFields: []CustomFieldMatch{{Name: "Colour", Value: "Red"}}},
		"search + warranty":     {Query: "drill", WarrantyStatus: WarrantyStatusLifetime},
		"search + date range":   {Query: "drill", CreatedFrom: i64Ptr(4000)},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.scopeA.Items().ListFiltered(t.Context(), filter, wholePage)
			if err != nil {
				t.Fatalf("ListFiltered: %v", err)
			}
			assertIDs(t, got, "itm-target")
		})
	}
}

func TestSearchPlusCustomFieldCombinationMatchingNothingIsEmptyList(t *testing.T) {
	f := newFilterFixture(t)
	seedFilterItem(t, f.storage, "groupA", "itm-drill", "Cordless Drill", "", "", 30)
	seedCustomField(t, f.storage, "groupA", customFieldSeed{ID: "cf-1", ItemID: "itm-drill", Name: "Colour", FieldType: "text", TextValue: strPtr("Blue")})

	got, err := f.scopeA.Items().ListFiltered(t.Context(), ItemFilter{
		Query:        "drill",
		CustomFields: []CustomFieldMatch{{Name: "Colour", Value: "Red"}},
	}, wholePage)
	if err != nil {
		t.Fatalf("ListFiltered returned an error for a search narrowed to nothing by a custom-field filter: %v -- want an empty list", err)
	}
	assertIDs(t, got)
}
