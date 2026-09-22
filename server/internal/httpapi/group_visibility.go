package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
)

type groupVisibilityBody struct {
	WarrantyVisible bool  `json:"warranty_visible"`
	SaleVisible     bool  `json:"sale_visible"`
	PurchaseVisible bool  `json:"purchase_visible"`
	Version         int64 `json:"version,omitempty"`
}

type groupVisibilityUpdateRequestBody struct {
	WarrantyVisible bool  `json:"warranty_visible"`
	SaleVisible     bool  `json:"sale_visible"`
	PurchaseVisible bool  `json:"purchase_visible"`
	Version         int64 `json:"version"`
}

func toGroupVisibilityBody(v storage.GroupVisibility) groupVisibilityBody {
	return groupVisibilityBody{
		WarrantyVisible: v.WarrantyVisible,
		SaleVisible:     v.SaleVisible,
		PurchaseVisible: v.PurchaseVisible,
		Version:         v.Version,
	}
}

func groupVisibilityGetHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		state, err := scope.Visibility().Get(r.Context())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get group visibility failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toGroupVisibilityBody(state))
	}
}

func groupVisibilityUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body groupVisibilityUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		state, err := groups.UpdateVisibility(r.Context(), scope.Visibility(), groups.UpdateVisibilityRequest{
			WarrantyVisible: body.WarrantyVisible,
			SaleVisible:     body.SaleVisible,
			PurchaseVisible: body.PurchaseVisible,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, groups.ErrVersionRequired):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update group visibility failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toGroupVisibilityBody(state))
	}
}
