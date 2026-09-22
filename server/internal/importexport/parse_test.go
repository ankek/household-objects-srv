package importexport

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"strings"
	"testing"
)

func buildCSV(t *testing.T, header []string, rows [][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return buf.Bytes()
}

func buildRow(overrides map[string]string) []string {
	row := make([]string, len(FixedColumns))
	for i, c := range FixedColumns {
		row[i] = overrides[c]
	}
	return row
}

func headerWithout(col string) []string {
	out := make([]string, 0, len(FixedColumns)-1)
	for _, c := range FixedColumns {
		if c != col {
			out = append(out, c)
		}
	}
	return out
}

func mustParse(t *testing.T, data []byte, existingItemIDs map[string]struct{}, defs []storage.CustomFieldDef) []StagedRow {
	t.Helper()
	rows, err := ParseCSV(bytes.NewReader(data), existingItemIDs, defs)
	if err != nil {
		t.Fatalf("ParseCSV: unexpected error: %v", err)
	}
	return rows
}

func TestParseCSVEmptyFileIsAnError(t *testing.T) {
	_, err := ParseCSV(strings.NewReader(""), nil, nil)
	if err == nil {
		t.Fatal("ParseCSV(empty) err = nil, want non-nil")
	}
}

func TestParseCSVMalformedHeaderRejected(t *testing.T) {
	tests := []struct {
		name   string
		header []string
	}{
		{"missing required column", headerWithout(ColumnQuantity)},
		{"duplicate column", append(append([]string{}, FixedColumns...), ColumnID)},
		{"unrecognised column", append(append([]string{}, FixedColumns...), "not_a_real_column")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := buildCSV(t, tc.header, nil)
			rows, err := ParseCSV(bytes.NewReader(data), nil, nil)
			if err == nil {
				t.Fatalf("ParseCSV: err = nil, want non-nil (rows = %v)", rows)
			}
			if rows != nil {
				t.Errorf("ParseCSV: rows = %v, want nil alongside a header error", rows)
			}
		})
	}
}

func TestParseCSVNameRequired(t *testing.T) {
	data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{ColumnID: "item-1"})})
	rows := mustParse(t, data, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
	if len(rows[0].Errors) != 1 || rows[0].Errors[0].Column != ColumnName {
		t.Errorf("Errors = %+v, want exactly one error on column %q", rows[0].Errors, ColumnName)
	}
}

func TestParseCSVQuantityDefaultsToZeroWhenBlank(t *testing.T) {
	data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{ColumnName: "Widget"})})
	rows := mustParse(t, data, nil, nil)
	if rows[0].Action == RowActionError {
		t.Fatalf("unexpected row error: %+v", rows[0].Errors)
	}
	if rows[0].Quantity != 0 {
		t.Errorf("Quantity = %d, want 0", rows[0].Quantity)
	}
}

func TestParseCSVQuantityMustBeInteger(t *testing.T) {
	data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{ColumnName: "Widget", ColumnQuantity: "not-a-number"})})
	rows := mustParse(t, data, nil, nil)
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
}

func TestParseCSVPriceMinorRejectsNegative(t *testing.T) {
	for _, tc := range []struct {
		name      string
		column    string
		vendorCol string
		vendorVal string
	}{
		{"purchase", ColumnPurchasePriceMinor, ColumnPurchaseVendor, "Store"},
		{"sale", ColumnSalePriceMinor, ColumnSaleBuyerName, "Jane"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{
				ColumnName:   "Widget",
				tc.vendorCol: tc.vendorVal,
				tc.column:    "-100",
			})})
			rows := mustParse(t, data, nil, nil)
			if rows[0].Action != RowActionError {
				t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
			}
		})
	}
}

func TestParseCSVWarrantyDateMustBeZeroPaddedISO(t *testing.T) {
	data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{
		ColumnName:             "Widget",
		ColumnWarrantyHolder:   "Acme",
		ColumnWarrantyStartsOn: "2026-8-5",
	})})
	rows := mustParse(t, data, nil, nil)
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
}

