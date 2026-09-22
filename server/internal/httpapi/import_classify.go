package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
)

func distinctNonEmptySourceIDs(rows []importexport.StagedRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var ids []string
	for _, row := range rows {
		if row.SourceID == "" {
			continue
		}
		if _, ok := seen[row.SourceID]; ok {
			continue
		}
		seen[row.SourceID] = struct{}{}
		ids = append(ids, row.SourceID)
	}
	return ids
}

type importClassifiedRow struct {
	Line    int
	Action  string
	Name    string
	ItemID  string
	Changes []string
	Errors  []importPreviewRowErrorResponse

	Proposed importexport.StagedRow
}

func classifyImportRows(ctx context.Context, scope storage.Scope, raw []byte) ([]importClassifiedRow, importPreviewSummaryResponse, error) {
	var summary importPreviewSummaryResponse

	defs, err := scope.CustomFieldDefs().List(ctx)
	if err != nil {
		return nil, summary, fmt.Errorf("httpapi: import classify: list custom field defs: %w", err)
	}

	prelim, err := importexport.ParseCSV(bytes.NewReader(raw), nil, defs)
	if err != nil {
		return nil, summary, fmt.Errorf("httpapi: import classify: parse staged file (pass 1): %w", err)
	}

	itemsByID := make(map[string]storage.Item)
	if ids := distinctNonEmptySourceIDs(prelim); len(ids) > 0 {
		items, err := scope.Items().GetByIDs(ctx, ids)
		if err != nil {
			return nil, summary, fmt.Errorf("httpapi: import classify: resolve existing item ids: %w", err)
		}
		for _, item := range items {
			itemsByID[item.ID] = item
		}
	}
	existingItemIDs := make(map[string]struct{}, len(itemsByID))
	for id := range itemsByID {
		existingItemIDs[id] = struct{}{}
	}

	rows, err := importexport.ParseCSV(bytes.NewReader(raw), existingItemIDs, defs)
	if err != nil {
		return nil, summary, fmt.Errorf("httpapi: import classify: parse staged file (pass 2): %w", err)
	}

	locationLive := make(map[string]bool)

	out := make([]importClassifiedRow, 0, len(rows))
	for _, row := range rows {
		cr := importClassifiedRow{Line: row.Line, Name: row.Name, Proposed: row}

		if row.Action == importexport.RowActionError {
			cr.Action = "error"
			summary.Error++
			for _, e := range row.Errors {
				cr.Errors = append(cr.Errors, importPreviewRowErrorResponse{Line: e.Line, Column: e.Column, Message: e.Message})
			}
			out = append(out, cr)
			continue
		}

		if row.LocationID.Valid && row.LocationID.String != "" {
			live, ok := locationLive[row.LocationID.String]
			if !ok {
				_, getErr := scope.Locations().Get(ctx, row.LocationID.String)
				switch {
				case getErr == nil:
					live = true
				case errors.Is(getErr, storage.ErrNotFound):
					live = false
				default:
					return nil, summary, fmt.Errorf("httpapi: import classify: check location %q: %w", row.LocationID.String, getErr)
				}
				locationLive[row.LocationID.String] = live
			}
			if !live {
				cr.Action = "error"
				cr.Errors = append(cr.Errors, importPreviewRowErrorResponse{
					Line:    row.Line,
					Column:  importexport.ColumnLocationID,
					Message: fmt.Sprintf("location %q does not exist in this group", row.LocationID.String),
				})
				summary.Error++
				out = append(out, cr)
				continue
			}
		}

		switch row.Action {
		case importexport.RowActionCreate:
			cr.Action = "create"
			summary.Create++

		case importexport.RowActionUpdate:
			cr.ItemID = row.ID
			item := itemsByID[row.ID]
			current, err := buildExportRow(ctx, scope, item, map[string]string{})
			if err != nil {
				return nil, summary, fmt.Errorf("httpapi: import classify: read current item state for %q: %w", row.ID, err)
			}
			if diffs := importexport.DiffStagedRow(current, row, defs); len(diffs) > 0 {
				cr.Action = "update"
				cr.Changes = diffs
				summary.Update++
			} else {
				cr.Action = "unchanged"
				summary.Unchanged++
			}
		}

		out = append(out, cr)
	}

	return out, summary, nil
}
