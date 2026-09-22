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
	"strconv"
	"strings"
)

const bomItemIDParam = "item_id"

var bomCSVHeader = []string{
	"item_id", "item_name", "location_id", "location_name",
	"quantity", "value_minor", "identifications", "running_total_minor",
}

func parseBoMSelection(r *http.Request) (locationIDs, labelIDs, itemIDs []string, err error) {
	q := r.URL.Query()
	locationIDs = dedupeNonEmptyStrings(q[itemLocationParam])
	labelIDs = dedupeNonEmptyStrings(q[itemLabelParam])
	itemIDs = dedupeNonEmptyStrings(q[bomItemIDParam])

	kinds := 0
	if len(locationIDs) > 0 {
		kinds++
	}
	if len(labelIDs) > 0 {
		kinds++
	}
	if len(itemIDs) > 0 {
		kinds++
	}
	if kinds > 1 {
		return nil, nil, nil, fmt.Errorf(
			"%s, %s and %s are mutually exclusive selection modes; supply values for exactly one (or none, for an empty export)",
			itemLocationParam, itemLabelParam, bomItemIDParam)
	}
	return locationIDs, labelIDs, itemIDs, nil
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func listAllBoMItems(ctx context.Context, items storage.ItemRepository, filter storage.ItemFilter) ([]storage.Item, error) {
	var all []storage.Item
	offset := int64(0)
	for {
		page, err := items.ListFiltered(ctx, filter, storage.Page{Limit: exportItemPageSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("httpapi: bom: list filtered items: %w", err)
		}
		all = append(all, page...)
		if int64(len(page)) < exportItemPageSize {
			return all, nil
		}
		offset += exportItemPageSize
	}
}

func selectItemsByAnyLabel(ctx context.Context, items storage.ItemRepository, labelIDs []string) ([]storage.Item, error) {
	seen := make(map[string]storage.Item)
	for _, labelID := range labelIDs {
		matched, err := listAllBoMItems(ctx, items, storage.ItemFilter{LabelIDs: []string{labelID}})
		if err != nil {
			return nil, err
		}
		for _, it := range matched {
			seen[it.ID] = it
		}
	}
	out := make([]storage.Item, 0, len(seen))
	for _, it := range seen {
		out = append(out, it)
	}
	return out, nil
}

func selectItemsByID(ctx context.Context, items storage.ItemRepository, itemIDs []string) ([]storage.Item, error) {
	seen := make(map[string]storage.Item, len(itemIDs))
	for _, id := range itemIDs {
		it, err := items.Get(ctx, id)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			continue
		case err != nil:
			return nil, fmt.Errorf("httpapi: bom: get item %q: %w", id, err)
		}
		seen[it.ID] = it
	}
	out := make([]storage.Item, 0, len(seen))
	for _, it := range seen {
		out = append(out, it)
	}
	return out, nil
}

func selectBoMItems(ctx context.Context, scope storage.Scope, locationIDs, labelIDs, itemIDs []string) ([]storage.Item, error) {
	switch {
	case len(locationIDs) > 0:
		return listAllBoMItems(ctx, scope.Items(), storage.ItemFilter{LocationIDs: locationIDs})
	case len(labelIDs) > 0:
		return selectItemsByAnyLabel(ctx, scope.Items(), labelIDs)
	case len(itemIDs) > 0:
		return selectItemsByID(ctx, scope.Items(), itemIDs)
	default:
		return nil, nil
	}
}

func bomItemValueMinor(ctx context.Context, purchases storage.PurchaseRepository, itemID string) (int64, error) {
	p, err := purchases.Get(ctx, itemID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("httpapi: bom: get purchase for item %q: %w", itemID, err)
	}
	return p.PurchasePriceMinor, nil
}

func bomEncodeIdentifications(ids []storage.Identification) string {
	if len(ids) == 0 {
		return ""
	}
	pairs := make([]string, len(ids))
	for i, id := range ids {
		pairs[i] = importexport.EscapeSubValue(id.Kind) + ":" + importexport.EscapeSubValue(id.Value)
	}
	return strings.Join(pairs, "|")
}

func bomHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		locationIDs, labelIDs, itemIDs, err := parseBoMSelection(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		items, err := selectBoMItems(r.Context(), scope, locationIDs, labelIDs, itemIDs)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "export bom: select items failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

		locations, err := scope.Locations().List(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "export bom: list locations failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		locationNames := make(map[string]string, len(locations))
		for _, l := range locations {
			locationNames[l.ID] = l.Name
		}

		records := make([][]string, 0, len(items))
		var runningTotal int64
		for _, item := range items {
			valueMinor, err := bomItemValueMinor(r.Context(), scope.Purchase(), item.ID)
			if err != nil {
				cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "export bom: get purchase failed",
					slog.String("request_id", requestid.FromContext(r.Context())),
					slog.String("item_id", item.ID),
					slog.String("error", err.Error()),
				)
				problem.Write(w, r, problem.Internal())
				return
			}
			idents, err := scope.Identifications().List(r.Context(), item.ID)
			if err != nil {
				cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "export bom: list identifications failed",
					slog.String("request_id", requestid.FromContext(r.Context())),
					slog.String("item_id", item.ID),
					slog.String("error", err.Error()),
				)
				problem.Write(w, r, problem.Internal())
				return
			}
			runningTotal += valueMinor

			var locationID, locationName string
			if item.LocationID.Valid {
				locationID = item.LocationID.String
				locationName = locationNames[locationID]
			}

			records = append(records, []string{
				item.ID,
				item.Name,
				locationID,
				locationName,
				strconv.FormatInt(item.Quantity, 10),
				strconv.FormatInt(valueMinor, 10),
				bomEncodeIdentifications(idents),
				strconv.FormatInt(runningTotal, 10),
			})
		}

		writeReportCSV(w, r, cfg, "bom", bomCSVHeader, records)
	}
}
