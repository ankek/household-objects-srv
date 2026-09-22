package invite

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"strings"
	"time"
)

const (
	minUsernameLen = 1
	maxUsernameLen = 64
	minPasswordLen = 8
	maxPasswordLen = 256
)

var (
	ErrTokenRequired   = errors.New("invite: Token is required")
	ErrUsernameInvalid = errors.New("invite: username is invalid")
	ErrPasswordInvalid = errors.New("invite: password is invalid")
	ErrNotRedeemable   = storage.ErrInviteNotRedeemable
	ErrUsernameTaken   = storage.ErrUsernameTaken
)

type RedeemRequest struct {
	Token    string
	Username string
	Password []byte
}

type Redeemed struct {
	GroupID  string
	UserID   string
	Username string
}

func (s *Service) Redeem(ctx context.Context, req RedeemRequest) (Redeemed, error) {
	defer auth.Zero(req.Password)

	token := strings.TrimSpace(req.Token)
	if token == "" {
		return Redeemed{}, ErrTokenRequired
	}
	username := strings.TrimSpace(req.Username)
	if l := len(username); l < minUsernameLen || l > maxUsernameLen {
		return Redeemed{}, fmt.Errorf("%w: must be %d-%d bytes", ErrUsernameInvalid, minUsernameLen, maxUsernameLen)
	}
	if l := len(req.Password); l < minPasswordLen || l > maxPasswordLen {
		return Redeemed{}, fmt.Errorf("%w: must be %d-%d bytes", ErrPasswordInvalid, minPasswordLen, maxPasswordLen)
	}

	tokenHash := bearertoken.Hash(token)
	now := time.Now()

	redeemable, err := s.storage.InviteRedeemable(ctx, tokenHash, now.UnixMilli())
	if err != nil {
		return Redeemed{}, fmt.Errorf("invite: check invite redeemability: %w", err)
	}
	if !redeemable {
		return Redeemed{}, ErrNotRedeemable
	}

	hash, err := s.hasher.Hash(ctx, req.Password)
	if err != nil {
		return Redeemed{}, fmt.Errorf("invite: hash password: %w", err)
	}

	userID, err := newID()
	if err != nil {
		return Redeemed{}, err
	}

	result, err := s.storage.RedeemInvite(ctx, storage.RedeemInviteParams{
		TokenHash:    tokenHash,
		UserID:       userID,
		Username:     username,
		PasswordHash: hash,
		Now:          now.UnixMilli(),
	})
	if err != nil {
		return Redeemed{}, err
	}

	return Redeemed{GroupID: result.GroupID, UserID: result.UserID, Username: result.Username}, nil
}
