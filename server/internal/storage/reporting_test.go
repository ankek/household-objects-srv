package storage

import (
	"database/sql"
	"errors"
	"sort"
	"testing"
)

func seedGroupRow(t *testing.T, s *Storage, groupID string) {
	t.Helper()
	err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT OR IGNORE INTO groups (id, name, created_at, updated_at) VALUES (?, ?, 1, 1)`,
			groupID, groupID)
		return err
	})
	if err != nil {
		t.Fatalf("seed group %q: %v", groupID, err)
	}
}

type reportFixture struct {
	reports ReportRepository

	garageID        string
	labelFragileID  string
	labelValuableID string

	itemInGarageID   string
	itemUnassignedID string
	itemTwoLabelsID  string
	itemWarrantyID   string
	itemLifetimeID   string
}

func newReportFixture(t *testing.T, s *Storage, groupID string) reportFixture {
	t.Helper()
	seedGroupRow(t, s, groupID)
	scope, err := s.ForGroup(MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%q): %v", groupID, err)
	}

	garage, err := scope.Locations().Create(t.Context(), CreateLocationParams{ID: groupID + "-garage", Name: "Garage", Now: 1})
	if err != nil {
		t.Fatalf("create garage location: %v", err)
	}

	fragile, err := scope.Labels().Create(t.Context(), CreateLabelParams{ID: groupID + "-fragile", Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("create fragile label: %v", err)
	}
	valuable, err := scope.Labels().Create(t.Context(), CreateLabelParams{ID: groupID + "-valuable", Name: "Valuable", Color: "#00ff00", Now: 1})
	if err != nil {
		t.Fatalf("create valuable label: %v", err)
	}

	itemInGarage, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: groupID + "-item-garage", Name: "Lawnmower", LocationID: garage.ID, ShortCode: groupID + "GARAGE1", Now: 1,
	})
	if err != nil {
		t.Fatalf("create item in garage: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID: groupID + "-purchase-garage", ItemID: itemInGarage.ID, PurchasedOn: "2026-01-01", PurchasePriceMinor: 1000, Now: 1,
	}); err != nil {
		t.Fatalf("create purchase for garage item: %v", err)
	}

	itemUnassigned, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: groupID + "-item-unassigned", Name: "Loose Screw", ShortCode: groupID + "LOOSE1", Now: 1,
	})
	if err != nil {
		t.Fatalf("create unassigned item: %v", err)
	}

	itemTwoLabels, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: groupID + "-item-two-labels", Name: "Vase", ShortCode: groupID + "VASE001", Now: 1,
	})
	if err != nil {
		t.Fatalf("create two-label item: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID: groupID + "-purchase-vase", ItemID: itemTwoLabels.ID, PurchasedOn: "2026-02-01", PurchasePriceMinor: 500, Now: 1,
	}); err != nil {
		t.Fatalf("create purchase for vase: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), AttachLabelParams{ID: groupID + "-edge-1", ItemID: itemTwoLabels.ID, LabelID: fragile.ID, Now: 1}); err != nil {
		t.Fatalf("attach fragile: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), AttachLabelParams{ID: groupID + "-edge-2", ItemID: itemTwoLabels.ID, LabelID: valuable.ID, Now: 1}); err != nil {
		t.Fatalf("attach valuable: %v", err)
	}

	itemWarranty, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: groupID + "-item-warranty", Name: "Fridge", ShortCode: groupID + "FRIDGE1", Now: 1,
	})
	if err != nil {
		t.Fatalf("create warranty item: %v", err)
	}
	if _, err := scope.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID: groupID + "-warranty-fridge", ItemID: itemWarranty.ID, ExpiresOn: "2026-10-01", Now: 1,
	}); err != nil {
		t.Fatalf("create warranty for fridge: %v", err)
	}

	itemLifetime, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: groupID + "-item-lifetime", Name: "Cast Iron Pan", ShortCode: groupID + "PAN0001", Now: 1,
	})
	if err != nil {
		t.Fatalf("create lifetime item: %v", err)
	}
	if _, err := scope.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID: groupID + "-warranty-pan", ItemID: itemLifetime.ID, IsLifetime: true, Now: 1,
	}); err != nil {
		t.Fatalf("create lifetime warranty for pan: %v", err)
	}

	reports, err := s.ForGroupReports(MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupReports(%q): %v", groupID, err)
	}

	return reportFixture{
		reports:          reports,
		garageID:         garage.ID,
		labelFragileID:   fragile.ID,
		labelValuableID:  valuable.ID,
		itemInGarageID:   itemInGarage.ID,
		itemUnassignedID: itemUnassigned.ID,
		itemTwoLabelsID:  itemTwoLabels.ID,
		itemWarrantyID:   itemWarranty.ID,
		itemLifetimeID:   itemLifetime.ID,
	}
}

func TestForGroupReportsRefusesEveryUnscopedBinding(t *testing.T) {
	t.Run("zero group id", func(t *testing.T) {
		s := newTestStorage(t)
		repo, err := s.ForGroupReports(GroupID{})
		if !errors.Is(err, ErrNoGroup) {
			t.Fatalf("ForGroupReports(GroupID{}) error = %v, want ErrNoGroup", err)
		}
		if repo != nil {
			t.Error("ForGroupReports returned a repository alongside its error")
		}
	})

	t.Run("unopened storage", func(t *testing.T) {
		if _, err := (&Storage{}).ForGroupReports(MustGroupID("groupA")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("ForGroupReports on a zero Storage: error = %v, want ErrNoGroup", err)
		}
	})

	t.Run("nil storage", func(t *testing.T) {
		var s *Storage
		if _, err := s.ForGroupReports(MustGroupID("groupA")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("ForGroupReports on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}

func TestReportRepositoryValuationByLocation(t *testing.T) {
	s := newTestStorage(t)
	fx := newReportFixture(t, s, "groupA")
	newReportFixture(t, s, "groupB")

	rows, err := fx.reports.ValuationByLocation(t.Context())
	if err != nil {
		t.Fatalf("ValuationByLocation: %v", err)
	}

	byKey := map[string]LocationValuation{}
	for _, r := range rows {
		byKey[r.LocationID] = r
	}

	garage, ok := byKey[fx.garageID]
	if !ok {
		t.Fatalf("no row for garage location %q; rows = %+v", fx.garageID, rows)
	}
	if garage.LocationName != "Garage" || garage.ItemCount != 1 || garage.TotalValueMinor != 1000 {
		t.Errorf("garage row = %+v, want {Garage, 1, 1000}", garage)
	}

	unassigned, ok := byKey[""]
	if !ok {
		t.Fatalf("no row for the unassigned bucket; rows = %+v", rows)
	}
	if unassigned.ItemCount != 4 {
		t.Errorf("unassigned bucket ItemCount = %d, want 4 (screw, vase, warranty item, lifetime item)", unassigned.ItemCount)
	}
	if unassigned.TotalValueMinor != 500 {
		t.Errorf("unassigned bucket TotalValueMinor = %d, want 500 (only the vase has a purchase price)", unassigned.TotalValueMinor)
	}

	for key := range byKey {
		if key != "" && key != fx.garageID {
			t.Errorf("ValuationByLocation returned a row for %q, which does not belong to group A's own fixture -- cross-tenant leak", key)
		}
	}
}

func TestReportRepositoryValuationByLabel(t *testing.T) {
	s := newTestStorage(t)
	fx := newReportFixture(t, s, "groupA")
	newReportFixture(t, s, "groupB")

	rows, err := fx.reports.ValuationByLabel(t.Context())
	if err != nil {
		t.Fatalf("ValuationByLabel: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ValuationByLabel returned %d rows, want 2 (fragile and valuable); rows = %+v", len(rows), rows)
	}

	byKey := map[string]LabelValuation{}
	for _, r := range rows {
		byKey[r.LabelID] = r
	}

	fragile, ok := byKey[fx.labelFragileID]
	if !ok {
		t.Fatalf("no row for the fragile label; rows = %+v", rows)
	}
	if fragile.ItemCount != 1 || fragile.TotalValueMinor != 500 {
		t.Errorf("fragile row = %+v, want {ItemCount 1, TotalValueMinor 500} (the vase, counted once under this label)", fragile)
	}

	valuable, ok := byKey[fx.labelValuableID]
	if !ok {
		t.Fatalf("no row for the valuable label; rows = %+v", rows)
	}
	if valuable.ItemCount != 1 || valuable.TotalValueMinor != 500 {
		t.Errorf("valuable row = %+v, want {ItemCount 1, TotalValueMinor 500} (the SAME vase, counted again under its second label)", valuable)
	}
}

func TestReportRepositoryWarrantyExpiring(t *testing.T) {
	s := newTestStorage(t)
	fx := newReportFixture(t, s, "groupA")
	newReportFixture(t, s, "groupB")

	t.Run("window includes the expiring item and excludes the lifetime one", func(t *testing.T) {
		rows, err := fx.reports.WarrantyExpiring(t.Context(), "2026-09-01", "2026-10-31")
		if err != nil {
			t.Fatalf("WarrantyExpiring: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("WarrantyExpiring returned %d rows, want 1; rows = %+v", len(rows), rows)
		}
		if rows[0].ItemID != fx.itemWarrantyID || rows[0].ExpiresOn != "2026-10-01" {
			t.Errorf("row = %+v, want the fridge expiring 2026-10-01", rows[0])
		}
	})

	t.Run("window before the expiry excludes it", func(t *testing.T) {
		rows, err := fx.reports.WarrantyExpiring(t.Context(), "2026-01-01", "2026-01-31")
		if err != nil {
			t.Fatalf("WarrantyExpiring: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("WarrantyExpiring outside the window returned %+v, want none", rows)
		}
	})
}

func TestReportRepositoryPurchasesInRange(t *testing.T) {
	s := newTestStorage(t)
	fx := newReportFixture(t, s, "groupA")
	newReportFixture(t, s, "groupB")

	rows, err := fx.reports.PurchasesInRange(t.Context(), "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("PurchasesInRange: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("PurchasesInRange(Jan) returned %d rows, want 1 (only the garage item); rows = %+v", len(rows), rows)
	}
	if rows[0].ItemID != fx.itemInGarageID || rows[0].PurchasePriceMinor != 1000 {
		t.Errorf("row = %+v, want the garage item at 1000", rows[0])
	}

	rows, err = fx.reports.PurchasesInRange(t.Context(), "2026-01-01", "2026-02-28")
	if err != nil {
		t.Fatalf("PurchasesInRange(Jan-Feb): %v", err)
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ItemID)
	}
	sort.Strings(ids)
	want := []string{fx.itemInGarageID, fx.itemTwoLabelsID}
	sort.Strings(want)
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Errorf("PurchasesInRange(Jan-Feb) item ids = %v, want %v", ids, want)
	}
}

func TestReportRepositoryMethodsRejectEmptyDateBounds(t *testing.T) {
	s := newTestStorage(t)
	fx := newReportFixture(t, s, "groupA")

	if _, err := fx.reports.WarrantyExpiring(t.Context(), "", "2026-12-31"); err == nil {
		t.Error("WarrantyExpiring(\"\", ...) succeeded; want an error")
	}
	if _, err := fx.reports.PurchasesInRange(t.Context(), "2026-01-01", ""); err == nil {
		t.Error("PurchasesInRange(..., \"\") succeeded; want an error")
	}
}
