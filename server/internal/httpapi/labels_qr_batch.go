package httpapi

import (
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/qrlabels"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
)

const maxLabelBatchItems = maxItemListLimit

type labelsQRBatchRequest struct {
	ItemIDs []string `json:"item_ids"`
}

type labelsQRBatchItem struct {
	ID        string `json:"id"`
	ShortCode string `json:"short_code"`
	Name      string `json:"name"`
	QRSVG     string `json:"qr_svg"`
}

type labelsQRBatchResponse struct {
	Items []labelsQRBatchItem `json:"items"`
}

func labelsQRBatchHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body labelsQRBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		switch {
		case len(body.ItemIDs) == 0:
			problem.Write(w, r, problem.BadRequest("item_ids must contain at least one item id"))
			return
		case len(body.ItemIDs) > maxLabelBatchItems:
			problem.Write(w, r, problem.BadRequest(fmt.Sprintf("item_ids must contain at most %d item ids per request", maxLabelBatchItems)))
			return
		}

		found, err := scope.Items().GetByIDs(r.Context(), body.ItemIDs)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "labels qr batch: get items by ids failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		byID := make(map[string]storage.Item, len(found))
		for _, item := range found {
			byID[item.ID] = item
		}

		origin := qrOrigin(cfg, r)

		items := make([]labelsQRBatchItem, 0, len(body.ItemIDs))
		for _, id := range body.ItemIDs {
			item, ok := byID[id]
			if !ok {
				continue
			}

			svg, err := qrlabels.SVG(qrlabels.ItemURL(origin, item.ID))
			if err != nil {
				cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "labels qr batch: render QR SVG failed",
					slog.String("request_id", requestid.FromContext(r.Context())),
					slog.String("item_id", item.ID),
					slog.String("error", err.Error()),
				)
				problem.Write(w, r, problem.Internal())
				return
			}

			items = append(items, labelsQRBatchItem{
				ID:        item.ID,
				ShortCode: item.ShortCode,
				Name:      item.Name,
				QRSVG:     string(svg),
			})
		}

		writeJSON(w, r, http.StatusOK, labelsQRBatchResponse{Items: items})
	}
}

func qrOrigin(cfg Config, r *http.Request) string {
	if cfg.PublicBaseURLResolver != nil {
		return cfg.PublicBaseURLResolver(r)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
