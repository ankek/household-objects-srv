package importexport

import (
	"bytes"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

func fullyPopulatedRow() (Row, []storage.CustomFieldDef) {
	def := storage.CustomFieldDef{ID: "def-color", Name: "Color", FieldType: "text"}
	item := storage.Item{
		ID:          "item-1",
		Name:        "Lawnmower",
		Description: "A fine lawnmower.",
		Quantity:    3,
		LocationID:  sql.NullString{String: "loc-1", Valid: true},
		ShortCode:   "SHORT1",
	}
	row := Row{
		Item:         item,
		LocationPath: "Garage",
		Labels: []storage.Label{
			{Name: "Outdoor"},
			{Name: "Tools"},
		},
		Identifications: []storage.Identification{
			{Kind: "serial", Value: "XYZ123"},
		},
		Attachments: []storage.Attachment{
			{Category: "image", OriginalFilename: "mower.jpg", Sha256: "abc123def456"},
		},
		Warranty: &storage.Warranty{
			Holder:     "Bob",
			Provider:   "Acme",
			StartsOn:   sql.NullString{String: "2024-01-01", Valid: true},
			ExpiresOn:  sql.NullString{String: "2026-01-01", Valid: true},
			IsLifetime: 0,
			Notes:      "warranty notes",
		},
		Purchase: &storage.Purchase{
			Vendor:             "Store",
			PurchasedOn:        sql.NullString{String: "2024-01-01", Valid: true},
			PurchasePriceMinor: 19_999,
			OrderReference:     "ORD-1",
			Notes:              "purchase notes",
		},
		Sale: &storage.Sale{
			BuyerName:      "Alice",
			SoldOn:         sql.NullString{String: "2025-01-01", Valid: true},
			SalePriceMinor: 9_999,
			Notes:          "sale notes",
		},
		CustomFields: []storage.ItemCustomField{
			{Name: "Color", FieldType: "text", FieldDefID: sql.NullString{String: def.ID, Valid: true}, TextValue: sql.NullString{String: "Red", Valid: true}},
			{Name: "Weight", FieldType: "number", NumberValue: sql.NullFloat64{Float64: 12.5, Valid: true}},
		},
	}
	return row, []storage.CustomFieldDef{def}
}

func TestDiffStagedRowRoundTripIsUnchanged(t *testing.T) {
	row, defs := fullyPopulatedRow()

	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, []Row{row}); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}

	existingItemIDs := map[string]struct{}{row.Item.ID: {}}
	parsed, err := ParseCSV(&buf, existingItemIDs, defs)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("ParseCSV returned %d rows, want 1", len(parsed))
	}
	staged := parsed[0]
	if staged.Action != RowActionUpdate {
		t.Fatalf("Action = %q, want %q (errors: %+v)", staged.Action, RowActionUpdate, staged.Errors)
	}
	if staged.ID != row.Item.ID {
		t.Fatalf("ID = %q, want %q", staged.ID, row.Item.ID)
	}

	if diffs := DiffStagedRow(row, staged, defs); len(diffs) != 0 {
		t.Errorf("DiffStagedRow round trip = %v, want none (export->import must produce no changes, FR-050)", diffs)
	}
}

func stagedFromRoundTrip(t *testing.T, row Row, defs []storage.CustomFieldDef) StagedRow {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, []Row{row}); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	existingItemIDs := map[string]struct{}{row.Item.ID: {}}
	parsed, err := ParseCSV(&buf, existingItemIDs, defs)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Action != RowActionUpdate {
		t.Fatalf("ParseCSV round trip did not produce one matched row: %+v", parsed)
	}
	return parsed[0]
}

