package groups

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"strings"
	"time"
)

const DefaultGroupName = "Household"

const (
	minUsernameLen = 1
	maxUsernameLen = 64
	minPasswordLen = 8
	maxPasswordLen = 256
)

var (
	ErrUsernameInvalid    = errors.New("groups: username is invalid")
	ErrPasswordInvalid    = errors.New("groups: password is invalid")
	ErrRegistrationClosed = storage.ErrRegistrationClosed
)

type RegisterRequest struct {
	Username string
	Password []byte
}

type Registered struct {
	GroupID  string
	UserID   string
	Username string
}

type Service struct {
	storage *storage.Storage
	hasher  *auth.Hasher
}

func NewService(store *storage.Storage, hasher *auth.Hasher) (*Service, error) {
	if store == nil {
		return nil, errors.New("groups: NewService needs a *storage.Storage")
	}
	if hasher == nil {
		return nil, errors.New("groups: NewService needs an *auth.Hasher")
	}
	return &Service{storage: store, hasher: hasher}, nil
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (Registered, error) {
	defer auth.Zero(req.Password)

	username := strings.TrimSpace(req.Username)
	if l := len(username); l < minUsernameLen || l > maxUsernameLen {
		return Registered{}, fmt.Errorf("%w: must be %d-%d bytes", ErrUsernameInvalid, minUsernameLen, maxUsernameLen)
	}
	if l := len(req.Password); l < minPasswordLen || l > maxPasswordLen {
		return Registered{}, fmt.Errorf("%w: must be %d-%d bytes", ErrPasswordInvalid, minPasswordLen, maxPasswordLen)
	}

	exists, err := s.storage.HasAnyGroup(ctx)
	if err != nil {
		return Registered{}, fmt.Errorf("groups: check for an existing group: %w", err)
	}
	if exists {
		return Registered{}, ErrRegistrationClosed
	}

	groupID, err := newID()
	if err != nil {
		return Registered{}, err
	}
	userID, err := newID()
	if err != nil {
		return Registered{}, err
	}

	hash, err := s.hasher.Hash(ctx, req.Password)
	if err != nil {
		return Registered{}, fmt.Errorf("groups: hash password: %w", err)
	}

	if err := s.storage.RegisterFirstUser(ctx, storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    DefaultGroupName,
		UserID:       userID,
		Username:     username,
		PasswordHash: hash,
		Now:          time.Now().UnixMilli(),
	}); err != nil {
		return Registered{}, err
	}

	return Registered{GroupID: groupID, UserID: userID, Username: username}, nil
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("groups: generate id: %w", err)
	}
	return id.String(), nil
}
