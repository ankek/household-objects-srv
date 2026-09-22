package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"strings"
	"time"
)

var ErrInvalidCredentials = errors.New("session: invalid username or password")

const DefaultSessionTTL = 30 * 24 * time.Hour

type LoginRequest struct {
	Username   string
	Password   []byte
	UserAgent  string
	RemoteAddr string
}

type LoggedIn struct {
	GroupID   string
	UserID    string
	Username  string
	Role      string
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	storage *storage.Storage
	hasher  *auth.Hasher
	ttl     time.Duration
}

func NewService(store *storage.Storage, hasher *auth.Hasher) (*Service, error) {
	if store == nil {
		return nil, errors.New("session: NewService needs a *storage.Storage")
	}
	if hasher == nil {
		return nil, errors.New("session: NewService needs an *auth.Hasher")
	}
	return &Service{storage: store, hasher: hasher, ttl: DefaultSessionTTL}, nil
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (LoggedIn, error) {
	defer auth.Zero(req.Password)

	username := strings.TrimSpace(req.Username)

	user, err := s.storage.UserForLogin(ctx, username)
	switch {
	case errors.Is(err, storage.ErrUserNotFound):
		return LoggedIn{}, s.declineAbsent(ctx, req.Password)
	case err != nil:
		return LoggedIn{}, fmt.Errorf("session: look up user: %w", err)
	}

	if verr := s.hasher.Verify(ctx, user.PasswordHash, req.Password); verr != nil {
		if errors.Is(verr, auth.ErrMismatch) || errors.Is(verr, auth.ErrMalformedHash) {
			return LoggedIn{}, fmt.Errorf("%w: %w", ErrInvalidCredentials, verr)
		}
		return LoggedIn{}, fmt.Errorf("session: verify password: %w", verr)
	}

	if s.hasher.NeedsRehash(user.PasswordHash) {
		if newHash, herr := s.hasher.Hash(ctx, req.Password); herr == nil {
			_ = s.storage.RehashPassword(ctx, user.GroupID, user.ID, newHash, time.Now().UnixMilli())
		}
	}

	return s.mintSession(ctx, user, req.UserAgent, req.RemoteAddr)
}

func (s *Service) declineAbsent(ctx context.Context, password []byte) error {
	if verr := s.hasher.VerifyAbsent(ctx, password); verr != nil && !errors.Is(verr, auth.ErrMismatch) {
		return fmt.Errorf("session: verify absent: %w", verr)
	}
	return ErrInvalidCredentials
}

func (s *Service) mintSession(ctx context.Context, user storage.LoginUser, userAgent, remoteAddr string) (LoggedIn, error) {
	token, err := bearertoken.New()
	if err != nil {
		return LoggedIn{}, fmt.Errorf("session: draw a token: %w", err)
	}

	id, err := newID()
	if err != nil {
		return LoggedIn{}, err
	}

	now := time.Now()
	expiresAt := now.Add(s.ttl)

	if err := s.storage.CreateSession(ctx, storage.SessionParams{
		ID:            id,
		GroupID:       user.GroupID,
		UserID:        user.ID,
		TokenHash:     bearertoken.Hash(token),
		ExpiresAt:     expiresAt.UnixMilli(),
		UserAgent:     userAgent,
		CreatedFromIP: remoteAddr,
		Now:           now.UnixMilli(),
	}); err != nil {
		return LoggedIn{}, fmt.Errorf("session: create session: %w", err)
	}

	return LoggedIn{
		GroupID:   user.GroupID,
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("session: generate id: %w", err)
	}
	return id.String(), nil
}
