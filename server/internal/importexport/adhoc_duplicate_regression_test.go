package importexport

import (
	"bytes"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

func TestEncodeCSVDuplicateAdHocPairOnSameItemKeepsOnlyFirst(t *testing.T) {
	rows := []Row{{
		Item: storage.Item{ID: "item-1", Name: "Widget"},
		CustomFields: []storage.ItemCustomField{
			{FieldType: "text", Name: "Notes", TextValue: sql.NullString{String: "first value wins", Valid: true}},
			{FieldType: "text", Name: "Notes", TextValue: sql.NullString{String: "second value is silently dropped", Valid: true}},
		},
	}}
	var buf bytes.Buffer
	if err := EncodeCSV(&buf, nil, rows); err != nil {
		t.Fatalf("EncodeCSV: %v", err)
	}
	records := decodeCSV(t, buf.Bytes())
	header, row := records[0], records[1]

	dynamic := header[len(FixedColumns):]
	if len(dynamic) != 1 || dynamic[0] != "cfx:text:Notes" {
		t.Fatalf("dynamic headers = %v, want exactly [cfx:text:Notes] (one column per distinct (type, name) pair)", dynamic)
	}
	if got, want := cell(t, header, row, "cfx:text:Notes"), "first value wins"; got != want {
		t.Errorf("cfx:text:Notes = %q, want %q (first value in Row.CustomFields wins; this is documented, not a bug)", got, want)
	}
}
