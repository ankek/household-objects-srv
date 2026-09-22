package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/labels"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type labelBody struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	Version   int64  `json:"version,omitempty"`
}

type labelCreateRequestBody struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type labelUpdateRequestBody struct {
	Name    string `json:"name"`
	Color   string `json:"color"`
	Version int64  `json:"version"`
}

type labelListResponse struct {
	Labels []labelBody `json:"labels"`
}

func toLabelBody(row storage.Label) labelBody {
	return labelBody{
		ID:        row.ID,
		Name:      row.Name,
		Color:     row.Color,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		Version:   row.Version,
	}
}

func labelListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		rows, err := scope.Labels().List(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list labels failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]labelBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toLabelBody(row))
		}
		writeJSON(w, r, http.StatusOK, labelListResponse{Labels: out})
	}
}

func labelCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body labelCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := labels.CreateLabel(r.Context(), scope.Labels(), labels.CreateLabelRequest{
			Name:  body.Name,
			Color: body.Color,
		})
		switch {
		case errors.Is(err, labels.ErrNameRequired), errors.Is(err, labels.ErrColorInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrLabelNameConflict):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create label failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toLabelBody(row))
	}
}

func labelUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		labelID := chi.URLParam(r, "labelID")

		var body labelUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := labels.UpdateLabel(r.Context(), scope.Labels(), labels.UpdateLabelRequest{
			LabelID:         labelID,
			Name:            body.Name,
			Color:           body.Color,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, labels.ErrVersionRequired), errors.Is(err, labels.ErrNameRequired), errors.Is(err, labels.ErrColorInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrLabelNameConflict):
			problem.Write(w, r, problem.Conflict())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update label failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toLabelBody(row))
	}
}

func labelDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		labelID := chi.URLParam(r, "labelID")

		err := scope.Labels().Delete(r.Context(), labelID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete label failed",
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
