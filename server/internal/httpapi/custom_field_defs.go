package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/customfields"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type customFieldDefBody struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	FieldType    string `json:"field_type"`
	DisplayOrder int64  `json:"display_order"`
	CreatedAt    int64  `json:"created_at,omitempty"`
	UpdatedAt    int64  `json:"updated_at,omitempty"`
	Version      int64  `json:"version,omitempty"`
}

type customFieldDefCreateRequestBody struct {
	Name         string `json:"name"`
	FieldType    string `json:"field_type"`
	DisplayOrder int64  `json:"display_order"`
}

type customFieldDefUpdateRequestBody struct {
	Name         string `json:"name"`
	FieldType    string `json:"field_type"`
	DisplayOrder int64  `json:"display_order"`
	Version      int64  `json:"version"`
}

type customFieldDefListResponse struct {
	CustomFieldDefs []customFieldDefBody `json:"custom_field_defs"`
}

func toCustomFieldDefBody(row storage.CustomFieldDef) customFieldDefBody {
	return customFieldDefBody{
		ID:           row.ID,
		Name:         row.Name,
		FieldType:    row.FieldType,
		DisplayOrder: row.DisplayOrder,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		Version:      row.Version,
	}
}

func customFieldDefListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		rows, err := scope.CustomFieldDefs().List(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list custom field defs failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]customFieldDefBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toCustomFieldDefBody(row))
		}
		writeJSON(w, r, http.StatusOK, customFieldDefListResponse{CustomFieldDefs: out})
	}
}

func customFieldDefCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body customFieldDefCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := customfields.CreateCustomFieldDef(r.Context(), scope.CustomFieldDefs(), customfields.CreateCustomFieldDefRequest{
			Name:         body.Name,
			FieldType:    body.FieldType,
			DisplayOrder: body.DisplayOrder,
		})
		switch {
		case errors.Is(err, customfields.ErrNameRequired), errors.Is(err, customfields.ErrFieldTypeInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create custom field def failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toCustomFieldDefBody(row))
	}
}

func customFieldDefUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		fieldDefID := chi.URLParam(r, "fieldDefID")

		var body customFieldDefUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := customfields.UpdateCustomFieldDef(r.Context(), scope.CustomFieldDefs(), customfields.UpdateCustomFieldDefRequest{
			FieldDefID:      fieldDefID,
			Name:            body.Name,
			FieldType:       body.FieldType,
			DisplayOrder:    body.DisplayOrder,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, customfields.ErrVersionRequired), errors.Is(err, customfields.ErrNameRequired), errors.Is(err, customfields.ErrFieldTypeInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update custom field def failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toCustomFieldDefBody(row))
	}
}

func customFieldDefDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		fieldDefID := chi.URLParam(r, "fieldDefID")

		err := scope.CustomFieldDefs().Delete(r.Context(), fieldDefID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete custom field def failed",
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
