package importexport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
)

type headerLayout struct {
	fixedIdx map[string]int
	dynCols  []dynamicColumn
}

type dynamicColumn struct {
	index  int
	parsed CustomFieldColumn
	header string
}

func ParseCSV(r io.Reader, existingItemIDs map[string]struct{}, defs []storage.CustomFieldDef) ([]StagedRow, error) {
	cr := csv.NewReader(r)

	layout, err := readHeaderLayout(cr)
	if err != nil {
		return nil, err
	}

	defsByID := make(map[string]storage.CustomFieldDef, len(defs))
	for _, d := range defs {
		defsByID[d.ID] = d
	}
	defNameCounts := customFieldDefNameCounts(defs)
	defsByUniqueName := make(map[string]storage.CustomFieldDef, len(defs))
	for _, d := range defs {
		if defNameCounts[d.Name] == 1 {
			defsByUniqueName[d.Name] = d
		}
	}

	var rows []StagedRow
	for {
		record, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				rows = append(rows, StagedRow{
					Line:   parseErr.StartLine,
					Action: RowActionError,
					Errors: []RowError{{Line: parseErr.StartLine, Message: err.Error()}},
				})
				continue
			}
			return nil, fmt.Errorf("importexport: read CSV row: %w", err)
		}

		line, _ := cr.FieldPos(0)
		rows = append(rows, parseRow(line, record, layout, existingItemIDs, defsByID, defsByUniqueName, defNameCounts))
	}

	return rows, nil
}

func readHeaderLayout(cr *csv.Reader) (headerLayout, error) {
	header, err := cr.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return headerLayout{}, errors.New("importexport: CSV file is empty (no header row)")
		}
		return headerLayout{}, fmt.Errorf("importexport: read CSV header: %w", err)
	}
	return parseHeaderLayout(header)
}

func parseHeaderLayout(header []string) (headerLayout, error) {
	fixedSet := make(map[string]bool, len(FixedColumns))
	for _, c := range FixedColumns {
		fixedSet[c] = true
	}

	layout := headerLayout{fixedIdx: make(map[string]int, len(FixedColumns))}
	seen := make(map[string]bool, len(header))
	for i, h := range header {
		if seen[h] {
			return headerLayout{}, fmt.Errorf("importexport: header column %q appears more than once", h)
		}
		seen[h] = true

		if fixedSet[h] {
			layout.fixedIdx[h] = i
			continue
		}
		col, ok := ParseCustomFieldColumn(h)
		if !ok {
			return headerLayout{}, fmt.Errorf("importexport: unrecognised header column %q (not one of the fixed columns, and not a valid cf:/cfx: column)", h)
		}
		layout.dynCols = append(layout.dynCols, dynamicColumn{index: i, parsed: col, header: h})
	}

	for _, c := range FixedColumns {
		if _, ok := layout.fixedIdx[c]; !ok {
			return headerLayout{}, fmt.Errorf("importexport: header is missing required column %q", c)
		}
	}

	return layout, nil
}
