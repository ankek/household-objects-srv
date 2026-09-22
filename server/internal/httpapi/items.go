package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultItemListLimit = 50
	maxItemListLimit     = 200
)

type itemBody struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	LocationID  string `json:"location_id,omitempty"`
	Quantity    int64  `json:"quantity,omitempty"`
	ShortCode   string `json:"short_code,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	UpdatedAt   int64  `json:"updated_at,omitempty"`
	Version     int64  `json:"version,omitempty"`
}

type itemUpdateRequestBody struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	LocationID  string `json:"location_id,omitempty"`
	Quantity    int64  `json:"quantity,omitempty"`
	Version     int64  `json:"version"`
}

type itemListResponse struct {
	Items []itemBody `json:"items"`
}

func toItemBody(it storage.Item) itemBody {
	body := itemBody{
		ID:          it.ID,
		Name:        it.Name,
		Description: it.Description,
		Quantity:    it.Quantity,
		ShortCode:   it.ShortCode,
		CreatedAt:   it.CreatedAt,
		UpdatedAt:   it.UpdatedAt,
		Version:     it.Version,
	}
	if it.LocationID.Valid {
		body.LocationID = it.LocationID.String
	}
	return body
}

func itemCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body itemBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		item, err := items.Create(r.Context(), scope.Items(), items.CreateRequest{
			Name:        body.Name,
			Description: body.Description,
			LocationID:  body.LocationID,
			Quantity:    body.Quantity,
		})
		switch {
		case errors.Is(err, items.ErrNameRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create item failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toItemBody(item))
	}
}

func parseItemPage(r *http.Request) (storage.Page, error) {
	page := storage.Page{Limit: defaultItemListLimit, Offset: 0}

	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 || n > maxItemListLimit {
			return storage.Page{}, fmt.Errorf("limit must be an integer between 1 and %d", maxItemListLimit)
		}
		page.Limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return storage.Page{}, errors.New("offset must be a non-negative integer")
		}
		page.Offset = n
	}
	return page, nil
}

const (
	itemQueryParam          = "q"
	itemLocationParam       = "location_id"
	itemDescendantsParam    = "descendants"
	itemLabelParam          = "label_id"
	itemCustomFieldParam    = "custom_field"
	itemWarrantyStatusParam = "warranty_status"
	itemCreatedFromParam    = "created_from"
	itemCreatedToParam      = "created_to"
	itemUpdatedFromParam    = "updated_from"
	itemUpdatedToParam      = "updated_to"
	itemSortParam           = "sort"
)

var itemSortTokens = map[string]storage.ItemSortField{
	"name":       storage.ItemSortName,
	"quantity":   storage.ItemSortQuantity,
	"created_at": storage.ItemSortCreatedAt,
	"updated_at": storage.ItemSortUpdatedAt,
}

func parseItemSort(r *http.Request) (storage.ItemSort, error) {
	raw := r.URL.Query().Get(itemSortParam)
	if raw == "" {
		return storage.ItemSort{}, nil
	}

	token, descending := raw, false
	if strings.HasPrefix(token, "-") {
		descending = true
		token = token[1:]
	}

	field, ok := itemSortTokens[token]
	if !ok {
		return storage.ItemSort{}, fmt.Errorf(
			"sort must be one of name, quantity, created_at, updated_at, optionally prefixed with '-' for descending order (got %q)", raw)
	}
	return storage.ItemSort{Field: field, Descending: descending}, nil
}

func parseItemFilter(r *http.Request, scope storage.Scope) (storage.ItemFilter, error) {
	query := r.URL.Query()
	filter := storage.ItemFilter{
		Query:          query.Get(itemQueryParam),
		LabelIDs:       query[itemLabelParam],
		CustomFields:   parseItemCustomFieldFilters(query[itemCustomFieldParam]),
		WarrantyStatus: query.Get(itemWarrantyStatusParam),
		CreatedFrom:    parseItemFilterEpochMillis(query.Get(itemCreatedFromParam)),
		CreatedTo:      parseItemFilterEpochMillis(query.Get(itemCreatedToParam)),
		UpdatedFrom:    parseItemFilterEpochMillis(query.Get(itemUpdatedFromParam)),
		UpdatedTo:      parseItemFilterEpochMillis(query.Get(itemUpdatedToParam)),
	}

	locationID := query.Get(itemLocationParam)
	if locationID == "" {
		return filter, nil
	}

	descendants, _ := strconv.ParseBool(query.Get(itemDescendantsParam))
	if !descendants {
		filter.LocationIDs = []string{locationID}
		return filter, nil
	}

	ids, err := scope.Locations().Descendants(r.Context(), locationID)
	if err != nil {
		return storage.ItemFilter{}, err
	}
	if len(ids) == 0 {
		ids = []string{locationID}
	}
	filter.LocationIDs = ids
	return filter, nil
}

func parseItemCustomFieldFilters(raw []string) []storage.CustomFieldMatch {
	var out []storage.CustomFieldMatch
	for _, entry := range raw {
		idx := strings.Index(entry, ":")
		if idx <= 0 {
			continue
		}
		out = append(out, storage.CustomFieldMatch{Name: entry[:idx], Value: entry[idx+1:]})
	}
	return out
}

func parseItemFilterEpochMillis(raw string) *int64 {
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func itemListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		page, err := parseItemPage(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		sort, err := parseItemSort(r)
		if err != nil {
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		}

		filter, err := parseItemFilter(r, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "resolve item filter failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		filter.Sort = sort

		list, err := scope.Items().ListFiltered(r.Context(), filter, page)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list items failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]itemBody, 0, len(list))
		for _, it := range list {
			out = append(out, toItemBody(it))
		}
		writeJSON(w, r, http.StatusOK, itemListResponse{Items: out})
	}
}

func itemGetHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		item, err := scope.Items().Get(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get item failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toItemBody(item))
	}
}

func itemUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body itemUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		item, err := items.Update(r.Context(), scope.Items(), items.UpdateRequest{
			ItemID:          itemID,
			Name:            body.Name,
			Description:     body.Description,
			LocationID:      body.LocationID,
			Quantity:        body.Quantity,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, items.ErrNameRequired), errors.Is(err, items.ErrVersionRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update item failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toItemBody(item))
	}
}

func itemDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		err := scope.Items().Delete(r.Context(), itemID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete item failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
