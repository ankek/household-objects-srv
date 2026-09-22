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
)

type stockAdjustmentBody struct {
	ID                string `json:"id,omitempty"`
	ItemID            string `json:"item_id,omitempty"`
	Delta             int64  `json:"delta"`
	Reason            string `json:"reason,omitempty"`
	Note              string `json:"note,omitempty"`
	ResultingQuantity int64  `json:"resulting_quantity"`
	CreatedAt         int64  `json:"created_at,omitempty"`
	UpdatedAt         int64  `json:"updated_at,omitempty"`
	Version           int64  `json:"version,omitempty"`
}

type stockAdjustmentCreateRequestBody struct {
	Delta  int64  `json:"delta"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

type stockAdjustmentListResponse struct {
	StockAdjustments []stockAdjustmentBody `json:"stock_adjustments"`
}

func toStockAdjustmentBody(row storage.StockAdjustment) stockAdjustmentBody {
	return stockAdjustmentBody{
		ID:                row.ID,
		ItemID:            row.ItemID,
		Delta:             row.Delta,
		Reason:            row.Reason,
		Note:              row.Note,
		ResultingQuantity: row.ResultingQuantity,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		Version:           row.Version,
	}
}

func stockAdjustmentListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		rows, err := scope.StockAdjustments().List(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list stock adjustments failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		out := make([]stockAdjustmentBody, 0, len(rows))
		for _, row := range rows {
			out = append(out, toStockAdjustmentBody(row))
		}
		writeJSON(w, r, http.StatusOK, stockAdjustmentListResponse{StockAdjustments: out})
	}
}

func stockAdjustmentCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body stockAdjustmentCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		row, err := items.CreateStockAdjustment(r.Context(), scope.StockAdjustments(), items.CreateStockAdjustmentRequest{
			ItemID: itemID,
			Delta:  body.Delta,
			Reason: body.Reason,
			Note:   body.Note,
		})
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create stock adjustment failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toStockAdjustmentBody(row))
	}
}
