package middleware

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/roles"
	"log/slog"
	"net/http"
)

func RequireOwner(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		panic("middleware: RequireOwner needs a Logger; a mis-mounted route would answer 500 with nothing to report why")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFromContext(r.Context())
			if !ok {
				logger.LogAttrs(r.Context(), slog.LevelError, "RequireOwner ran with no identity in context; it is mounted outside TenantScope",
					slog.String("request_id", requestid.FromContext(r.Context())),
				)
				problem.Write(w, r, problem.Internal())
				return
			}

			if identity.Role != roles.Owner {
				problem.Write(w, r, problem.Forbidden())
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
