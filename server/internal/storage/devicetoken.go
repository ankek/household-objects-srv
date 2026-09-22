package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

var ErrDeviceTokenNotFound = errors.New("storage: device token not found")

type DeviceTokenParams struct {
	ID          string
	GroupID     string
	UserID      string
	TokenHash   string
	DeviceLabel string
	Now         int64
}

func (p DeviceTokenParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: DeviceTokenParams: ID is empty")
	case p.GroupID == "":
		return errors.New("storage: DeviceTokenParams: GroupID is empty")
	case p.UserID == "":
		return errors.New("storage: DeviceTokenParams: UserID is empty")
	case p.TokenHash == "":
		return errors.New("storage: DeviceTokenParams: TokenHash is empty")
	case p.DeviceLabel == "":
		return errors.New("storage: DeviceTokenParams: DeviceLabel is empty")
	case p.Now <= 0:
		return errors.New("storage: DeviceTokenParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

func (s *Storage) CreateDeviceToken(ctx context.Context, p DeviceTokenParams) error {
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
			return fmt.Errorf("storage: allocate change_seq for device token: %w", err)
		}

		if err := q.CreateDeviceToken(ctx, gen.CreateDeviceTokenParams{
			ID:          p.ID,
			GroupID:     p.GroupID,
			UserID:      p.UserID,
			TokenHash:   p.TokenHash,
			DeviceLabel: p.DeviceLabel,
			Now:         p.Now,
			ChangeSeq:   seq,
		}); err != nil {
			return fmt.Errorf("storage: create device token: %w", err)
		}
		return nil
	})
}

type DeviceTokenAuth struct {
	GroupID string
	UserID  string
	Role    string
	Revoked bool
}

func (s *Storage) DeviceTokenAuth(ctx context.Context, tokenHash string) (DeviceTokenAuth, error) {
	if s == nil || s.store == nil {
		return DeviceTokenAuth{}, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	q := gen.New(s.store.Reader())
	row, err := q.GetDeviceTokenForAuth(ctx, tokenHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return DeviceTokenAuth{}, ErrDeviceTokenNotFound
	case err != nil:
		return DeviceTokenAuth{}, fmt.Errorf("storage: get device token for auth: %w", err)
	}
	return DeviceTokenAuth{
		GroupID: row.GroupID,
		UserID:  row.UserID,
		Role:    row.Role,
		Revoked: row.RevokedAt.Valid,
	}, nil
}
