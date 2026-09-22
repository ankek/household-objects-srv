package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type warrantyBody struct {
	ItemID     string `json:"item_id,omitempty"`
	Holder     string `json:"holder,omitempty"`
	Provider   string `json:"provider,omitempty"`
	StartsOn   string `json:"starts_on,omitempty"`
	ExpiresOn  string `json:"expires_on,omitempty"`
	IsLifetime bool   `json:"is_lifetime"`
	Notes      string `json:"notes,omitempty"`
	CreatedAt  int64  `json:"created_at,omitempty"`
	UpdatedAt  int64  `json:"updated_at,omitempty"`
	Version    int64  `json:"version,omitempty"`
}

type warrantyCreateRequestBody struct {
	Holder     string `json:"holder,omitempty"`
	Provider   string `json:"provider,omitempty"`
	StartsOn   string `json:"starts_on,omitempty"`
	ExpiresOn  string `json:"expires_on,omitempty"`
	IsLifetime bool   `json:"is_lifetime,omitempty"`
	Notes      string `json:"notes,omitempty"`
}

type warrantyUpdateRequestBody struct {
	Holder     string `json:"holder,omitempty"`
	Provider   string `json:"provider,omitempty"`
	StartsOn   string `json:"starts_on,omitempty"`
	ExpiresOn  string `json:"expires_on,omitempty"`
	IsLifetime bool   `json:"is_lifetime,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Version    int64  `json:"version"`
}

func toWarrantyBody(w storage.Warranty) warrantyBody {
	body := warrantyBody{
		ItemID:     w.ItemID,
		Holder:     w.Holder,
		Provider:   w.Provider,
		IsLifetime: w.IsLifetime != 0,
		Notes:      w.Notes,
		CreatedAt:  w.CreatedAt,
		UpdatedAt:  w.UpdatedAt,
		Version:    w.Version,
	}
	if w.StartsOn.Valid {
		body.StartsOn = w.StartsOn.String
	}
	if w.ExpiresOn.Valid {
		body.ExpiresOn = w.ExpiresOn.String
	}
	return body
}

func warrantyGetHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		block, err := scope.Warranty().Get(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get item warranty failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toWarrantyBody(block))
	}
}

func warrantyCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body warrantyCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		block, err := items.CreateWarranty(r.Context(), scope.Warranty(), items.CreateWarrantyRequest{
			ItemID:     itemID,
			Holder:     body.Holder,
			Provider:   body.Provider,
			StartsOn:   body.StartsOn,
			ExpiresOn:  body.ExpiresOn,
			IsLifetime: body.IsLifetime,
			Notes:      body.Notes,
		})
		switch {
		case errors.Is(err, items.ErrWarrantyDateInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrWarrantyExists):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create item warranty failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toWarrantyBody(block))
	}
}

func warrantyUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body warrantyUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		block, err := items.UpdateWarranty(r.Context(), scope.Warranty(), items.UpdateWarrantyRequest{
			ItemID:          itemID,
			Holder:          body.Holder,
			Provider:        body.Provider,
			StartsOn:        body.StartsOn,
			ExpiresOn:       body.ExpiresOn,
			IsLifetime:      body.IsLifetime,
			Notes:           body.Notes,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, items.ErrVersionRequired), errors.Is(err, items.ErrWarrantyDateInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update item warranty failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toWarrantyBody(block))
	}
}

func warrantyDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		err := scope.Warranty().Delete(r.Context(), itemID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete item warranty failed",
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
