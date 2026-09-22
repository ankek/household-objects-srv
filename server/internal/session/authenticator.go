package session

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"time"
)

const CookieName = "hho_session"

type Authenticator struct {
	storage *storage.Storage
}

var _ middleware.Authenticator = (*Authenticator)(nil)

func NewAuthenticator(store *storage.Storage) (*Authenticator, error) {
	if store == nil {
		return nil, errors.New("session: NewAuthenticator needs a *storage.Storage")
	}
	return &Authenticator{storage: store}, nil
}

func (a *Authenticator) Authenticate(r *http.Request) (middleware.Identity, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return middleware.Identity{}, fmt.Errorf("session: no session cookie present: %w", middleware.ErrUnauthenticated)
	}

	found, err := a.storage.SessionAuth(r.Context(), bearertoken.Hash(cookie.Value))
	switch {
	case errors.Is(err, storage.ErrSessionNotFound):
		return middleware.Identity{}, fmt.Errorf("session: no session matches the presented cookie: %w", middleware.ErrUnauthenticated)
	case err != nil:
		return middleware.Identity{}, fmt.Errorf("session: resolve session: %w", err)
	}

	if found.Revoked {
		return middleware.Identity{}, fmt.Errorf("session: session was revoked: %w", middleware.ErrUnauthenticated)
	}
	if !time.UnixMilli(found.ExpiresAtUnixMilli).After(time.Now()) {
		return middleware.Identity{}, fmt.Errorf("session: session has expired: %w", middleware.ErrUnauthenticated)
	}

	return middleware.Identity{Group: found.GroupID, UserID: found.UserID, Role: found.Role}, nil
}
