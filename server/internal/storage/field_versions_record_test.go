package storage

import (
	"errors"
	"testing"
)

func fvPushRepo(t *testing.T, s *Storage, groupID string) PushRepository {
	t.Helper()
	repo, err := s.ForGroupPush(MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupPush(%s): %v", groupID, err)
	}
	return repo
}

func fvFieldVersions(t *testing.T, push PushRepository, entityType, entityID string) map[string]int64 {
	t.Helper()
	rows, err := push.LookupFieldVersions(t.Context(), entityType, entityID)
	if err != nil {
		t.Fatalf("LookupFieldVersions(%s, %s): %v", entityType, entityID, err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.FieldName] = r.Version
	}
	return out
}

func TestItemUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "Lamp", Description: "desk lamp", Quantity: 2, ShortCode: "CODE0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Items().Update(t.Context(), UpdateItemParams{
		ItemID: created.ID, Name: created.Name, Description: created.Description,
		Quantity: 9, ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushItemFields["quantity"] {
		t.Fatal("sanity: \"quantity\" is not in push_commit.go's own pushItemFields -- the key this test asserts would not match what push reads")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for item %q = %+v, want exactly one entry (quantity)", created.ID, got)
	}
	if v, ok := got["quantity"]; !ok || v != updated.Version {
		t.Errorf("field_versions[quantity] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	for _, unchanged := range []string{"name", "description", "location_id"} {
		if _, ok := got[unchanged]; ok {
			t.Errorf("field_versions[%s] was recorded despite that field being resent unchanged", unchanged)
		}
	}
}

func TestItemUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "Lamp", Description: "desk lamp", Quantity: 2, ShortCode: "CODE0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Items().Update(t.Context(), UpdateItemParams{
		ItemID: created.ID, Name: created.Name, Description: created.Description,
		Quantity: created.Quantity, ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op update = %+v, want none", got)
	}
}

func TestItemUpdateFieldVersionsRollBackWithTheFailedWrite(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "Lamp", Quantity: 2, ShortCode: "CODE0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = scope.Items().Update(t.Context(), UpdateItemParams{
		ItemID: created.ID, Name: "New Name", Quantity: 99,
		ExpectedVersion: created.Version + 5, Now: 1001,
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale ExpectedVersion: err = %v, want ErrVersionMismatch", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a FAILED update = %+v, want none", got)
	}
}

func TestItemUpdateFieldVersionsAreGroupScoped(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	seedGroupRow(t, s, "groupB")
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}

	created, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-a", Name: "Lamp", Quantity: 2, ShortCode: "CODE0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: created.ID, Name: created.Name, Quantity: 7, ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	gotA := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item", created.ID)
	if len(gotA) != 1 || gotA["quantity"] == 0 {
		t.Fatalf("groupA field_versions = %+v, want exactly one tracked quantity row", gotA)
	}

	gotB := fvFieldVersions(t, fvPushRepo(t, s, "groupB"), "item", created.ID)
	if len(gotB) != 0 {
		t.Fatalf("groupB field_versions for groupA's own item = %+v, want none (P-3)", gotB)
	}
}

func TestWarrantyUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID: "war-1", ItemID: "itemA", Holder: "Alice", Provider: "Acme", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Warranty().Update(t.Context(), UpdateWarrantyParams{
		ItemID: "itemA", Holder: created.Holder, Provider: "New Provider",
		ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushWarrantyUpdateFields["provider"] {
		t.Fatal("sanity: \"provider\" is not in push_commit.go's own pushWarrantyUpdateFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "warranty_block", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for warranty_block %q = %+v, want exactly one entry (provider)", created.ID, got)
	}
	if v, ok := got["provider"]; !ok || v != updated.Version {
		t.Errorf("field_versions[provider] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["holder"]; ok {
		t.Errorf("field_versions[holder] recorded despite being resent unchanged")
	}
}

func TestWarrantyUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID: "war-1", ItemID: "itemA", Holder: "Alice", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Warranty().Update(t.Context(), UpdateWarrantyParams{
		ItemID: "itemA", Holder: created.Holder, Provider: created.Provider,
		StartsOn: "", ExpiresOn: "", IsLifetime: false, Notes: created.Notes,
		ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "warranty_block", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op warranty update = %+v, want none", got)
	}
}

func TestSaleUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Sale().Create(t.Context(), CreateSaleParams{
		ID: "sale-1", ItemID: "itemA", BuyerName: "Bob", SalePriceMinor: 1000, Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Sale().Update(t.Context(), UpdateSaleParams{
		ItemID: "itemA", BuyerName: created.BuyerName, SalePriceMinor: 2500, Notes: created.Notes,
		ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushSaleUpdateFields["sale_price_minor"] {
		t.Fatal("sanity: \"sale_price_minor\" is not in push_commit.go's own pushSaleUpdateFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "sold_to_block", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for sold_to_block %q = %+v, want exactly one entry (sale_price_minor)", created.ID, got)
	}
	if v, ok := got["sale_price_minor"]; !ok || v != updated.Version {
		t.Errorf("field_versions[sale_price_minor] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["buyer_name"]; ok {
		t.Errorf("field_versions[buyer_name] recorded despite being resent unchanged")
	}
}

func TestSaleUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Sale().Create(t.Context(), CreateSaleParams{
		ID: "sale-1", ItemID: "itemA", BuyerName: "Bob", SalePriceMinor: 1000, Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Sale().Update(t.Context(), UpdateSaleParams{
		ItemID: "itemA", BuyerName: created.BuyerName, SoldOn: "", SalePriceMinor: created.SalePriceMinor,
		Notes: created.Notes, ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "sold_to_block", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op sale update = %+v, want none", got)
	}
}

func TestPurchaseUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID: "pur-1", ItemID: "itemA", Vendor: "Acme", PurchasePriceMinor: 500, Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Purchase().Update(t.Context(), UpdatePurchaseParams{
		ItemID: "itemA", Vendor: "New Vendor", PurchasePriceMinor: created.PurchasePriceMinor,
		OrderReference: created.OrderReference, Notes: created.Notes,
		ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushPurchaseUpdateFields["vendor"] {
		t.Fatal("sanity: \"vendor\" is not in push_commit.go's own pushPurchaseUpdateFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "purchased_from_block", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for purchased_from_block %q = %+v, want exactly one entry (vendor)", created.ID, got)
	}
	if v, ok := got["vendor"]; !ok || v != updated.Version {
		t.Errorf("field_versions[vendor] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["purchase_price_minor"]; ok {
		t.Errorf("field_versions[purchase_price_minor] recorded despite being resent unchanged")
	}
}

func TestPurchaseUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Purchase().Create(t.Context(), CreatePurchaseParams{
		ID: "pur-1", ItemID: "itemA", Vendor: "Acme", PurchasePriceMinor: 500, Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Purchase().Update(t.Context(), UpdatePurchaseParams{
		ItemID: "itemA", Vendor: created.Vendor, PurchasedOn: "", PurchasePriceMinor: created.PurchasePriceMinor,
		OrderReference: created.OrderReference, Notes: created.Notes,
		ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "purchased_from_block", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op purchase update = %+v, want none", got)
	}
}

func TestIdentificationUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "ident-1", ItemID: "itemA", Kind: "serial", Value: "SN-1", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA", ID: created.ID, Kind: created.Kind, Value: "SN-2",
		ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushIdentificationUpdateFields["value"] {
		t.Fatal("sanity: \"value\" is not in push_commit.go's own pushIdentificationUpdateFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item_identification", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for item_identification %q = %+v, want exactly one entry (value)", created.ID, got)
	}
	if v, ok := got["value"]; !ok || v != updated.Version {
		t.Errorf("field_versions[value] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["kind"]; ok {
		t.Errorf("field_versions[kind] recorded despite being resent unchanged")
	}
}

func TestIdentificationUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "ident-1", ItemID: "itemA", Kind: "serial", Value: "SN-1", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA", ID: created.ID, Kind: created.Kind, Value: created.Value,
		ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item_identification", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op identification update = %+v, want none", got)
	}
}

func TestItemCustomFieldUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	textValue := "red"
	created, err := scope.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: &textValue, Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newValue := "blue"
	updated, err := scope.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA", ID: created.ID, FieldDefID: created.FieldDefID.String, Name: created.Name,
		FieldType: created.FieldType, TextValue: &newValue,
		ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushCustomFieldUpdateFields["text_value"] {
		t.Fatal("sanity: \"text_value\" is not in push_commit.go's own pushCustomFieldUpdateFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item_custom_field", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for item_custom_field %q = %+v, want exactly one entry (text_value)", created.ID, got)
	}
	if v, ok := got["text_value"]; !ok || v != updated.Version {
		t.Errorf("field_versions[text_value] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["name"]; ok {
		t.Errorf("field_versions[name] recorded despite being resent unchanged")
	}
}

func TestItemCustomFieldUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	textValue := "red"
	created, err := scope.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: &textValue, Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA", ID: created.ID, FieldDefID: created.FieldDefID.String, Name: created.Name,
		FieldType: created.FieldType, TextValue: &textValue,
		ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item_custom_field", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op custom field update = %+v, want none", got)
	}
}

func TestLocationUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Locations().Create(t.Context(), CreateLocationParams{
		ID: "loc-1", Name: "Garage", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: created.ID, Name: "Garage Shelf", ParentID: created.ParentID.String,
		ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushLocationFields["name"] {
		t.Fatal("sanity: \"name\" is not in push_commit.go's own pushLocationFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "location", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for location %q = %+v, want exactly one entry (name)", created.ID, got)
	}
	if v, ok := got["name"]; !ok || v != updated.Version {
		t.Errorf("field_versions[name] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["parent_id"]; ok {
		t.Errorf("field_versions[parent_id] recorded despite being resent unchanged")
	}
}

func TestLocationUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Locations().Create(t.Context(), CreateLocationParams{
		ID: "loc-1", Name: "Garage", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: created.ID, Name: created.Name, ParentID: created.ParentID.String,
		ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "location", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op location update = %+v, want none", got)
	}
}

func TestLabelUpdateRecordsFieldVersionsForChangedFieldOnly(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Labels().Create(t.Context(), CreateLabelParams{
		ID: "label-1", Name: "Fragile", Color: "#ff0000", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: created.Name, Color: "#00ff00", ExpectedVersion: created.Version, Now: 1001,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !pushLabelFields["color"] {
		t.Fatal("sanity: \"color\" is not in push_commit.go's own pushLabelFields")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "label", created.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for label %q = %+v, want exactly one entry (color)", created.ID, got)
	}
	if v, ok := got["color"]; !ok || v != updated.Version {
		t.Errorf("field_versions[color] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["name"]; ok {
		t.Errorf("field_versions[name] recorded despite being resent unchanged")
	}
}

func TestLabelUpdateNoOpRecordsNoFieldVersions(t *testing.T) {
	s := newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	created, err := scope.Labels().Create(t.Context(), CreateLabelParams{
		ID: "label-1", Name: "Fragile", Color: "#ff0000", Now: 1000,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: created.Name, Color: created.Color, ExpectedVersion: created.Version, Now: 1001,
	}); err != nil {
		t.Fatalf("Update (no-op): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "label", created.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions after a no-op label update = %+v, want none", got)
	}
}

func TestStockAdjustmentCreateRecordsItemQuantityFieldVersion(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	itemBefore, err := scope.Items().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Get(itemA): %v", err)
	}

	if _, err := scope.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
		ID: "adj-1", ItemID: "itemA", Delta: 3, Reason: "recount", Now: 1001,
	}); err != nil {
		t.Fatalf("StockAdjustments.Create: %v", err)
	}

	itemAfter, err := scope.Items().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Get(itemA) after adjustment: %v", err)
	}
	if itemAfter.Quantity != itemBefore.Quantity+3 {
		t.Fatalf("item quantity after adjustment = %d, want %d", itemAfter.Quantity, itemBefore.Quantity+3)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item", "itemA")
	if len(got) != 1 {
		t.Fatalf("field_versions for item %q after a stock adjustment = %+v, want exactly one entry (quantity)", "itemA", got)
	}
	if v, ok := got["quantity"]; !ok || v != itemAfter.Version {
		t.Errorf("field_versions[quantity] = %v (present=%v), want %d", v, ok, itemAfter.Version)
	}
}

func TestStockAdjustmentCreateWithZeroDeltaRecordsNoFieldVersion(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	if _, err := scope.StockAdjustments().Create(t.Context(), CreateStockAdjustmentParams{
		ID: "adj-1", ItemID: "itemA", Delta: 0, Reason: "recount, no change", Now: 1001,
	}); err != nil {
		t.Fatalf("StockAdjustments.Create: %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, "groupA"), "item", "itemA")
	if len(got) != 0 {
		t.Fatalf("field_versions after a zero-delta stock adjustment = %+v, want none (quantity did not actually change)", got)
	}
}

func TestImportCommitUpdateRecordsItemCoreFieldVersion(t *testing.T) {
	const group = "grp-import-fv"
	commit, scope, s := importCommitScope(t, group)

	itemToUpdate, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-upd", Name: "Old Name", Quantity: 2, ShortCode: "UPD0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("create item-upd: %v", err)
	}
	stageSession(t, s, group, "import-1", 2000)

	result, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-1",
		Now:             3000,
		Rows: []ImportCommitRow{
			{
				Line: 2, Action: ImportCommitRowUpdate, ItemID: itemToUpdate.ID,
				Changes:  []string{"name"},
				Proposed: ImportCommitProposedRow{Name: "New Name", Quantity: itemToUpdate.Quantity},
			},
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("result = %+v, want Updated:1", result)
	}

	updated, err := scope.Items().Get(t.Context(), itemToUpdate.ID)
	if err != nil {
		t.Fatalf("Get(updated): %v", err)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, group), "item", itemToUpdate.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for imported item %q = %+v, want exactly one entry (name)", itemToUpdate.ID, got)
	}
	if v, ok := got["name"]; !ok || v != updated.Version {
		t.Errorf("field_versions[name] = %v (present=%v), want %d", v, ok, updated.Version)
	}
	if _, ok := got["quantity"]; ok {
		t.Errorf("field_versions[quantity] recorded despite being resent unchanged")
	}
}

func TestImportCommitUpdateSkippingCoreFieldsRecordsNoItemFieldVersion(t *testing.T) {
	const group = "grp-import-fv-skip"
	commit, scope, s := importCommitScope(t, group)

	itemToUpdate, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-upd", Name: "Same Name", Quantity: 2, ShortCode: "UPD0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("create item-upd: %v", err)
	}
	stageSession(t, s, group, "import-1", 2000)

	result, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-1",
		Now:             3000,
		Rows: []ImportCommitRow{
			{
				Line: 2, Action: ImportCommitRowUpdate, ItemID: itemToUpdate.ID,
				Changes:  []string{"labels"},
				Proposed: ImportCommitProposedRow{Name: itemToUpdate.Name, Quantity: itemToUpdate.Quantity, Labels: []string{"Fragile"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("result = %+v, want Updated:1", result)
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, group), "item", itemToUpdate.ID)
	if len(got) != 0 {
		t.Fatalf("field_versions for entity_type item after a labels-only import update = %+v, want none", got)
	}
}

func TestImportCommitUpdateRecordsWarrantyBlockFieldVersion(t *testing.T) {
	const group = "grp-import-fv-warranty"
	commit, scope, s := importCommitScope(t, group)

	item, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "Lamp", ShortCode: "WAR0001", Now: 1000,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	warranty, err := scope.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID: "war-1", ItemID: item.ID, Holder: "Alice", Now: 1000,
	})
	if err != nil {
		t.Fatalf("create warranty: %v", err)
	}
	stageSession(t, s, group, "import-1", 2000)

	result, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-1",
		Now:             3000,
		Rows: []ImportCommitRow{
			{
				Line: 2, Action: ImportCommitRowUpdate, ItemID: item.ID,
				Changes: []string{"warranty"},
				Proposed: ImportCommitProposedRow{
					Name: item.Name, Quantity: item.Quantity,
					Warranty: &ImportCommitWarranty{Holder: "New Holder"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("result = %+v, want Updated:1", result)
	}

	updatedWarranty, err := scope.Warranty().Get(t.Context(), item.ID)
	if err != nil {
		t.Fatalf("Warranty.Get after import update: %v", err)
	}
	if updatedWarranty.Holder != "New Holder" {
		t.Fatalf("warranty.Holder = %q, want %q", updatedWarranty.Holder, "New Holder")
	}

	got := fvFieldVersions(t, fvPushRepo(t, s, group), "warranty_block", warranty.ID)
	if len(got) != 1 {
		t.Fatalf("field_versions for warranty_block %q = %+v, want exactly one entry (holder)", warranty.ID, got)
	}
	if v, ok := got["holder"]; !ok || v != updatedWarranty.Version {
		t.Errorf("field_versions[holder] = %v (present=%v), want %d", v, ok, updatedWarranty.Version)
	}

	gotItem := fvFieldVersions(t, fvPushRepo(t, s, group), "item", item.ID)
	if len(gotItem) != 0 {
		t.Fatalf("field_versions for entity_type item after a warranty-only import update = %+v, want none", gotItem)
	}
}