func TestParseCSVBooleanCaseInsensitive(t *testing.T) {
	for _, val := range []string{"true", "TRUE", "True", "false", "FALSE"} {
		t.Run(val, func(t *testing.T) {
			data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{
				ColumnName:               "Widget",
				ColumnWarrantyHolder:     "Acme",
				ColumnWarrantyIsLifetime: val,
			})})
			rows := mustParse(t, data, nil, nil)
			if rows[0].Action == RowActionError {
				t.Fatalf("unexpected row error: %+v", rows[0].Errors)
			}
		})
	}
}

func TestParseCSVDetailBlockNilWhenEveryColumnEmpty(t *testing.T) {
	data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{ColumnName: "Widget"})})
	rows := mustParse(t, data, nil, nil)
	if rows[0].Warranty != nil || rows[0].Purchase != nil || rows[0].Sale != nil {
		t.Errorf("detail blocks = %+v/%+v/%+v, want all nil", rows[0].Warranty, rows[0].Purchase, rows[0].Sale)
	}
}

func TestParseCSVIdentificationInvalidKindAndEmptyValue(t *testing.T) {
	cell := EscapeSubValue("note") + ":" + EscapeSubValue("")
	data := buildCSV(t, FixedColumns, [][]string{buildRow(map[string]string{
		ColumnName:            "Widget",
		ColumnIdentifications: cell,
	})})
	rows := mustParse(t, data, nil, nil)
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
	if len(rows[0].Errors) != 2 {
		t.Fatalf("Errors = %+v, want exactly 2 (invalid kind AND empty value)", rows[0].Errors)
	}
}

