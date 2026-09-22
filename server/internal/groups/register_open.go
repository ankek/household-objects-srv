package groups

import (
	"context"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"strings"
	"time"
)

func (s *Service) registerNewGroup(ctx context.Context, req RegisterRequest) (Registered, error) {
	defer auth.Zero(req.Password)

	username := strings.TrimSpace(req.Username)
	if l := len(username); l < minUsernameLen || l > maxUsernameLen {
		return Registered{}, fmt.Errorf("%w: must be %d-%d bytes", ErrUsernameInvalid, minUsernameLen, maxUsernameLen)
	}
	if l := len(req.Password); l < minPasswordLen || l > maxPasswordLen {
		return Registered{}, fmt.Errorf("%w: must be %d-%d bytes", ErrPasswordInvalid, minPasswordLen, maxPasswordLen)
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

	if err := s.storage.RegisterNewGroup(ctx, storage.FirstUserParams{
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
