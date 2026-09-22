package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	syncpkg "github.com/ankek/Household-Objects-Dev/server/internal/sync"
	"log/slog"
	"net/http"
)

const maxSyncPullLimit = 500

type syncPullRequestBody struct {
	DeviceID string `json:"device_id"`
	Since    int64  `json:"since"`
	Limit    int64  `json:"limit"`
}

type syncChangeEnvelope struct {
	EntityType     string `json:"entity_type"`
	ID             string `json:"id"`
	GroupChangeSeq int64  `json:"group_change_seq"`
	Data           any    `json:"data"`
}

type syncTombstoneBody struct {
	EntityType string `json:"entity_type"`
	ID         string `json:"id"`
	DeletedAt  int64  `json:"deleted_at"`
}

type syncPullResultBody struct {
	Changes       []syncChangeEnvelope `json:"changes"`
	Tombstones    []syncTombstoneBody  `json:"tombstones"`
	NextWatermark int64                `json:"next_watermark"`
	HasMore       bool                 `json:"has_more"`
}

type syncCursorTooOldBody struct {
	CursorTooOld bool `json:"cursor_too_old"`
}

type syncItemLabelAssignmentBody struct {
	ItemID  string `json:"item_id"`
	LabelID string `json:"label_id"`
}

func syncRepositoryFor(cfg Config, scope storage.Scope) (storage.SyncRepository, error) {
	if cfg.Store == nil {
		return nil, errors.New("httpapi: sync: no storage configured")
	}
	return cfg.Store.ForGroupSync(scope.GroupID())
}

func syncData[T any](c syncpkg.Change) (T, error) {
	v, ok := c.Data.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("sync: pull: %s entry carries unexpected data type %T", c.EntityType, c.Data)
	}
	return v, nil
}

func toSyncChangeData(c syncpkg.Change) (any, error) {
	switch c.EntityType {
	case syncpkg.EntityItem:
		row, err := syncData[storage.Item](c)
		if err != nil {
			return nil, err
		}
		return toItemBody(row), nil
	case syncpkg.EntityWarrantyBlock:
		row, err := syncData[storage.Warranty](c)
		if err != nil {
			return nil, err
		}
		return toWarrantyBody(row), nil
	case syncpkg.EntitySoldToBlock:
		row, err := syncData[storage.Sale](c)
		if err != nil {
			return nil, err
		}
		return toSaleBody(row), nil
	case syncpkg.EntityPurchasedFromBlock:
		row, err := syncData[storage.Purchase](c)
		if err != nil {
			return nil, err
		}
		return toPurchaseBody(row), nil
	case syncpkg.EntityItemIdentification:
		row, err := syncData[storage.Identification](c)
		if err != nil {
			return nil, err
		}
		return toIdentificationBody(row), nil
	case syncpkg.EntityItemCustomField:
		row, err := syncData[storage.ItemCustomField](c)
		if err != nil {
			return nil, err
		}
		return toItemCustomFieldBody(row), nil
	case syncpkg.EntityStockAdjustment:
		row, err := syncData[storage.StockAdjustment](c)
		if err != nil {
			return nil, err
		}
		return toStockAdjustmentBody(row), nil
	case syncpkg.EntityLocation:
		row, err := syncData[storage.Location](c)
		if err != nil {
			return nil, err
		}
		return toLocationBody(row), nil
	case syncpkg.EntityLabel:
		row, err := syncData[storage.Label](c)
		if err != nil {
			return nil, err
		}
		return toLabelBody(row), nil
	case syncpkg.EntityItemLabel:
		row, err := syncData[storage.ItemLabelAssignment](c)
		if err != nil {
			return nil, err
		}
		return syncItemLabelAssignmentBody{ItemID: row.ItemID, LabelID: row.LabelID}, nil
	case syncpkg.EntityAttachment:
		row, err := syncData[storage.Attachment](c)
		if err != nil {
			return nil, err
		}
		return toAttachmentBody(row), nil
	default:
		return nil, fmt.Errorf("sync: pull: unknown entity_type %q", c.EntityType)
	}
}

func toSyncChangeEnvelope(c syncpkg.Change) (syncChangeEnvelope, error) {
	data, err := toSyncChangeData(c)
	if err != nil {
		return syncChangeEnvelope{}, err
	}
	return syncChangeEnvelope{
		EntityType:     string(c.EntityType),
		ID:             c.ID,
		GroupChangeSeq: c.GroupChangeSeq,
		Data:           data,
	}, nil
}

func toSyncPullResultBody(page syncpkg.Page) (syncPullResultBody, error) {
	changes := make([]syncChangeEnvelope, 0, len(page.Changes))
	for _, c := range page.Changes {
		envelope, err := toSyncChangeEnvelope(c)
		if err != nil {
			return syncPullResultBody{}, err
		}
		changes = append(changes, envelope)
	}

	tombstones := make([]syncTombstoneBody, 0, len(page.Tombstones))
	for _, t := range page.Tombstones {
		tombstones = append(tombstones, syncTombstoneBody{
			EntityType: string(t.EntityType),
			ID:         t.ID,
			DeletedAt:  t.DeletedAt,
		})
	}

	return syncPullResultBody{
		Changes:       changes,
		Tombstones:    tombstones,
		NextWatermark: page.NextWatermark,
		HasMore:       page.HasMore,
	}, nil
}

func syncPullHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		var body syncPullRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}
		if body.DeviceID == "" {
			problem.Write(w, r, problem.BadRequest("device_id is required"))
			return
		}
		if body.Since < 0 {
			problem.Write(w, r, problem.BadRequest("since must not be negative"))
			return
		}
		if body.Limit <= 0 || body.Limit > maxSyncPullLimit {
			problem.Write(w, r, problem.BadRequest(
				fmt.Sprintf("limit must be an integer between 1 and %d", maxSyncPullLimit)))
			return
		}

		repo, err := syncRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "sync: pull: bind sync repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		reader := syncpkg.NewReader(repo)
		page, cursorTooOld, err := reader.PullOrTooOld(r.Context(), body.Since, body.Limit)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "sync: pull failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if cursorTooOld {
			writeJSON(w, r, http.StatusOK, syncCursorTooOldBody{CursorTooOld: true})
			return
		}

		result, err := toSyncPullResultBody(page)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "sync: pull: assemble envelope failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, result)
	}
}

func syncPushHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.NotImplemented())
	}
}