func TestDiffStagedRowDetectsSingleFieldChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*StagedRow)
		want   []string
	}{
		{"name", func(s *StagedRow) { s.Name = "Different Name" }, []string{"name"}},
		{"description", func(s *StagedRow) { s.Description = "Different description." }, []string{"description"}},
		{"quantity", func(s *StagedRow) { s.Quantity = 99 }, []string{"quantity"}},
		{"location_id", func(s *StagedRow) { s.LocationID = sql.NullString{String: "loc-2", Valid: true} }, []string{"location_id"}},
		{"label_added", func(s *StagedRow) { s.Labels = append(s.Labels, "NewLabel") }, []string{"labels"}},
		{"identification_added", func(s *StagedRow) {
			s.Identifications = append(s.Identifications, StagedIdentification{Kind: "model", Value: "M-1"})
		}, []string{"identifications"}},
		{"attachment_added_is_never_a_change_A128", func(s *StagedRow) {
			s.Attachments = append(s.Attachments, StagedAttachmentRef{Category: "manual", OriginalFilename: "b.pdf", Sha256: "deadbeef"})
		}, nil},
		{"warranty_holder", func(s *StagedRow) { s.Warranty.Holder = "Someone Else" }, []string{"warranty"}},
		{"purchase_vendor", func(s *StagedRow) { s.Purchase.Vendor = "Different Store" }, []string{"purchase"}},
		{"sale_buyer", func(s *StagedRow) { s.Sale.BuyerName = "Different Buyer" }, []string{"sale"}},
		{"custom_field_def_bound_value", func(s *StagedRow) {
			for i := range s.CustomFields {
				if s.CustomFields[i].FieldDefID.Valid {
					s.CustomFields[i].TextValue = sql.NullString{String: "Blue", Valid: true}
				}
			}
		}, []string{"custom_fields"}},
		{"custom_field_ad_hoc_value", func(s *StagedRow) {
			for i := range s.CustomFields {
				if !s.CustomFields[i].FieldDefID.Valid {
					s.CustomFields[i].NumberValue = sql.NullFloat64{Float64: 99, Valid: true}
				}
			}
		}, []string{"custom_fields"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, defs := fullyPopulatedRow()
			staged := stagedFromRoundTrip(t, row, defs)
			tt.mutate(&staged)

			diffs := DiffStagedRow(row, staged, defs)
			if len(diffs) != len(tt.want) {
				t.Fatalf("DiffStagedRow = %v, want exactly %v", diffs, tt.want)
			}
			for i := range diffs {
				if diffs[i] != tt.want[i] {
					t.Errorf("DiffStagedRow = %v, want exactly %v", diffs, tt.want)
					break
				}
			}
		})
	}
}

func TestDiffStagedRowTreatsNullAndEmptyStringAsEqual(t *testing.T) {
	current := Row{
		Item: storage.Item{ID: "item-1", Name: "Thing", LocationID: sql.NullString{}},
	}
	proposed := StagedRow{
		Name:       "Thing",
		LocationID: sql.NullString{},
	}
	if diffs := DiffStagedRow(current, proposed, nil); len(diffs) != 0 {
		t.Errorf("DiffStagedRow = %v, want none (NULL and \"\" must compare equal)", diffs)
	}
}

func TestDiffStagedRowTreatsNumberFormattingAsEqual(t *testing.T) {
	current := Row{
		Item: storage.Item{ID: "item-1", Name: "Thing"},
		CustomFields: []storage.ItemCustomField{
			{Name: "Weight", FieldType: "number", NumberValue: sql.NullFloat64{Float64: 10, Valid: true}},
		},
	}
	proposed := StagedRow{
		Name: "Thing",
		CustomFields: []storage.ItemCustomField{
			{Name: "Weight", FieldType: "number", NumberValue: sql.NullFloat64{Float64: 10.0, Valid: true}},
		},
	}
	if diffs := DiffStagedRow(current, proposed, nil); len(diffs) != 0 {
		t.Errorf("DiffStagedRow = %v, want none (10 and 10.0 are the identical float64)", diffs)
	}
}

func TestDiffStagedRowIsLifetimeIntVsBool(t *testing.T) {
	current := Row{
		Item:     storage.Item{ID: "item-1", Name: "Thing"},
		Warranty: &storage.Warranty{IsLifetime: 1},
	}
	sameProposed := StagedRow{Name: "Thing", Warranty: &StagedWarranty{IsLifetime: true}}
	if diffs := DiffStagedRow(current, sameProposed, nil); len(diffs) != 0 {
		t.Errorf("DiffStagedRow = %v, want none (IsLifetime 1 == true)", diffs)
	}

	flippedProposed := StagedRow{Name: "Thing", Warranty: &StagedWarranty{IsLifetime: false}}
	if diffs := DiffStagedRow(current, flippedProposed, nil); len(diffs) != 1 || diffs[0] != "warranty" {
		t.Errorf("DiffStagedRow = %v, want exactly [\"warranty\"] for a genuine IsLifetime flip", diffs)
	}
}

func TestDiffStagedRowLabelDuplicatesCarryNoMeaning(t *testing.T) {
	current := Row{
		Item:   storage.Item{ID: "item-1", Name: "Thing"},
		Labels: []storage.Label{{Name: "Fragile"}},
	}
	proposed := StagedRow{Name: "Thing", Labels: []string{"Fragile", "Fragile"}}
	if diffs := DiffStagedRow(current, proposed, nil); len(diffs) != 0 {
		t.Errorf("DiffStagedRow = %v, want none (duplicate label names carry no meaning)", diffs)
	}
}

func TestDiffStagedRowShortCodeAndLocationPathAreNeverCompared(t *testing.T) {
	current := Row{
		Item:         storage.Item{ID: "item-1", Name: "Thing", ShortCode: "AAAA1"},
		LocationPath: "Garage/Shelf",
	}
	proposed := StagedRow{Name: "Thing", ShortCode: "totally-different"}
	if diffs := DiffStagedRow(current, proposed, nil); len(diffs) != 0 {
		t.Errorf("DiffStagedRow = %v, want none (short_code/location_path are never compared)", diffs)
	}
}
