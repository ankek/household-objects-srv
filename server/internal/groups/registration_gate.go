package groups

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
)

type RegistrationGate struct {
	inner *Service
	open  bool
}

func NewRegistrationGate(inner *Service, open bool) (*RegistrationGate, error) {
	if inner == nil {
		return nil, errors.New("groups: NewRegistrationGate needs a *Service")
	}
	return &RegistrationGate{inner: inner, open: open}, nil
}

func (g *RegistrationGate) Register(ctx context.Context, req RegisterRequest) (Registered, error) {
	if !g.open {
		return g.inner.Register(ctx, req)
	}

	fallback := make([]byte, len(req.Password))
	copy(fallback, req.Password)
	defer auth.Zero(fallback)

	result, err := g.inner.Register(ctx, req)
	if err == nil || !errors.Is(err, ErrRegistrationClosed) {
		return result, err
	}
	return g.inner.registerNewGroup(ctx, RegisterRequest{Username: req.Username, Password: fallback})
}
