package httpapi

import (
	"database/sql"
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

type itemCustomFieldBody struct {
	ID          string   `json:"id,omitempty"`
	ItemID      string   `json:"item_id,omitempty"`
	FieldDefID  string   `json:"field_def_id,omitempty"`
	Name        string   `json:"name"`
	FieldType   string   `json:"field_type"`
	TextValue   *string  `json:"text_value,omitempty"`
	NumberValue *float64 `json:"number_value,omitempty"`
	BoolValue   *bool    `json:"bool_value,omitempty"`
	DateValue   *string  `json:"date_value,omitempty"`
	CreatedAt   int64    `json:"created_at,omitempty"`
	UpdatedAt   int64    `json:"updated_at,omitempty"`
	Version     int64    `json:"version,omitempty"`
}

type itemCustomFieldCreateRequestBody struct {
	FieldDefID  string   `json:"field_def_id"`
	Name        string   `json:"name"`
	FieldType   string   `json:"field_type"`
	TextValue   *string  `json:"text_value"`
	NumberValue *float64 `json:"number_value"`
	BoolValue   *bool    `json:"bool_value"`
	DateValue   *string  `json:"date_value"`
}

type itemCustomFieldUpdateRequestBody struct {
	FieldDefID  string   `json:"field_def_id"`
	Name        string   `json:"name"`
	FieldType   string   `json:"field_type"`
	TextValue   *string  `json:"text_value"`
	NumberValue *float64 `json:"number_value"`
	BoolValue   *bool    `json:"bool_value"`
	DateValue   *string  `json:"date_value"`
	Version     int64    `json:"version"`
}

type itemCustomFieldListResponse struct {
	CustomFields []itemCustomFieldBody `json:"custom_fields"`
}

func toItemCustomFieldBody(row storage.ItemCustomField) itemCustomFieldBody {
	return itemCustomFieldBody{
		ID:          row.ID,
		ItemID:      row.ItemID,
		FieldDefID:  row.FieldDefID.String,
		Name:        row.Name,
		FieldType:   row.FieldType,
		TextValue:   nullStringPtrOut(row.TextValue),
		NumberValue: nullFloat64PtrOut(row.NumberValue),
		BoolValue:   nullBoolPtrOut(row.BoolValue),
		DateValue:   nullStringPtrOut(row.DateValue),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Version:     row.Version,
	}
}

func nullStringPtrOut(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullFloat64PtrOut(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func nullBoolPtrOut(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}

func itemCustomFieldListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		rows, err := scope.ItemCustomFields().List(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list item custom fields failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]itemCustomFieldBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toItemCustomFieldBody(row))
		}
		writeJSON(w, r, http.StatusOK, itemCustomFieldListResponse{CustomFields: out})
	}
}

func itemCustomFieldCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body itemCustomFieldCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := items.CreateItemCustomField(r.Context(), scope.ItemCustomFields(), scope.CustomFieldDefs(), items.CreateItemCustomFieldRequest{
			ItemID:      itemID,
			FieldDefID:  body.FieldDefID,
			Name:        body.Name,
			FieldType:   body.FieldType,
			TextValue:   body.TextValue,
			NumberValue: body.NumberValue,
			BoolValue:   body.BoolValue,
			DateValue:   body.DateValue,
		})
		switch {
		case errors.Is(err, items.ErrCustomFieldNameRequired),
			errors.Is(err, items.ErrCustomFieldTypeInvalid),
			errors.Is(err, items.ErrCustomFieldValueInvalid),
			errors.Is(err, items.ErrCustomFieldDefNotFound),
			errors.Is(err, items.ErrCustomFieldTypeMismatch):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create item custom field failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toItemCustomFieldBody(row))
	}
}

func itemCustomFieldUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		customFieldID := chi.URLParam(r, "customFieldID")

		var body itemCustomFieldUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := items.UpdateItemCustomField(r.Context(), scope.ItemCustomFields(), scope.CustomFieldDefs(), items.UpdateItemCustomFieldRequest{
			ItemID:          itemID,
			CustomFieldID:   customFieldID,
			FieldDefID:      body.FieldDefID,
			Name:            body.Name,
			FieldType:       body.FieldType,
			TextValue:       body.TextValue,
			NumberValue:     body.NumberValue,
			BoolValue:       body.BoolValue,
			DateValue:       body.DateValue,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, items.ErrVersionRequired),
			errors.Is(err, items.ErrCustomFieldNameRequired),
			errors.Is(err, items.ErrCustomFieldTypeInvalid),
			errors.Is(err, items.ErrCustomFieldValueInvalid),
			errors.Is(err, items.ErrCustomFieldDefNotFound),
			errors.Is(err, items.ErrCustomFieldTypeMismatch):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update item custom field failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toItemCustomFieldBody(row))
	}
}

func itemCustomFieldDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")
		customFieldID := chi.URLParam(r, "customFieldID")

		err := scope.ItemCustomFields().Delete(r.Context(), itemID, customFieldID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete item custom field failed",
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
