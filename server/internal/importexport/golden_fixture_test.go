package importexport_test

import (
	"encoding/csv"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport/goldenfixture"
	"strings"
	"testing"
)

func decodeCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
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

func TestGoldenFixtureCSVMatchesEncodedRowsAndDefs(t *testing.T) {
	var buf strings.Builder
	if err := importexport.EncodeCSV(&buf, goldenfixture.Defs(), goldenfixture.Rows()); err != nil {
		t.Fatalf("EncodeCSV(goldenfixture.Defs(), goldenfixture.Rows()): %v", err)
	}
	got := buf.String()
	want := string(goldenfixture.CSV())
	if got != want {
		t.Fatalf("EncodeCSV(goldenfixture.Defs(), goldenfixture.Rows()) does not match testdata/golden_export.csv;\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGoldenFixtureRoundTripsEveryTrickyCell(t *testing.T) {
	records := decodeCSV(t, goldenfixture.CSV())
	if len(records) != 1+len(goldenfixture.Rows()) {
		t.Fatalf("records = %d, want %d (header + %d fixture rows)", len(records), 1+len(goldenfixture.Rows()), len(goldenfixture.Rows()))
	}
	header := records[0]

	byID := make(map[string][]string, len(records)-1)
	for _, row := range records[1:] {
		byID[cell(t, header, row, importexport.ColumnID)] = row
	}

	t.Run("comma_newline_and_double_quote_in_fixed_columns", func(t *testing.T) {
		row, ok := byID["golden-001"]
		if !ok {
			t.Fatal("golden-001 not found")
		}
		if got, want := cell(t, header, row, importexport.ColumnName), "Dining Chair, Oak"; got != want {
			t.Errorf("name = %q, want %q", got, want)
		}
		if got, want := cell(t, header, row, importexport.ColumnDescription), "Seats four.\nCondition: \"like new\", minor scuff on left leg."; got != want {
			t.Errorf("description = %q, want %q", got, want)
		}
		if got, want := cell(t, header, row, importexport.ColumnWarrantyNotes), "Registered \"online\"\nSee receipt for details."; got != want {
			t.Errorf("warranty_notes = %q, want %q", got, want)
		}
	})

	t.Run("pipe_and_colon_escaping_in_labels_and_identifications", func(t *testing.T) {
		row, ok := byID["golden-001"]
		if !ok {
			t.Fatal("golden-001 not found")
		}
		labelsCell := cell(t, header, row, importexport.ColumnLabels)
		var gotLabels []string
		for _, raw := range importexport.SplitEscaped(labelsCell, '|') {
			gotLabels = append(gotLabels, importexport.UnescapeSubValue(raw))
		}
		wantLabels := []string{"Furniture", "Style: Modern|Chic"}
		if !equalStrings(gotLabels, wantLabels) {
			t.Errorf("labels round-trip = %v, want %v (raw cell %q)", gotLabels, wantLabels, labelsCell)
		}

		idCell := cell(t, header, row, importexport.ColumnIdentifications)
		pairs := importexport.SplitEscaped(idCell, '|')
		if len(pairs) != 1 {
			t.Fatalf("identification pairs = %v, want exactly 1", pairs)
		}
		kv := importexport.SplitEscaped(pairs[0], ':')
		if len(kv) != 2 {
			t.Fatalf("kind/value raw parts = %v, want exactly 2", kv)
		}
		kind, value := importexport.UnescapeSubValue(kv[0]), importexport.UnescapeSubValue(kv[1])
		if kind != "serial" || value != "SN-100:Special|Edition" {
			t.Errorf("kind/value = [%q %q], want [serial SN-100:Special|Edition]", kind, value)
		}
	})

	t.Run("single_identification_survives_after_fu36s_invalid_entry_was_dropped", func(t *testing.T) {
		row, ok := byID["golden-003"]
		if !ok {
			t.Fatal("golden-003 not found")
		}
		idCell := cell(t, header, row, importexport.ColumnIdentifications)
		pairs := importexport.SplitEscaped(idCell, '|')
		if len(pairs) != 1 {
			t.Fatalf("identification pairs = %v, want exactly 1", pairs)
		}
		kv := importexport.SplitEscaped(pairs[0], ':')
		if len(kv) != 2 {
			t.Fatalf("kind/value raw parts = %v, want exactly 2", kv)
		}
		kind, value := importexport.UnescapeSubValue(kv[0]), importexport.UnescapeSubValue(kv[1])
		if kind != "serial" || value != "XYZ" {
			t.Errorf("kind/value = [%q %q], want [serial XYZ]", kind, value)
		}
	})

	t.Run("unicode_survives_the_outer_csv_layer_untouched", func(t *testing.T) {
		row, ok := byID["golden-002"]
		if !ok {
			t.Fatal("golden-002 not found")
		}
		if got, want := cell(t, header, row, importexport.ColumnName), "テーブル 🪑 café"; got != want {
			t.Errorf("name = %q, want %q", got, want)
		}
		if got, want := cell(t, header, row, "cfx:text:備考"), "日本語のテスト 🎌"; got != want {
			t.Errorf("cfx:text:備考 = %q, want %q", got, want)
		}
	})

	t.Run("live_def_header_with_an_escaped_colon_in_its_own_name", func(t *testing.T) {
		col, ok := importexport.ParseCustomFieldColumn(`cf:Model\:Number`)
		if !ok {
			t.Fatal(`ParseCustomFieldColumn("cf:Model\:Number") ok = false, want true`)
		}
		if col.Kind != importexport.CustomFieldColumnDef || col.Name != "Model:Number" {
			t.Errorf("parsed = %+v, want Kind=CustomFieldColumnDef Name=%q", col, "Model:Number")
		}
		row, ok := byID["golden-001"]
		if !ok {
			t.Fatal("golden-001 not found")
		}
		if got, want := cell(t, header, row, `cf:Model\:Number`), "MN-123"; got != want {
			t.Errorf(`cf:Model\:Number = %q, want %q`, got, want)
		}
	})

	t.Run("adhoc_header_needing_both_delimiter_characters_escaped_at_once", func(t *testing.T) {
		col, ok := importexport.ParseCustomFieldColumn(`cfx:text:A\|B\:C`)
		if !ok {
			t.Fatal(`ParseCustomFieldColumn("cfx:text:A\|B\:C") ok = false, want true`)
		}
		if col.Kind != importexport.CustomFieldColumnAdHoc || col.FieldType != "text" || col.Name != "A|B:C" {
			t.Errorf("parsed = %+v, want Kind=CustomFieldColumnAdHoc FieldType=text Name=%q", col, "A|B:C")
		}
		row, ok := byID["golden-004"]
		if !ok {
			t.Fatal("golden-004 not found")
		}
		if got, want := cell(t, header, row, `cfx:text:A\|B\:C`), `value|with:delims\and\backslash`; got != want {
			t.Errorf(`cfx:text:A\|B\:C = %q, want %q`, got, want)
		}
	})

	t.Run("detail_block_combinations_are_independently_optional", func(t *testing.T) {
		for _, tc := range []struct {
			id                                   string
			wantWarranty, wantPurchase, wantSale bool
		}{
			{id: "golden-001", wantWarranty: true, wantPurchase: false, wantSale: false},
			{id: "golden-002", wantWarranty: false, wantPurchase: false, wantSale: false},
			{id: "golden-003", wantWarranty: true, wantPurchase: true, wantSale: true},
			{id: "golden-004", wantWarranty: false, wantPurchase: true, wantSale: false},
			{id: "golden-005", wantWarranty: false, wantPurchase: false, wantSale: true},
		} {
			row, ok := byID[tc.id]
			if !ok {
				t.Fatalf("%s not found", tc.id)
			}
			if got := cell(t, header, row, importexport.ColumnWarrantyHolder) != ""; got != tc.wantWarranty {
				t.Errorf("%s: warranty present = %v, want %v", tc.id, got, tc.wantWarranty)
			}
			if got := cell(t, header, row, importexport.ColumnPurchaseVendor) != ""; got != tc.wantPurchase {
				t.Errorf("%s: purchase present = %v, want %v", tc.id, got, tc.wantPurchase)
			}
			if got := cell(t, header, row, importexport.ColumnSaleBuyerName) != ""; got != tc.wantSale {
				t.Errorf("%s: sale present = %v, want %v", tc.id, got, tc.wantSale)
			}
		}
	})
}
