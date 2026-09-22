package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

const exportItemPageSize = maxItemListLimit

const exportCSVContentType = "text/csv; charset=utf-8"

func filenameForExport(now time.Time) string {
	return "hho-items-" + now.UTC().Format("2006-01-02") + ".csv"
}

func exportItemsCSVHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		rows, defs, err := assembleExportRows(r.Context(), scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "export items.csv: assemble rows failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		filename := filenameForExport(time.Now())
		h := w.Header()
		h.Set("Content-Type", exportCSVContentType)
		h.Set("Content-Disposition", attachmentContentDisposition("attachment", filename))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")

		if err := importexport.EncodeCSV(w, defs, rows); err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "export items.csv: stream failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			return
		}
	}
}

func assembleExportRows(ctx context.Context, scope storage.Scope) ([]importexport.Row, []storage.CustomFieldDef, error) {
	items, err := listAllItemsForExport(ctx, scope.Items())
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	defs, err := scope.CustomFieldDefs().List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("httpapi: export: list custom field defs: %w", err)
	}

	locations, err := scope.Locations().List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("httpapi: export: list locations: %w", err)
	}
	paths := buildLocationPaths(locations)

	rows := make([]importexport.Row, 0, len(items))
	for _, item := range items {
		row, err := buildExportRow(ctx, scope, item, paths)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
	return rows, defs, nil
}

func listAllItemsForExport(ctx context.Context, items storage.ItemRepository) ([]storage.Item, error) {
	var all []storage.Item
	offset := int64(0)
	for {
		page, err := items.List(ctx, storage.Page{Limit: exportItemPageSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("httpapi: export: list items: %w", err)
		}
		all = append(all, page...)
		if int64(len(page)) < exportItemPageSize {
			return all, nil
		}
		offset += exportItemPageSize
	}
}

func buildExportRow(ctx context.Context, scope storage.Scope, item storage.Item, paths map[string]string) (importexport.Row, error) {
	labels, err := scope.ItemLabels().ListForItem(ctx, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: list labels: %w", item.ID, err)
	}
	identifications, err := scope.Identifications().List(ctx, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: list identifications: %w", item.ID, err)
	}
	attachments, err := scope.Attachments().ListForItem(ctx, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: list attachments: %w", item.ID, err)
	}
	customFields, err := scope.ItemCustomFields().List(ctx, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: list custom fields: %w", item.ID, err)
	}
	warranty, err := detailBlockOrNil(ctx, scope.Warranty().Get, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: get warranty: %w", item.ID, err)
	}
	purchase, err := detailBlockOrNil(ctx, scope.Purchase().Get, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: get purchase: %w", item.ID, err)
	}
	sale, err := detailBlockOrNil(ctx, scope.Sale().Get, item.ID)
	if err != nil {
		return importexport.Row{}, fmt.Errorf("httpapi: export: item %q: get sale: %w", item.ID, err)
	}

	var locationPath string
	if item.LocationID.Valid {
		locationPath = paths[item.LocationID.String]
	}

	return importexport.Row{
		Item:            item,
		LocationPath:    locationPath,
		Labels:          labels,
		Identifications: identifications,
		Attachments:     attachments,
		Warranty:        warranty,
		Purchase:        purchase,
		Sale:            sale,
		CustomFields:    customFields,
	}, nil
}

func detailBlockOrNil[T any](ctx context.Context, get func(context.Context, string) (T, error), itemID string) (*T, error) {
	v, err := get(ctx, itemID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func buildLocationPaths(locations []storage.Location) map[string]string {
	byID := make(map[string]storage.Location, len(locations))
	for _, l := range locations {
		byID[l.ID] = l
	}

	paths := make(map[string]string, len(locations))
	for _, l := range locations {
		resolveLocationPath(l.ID, byID, paths, make(map[string]struct{}, len(locations)))
	}
	return paths
}

func resolveLocationPath(id string, byID map[string]storage.Location, paths map[string]string, seen map[string]struct{}) string {
	if p, ok := paths[id]; ok {
		return p
	}
	loc, ok := byID[id]
	if !ok {
		return ""
	}
	if _, cyc := seen[id]; cyc {
		return loc.Name
	}
	seen[id] = struct{}{}

	if !loc.ParentID.Valid || loc.ParentID.String == "" {
		paths[id] = loc.Name
		return loc.Name
	}
	parentPath := resolveLocationPath(loc.ParentID.String, byID, paths, seen)
	full := loc.Name
	if parentPath != "" {
		full = parentPath + "/" + loc.Name
	}
	paths[id] = full
	return full
}
