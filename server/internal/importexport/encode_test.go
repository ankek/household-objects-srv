package importexport

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

func decodeCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("decode CSV: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("decode CSV: no records at all (expected at least a header row)")
	}
	return records
}

func cell(t *testing.T, header, row []string, col string) string {
	t.Helper()
	for i, h := range header {
		if h == col {
			if i >= len(row) {
				t.Fatalf("row shorter than header at column %q", col)
			}
			return row[i]
		}
	}
	t.Fatalf("column %q not found in header %v", col, header)
	return ""
}

func TestEncodeCSVHeaderColumnOrder(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, nil); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	if len(records) != 1 {
		t.Fatalf("expected header-only output for zero rows, got %d records", len(records))
	}
	got := records[0]
	if len(got) != len(FixedColumns) {
		t.Fatalf("header length = %d, want %d (FixedColumns)", len(got), len(FixedColumns))
	}
	for i, want := range FixedColumns {
		if got[i] != want {
			t.Errorf("header[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestEncodeCSVCustomFieldColumnsSortedByNameThenID(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-z", Name: "Zebra", FieldType: "text"},
		{ID: "def-a2", Name: "Amp", FieldType: "text"},
		{ID: "def-a1", Name: "Amp", FieldType: "text"},
	}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, nil); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	header := decodeCSV(t, buf.Bytes())[0]
	got := header[len(FixedColumns):]
	want := []string{"cf:Amp#def-a1", "cf:Amp#def-a2", "cf:Zebra"}
	if len(got) != len(want) {
		t.Fatalf("dynamic columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dynamic column[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEncodeCSVCustomFieldValuesMatchedByFieldDefIDNotName(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-a2", Name: "Amp", FieldType: "text"},
		{ID: "def-a1", Name: "Amp", FieldType: "text"},
	}
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		CustomFields: []storage.ItemCustomField{
			{FieldDefID: sql.NullString{String: "def-a1", Valid: true}, FieldType: "text", TextValue: sql.NullString{String: "first", Valid: true}},
			{FieldDefID: sql.NullString{String: "def-a2", Valid: true}, FieldType: "text", TextValue: sql.NullString{String: "second", Valid: true}},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]
	if got, want := cell(t, header, row, "cf:Amp#def-a1"), "first"; got != want {
		t.Errorf("cf:Amp#def-a1 = %q, want %q", got, want)
	}
	if got, want := cell(t, header, row, "cf:Amp#def-a2"), "second"; got != want {
		t.Errorf("cf:Amp#def-a2 = %q, want %q", got, want)
	}
}

func TestEncodeCSVDuplicateDefNameSuffixAppliesOnlyToCollidingNames(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-1", Name: "Color", FieldType: "text"},
		{ID: "def-2", Name: "Color", FieldType: "text"},
		{ID: "def-3", Name: "Weight", FieldType: "number"},
	}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, nil); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	header := decodeCSV(t, buf.Bytes())[0]
	dynamic := header[len(FixedColumns):]
	want := []string{"cf:Color#def-1", "cf:Color#def-2", "cf:Weight"}
	if len(dynamic) != len(want) {
		t.Fatalf("dynamic columns = %v, want %v", dynamic, want)
	}
	for i := range want {
		if dynamic[i] != want[i] {
			t.Errorf("dynamic column[%d] = %q, want %q", i, dynamic[i], want[i])
		}
	}
}

