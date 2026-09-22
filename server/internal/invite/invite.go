package invite

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"time"
)

const DefaultTTL = 24 * time.Hour

var ErrGroupIDRequired = errors.New("invite: GroupID is required")

var ErrCreatedByUserIDRequired = errors.New("invite: CreatedByUserID is required")

type Service struct {
	storage *storage.Storage
	hasher  *auth.Hasher
	ttl     time.Duration
}

func NewService(store *storage.Storage, hasher *auth.Hasher) (*Service, error) {
	if store == nil {
		return nil, errors.New("invite: NewService needs a *storage.Storage")
	}
	if hasher == nil {
		return nil, errors.New("invite: NewService needs an *auth.Hasher")
	}
	return &Service{storage: store, hasher: hasher, ttl: DefaultTTL}, nil
}

type IssueRequest struct {
	GroupID         string
	CreatedByUserID string
}

type Issued struct {
	ID        string
	Token     string
	ExpiresAt time.Time
}

func (s *Service) Issue(ctx context.Context, req IssueRequest) (Issued, error) {
	if req.GroupID == "" {
		return Issued{}, ErrGroupIDRequired
	}
	if req.CreatedByUserID == "" {
		return Issued{}, ErrCreatedByUserIDRequired
	}

	token, err := bearertoken.New()
	if err != nil {
		return Issued{}, fmt.Errorf("invite: draw a token: %w", err)
	}

	id, err := newID()
	if err != nil {
		return Issued{}, err
	}

	now := time.Now()
	expiresAt := now.Add(s.ttl)

	if err := s.storage.CreateInvite(ctx, storage.InviteParams{
		ID:              id,
		GroupID:         req.GroupID,
		CreatedByUserID: req.CreatedByUserID,
		TokenHash:       bearertoken.Hash(token),
		ExpiresAt:       expiresAt.UnixMilli(),
		Now:             now.UnixMilli(),
	}); err != nil {
		return Issued{}, fmt.Errorf("invite: create invite: %w", err)
	}

	return Issued{ID: id, Token: token, ExpiresAt: expiresAt}, nil
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("invite: generate id: %w", err)
	}
	return id.String(), nil
}
