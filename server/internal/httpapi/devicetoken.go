package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/devicetoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type DeviceTokenIssuer interface {
	Issue(ctx context.Context, req devicetoken.IssueRequest) (devicetoken.Issued, error)
}

var _ DeviceTokenIssuer = (*devicetoken.Service)(nil)

type deviceTokenIssueRequestBody struct {
	DeviceLabel string `json:"device_label"`
}

type deviceTokenIssueResponseBody struct {
	ID          string `json:"id"`
	DeviceLabel string `json:"device_label"`
	Token       string `json:"token"`
}

func deviceTokenIssueHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		var body deviceTokenIssueRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		result, err := cfg.DeviceTokenService.Issue(r.Context(), devicetoken.IssueRequest{
			GroupID:     identity.Group,
			UserID:      identity.UserID,
			DeviceLabel: body.DeviceLabel,
		})
		switch {
		case errors.Is(err, devicetoken.ErrDeviceLabelRequired), errors.Is(err, devicetoken.ErrDeviceLabelTooLong):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "device token issuance failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		respBody, err := json.Marshal(deviceTokenIssueResponseBody{
			ID:          result.ID,
			DeviceLabel: result.DeviceLabel,
			Token:       result.Token,
		})
		if err != nil {
			problem.Write(w, r, problem.Internal())
			return
		}

		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(respBody)
	}
}

type DeviceTokenAdmin interface {
	ListDeviceTokens(ctx context.Context, groupID, userID string) ([]storage.DeviceTokenInfo, error)
	RevokeDeviceToken(ctx context.Context, groupID, userID, deviceTokenID string, now int64) error
}

var _ DeviceTokenAdmin = (*storage.Storage)(nil)

type deviceTokenListItem struct {
	ID          string `json:"id"`
	DeviceLabel string `json:"device_label"`
	CreatedAt   int64  `json:"created_at"`
	Revoked     bool   `json:"revoked"`
	RevokedAt   int64  `json:"revoked_at,omitempty"`
}

type deviceTokenListResponse struct {
	DeviceTokens []deviceTokenListItem `json:"device_tokens"`
}

func toDeviceTokenListItem(d storage.DeviceTokenInfo) deviceTokenListItem {
	item := deviceTokenListItem{
		ID:          d.ID,
		DeviceLabel: d.DeviceLabel,
		CreatedAt:   d.CreatedAtUnixMilli,
		Revoked:     d.Revoked,
	}
	if d.Revoked {
		item.RevokedAt = d.RevokedAtUnixMilli
	}
	return item
}

func deviceTokensListHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		tokens, err := cfg.DeviceTokens.ListDeviceTokens(r.Context(), identity.Group, identity.UserID)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list device tokens failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		items := make([]deviceTokenListItem, 0, len(tokens))
		for _, d := range tokens {
			items = append(items, toDeviceTokenListItem(d))
		}
		writeJSON(w, r, http.StatusOK, deviceTokenListResponse{DeviceTokens: items})
	}
}

func deviceTokenRevokeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		deviceTokenID := chi.URLParam(r, "deviceTokenID")
		err := cfg.DeviceTokens.RevokeDeviceToken(r.Context(), identity.Group, identity.UserID, deviceTokenID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "revoke device token failed",
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
