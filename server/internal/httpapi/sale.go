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

type saleBody struct {
	ItemID         string `json:"item_id,omitempty"`
	BuyerName      string `json:"buyer_name,omitempty"`
	SoldOn         string `json:"sold_on,omitempty"`
	SalePriceMinor int64  `json:"sale_price_minor"`
	Notes          string `json:"notes,omitempty"`
	CreatedAt      int64  `json:"created_at,omitempty"`
	UpdatedAt      int64  `json:"updated_at,omitempty"`
	Version        int64  `json:"version,omitempty"`
}

type saleCreateRequestBody struct {
	BuyerName      string `json:"buyer_name,omitempty"`
	SoldOn         string `json:"sold_on,omitempty"`
	SalePriceMinor int64  `json:"sale_price_minor,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

type saleUpdateRequestBody struct {
	BuyerName      string `json:"buyer_name,omitempty"`
	SoldOn         string `json:"sold_on,omitempty"`
	SalePriceMinor int64  `json:"sale_price_minor,omitempty"`
	Notes          string `json:"notes,omitempty"`
	Version        int64  `json:"version"`
}

func toSaleBody(s storage.Sale) saleBody {
	body := saleBody{
		ItemID:         s.ItemID,
		BuyerName:      s.BuyerName,
		SalePriceMinor: s.SalePriceMinor,
		Notes:          s.Notes,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		Version:        s.Version,
	}
	if s.SoldOn.Valid {
		body.SoldOn = s.SoldOn.String
	}
	return body
}

func saleGetHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		block, err := scope.Sale().Get(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get item sale failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toSaleBody(block))
	}
}

func saleCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body saleCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		block, err := items.CreateSale(r.Context(), scope.Sale(), items.CreateSaleRequest{
			ItemID:         itemID,
			BuyerName:      body.BuyerName,
			SoldOn:         body.SoldOn,
			SalePriceMinor: body.SalePriceMinor,
			Notes:          body.Notes,
		})
		switch {
		case errors.Is(err, items.ErrSaleDateInvalid), errors.Is(err, items.ErrSalePriceNegative):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrSaleExists):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create item sale failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toSaleBody(block))
	}
}

func saleUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body saleUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		block, err := items.UpdateSale(r.Context(), scope.Sale(), items.UpdateSaleRequest{
			ItemID:          itemID,
			BuyerName:       body.BuyerName,
			SoldOn:          body.SoldOn,
			SalePriceMinor:  body.SalePriceMinor,
			Notes:           body.Notes,
			ExpectedVersion: body.Version,
		})
		switch {
		case errors.Is(err, items.ErrVersionRequired), errors.Is(err, items.ErrSaleDateInvalid), errors.Is(err, items.ErrSalePriceNegative):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update item sale failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toSaleBody(block))
	}
}

func saleDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		err := scope.Sale().Delete(r.Context(), itemID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete item sale failed",
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