func TestEncodeCSVCoreItemFields(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{
			ID:          "item-1",
			Name:        "Cordless Drill",
			Description: "18V, yellow case",
			Quantity:    3,
			LocationID:  sql.NullString{String: "loc-1", Valid: true},
			ShortCode:   "AB23CD45",
		},
		LocationPath: "Garage/Shelf 1",
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	for _, tc := range []struct{ col, want string }{
		{ColumnID, "item-1"},
		{ColumnName, "Cordless Drill"},
		{ColumnDescription, "18V, yellow case"},
		{ColumnQuantity, "3"},
		{ColumnLocationPath, "Garage/Shelf 1"},
		{ColumnLocationID, "loc-1"},
		{ColumnShortCode, "AB23CD45"},
		{ColumnLabels, ""},
		{ColumnIdentifications, ""},
		{ColumnAttachments, ""},
		{ColumnWarrantyHolder, ""},
		{ColumnWarrantyIsLifetime, ""},
		{ColumnWarrantyNotes, ""},
		{ColumnPurchasePriceMinor, ""},
		{ColumnPurchaseNotes, ""},
		{ColumnSalePriceMinor, ""},
		{ColumnSaleNotes, ""},
	} {
		if got := cell(t, header, row, tc.col); got != tc.want {
			t.Errorf("column %q = %q, want %q", tc.col, got, tc.want)
		}
	}
}

func TestEncodeCSVNoLocationIsEmptyNotZeroValue(t *testing.T) {
	rows := []Row{{Item: storage.Item{ID: "item-1", Name: "Widget"}}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]
	if got := cell(t, header, row, ColumnLocationID); got != "" {
		t.Errorf("location_id = %q, want empty (LocationID.Valid == false)", got)
	}
}

func TestEncodeCSVDetailBlocks(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
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
			PurchasePriceMinor: 12999,
			OrderReference:     "ORD-42",
			Notes:              "Bought on sale",
		},
		Sale: &storage.Sale{
			BuyerName:      "Jane Doe",
			SoldOn:         sql.NullString{String: "2025-06-15", Valid: true},
			SalePriceMinor: 5000,
			Notes:          "Local pickup",
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	for _, tc := range []struct{ col, want string }{
		{ColumnWarrantyHolder, "Acme Co"},
		{ColumnWarrantyProvider, "Acme Warranty Services"},
		{ColumnWarrantyStartsOn, "2024-01-01"},
		{ColumnWarrantyExpiresOn, "2026-01-01"},
		{ColumnWarrantyIsLifetime, "false"},
		{ColumnWarrantyNotes, "Registered online"},
		{ColumnPurchaseVendor, "Hardware Store"},
		{ColumnPurchasePurchasedOn, "2024-01-01"},
		{ColumnPurchasePriceMinor, "12999"},
		{ColumnPurchaseOrderReference, "ORD-42"},
		{ColumnPurchaseNotes, "Bought on sale"},
		{ColumnSaleBuyerName, "Jane Doe"},
		{ColumnSaleSoldOn, "2025-06-15"},
		{ColumnSalePriceMinor, "5000"},
		{ColumnSaleNotes, "Local pickup"},
	} {
		if got := cell(t, header, row, tc.col); got != tc.want {
			t.Errorf("column %q = %q, want %q", tc.col, got, tc.want)
		}
	}
}

