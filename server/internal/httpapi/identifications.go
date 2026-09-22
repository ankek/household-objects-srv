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

type identificationBody struct {
	ID        string `json:"id,omitempty"`
	ItemID    string `json:"item_id,omitempty"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	Version   int64  `json:"version,omitempty"`
}

type identificationCreateRequestBody struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type identificationUpdateRequestBody struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

type identificationListResponse struct {
	Identifications []identificationBody `json:"identifications"`
}

func toIdentificationBody(row storage.Identification) identificationBody {
	return identificationBody{
		ID:        row.ID,
		ItemID:    row.ItemID,
		Kind:      row.Kind,
		Value:     row.Value,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		Version:   row.Version,
	}
}

func identificationListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		rows, err := scope.Identifications().List(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list item identifications failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]identificationBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toIdentificationBody(row))
		}
		writeJSON(w, r, http.StatusOK, identificationListResponse{Identifications: out})
	}
}

func identificationCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body identificationCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := items.CreateIdentification(r.Context(), scope.Identifications(), items.CreateIdentificationRequest{
			ItemID: itemID,
			Kind:   body.Kind,
			Value:  body.Value,
		})
		switch {
		case errors.Is(err, items.ErrIdentificationKindInvalid), errors.Is(err, items.ErrIdentificationValueRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create item identification failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toIdentificationBody(row))
	}
}

func identificationUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		identificationID := chi.URLParam(r, "identificationID")

		var body identificationUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := items.UpdateIdentification(r.Context(), scope.Identifications(), items.UpdateIdentificationRequest{
			ItemID:           itemID,
			IdentificationID: identificationID,
			Kind:             body.Kind,
			Value:            body.Value,
			ExpectedVersion:  body.Version,
		})
		switch {
		case errors.Is(err, items.ErrVersionRequired), errors.Is(err, items.ErrIdentificationKindInvalid), errors.Is(err, items.ErrIdentificationValueRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update item identification failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toIdentificationBody(row))
	}
}

func identificationDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		identificationID := chi.URLParam(r, "identificationID")

		err := scope.Identifications().Delete(r.Context(), itemID, identificationID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete item identification failed",
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