func TestParseIdentificationsAdversarialEscaping(t *testing.T) {
	entry1 := EscapeSubValue("other") + ":" + EscapeSubValue("A|B")
	entry2 := EscapeSubValue("serial") + ":" + EscapeSubValue(`C:D\E`)
	cell := entry1 + "|" + entry2

	got, errs := parseIdentifications(2, cell)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := []StagedIdentification{
		{Kind: "other", Value: "A|B"},
		{Kind: "serial", Value: `C:D\E`},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseAttachmentsAdversarialEscaping(t *testing.T) {
	entry1 := EscapeSubValue("general") + ":" + EscapeSubValue(`w|eird:name\here.txt`) + ":" + EscapeSubValue("sha-1")
	entry2 := EscapeSubValue("image") + ":" + EscapeSubValue("plain.jpg") + ":" + EscapeSubValue("sha-2")
	cell := entry1 + "|" + entry2

	got, errs := parseAttachments(2, cell)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	want := []StagedAttachmentRef{
		{Category: "general", OriginalFilename: `w|eird:name\here.txt`, Sha256: "sha-1"},
		{Category: "image", OriginalFilename: "plain.jpg", Sha256: "sha-2"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseAttachmentsCategoryValidatedAgainstEnum(t *testing.T) {
	got, errs := parseAttachments(2, "photo:chair.jpg:abc123")
	if len(got) != 0 {
		t.Fatalf("got %+v, want no entries (the whole triple is rejected)", got)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %+v, want exactly 1 (invalid category)", errs)
	}
	if errs[0].Column != ColumnAttachments {
		t.Errorf("errs[0].Column = %q, want %q", errs[0].Column, ColumnAttachments)
	}

	got, errs = parseAttachments(2, "image:chair.jpg:abc123")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors for a valid category: %+v", errs)
	}
	if len(got) != 1 || got[0].Category != "image" {
		t.Fatalf("got %+v, want one entry with Category \"image\"", got)
	}
}

func TestParseCSVPerRowErrorDoesNotAbortWholeParse(t *testing.T) {
	rows := [][]string{
		buildRow(map[string]string{ColumnID: "id-1", ColumnName: "First"}),
		buildRow(map[string]string{ColumnID: "id-2"}),
		buildRow(map[string]string{ColumnID: "id-3", ColumnName: "Third"}),
	}
	data := buildCSV(t, FixedColumns, rows)
	got := mustParse(t, data, nil, nil)
	if len(got) != 3 {
		t.Fatalf("len(rows) = %d, want 3 (a bad row must not swallow the others)", len(got))
	}
	if got[0].Action == RowActionError {
		t.Errorf("row 0 (valid) unexpectedly errored: %+v", got[0].Errors)
	}
	if got[1].Action != RowActionError {
		t.Errorf("row 1 (missing name) Action = %v, want RowActionError", got[1].Action)
	}
	if got[2].Action == RowActionError {
		t.Errorf("row 2 (valid) unexpectedly errored: %+v", got[2].Errors)
	}
}

func TestParseCSVLineNumbersSurviveEmbeddedNewlines(t *testing.T) {
	rows := [][]string{
		buildRow(map[string]string{ColumnID: "id-1", ColumnName: "First", ColumnDescription: "line one\nline two"}),
		buildRow(map[string]string{ColumnID: "id-2", ColumnName: "Second"}),
	}
	data := buildCSV(t, FixedColumns, rows)
	got := mustParse(t, data, nil, nil)
	if len(got) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(got))
	}
	if got[0].Line != 2 {
		t.Errorf("row 0 Line = %d, want 2", got[0].Line)
	}
	if got[1].Line != 4 {
		t.Errorf("row 1 Line = %d, want 4 (must account for row 0's embedded newline)", got[1].Line)
	}
}

func TestParseCSVMalformedRowErrorCarriesCorrectLine(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(strings.Join(FixedColumns, ",") + "\n")
	buf.WriteString("short,row\n")
	goodRow := buildRow(map[string]string{ColumnID: "id-2", ColumnName: "Second"})
	w := csv.NewWriter(&buf)
	if err := w.Write(goodRow); err != nil {
		t.Fatalf("write good row: %v", err)
	}
	w.Flush()

	rows, err := ParseCSV(&buf, nil, nil)
	if err != nil {
		t.Fatalf("ParseCSV: unexpected fatal error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (malformed row + the good row after it)", len(rows))
	}
	if rows[0].Action != RowActionError || rows[0].Line != 2 {
		t.Errorf("row 0 = %+v, want Action=error Line=2", rows[0])
	}
	if rows[1].Action == RowActionError || rows[1].Line != 3 {
		t.Errorf("row 1 = %+v, want a clean row at line 3", rows[1])
	}
}

func TestParseCSVCreateVsUpdateClassification(t *testing.T) {
	rows := [][]string{
		buildRow(map[string]string{ColumnID: "", ColumnName: "Blank id"}),
		buildRow(map[string]string{ColumnID: "unmatched-id", ColumnName: "Unmatched id"}),
		buildRow(map[string]string{ColumnID: "existing-id", ColumnName: "Matched id"}),
	}
	data := buildCSV(t, FixedColumns, rows)
	existing := map[string]struct{}{"existing-id": {}}
	got := mustParse(t, data, existing, nil)

	if got[0].Action != RowActionCreate || got[0].ID != "" {
		t.Errorf("row 0 (blank id) = %+v, want Action=create ID=\"\"", got[0])
	}
	if got[1].Action != RowActionCreate || got[1].ID != "" || got[1].SourceID != "unmatched-id" {
		t.Errorf("row 1 (unmatched id) = %+v, want Action=create ID=\"\" SourceID=unmatched-id", got[1])
	}
	if got[2].Action != RowActionUpdate || got[2].ID != "existing-id" {
		t.Errorf("row 2 (matched id) = %+v, want Action=update ID=existing-id", got[2])
	}
}

func TestParseCSVRoundTripsEncodeCSVOutput(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-color", Name: "Color", FieldType: "text"},
		{ID: "def-weight", Name: "Weight", FieldType: "number"},
	}
	row := Row{
		Item: storage.Item{
			ID:          "item-rt-1",
			Name:        "Round Trip Widget",
			Description: "A widget for round-tripping",
			Quantity:    5,
			LocationID:  sql.NullString{String: "loc-1", Valid: true},
			ShortCode:   "RT000001",
		},
		LocationPath: "Garage/Shelf 1",
		Labels: []storage.Label{
			{Name: "Kitchen"},
			{Name: "Fragile"},
		},
		Identifications: []storage.Identification{
			{Kind: "serial", Value: "SN123"},
		},
		Attachments: []storage.Attachment{
			{Category: "image", OriginalFilename: "photo.jpg", Sha256: "abc123"},
		},
		Warranty: &storage.Warranty{
			Holder:     "Acme Co",
			Provider:   "Acme Warranty Services",
			StartsOn:   sql.NullString{String: "2024-01-01", Valid: true},
			ExpiresOn:  sql.NullString{String: "2026-01-01", Valid: true},
			IsLifetime: 0,
			Notes:      "Registered online",
		},
		Purchase: &storage.Purchase{
			Vendor:             "Hardware Store",
			PurchasedOn:        sql.NullString{String: "2024-01-01", Valid: true},
			PurchasePriceMinor: 1999,
			OrderReference:     "ORD-1",
			Notes:              "Bought on sale",
		},
		Sale: &storage.Sale{
			BuyerName:      "Jane Doe",
			SoldOn:         sql.NullString{String: "2025-06-15", Valid: true},
			SalePriceMinor: 999,
			Notes:          "Local pickup",
		},
		CustomFields: []storage.ItemCustomField{
			{FieldDefID: sql.NullString{String: "def-color", Valid: true}, Name: "Color", FieldType: "text", TextValue: sql.NullString{String: "Red", Valid: true}},
			{FieldDefID: sql.NullString{String: "def-weight", Valid: true}, Name: "Weight", FieldType: "number", NumberValue: sql.NullFloat64{Float64: 2.5, Valid: true}},
			{Name: "Ad Hoc Notes", FieldType: "text", TextValue: sql.NullString{String: "hand-written", Valid: true}},
		},
	}

	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, []Row{row}); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}

	t.Run("update (id matched)", func(t *testing.T) {
		got := mustParse(t, buf.Bytes(), map[string]struct{}{"item-rt-1": {}}, defs)
		if len(got) != 1 {
			t.Fatalf("len(rows) = %d, want 1", len(got))
		}
		r := got[0]
		if r.Action == RowActionError {
			t.Fatalf("unexpected row error: %+v", r.Errors)
		}
		if r.Action != RowActionUpdate || r.ID != "item-rt-1" || r.SourceID != "item-rt-1" {
			t.Errorf("Action/ID/SourceID = %v/%q/%q, want update/item-rt-1/item-rt-1", r.Action, r.ID, r.SourceID)
		}
		assertRoundTrippedRow(t, r)
	})

	t.Run("create (id unmatched)", func(t *testing.T) {
		got := mustParse(t, buf.Bytes(), nil, defs)
		r := got[0]
		if r.Action == RowActionError {
			t.Fatalf("unexpected row error: %+v", r.Errors)
		}
		if r.Action != RowActionCreate || r.ID != "" || r.SourceID != "item-rt-1" {
			t.Errorf("Action/ID/SourceID = %v/%q/%q, want create/\"\"/item-rt-1", r.Action, r.ID, r.SourceID)
		}
		assertRoundTrippedRow(t, r)
	})
}

