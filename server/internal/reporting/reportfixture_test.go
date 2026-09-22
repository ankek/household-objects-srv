package reporting

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

var fixtureToday = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

const fixtureWithinDays = 5

const (
	fixtureRangeFrom = "2026-03-01"
	fixtureRangeTo   = "2026-03-10"
)

func newFixtureStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func newFixtureGroupScope(t *testing.T, s *storage.Storage, groupID string) storage.Scope {
	t.Helper()
	err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	})
	if err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	return scope
}

type d24Fixture struct {
	scope     storage.Scope
	reports   storage.ReportRepository
	locations storage.LocationRepository

	houseID, garageID, shelfID string

	noLocationID       string
	houseItemID        string
	garageItemID       string
	shelfItemID        string
	twoLabelsID        string
	softDeletedID      string
	deletedPurchaseID  string
	labelFragileID     string
	labelValuableID    string
	warrantyTodayID    string
	warrantyPlusNID    string
	warrantyPlusNPlus1 string
	warrantyExpiredID  string
	warrantyLifetimeID string
	purchaseOnFromID   string
	purchaseOnToID     string
	purchaseBeforeFrom string
	purchaseAfterToID  string
	purchaseZeroID     string
	purchaseNullDateID string
}

func seedD24Fixture(t *testing.T, s *storage.Storage, groupID string) d24Fixture {
	t.Helper()
	scope := newFixtureGroupScope(t, s, groupID)
	id := func(suffix string) string { return groupID + "-" + suffix }

	house, err := scope.Locations().Create(t.Context(), storage.CreateLocationParams{ID: id("loc-house"), Name: "House", Now: 1})
	if err != nil {
		t.Fatalf("create House: %v", err)
	}
	garage, err := scope.Locations().Create(t.Context(), storage.CreateLocationParams{ID: id("loc-garage"), Name: "Garage", ParentID: house.ID, Now: 1})
	if err != nil {
		t.Fatalf("create Garage: %v", err)
	}
	shelf, err := scope.Locations().Create(t.Context(), storage.CreateLocationParams{ID: id("loc-shelf"), Name: "Shelf", ParentID: garage.ID, Now: 1})
	if err != nil {
		t.Fatalf("create Shelf: %v", err)
	}

	fragile, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{ID: id("label-fragile"), Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("create Fragile label: %v", err)
	}
	valuable, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{ID: id("label-valuable"), Name: "Valuable", Color: "#00ff00", Now: 1})
	if err != nil {
		t.Fatalf("create Valuable label: %v", err)
	}

	createItem := func(suffix, name, locationID string) storage.Item {
		t.Helper()
		item, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
			ID: id(suffix), Name: name, LocationID: locationID, ShortCode: id(suffix), Now: 1,
		})
		if err != nil {
			t.Fatalf("create item %q: %v", suffix, err)
		}
		return item
	}
	createPurchase := func(suffix, itemID, purchasedOn string, priceMinor int64) {
		t.Helper()
		if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{
			ID: id(suffix), ItemID: itemID, PurchasedOn: purchasedOn, PurchasePriceMinor: priceMinor, Now: 1,
		}); err != nil {
			t.Fatalf("create purchase %q: %v", suffix, err)
		}
	}

	noLocation := createItem("item-no-location", "Loose Screw", "")

	houseItem := createItem("item-house", "Umbrella Stand", house.ID)
	createPurchase("purchase-house", houseItem.ID, "2026-01-15", 1500)

	garageItem := createItem("item-garage", "Rake", garage.ID)

	shelfItem := createItem("item-shelf", "Paint Can", shelf.ID)
	createPurchase("purchase-shelf", shelfItem.ID, "2026-01-20", 2000)

	twoLabels := createItem("item-two-labels", "Vase", "")
	createPurchase("purchase-two-labels", twoLabels.ID, "2026-01-10", 300)
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: id("edge-fragile"), ItemID: twoLabels.ID, LabelID: fragile.ID, Now: 1}); err != nil {
		t.Fatalf("attach fragile to two-labels item: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: id("edge-valuable"), ItemID: twoLabels.ID, LabelID: valuable.ID, Now: 1}); err != nil {
		t.Fatalf("attach valuable to two-labels item: %v", err)
	}

	softDeleted := createItem("item-soft-deleted", "Broken Lamp", garage.ID)
	createPurchase("purchase-soft-deleted", softDeleted.ID, "2026-01-01", 9999)
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: id("edge-soft-deleted"), ItemID: softDeleted.ID, LabelID: fragile.ID, Now: 1}); err != nil {
		t.Fatalf("attach fragile to soft-deleted item: %v", err)
	}
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: id("warranty-soft-deleted"), ItemID: softDeleted.ID, ExpiresOn: "2026-06-15", Now: 1,
	}); err != nil {
		t.Fatalf("create warranty for soft-deleted item: %v", err)
	}
	if err := scope.Items().Delete(t.Context(), softDeleted.ID, 2); err != nil {
		t.Fatalf("soft-delete item: %v", err)
	}

	deletedPurchase := createItem("item-deleted-purchase", "Old Toolbox", house.ID)
	createPurchase("purchase-deleted", deletedPurchase.ID, "2026-01-05", 500)
	if err := scope.Purchase().Delete(t.Context(), deletedPurchase.ID, 2); err != nil {
		t.Fatalf("delete purchase: %v", err)
	}

	warrantyToday := createItem("item-warranty-today", "Fridge", "")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: id("warranty-today"), ItemID: warrantyToday.ID, ExpiresOn: "2026-06-15", Now: 1,
	}); err != nil {
		t.Fatalf("create warranty-today: %v", err)
	}
	warrantyPlusN := createItem("item-warranty-plus-n", "Dishwasher", "")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: id("warranty-plus-n"), ItemID: warrantyPlusN.ID, ExpiresOn: "2026-06-20", Now: 1,
	}); err != nil {
		t.Fatalf("create warranty-plus-n: %v", err)
	}
	warrantyPlusNPlus1 := createItem("item-warranty-plus-n-plus-1", "Microwave", "")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: id("warranty-plus-n-plus-1"), ItemID: warrantyPlusNPlus1.ID, ExpiresOn: "2026-06-21", Now: 1,
	}); err != nil {
		t.Fatalf("create warranty-plus-n-plus-1: %v", err)
	}
	warrantyExpired := createItem("item-warranty-expired", "Blender", "")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: id("warranty-expired"), ItemID: warrantyExpired.ID, ExpiresOn: "2026-06-14", Now: 1,
	}); err != nil {
		t.Fatalf("create warranty-expired: %v", err)
	}
	warrantyLifetime := createItem("item-warranty-lifetime", "Cast Iron Pan", "")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: id("warranty-lifetime"), ItemID: warrantyLifetime.ID, IsLifetime: true, Now: 1,
	}); err != nil {
		t.Fatalf("create warranty-lifetime: %v", err)
	}

	purchaseOnFrom := createItem("item-purchase-on-from", "Kettle", "")
	createPurchase("purchase-on-from", purchaseOnFrom.ID, fixtureRangeFrom, 111)
	purchaseOnTo := createItem("item-purchase-on-to", "Toaster", "")
	createPurchase("purchase-on-to", purchaseOnTo.ID, fixtureRangeTo, 222)
	purchaseBeforeFrom := createItem("item-purchase-before-from", "Iron", "")
	createPurchase("purchase-before-from", purchaseBeforeFrom.ID, "2026-02-28", 333)
	purchaseAfterTo := createItem("item-purchase-after-to", "Mixer", "")
	createPurchase("purchase-after-to", purchaseAfterTo.ID, "2026-03-11", 444)
	purchaseZero := createItem("item-purchase-zero", "Coaster Set", "")
	createPurchase("purchase-zero", purchaseZero.ID, "2026-03-05", 0)
	purchaseNullDate := createItem("item-purchase-null-date", "Unreceipted Gift", "")
	createPurchase("purchase-null-date", purchaseNullDate.ID, "", 12345)

	reports, err := s.ForGroupReports(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupReports(%q): %v", groupID, err)
	}

	return d24Fixture{
		scope:              scope,
		reports:            reports,
		locations:          scope.Locations(),
		houseID:            house.ID,
		garageID:           garage.ID,
		shelfID:            shelf.ID,
		noLocationID:       noLocation.ID,
		houseItemID:        houseItem.ID,
		garageItemID:       garageItem.ID,
		shelfItemID:        shelfItem.ID,
		twoLabelsID:        twoLabels.ID,
		softDeletedID:      softDeleted.ID,
		deletedPurchaseID:  deletedPurchase.ID,
		labelFragileID:     fragile.ID,
		labelValuableID:    valuable.ID,
		warrantyTodayID:    warrantyToday.ID,
		warrantyPlusNID:    warrantyPlusN.ID,
		warrantyPlusNPlus1: warrantyPlusNPlus1.ID,
		warrantyExpiredID:  warrantyExpired.ID,
		warrantyLifetimeID: warrantyLifetime.ID,
		purchaseOnFromID:   purchaseOnFrom.ID,
		purchaseOnToID:     purchaseOnTo.ID,
		purchaseBeforeFrom: purchaseBeforeFrom.ID,
		purchaseAfterToID:  purchaseAfterTo.ID,
		purchaseZeroID:     purchaseZero.ID,
		purchaseNullDateID: purchaseNullDate.ID,
	}
}