func TestEncodeCSVWarrantyIsLifetimeTrue(t *testing.T) {
	rows := []Row{{
		Item:     storage.Item{ID: "item-1", Name: "Widget"},
		Warranty: &storage.Warranty{IsLifetime: 1},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]
	if got := cell(t, header, row, ColumnWarrantyIsLifetime); got != "true" {
		t.Errorf("warranty_is_lifetime = %q, want %q", got, "true")
	}
}

func TestEncodeCSVLabelsIdentificationsAttachments(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		Labels: []storage.Label{
			{Name: "Kitchen"},
			{Name: "Fragile"},
		},
		Identifications: []storage.Identification{
			{Kind: "serial", Value: "SN123"},
			{Kind: "barcode", Value: "0012345"},
		},
		Attachments: []storage.Attachment{
			{Category: "image", OriginalFilename: "photo.jpg", Sha256: "abc123"},
			{Category: "manual", OriginalFilename: "manual.pdf", Sha256: "def456"},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	if got, want := cell(t, header, row, ColumnLabels), "Kitchen|Fragile"; got != want {
		t.Errorf("labels = %q, want %q", got, want)
	}
	if got, want := cell(t, header, row, ColumnIdentifications), "serial:SN123|barcode:0012345"; got != want {
		t.Errorf("identifications = %q, want %q", got, want)
	}
	if got, want := cell(t, header, row, ColumnAttachments), "image:photo.jpg:abc123|manual:manual.pdf:def456"; got != want {
		t.Errorf("attachments = %q, want %q", got, want)
	}
}

func TestEncodeCSVEscapesAndRoundTripsPipeAndColonInSubValues(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		Labels: []storage.Label{
			{Name: "A|B"},
			{Name: "C:D"},
			{Name: `E\F`},
		},
		Identifications: []storage.Identification{
			{Kind: "other", Value: "weird:value|with|pipes"},
		},
		Attachments: []storage.Attachment{
			{Category: "general", OriginalFilename: "a:b|c.txt", Sha256: "sha1"},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	labelsCell := cell(t, header, row, ColumnLabels)
	rawLabels := SplitEscaped(labelsCell, '|')
	gotLabels := make([]string, len(rawLabels))
	for i, raw := range rawLabels {
		gotLabels[i] = UnescapeSubValue(raw)
	}
	wantLabels := []string{"A|B", "C:D", `E\F`}
	if !equalStrings(gotLabels, wantLabels) {
		t.Errorf("round-tripped labels = %v, want %v (raw cell %q)", gotLabels, wantLabels, labelsCell)
	}

	idCell := cell(t, header, row, ColumnIdentifications)
	rawIDPairs := SplitEscaped(idCell, '|')
	if len(rawIDPairs) != 1 {
		t.Fatalf("identification pairs = %v, want exactly 1", rawIDPairs)
	}
	rawKindValue := SplitEscaped(rawIDPairs[0], ':')
	if len(rawKindValue) != 2 {
		t.Fatalf("kind/value raw parts = %v, want exactly 2", rawKindValue)
	}
	kind, value := UnescapeSubValue(rawKindValue[0]), UnescapeSubValue(rawKindValue[1])
	if kind != "other" || value != "weird:value|with|pipes" {
		t.Errorf("kind/value = [%q %q], want [other weird:value|with|pipes]", kind, value)
	}

	attCell := cell(t, header, row, ColumnAttachments)
	rawAttTriples := SplitEscaped(attCell, '|')
	if len(rawAttTriples) != 1 {
		t.Fatalf("attachment triples = %v, want exactly 1", rawAttTriples)
	}
	rawFields := SplitEscaped(rawAttTriples[0], ':')
	if len(rawFields) != 3 {
		t.Fatalf("attachment raw fields = %v, want exactly 3", rawFields)
	}
	category := UnescapeSubValue(rawFields[0])
	filename := UnescapeSubValue(rawFields[1])
	sha := UnescapeSubValue(rawFields[2])
	if category != "general" || filename != "a:b|c.txt" || sha != "sha1" {
		t.Errorf("attachment fields = [%q %q %q], want [general a:b|c.txt sha1]", category, filename, sha)
	}
}

func TestEncodeCSVCustomFieldValueTypes(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-text", Name: "Notes", FieldType: "text"},
		{ID: "def-number", Name: "Weight", FieldType: "number"},
		{ID: "def-bool", Name: "Registered", FieldType: "boolean"},
		{ID: "def-date", Name: "InstalledOn", FieldType: "date"},
	}
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		CustomFields: []storage.ItemCustomField{
			{FieldDefID: sql.NullString{String: "def-text", Valid: true}, FieldType: "text", TextValue: sql.NullString{String: "hello", Valid: true}},
			{FieldDefID: sql.NullString{String: "def-number", Valid: true}, FieldType: "number", NumberValue: sql.NullFloat64{Float64: 12.5, Valid: true}},
			{FieldDefID: sql.NullString{String: "def-bool", Valid: true}, FieldType: "boolean", BoolValue: sql.NullInt64{Int64: 1, Valid: true}},
			{FieldDefID: sql.NullString{String: "def-date", Valid: true}, FieldType: "date", DateValue: sql.NullString{String: "2024-03-04", Valid: true}},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	for _, tc := range []struct{ col, want string }{
		{"cf:Notes", "hello"},
		{"cf:Weight", "12.5"},
		{"cf:Registered", "true"},
		{"cf:InstalledOn", "2024-03-04"},
	} {
		if got := cell(t, header, row, tc.col); got != tc.want {
			t.Errorf("column %q = %q, want %q", tc.col, got, tc.want)
		}
	}
}

func TestEncodeCSVCustomFieldWithNoValueIsEmptyCell(t *testing.T) {
	defs := []storage.CustomFieldDef{{ID: "def-1", Name: "Notes", FieldType: "text"}}
	rows := []Row{{Item: storage.Item{ID: "item-1", Name: "Widget"}}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]
	if got := cell(t, header, row, "cf:Notes"); got != "" {
		t.Errorf("cf:Notes = %q, want empty", got)
	}
}

func TestEncodeCSVAdHocCustomFieldGetsCfxColumnOfEveryType(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		CustomFields: []storage.ItemCustomField{
			{FieldType: "text", Name: "Notes", TextValue: sql.NullString{String: "hand-written label", Valid: true}},
			{FieldType: "number", Name: "Weight (kg)", NumberValue: sql.NullFloat64{Float64: 2.75, Valid: true}},
			{FieldType: "boolean", Name: "Registered", BoolValue: sql.NullInt64{Int64: 1, Valid: true}},
			{FieldType: "date", Name: "InstalledOn", DateValue: sql.NullString{String: "2024-03-04", Valid: true}},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	dynamic := header[len(FixedColumns):]
	wantHeaders := []string{"cfx:boolean:Registered", "cfx:date:InstalledOn", "cfx:number:Weight (kg)", "cfx:text:Notes"}
	if len(dynamic) != len(wantHeaders) {
		t.Fatalf("dynamic headers = %v, want %v", dynamic, wantHeaders)
	}
	for i := range wantHeaders {
		if dynamic[i] != wantHeaders[i] {
			t.Errorf("dynamic header[%d] = %q, want %q", i, dynamic[i], wantHeaders[i])
		}
	}

	for _, tc := range []struct{ col, want string }{
		{"cfx:text:Notes", "hand-written label"},
		{"cfx:number:Weight (kg)", "2.75"},
		{"cfx:boolean:Registered", "true"},
		{"cfx:date:InstalledOn", "2024-03-04"},
	} {
		if got := cell(t, header, row, tc.col); got != tc.want {
			t.Errorf("column %q = %q, want %q", tc.col, got, tc.want)
		}
	}
}

func TestEncodeCSVCustomFieldValueForDeletedDefFallsIntoCfx(t *testing.T) {
	defs := []storage.CustomFieldDef{{ID: "def-1", Name: "Notes", FieldType: "text"}}
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		CustomFields: []storage.ItemCustomField{
			{FieldDefID: sql.NullString{String: "def-deleted", Valid: true}, Name: "Orphaned Field", FieldType: "text", TextValue: sql.NullString{String: "orphaned value", Valid: true}},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	if got := cell(t, header, row, "cf:Notes"); got != "" {
		t.Errorf("cf:Notes = %q, want empty (the orphaned value does not match any live def)", got)
	}
	if got, want := cell(t, header, row, "cfx:text:Orphaned Field"), "orphaned value"; got != want {
		t.Errorf("cfx:text:Orphaned Field = %q, want %q", got, want)
	}
}

func TestEncodeCSVDistinctAdHocPairCollapsesAcrossRows(t *testing.T) {
	rows := []Row{
		{
			Item:         storage.Item{ID: "item-1", Name: "First"},
			CustomFields: []storage.ItemCustomField{{FieldType: "text", Name: "Notes", TextValue: sql.NullString{String: "one", Valid: true}}},
		},
		{
			Item:         storage.Item{ID: "item-2", Name: "Second"},
			CustomFields: []storage.ItemCustomField{{FieldType: "text", Name: "Notes", TextValue: sql.NullString{String: "two", Valid: true}}},
		},
	}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header := records[0]
	dynamic := header[len(FixedColumns):]
	if len(dynamic) != 1 || dynamic[0] != "cfx:text:Notes" {
		t.Fatalf("dynamic headers = %v, want exactly [cfx:text:Notes]", dynamic)
	}
	if got, want := cell(t, header, records[1], "cfx:text:Notes"), "one"; got != want {
		t.Errorf("row 1 cfx:text:Notes = %q, want %q", got, want)
	}
	if got, want := cell(t, header, records[2], "cfx:text:Notes"), "two"; got != want {
		t.Errorf("row 2 cfx:text:Notes = %q, want %q", got, want)
	}
}

func TestEncodeCSVMultipleRowsPreserveInputOrder(t *testing.T) {
	rows := []Row{
		{Item: storage.Item{ID: "item-2", Name: "Second"}},
		{Item: storage.Item{ID: "item-1", Name: "First"}},
	}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header := records[0]
	if got := cell(t, header, records[1], ColumnID); got != "item-2" {
		t.Errorf("row 1 id = %q, want item-2 (input order must be preserved)", got)
	}
	if got := cell(t, header, records[2], ColumnID); got != "item-1" {
		t.Errorf("row 2 id = %q, want item-1", got)
	}
}

func TestEscapeSubValueUnescapeSubValueRoundTrip(t *testing.T) {
	cases := []string{
		"",
		"plain",
		"a|b",
		"a:b",
		`a\b`,
		`a\|b:c`,
		"|||",
		":::",
		`\\\`,
		"unicode: héllo wörld 日本語",
	}
	for _, in := range cases {
		escaped := EscapeSubValue(in)
		got := UnescapeSubValue(escaped)
		if got != in {
			t.Errorf("EscapeSubValue/UnescapeSubValue round trip: in=%q escaped=%q got=%q", in, escaped, got)
		}
	}
}

func TestEscapeSubValueNoAllocationFastPathReturnsSameString(t *testing.T) {
	in := "no special characters here"
	if got := EscapeSubValue(in); got != in {
		t.Errorf("EscapeSubValue(%q) = %q, want unchanged", in, got)
	}
}

func TestSplitEscapedEmptyStringIsNil(t *testing.T) {
	if got := SplitEscaped("", '|'); got != nil {
		t.Errorf("SplitEscaped(\"\", '|') = %v, want nil", got)
	}
}

func TestEncodeCSVHeaderOrderIsDeterministicRegardlessOfInputOrder(t *testing.T) {
	defsA := []storage.CustomFieldDef{
		{ID: "def-1", Name: "Weight", FieldType: "number"},
		{ID: "def-2", Name: "Color", FieldType: "text"},
	}
	defsB := []storage.CustomFieldDef{
		{ID: "def-2", Name: "Color", FieldType: "text"},
		{ID: "def-1", Name: "Weight", FieldType: "number"},
	}
	rowsA := []Row{
		{Item: storage.Item{ID: "item-1"}, CustomFields: []storage.ItemCustomField{{FieldType: "boolean", Name: "Zeta", BoolValue: sql.NullInt64{Int64: 1, Valid: true}}}},
		{Item: storage.Item{ID: "item-2"}, CustomFields: []storage.ItemCustomField{{FieldType: "text", Name: "Alpha", TextValue: sql.NullString{String: "x", Valid: true}}}},
	}
	rowsB := []Row{
		rowsA[1], rowsA[0],
	}

	var bufA, bufB bytes.Buffer
	if err := EncodeCSV(&bufA, defsA, rowsA); err != nil {
		t.Fatalf("EncodeCSV(A): %v", err)
	}
	if err := EncodeCSV(&bufB, defsB, rowsB); err != nil {
		t.Fatalf("EncodeCSV(B): %v", err)
	}
	headerA := decodeCSV(t, bufA.Bytes())[0]
	headerB := decodeCSV(t, bufB.Bytes())[0]
	if !equalStrings(headerA, headerB) {
		t.Errorf("header order depends on input order:\n  A = %v\n  B = %v", headerA, headerB)
	}
}

func TestFixedColumnsMatchesEncodedColumnCount(t *testing.T) {
	rows := []Row{{Item: storage.Item{ID: "item-1", Name: "Widget"}}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	if len(records[1]) != len(FixedColumns) {
		t.Fatalf("row length = %d, want %d (len(FixedColumns))", len(records[1]), len(FixedColumns))
	}
}

func TestParseCustomFieldColumnRoundTripsPlainAndDisambiguatedDefHeaders(t *testing.T) {
	defs := []storage.CustomFieldDef{
		{ID: "def-1", Name: "Color", FieldType: "text"},
		{ID: "def-2", Name: "Color", FieldType: "text"},
		{ID: "def-3", Name: "Weight", FieldType: "number"},
	}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, defs, nil); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	header := decodeCSV(t, buf.Bytes())[0]
	dynamic := header[len(FixedColumns):]

	for _, tc := range []struct {
		header       string
		wantName     string
		wantDefID    string
		wantHasDefID bool
	}{
		{"cf:Color#def-1", "Color", "def-1", true},
		{"cf:Color#def-2", "Color", "def-2", true},
		{"cf:Weight", "Weight", "", false},
	} {
		found := false
		for _, h := range dynamic {
			if h == tc.header {
				found = true
			}
		}
		if !found {
			t.Fatalf("header %q not found in encoded dynamic columns %v", tc.header, dynamic)
		}
		col, ok := ParseCustomFieldColumn(tc.header)
		if !ok {
			t.Fatalf("ParseCustomFieldColumn(%q) ok = false, want true", tc.header)
		}
		if col.Kind != CustomFieldColumnDef {
			t.Errorf("ParseCustomFieldColumn(%q).Kind = %v, want CustomFieldColumnDef", tc.header, col.Kind)
		}
		if col.Name != tc.wantName {
			t.Errorf("ParseCustomFieldColumn(%q).Name = %q, want %q", tc.header, col.Name, tc.wantName)
		}
		if col.DefID != tc.wantDefID {
			t.Errorf("ParseCustomFieldColumn(%q).DefID = %q, want %q", tc.header, col.DefID, tc.wantDefID)
		}
	}
}

func TestParseCustomFieldColumnRoundTripsAdHocNameContainingColonOrPipe(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{ID: "item-1"},
		CustomFields: []storage.ItemCustomField{
			{FieldType: "text", Name: "Model:Number", TextValue: sql.NullString{String: "x", Valid: true}},
			{FieldType: "text", Name: "A|B", TextValue: sql.NullString{String: "y", Valid: true}},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	header := decodeCSV(t, buf.Bytes())[0]
	dynamic := header[len(FixedColumns):]
	if len(dynamic) != 2 {
		t.Fatalf("dynamic headers = %v, want exactly 2", dynamic)
	}

	gotNames := make(map[string]bool)
	for _, h := range dynamic {
		col, ok := ParseCustomFieldColumn(h)
		if !ok {
			t.Fatalf("ParseCustomFieldColumn(%q) ok = false, want true", h)
		}
		if col.Kind != CustomFieldColumnAdHoc {
			t.Errorf("ParseCustomFieldColumn(%q).Kind = %v, want CustomFieldColumnAdHoc", h, col.Kind)
		}
		if col.FieldType != "text" {
			t.Errorf("ParseCustomFieldColumn(%q).FieldType = %q, want %q", h, col.FieldType, "text")
		}
		gotNames[col.Name] = true
	}
	for _, want := range []string{"Model:Number", "A|B"} {
		if !gotNames[want] {
			t.Errorf("parsed ad hoc names = %v, missing %q", gotNames, want)
		}
	}
}

func TestParseCustomFieldColumnRejectsNonDynamicHeader(t *testing.T) {
	for _, h := range []string{ColumnID, ColumnName, "not_a_cf_column", ""} {
		if _, ok := ParseCustomFieldColumn(h); ok {
			t.Errorf("ParseCustomFieldColumn(%q) ok = true, want false", h)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