func assertRoundTrippedRow(t *testing.T, r StagedRow) {
	t.Helper()
	if r.Name != "Round Trip Widget" {
		t.Errorf("Name = %q", r.Name)
	}
	if r.Description != "A widget for round-tripping" {
		t.Errorf("Description = %q", r.Description)
	}
	if r.Quantity != 5 {
		t.Errorf("Quantity = %d, want 5", r.Quantity)
	}
	if !r.LocationID.Valid || r.LocationID.String != "loc-1" {
		t.Errorf("LocationID = %+v, want valid \"loc-1\"", r.LocationID)
	}
	if r.ShortCode != "RT000001" {
		t.Errorf("ShortCode = %q, want RT000001", r.ShortCode)
	}
	wantLabels := []string{"Kitchen", "Fragile"}
	if !equalStrings(r.Labels, wantLabels) {
		t.Errorf("Labels = %v, want %v", r.Labels, wantLabels)
	}
	wantIDs := []StagedIdentification{{Kind: "serial", Value: "SN123"}}
	if len(r.Identifications) != 1 || r.Identifications[0] != wantIDs[0] {
		t.Errorf("Identifications = %+v, want %+v", r.Identifications, wantIDs)
	}
	wantAtt := StagedAttachmentRef{Category: "image", OriginalFilename: "photo.jpg", Sha256: "abc123"}
	if len(r.Attachments) != 1 || r.Attachments[0] != wantAtt {
		t.Errorf("Attachments = %+v, want [%+v]", r.Attachments, wantAtt)
	}
	if r.Warranty == nil {
		t.Fatal("Warranty = nil, want non-nil")
	}
	wantWarranty := StagedWarranty{Holder: "Acme Co", Provider: "Acme Warranty Services", StartsOn: "2024-01-01", ExpiresOn: "2026-01-01", IsLifetime: false, Notes: "Registered online"}
	if *r.Warranty != wantWarranty {
		t.Errorf("Warranty = %+v, want %+v", *r.Warranty, wantWarranty)
	}
	if r.Purchase == nil {
		t.Fatal("Purchase = nil, want non-nil")
	}
	wantPurchase := StagedPurchase{Vendor: "Hardware Store", PurchasedOn: "2024-01-01", PurchasePriceMinor: 1999, OrderReference: "ORD-1", Notes: "Bought on sale"}
	if *r.Purchase != wantPurchase {
		t.Errorf("Purchase = %+v, want %+v", *r.Purchase, wantPurchase)
	}
	if r.Sale == nil {
		t.Fatal("Sale = nil, want non-nil")
	}
	wantSale := StagedSale{BuyerName: "Jane Doe", SoldOn: "2025-06-15", SalePriceMinor: 999, Notes: "Local pickup"}
	if *r.Sale != wantSale {
		t.Errorf("Sale = %+v, want %+v", *r.Sale, wantSale)
	}

	if len(r.CustomFields) != 3 {
		t.Fatalf("CustomFields = %+v, want 3 entries", r.CustomFields)
	}
	color := r.CustomFields[0]
	if !color.FieldDefID.Valid || color.FieldDefID.String != "def-color" || color.Name != "Color" || color.FieldType != "text" || !color.TextValue.Valid || color.TextValue.String != "Red" {
		t.Errorf("CustomFields[0] (Color) = %+v", color)
	}
	weight := r.CustomFields[1]
	if !weight.FieldDefID.Valid || weight.FieldDefID.String != "def-weight" || weight.Name != "Weight" || weight.FieldType != "number" || !weight.NumberValue.Valid || weight.NumberValue.Float64 != 2.5 {
		t.Errorf("CustomFields[1] (Weight) = %+v", weight)
	}
	adhoc := r.CustomFields[2]
	if adhoc.FieldDefID.Valid || adhoc.Name != "Ad Hoc Notes" || adhoc.FieldType != "text" || !adhoc.TextValue.Valid || adhoc.TextValue.String != "hand-written" {
		t.Errorf("CustomFields[2] (ad hoc) = %+v", adhoc)
	}
}

