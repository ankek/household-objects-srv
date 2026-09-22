package devicetoken

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"strings"
)

type Authenticator struct {
	storage *storage.Storage
}

var _ middleware.Authenticator = (*Authenticator)(nil)

func NewAuthenticator(store *storage.Storage) (*Authenticator, error) {
	if store == nil {
		return nil, errors.New("devicetoken: NewAuthenticator needs a *storage.Storage")
	}
	return &Authenticator{storage: store}, nil
}

func (a *Authenticator) Authenticate(r *http.Request) (middleware.Identity, error) {
	header := r.Header.Get("Authorization")
	scheme, token, hasScheme := strings.Cut(header, " ")
	if !hasScheme || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return middleware.Identity{}, fmt.Errorf("devicetoken: no bearer credential present: %w", middleware.ErrUnauthenticated)
	}

	resolved, err := a.storage.DeviceTokenAuth(r.Context(), bearertoken.Hash(token))
	switch {
	case errors.Is(err, storage.ErrDeviceTokenNotFound):
		return middleware.Identity{}, fmt.Errorf("devicetoken: no device token matches the presented bearer credential: %w", middleware.ErrUnauthenticated)
	case err != nil:
		return middleware.Identity{}, fmt.Errorf("devicetoken: resolve device token: %w", err)
	}

	if resolved.Revoked {
		return middleware.Identity{}, fmt.Errorf("devicetoken: device token was revoked: %w", middleware.ErrUnauthenticated)
	}

	return middleware.Identity{Group: resolved.GroupID, UserID: resolved.UserID, Role: resolved.Role}, nil
}
