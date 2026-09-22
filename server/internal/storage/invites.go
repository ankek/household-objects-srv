package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type InviteParams struct {
	ID              string
	GroupID         string
	CreatedByUserID string
	TokenHash       string
	ExpiresAt       int64
	Now             int64
}

func (p InviteParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: InviteParams: ID is empty")
	case p.GroupID == "":
		return errors.New("storage: InviteParams: GroupID is empty")
	case p.CreatedByUserID == "":
		return errors.New("storage: InviteParams: CreatedByUserID is empty")
	case p.TokenHash == "":
		return errors.New("storage: InviteParams: TokenHash is empty")
	case p.ExpiresAt <= 0:
		return errors.New("storage: InviteParams: ExpiresAt must be a positive Unix-millisecond timestamp")
	case p.Now <= 0:
		return errors.New("storage: InviteParams: Now must be a positive Unix-millisecond timestamp")
	case p.ExpiresAt <= p.Now:
		return errors.New("storage: InviteParams: ExpiresAt must be after Now")
	}
	return nil
}

func (s *Storage) CreateInvite(ctx context.Context, p InviteParams) error {
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
			return fmt.Errorf("storage: allocate change_seq for invite: %w", err)
		}

		if err := q.CreateInvite(ctx, gen.CreateInviteParams{
			ID:              p.ID,
			GroupID:         p.GroupID,
			TokenHash:       p.TokenHash,
			CreatedByUserID: p.CreatedByUserID,
			ExpiresAt:       p.ExpiresAt,
			Now:             p.Now,
			ChangeSeq:       seq,
		}); err != nil {
			return fmt.Errorf("storage: create invite: %w", err)
		}
		return nil
	})
}
