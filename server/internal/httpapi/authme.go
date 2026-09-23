package httpapi

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
)

type authMeResponseBody struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func authMeHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		identity, ok := middleware.IdentityFromContext(r.Context())
		if !ok {
			problem.Write(w, r, problem.Internal())
			return
		}

		members, err := scope.Members().List(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "auth/me: list group members failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		username, found := usernameForCaller(members, identity.UserID)
		if !found {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "auth/me: authenticated user not found in own group's roster",
				slog.String("request_id", requestid.FromContext(r.Context())),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusOK, authMeResponseBody{
			GroupID:  identity.Group,
			UserID:   identity.UserID,
			Username: username,
			Role:     identity.Role,
		})
	}
}

func usernameForCaller(members []storage.Member, userID string) (username string, found bool) {
	for _, m := range members {
		if m.ID == userID {
			return m.Username, true
		}
	}
	return "", false
}
