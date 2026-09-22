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

type purchaseBody struct {
	ItemID             string `json:"item_id,omitempty"`
	Vendor             string `json:"vendor,omitempty"`
	PurchasedOn        string `json:"purchased_on,omitempty"`
	PurchasePriceMinor int64  `json:"purchase_price_minor"`
	OrderReference     string `json:"order_reference,omitempty"`
	Notes              string `json:"notes,omitempty"`
	CreatedAt          int64  `json:"created_at,omitempty"`
	UpdatedAt          int64  `json:"updated_at,omitempty"`
	Version            int64  `json:"version,omitempty"`
}

type purchaseCreateRequestBody struct {
	Vendor             string `json:"vendor,omitempty"`
	PurchasedOn        string `json:"purchased_on,omitempty"`
	PurchasePriceMinor int64  `json:"purchase_price_minor,omitempty"`
	OrderReference     string `json:"order_reference,omitempty"`
	Notes              string `json:"notes,omitempty"`
}

type purchaseUpdateRequestBody struct {
	Vendor             string `json:"vendor,omitempty"`
	PurchasedOn        string `json:"purchased_on,omitempty"`
	PurchasePriceMinor int64  `json:"purchase_price_minor,omitempty"`
	OrderReference     string `json:"order_reference,omitempty"`
	Notes              string `json:"notes,omitempty"`
	Version            int64  `json:"version"`
}

func toPurchaseBody(p storage.Purchase) purchaseBody {
	body := purchaseBody{
		ItemID:             p.ItemID,
		Vendor:             p.Vendor,
		PurchasePriceMinor: p.PurchasePriceMinor,
		OrderReference:     p.OrderReference,
		Notes:              p.Notes,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
		Version:            p.Version,
	}
	if p.PurchasedOn.Valid {
		body.PurchasedOn = p.PurchasedOn.String
	}
	return body
}

func purchaseGetHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		block, err := scope.Purchase().Get(r.Context(), itemID)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "get item purchase failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toPurchaseBody(block))
	}
}

func purchaseCreateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body purchaseCreateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		block, err := items.CreatePurchase(r.Context(), scope.Purchase(), items.CreatePurchaseRequest{
			ItemID:             itemID,
			Vendor:             body.Vendor,
			PurchasedOn:        body.PurchasedOn,
			PurchasePriceMinor: body.PurchasePriceMinor,
			OrderReference:     body.OrderReference,
			Notes:              body.Notes,
		})
		switch {
		case errors.Is(err, items.ErrPurchaseDateInvalid), errors.Is(err, items.ErrPurchasePriceNegative):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrPurchaseExists):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create item purchase failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, toPurchaseBody(block))
	}
}

func purchaseUpdateHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		var body purchaseUpdateRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		block, err := items.UpdatePurchase(r.Context(), scope.Purchase(), items.UpdatePurchaseRequest{
			ItemID:             itemID,
			Vendor:             body.Vendor,
			PurchasedOn:        body.PurchasedOn,
			PurchasePriceMinor: body.PurchasePriceMinor,
			OrderReference:     body.OrderReference,
			Notes:              body.Notes,
			ExpectedVersion:    body.Version,
		})
		switch {
		case errors.Is(err, items.ErrVersionRequired), errors.Is(err, items.ErrPurchaseDateInvalid), errors.Is(err, items.ErrPurchasePriceNegative):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case errors.Is(err, storage.ErrVersionMismatch):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "update item purchase failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, toPurchaseBody(block))
	}
}

func purchaseDeleteHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		itemID := chi.URLParam(r, "itemID")

		err := scope.Purchase().Delete(r.Context(), itemID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "delete item purchase failed",
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
