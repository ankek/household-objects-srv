package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/locations"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type locationBody struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	ParentID  string `json:"parent_id,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	Version   int64  `json:"version,omitempty"`
}

type locationCreateRequestBody struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
}

type locationUpdateRequestBody struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	Version  int64  `json:"version"`
}

type locationListResponse struct {
	Locations []locationBody `json:"locations"`
}

func toLocationBody(loc storage.Location) locationBody {
	body := locationBody{
		ID:        loc.ID,
		Name:      loc.Name,
		CreatedAt: loc.CreatedAt,
		UpdatedAt: loc.UpdatedAt,
		Version:   loc.Version,
	}
	if loc.ParentID.Valid {
		body.ParentID = loc.ParentID.String
	}
	return body
}

func locationListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		rows, err := scope.Locations().List(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list locations failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]locationBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toLocationBody(row))
		}
		writeJSON(w, r, http.StatusOK, locationListResponse{Locations: out})
	}
}

type locationTreeNode struct {
	locationBody
	ItemCount      int64              `json:"item_count"`
	TotalItemCount int64              `json:"total_item_count"`
	Children       []locationTreeNode `json:"children"`
}

type locationTreeResponse struct {
	Tree []locationTreeNode `json:"tree"`
}

func toLocationTreeNodes(nodes []*storage.LocationNode) []locationTreeNode {
	out := make([]locationTreeNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, locationTreeNode{
			locationBody:   toLocationBody(n.Location),
			ItemCount:      n.ItemCount,
			TotalItemCount: n.TotalItemCount,
			Children:       toLocationTreeNodes(n.Children),
		})
	}
	return out
}

func locationTreeHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		tree, err := scope.Locations().Tree(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "build location tree failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, locationTreeResponse{Tree: toLocationTreeNodes(tree)})
	}
}

func locationCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body locationCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		loc, err := locations.Create(r.Context(), scope.Locations(), locations.CreateRequest{
			Name:     body.Name,
			ParentID: body.ParentID,
		})
		switch {
		case errors.Is(err, locations.ErrNameRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrLocationParentNotFound):
			problem.Write(w, r, problem.BadRequest("parent_id does not resolve to a location in this group"))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create location failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toLocationBody(loc))
	}
}

func locationGetHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		locationID := chi.URLParam(r, "locationID")

		loc, err := scope.Locations().Get(r.Context(), locationID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get location failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toLocationBody(loc))
	}
}

func locationUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		locationID := chi.URLParam(r, "locationID")

		var body locationUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		loc, err := locations.Update(r.Context(), scope.Locations(), locations.UpdateRequest{
			LocationID:      locationID,
			Name:            body.Name,
			ParentID:        body.ParentID,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, locations.ErrNameRequired), errors.Is(err, locations.ErrVersionRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrLocationParentNotFound):
			problem.Write(w, r, problem.BadRequest("parent_id does not resolve to a location in this group"))
			return
		case errors.Is(err, storage.ErrLocationCycle):
			problem.Write(w, r, problem.BadRequest("parent_id would create a cycle in the location tree"))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update location failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toLocationBody(loc))
	}
}

const reassignToParam = "reassign_to"

func locationDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		locationID := chi.URLParam(r, "locationID")

		query := r.URL.Query()
		params := storage.DeleteLocationParams{
			LocationID: locationID,
			Reassign:   query.Has(reassignToParam),
			ReassignTo: query.Get(reassignToParam),
			Now:        time.Now().UnixMilli(),
		}

		err := scope.Locations().Delete(r.Context(), params)

		var notEmpty *storage.LocationNotEmptyError
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.As(err, &notEmpty):
			writeLocationNotEmpty(w, r, notEmpty)
			return
		case errors.Is(err, storage.ErrLocationParentNotFound):
			problem.Write(w, r, problem.BadRequest(reassignToParam+" does not resolve to a location in this group"))
			return
		case errors.Is(err, storage.ErrLocationReassignTarget):
			problem.Write(w, r, problem.BadRequest(reassignToParam+" is the location being deleted or one of its own descendants"))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete location failed",
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

type locationNotEmptyBody struct {
	problem.Problem
	ChildCount int64 `json:"child_count"`
	ItemCount  int64 `json:"item_count"`
}

func writeLocationNotEmpty(w http.ResponseWriter, r *http.Request, e *storage.LocationNotEmptyError) {
	body := locationNotEmptyBody{
		Problem: problem.New(http.StatusConflict, "location-not-empty", "Location Not Empty").
			WithDetail("this location still holds live child locations or items; pass ?" + reassignToParam +
				"= to move them (empty value detaches them to the root)"),
		ChildCount: e.ChildCount,
		ItemCount:  e.ItemCount,
	}
	body.RequestID = requestid.FromContext(r.Context())

	encoded, err := json.Marshal(body)
	if err != nil {
		problem.Write(w, r, problem.Internal())
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/problem+json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusConflict)
	_, _ = w.Write(encoded)
}
