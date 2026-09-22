package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

var ErrUserNotFound = errors.New("storage: user not found")

var ErrSessionNotFound = errors.New("storage: session not found")

type LoginUser struct {
	ID           string
	GroupID      string
	Username     string
	PasswordHash string
	Role         string
}

func (s *Storage) UserForLogin(ctx context.Context, username string) (LoginUser, error) {
	if s == nil || s.store == nil {
		return LoginUser{}, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	q := gen.New(s.store.Reader())
	row, err := q.GetUserForLogin(ctx, username)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return LoginUser{}, ErrUserNotFound
	case err != nil:
		return LoginUser{}, fmt.Errorf("storage: get user for login: %w", err)
	}
	return LoginUser{
		ID:           row.ID,
		GroupID:      row.GroupID,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
	}, nil
}

func (s *Storage) RehashPassword(ctx context.Context, groupID, userID, newHash string, now int64) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || userID == "" || newHash == "" {
		return errors.New("storage: RehashPassword needs a non-empty groupID, userID and newHash")
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		seq, err := db.AllocChangeSeq(ctx, tx, groupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for password rehash: %w", err)
		}

		rows, err := q.UpdateUserPasswordHash(ctx, gen.UpdateUserPasswordHashParams{
			PasswordHash: newHash,
			Now:          now,
			ChangeSeq:    seq,
			GroupID:      groupID,
			ID:           userID,
		})
		if err != nil {
			return fmt.Errorf("storage: rehash password: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: rehash password: %w", ErrUserNotFound)
		}
		return nil
	})
}

type SessionParams struct {
	ID            string
	GroupID       string
	UserID        string
	TokenHash     string
	ExpiresAt     int64
	UserAgent     string
	CreatedFromIP string
	Now           int64
}

func (p SessionParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: SessionParams: ID is empty")
	case p.GroupID == "":
		return errors.New("storage: SessionParams: GroupID is empty")
	case p.UserID == "":
		return errors.New("storage: SessionParams: UserID is empty")
	case p.TokenHash == "":
		return errors.New("storage: SessionParams: TokenHash is empty")
	case p.ExpiresAt <= 0:
		return errors.New("storage: SessionParams: ExpiresAt must be a positive Unix-millisecond timestamp")
	case p.Now <= 0:
		return errors.New("storage: SessionParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

func (s *Storage) CreateSession(ctx context.Context, p SessionParams) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if err := p.validate(); err != nil {
		return err
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		seq, err := db.AllocChangeSeq(ctx, tx, p.GroupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for session: %w", err)
		}

		if err := q.CreateSession(ctx, gen.CreateSessionParams{
			ID:            p.ID,
			GroupID:       p.GroupID,
			UserID:        p.UserID,
			TokenHash:     p.TokenHash,
			ExpiresAt:     p.ExpiresAt,
			UserAgent:     p.UserAgent,
			CreatedFromIp: p.CreatedFromIP,
			Now:           p.Now,
			ChangeSeq:     seq,
		}); err != nil {
			return fmt.Errorf("storage: create session: %w", err)
		}
		return nil
	})
}

type SessionAuth struct {
	GroupID            string
	UserID             string
	Role               string
	ExpiresAtUnixMilli int64
	Revoked            bool
}

func (s *Storage) SessionAuth(ctx context.Context, tokenHash string) (SessionAuth, error) {
	if s == nil || s.store == nil {
		return SessionAuth{}, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	q := gen.New(s.store.Reader())
	row, err := q.GetSessionForAuth(ctx, tokenHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return SessionAuth{}, ErrSessionNotFound
	case err != nil:
		return SessionAuth{}, fmt.Errorf("storage: get session for auth: %w", err)
	}
	return SessionAuth{
		GroupID:            row.GroupID,
		UserID:             row.UserID,
		Role:               row.Role,
		ExpiresAtUnixMilli: row.ExpiresAt,
		Revoked:            row.RevokedAt.Valid,
	}, nil
}
