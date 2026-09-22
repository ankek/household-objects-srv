package httpapi

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"net/url"
)

func itemResolveHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")

		identity, err := cfg.Authenticator.Authenticate(r)
		switch {
		case errors.Is(err, middleware.ErrUnauthenticated):
			redirectToLogin(w, r, cfg, token)
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "resolve /i/{token}: authentication could not be performed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		if parsed, uerr := uuid.Parse(token); uerr == nil {
			redirectTo(w, r, cfg, "/items/"+parsed.String())
			return
		}

		group, err := storage.NewGroupID(identity.Group)
		if err != nil {
			resolveUnusableGroup(w, r, cfg, err)
			return
		}
		scope, err := cfg.Scopes.ForGroup(group)
		if err != nil {
			resolveUnusableGroup(w, r, cfg, err)
			return
		}

		item, err := scope.Items().GetByShortCode(r.Context(), token)
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "resolve /i/{token}: get item by short code failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		redirectTo(w, r, cfg, "/items/"+item.ID)
	}
}

func resolveUnusableGroup(w http.ResponseWriter, r *http.Request, cfg Config, err error) {
	cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "resolve /i/{token}: authenticated request resolved to no usable tenant scope",
		slog.String("request_id", requestid.FromContext(r.Context())),
		slog.String("error", err.Error()),
	)
	problem.Write(w, r, problem.Internal())
}

func publicBase(cfg Config, r *http.Request) string {
	if cfg.PublicBaseURLResolver == nil {
		return ""
	}
	return cfg.PublicBaseURLResolver(r)
}

func redirectTo(w http.ResponseWriter, r *http.Request, cfg Config, path string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, publicBase(cfg, r)+path, http.StatusFound)
}

func redirectToLogin(w http.ResponseWriter, r *http.Request, cfg Config, token string) {
	base := publicBase(cfg, r)
	next := base + r.URL.EscapedPath()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, base+"/login?next="+url.QueryEscape(next), http.StatusFound)
}
