package middleware

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
)

var ErrUnauthenticated = errors.New("middleware: request carries no valid credential")

type Identity struct {
	Group string

	UserID string

	Role string
}

type Authenticator interface {
	Authenticate(r *http.Request) (Identity, error)
}

type AuthenticatorFunc func(r *http.Request) (Identity, error)

func (f AuthenticatorFunc) Authenticate(r *http.Request) (Identity, error) { return f(r) }

type ScopeSource interface {
	ForGroup(storage.GroupID) (storage.Scope, error)
}

var _ ScopeSource = (*storage.Storage)(nil)

type tenantContextKey struct{}

type boundTenant struct {
	scope    storage.Scope
	identity Identity
}

func TenantScope(auth Authenticator, scopes ScopeSource, logger *slog.Logger) func(http.Handler) http.Handler {
	switch {
	case auth == nil:
		panic("middleware: TenantScope needs an Authenticator; without one the authenticated route group authenticates nothing")
	case scopes == nil:
		panic("middleware: TenantScope needs a ScopeSource; without one no request can be bound to a group and every route below would answer 500")
	case logger == nil:
		panic("middleware: TenantScope needs a Logger; the failure paths below are the only report an operator gets that authentication is broken")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, err := auth.Authenticate(r)
			switch {
			case errors.Is(err, ErrUnauthenticated):
				unauthenticated(w, r)
				return
			case err != nil:
				logger.LogAttrs(r.Context(), slog.LevelError, "authentication could not be performed",
					slog.String("request_id", requestid.FromContext(r.Context())),
					slog.String("error", errSummary(err)),
				)
				problem.Write(w, r, problem.Internal())
				return
			}

			group, err := storage.NewGroupID(identity.Group)
			if err != nil {
				unusableGroup(w, r, logger, err)
				return
			}
			scope, err := scopes.ForGroup(group)
			if err != nil {
				unusableGroup(w, r, logger, err)
				return
			}

			ctx := context.WithValue(r.Context(), tenantContextKey{}, boundTenant{scope: scope, identity: identity})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func unauthenticated(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	problem.Write(w, r, problem.Unauthorized())
}

func unusableGroup(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	logger.LogAttrs(r.Context(), slog.LevelError, "authenticated request resolved to no usable tenant scope",
		slog.String("request_id", requestid.FromContext(r.Context())),
		slog.String("error", err.Error()),
	)
	problem.Write(w, r, problem.Internal())
}

func errSummary(err error) string {
	return fmt.Sprintf("error of type %T (contents withheld: NFR-018)", err)
}

type ScopedHandler func(w http.ResponseWriter, r *http.Request, scope storage.Scope)

func Scoped(h ScopedHandler) http.HandlerFunc {
	if h == nil {
		panic("middleware: Scoped was given a nil handler; the route would answer 500 for the life of the process")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		bound, ok := r.Context().Value(tenantContextKey{}).(boundTenant)
		if !ok || bound.scope == nil {
			problem.Write(w, r, problem.Internal())
			return
		}
		h(w, r, bound.scope)
	}
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	bound, ok := ctx.Value(tenantContextKey{}).(boundTenant)
	return bound.identity, ok
}