func TestParseCSVCustomFieldColumnAmbiguousWithoutDefIDDisambiguator(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-1", Name: "Color", FieldType: "text"},
		{ID: "def-2", Name: "Color", FieldType: "text"},
	}
	header := append(append([]string{}, FixedColumns...), "cf:Color")
	row := append(buildRow(map[string]string{ColumnName: "Widget"}), "Red")
	data := buildCSV(t, header, [][]string{row})

	rows := mustParse(t, data, nil, defs)
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
}

func TestParseCSVCustomFieldColumnDeletedDefinitionRejected(t *testing.T) {
	header := append(append([]string{}, FixedColumns...), "cf:Color#def-gone")
	row := append(buildRow(map[string]string{ColumnName: "Widget"}), "Red")
	data := buildCSV(t, header, [][]string{row})

	rows := mustParse(t, data, nil, nil)
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
}

func TestParseCSVAdHocCustomFieldColumnInvalidType(t *testing.T) {
	header := append(append([]string{}, FixedColumns...), "cfx:currency:Price")
	row := append(buildRow(map[string]string{ColumnName: "Widget"}), "9.99")
	data := buildCSV(t, header, [][]string{row})

	rows := mustParse(t, data, nil, nil)
	if rows[0].Action != RowActionError {
		t.Fatalf("Action = %v, want RowActionError", rows[0].Action)
	}
}

func TestParseCSVHeaderColumnOrderIsNotEnforced(t *testing.T) {
	reordered := make([]string, len(FixedColumns))
	copy(reordered, FixedColumns)
	reordered[0], reordered[1] = reordered[1], reordered[0]

	row := make([]string, len(reordered))
	for i, c := range reordered {
		if c == ColumnName {
			row[i] = "Widget"
		}
	}
	data := buildCSV(t, reordered, [][]string{row})
	rows := mustParse(t, data, nil, nil)
	if rows[0].Action == RowActionError {
		t.Fatalf("unexpected row error: %+v", rows[0].Errors)
	}
	if rows[0].Name != "Widget" {
		t.Errorf("Name = %q, want Widget", rows[0].Name)
	}
}