func seedD24PoisonGroup(t *testing.T, s *storage.Storage, groupID string) (locationID, labelID, itemID string) {
	t.Helper()
	scope := newFixtureGroupScope(t, s, groupID)

	house, err := scope.Locations().Create(t.Context(), storage.CreateLocationParams{ID: groupID + "-loc-house", Name: "House", Now: 1})
	if err != nil {
		t.Fatalf("poison group: create House: %v", err)
	}
	fragile, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{ID: groupID + "-label-fragile", Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("poison group: create Fragile label: %v", err)
	}
	item, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: groupID + "-item", Name: "Loose Screw", LocationID: house.ID, ShortCode: groupID + "-SCREW1", Now: 1,
	})
	if err != nil {
		t.Fatalf("poison group: create item: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{
		ID: groupID + "-purchase", ItemID: item.ID, PurchasedOn: "2026-03-05", PurchasePriceMinor: 999999, Now: 1,
	}); err != nil {
		t.Fatalf("poison group: create purchase: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: groupID + "-edge", ItemID: item.ID, LabelID: fragile.ID, Now: 1}); err != nil {
		t.Fatalf("poison group: attach label: %v", err)
	}
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: groupID + "-warranty", ItemID: item.ID, ExpiresOn: "2026-06-15", Now: 1,
	}); err != nil {
		t.Fatalf("poison group: create warranty: %v", err)
	}

	return house.ID, fragile.ID, item.ID
}
