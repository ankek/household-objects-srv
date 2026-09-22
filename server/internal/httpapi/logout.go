package httpapi

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"log/slog"
	"net/http"
	"time"
)

func logoutHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		cookie, err := r.Cookie(session.CookieName)
		if err != nil || cookie.Value == "" {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "logout reached with no session cookie",
				slog.String("request_id", requestid.FromContext(r.Context())),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if err := cfg.Sessions.RevokeCallingSession(
			r.Context(), identity.Group, identity.UserID, bearertoken.Hash(cookie.Value), time.Now().UnixMilli(),
		); err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "logout failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		clearSessionCookie(w)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
