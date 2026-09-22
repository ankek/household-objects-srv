package importexport_test

import (
	"bytes"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport/goldenfixture"
	"testing"
)

func TestParseCSVAgainstGoldenFixture(t *testing.T) {
	defs := goldenfixture.Defs()
	data := goldenfixture.CSV()

	rows, err := importexport.ParseCSV(bytes.NewReader(data), nil, defs)
	if err != nil {
		t.Fatalf("ParseCSV: unexpected fatal error: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want 5 (matching goldenfixture.Rows)", len(rows))
	}

	for i, r := range rows {
		if r.Action == importexport.RowActionError {
			t.Errorf("row %d (line %d): unexpected error(s): %+v", i, r.Line, r.Errors)
		}
	}
}
