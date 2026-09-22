package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

type SessionAdmin interface {
	ListSessions(ctx context.Context, groupID, userID string) ([]storage.SessionInfo, error)
	RevokeSession(ctx context.Context, groupID, userID, sessionID string, now int64) error

	RevokeCallingSession(ctx context.Context, groupID, userID, tokenHash string, now int64) error
}

var _ SessionAdmin = (*storage.Storage)(nil)

type sessionListItem struct {
	ID            string `json:"id"`
	UserAgent     string `json:"user_agent"`
	CreatedFromIP string `json:"created_from_ip"`
	CreatedAt     int64  `json:"created_at"`
	ExpiresAt     int64  `json:"expires_at"`
	Revoked       bool   `json:"revoked"`
	RevokedAt     int64  `json:"revoked_at,omitempty"`
}

type sessionListResponse struct {
	Sessions []sessionListItem `json:"sessions"`
}

func toSessionListItem(s storage.SessionInfo) sessionListItem {
	item := sessionListItem{
		ID:            s.ID,
		UserAgent:     s.UserAgent,
		CreatedFromIP: s.CreatedFromIP,
		CreatedAt:     s.CreatedAtUnixMilli,
		ExpiresAt:     s.ExpiresAtUnixMilli,
		Revoked:       s.Revoked,
	}
	if s.Revoked {
		item.RevokedAt = s.RevokedAtUnixMilli
	}
	return item
}

func sessionsListHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		sessions, err := cfg.Sessions.ListSessions(r.Context(), identity.Group, identity.UserID)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list sessions failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		items := make([]sessionListItem, 0, len(sessions))
		for _, s := range sessions {
			items = append(items, toSessionListItem(s))
		}
		writeJSON(w, r, http.StatusOK, sessionListResponse{Sessions: items})
	}
}

func sessionRevokeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		sessionID := chi.URLParam(r, "sessionID")
		err := cfg.Sessions.RevokeSession(r.Context(), identity.Group, identity.UserID, sessionID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "revoke session failed",
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

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		problem.Write(w, r, problem.Internal())
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
