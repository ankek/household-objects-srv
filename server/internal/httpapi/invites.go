package httpapi

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type InviteIssuer interface {
	Issue(ctx context.Context, req invite.IssueRequest) (invite.Issued, error)
}

var _ InviteIssuer = (*invite.Service)(nil)

type InviteAdmin interface {
	ListInvites(ctx context.Context, groupID string) ([]storage.InviteInfo, error)
	RevokeInvite(ctx context.Context, groupID, inviteID string, now int64) error
}

var _ InviteAdmin = (*storage.Storage)(nil)

type inviteCreateResponseBody struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

func inviteCreateHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		result, err := cfg.InviteService.Issue(r.Context(), invite.IssueRequest{
			GroupID:         identity.Group,
			CreatedByUserID: identity.UserID,
		})
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "invite issuance failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, inviteCreateResponseBody{
			ID:        result.ID,
			Token:     result.Token,
			ExpiresAt: result.ExpiresAt.UnixMilli(),
		})
	}
}

type inviteListItem struct {
	ID               string `json:"id"`
	CreatedByUserID  string `json:"created_by_user_id"`
	CreatedAt        int64  `json:"created_at"`
	ExpiresAt        int64  `json:"expires_at"`
	Redeemed         bool   `json:"redeemed"`
	RedeemedAt       int64  `json:"redeemed_at,omitempty"`
	RedeemedByUserID string `json:"redeemed_by_user_id,omitempty"`
}

type inviteListResponse struct {
	Invites []inviteListItem `json:"invites"`
}

func toInviteListItem(inv storage.InviteInfo) inviteListItem {
	item := inviteListItem{
		ID:              inv.ID,
		CreatedByUserID: inv.CreatedByUserID,
		CreatedAt:       inv.CreatedAtUnixMilli,
		ExpiresAt:       inv.ExpiresAtUnixMilli,
		Redeemed:        inv.Redeemed,
	}
	if inv.Redeemed {
		item.RedeemedAt = inv.RedeemedAtUnixMilli
		item.RedeemedByUserID = inv.RedeemedByUserID
	}
	return item
}

func inviteListHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		invites, err := cfg.Invites.ListInvites(r.Context(), identity.Group)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list invites failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		items := make([]inviteListItem, 0, len(invites))
		for _, inv := range invites {
			items = append(items, toInviteListItem(inv))
		}
		writeJSON(w, r, http.StatusOK, inviteListResponse{Invites: items})
	}
}

func inviteRevokeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		inviteID := chi.URLParam(r, "inviteID")
		err := cfg.Invites.RevokeInvite(r.Context(), identity.Group, inviteID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "revoke invite failed",
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
