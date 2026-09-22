package storage

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

func (s *Storage) RegisterNewGroup(ctx context.Context, p FirstUserParams) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if err := p.validate(); err != nil {
		return err
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		if err := q.CreateGroup(ctx, gen.CreateGroupParams{
			ID:   p.GroupID,
			Name: p.GroupName,
			Now:  p.Now,
		}); err != nil {
			return fmt.Errorf("storage: create new group: %w", err)
		}

		seq, err := db.AllocChangeSeq(ctx, tx, p.GroupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for new group's owner: %w", err)
		}

		if err := q.CreateUser(ctx, gen.CreateUserParams{
			ID:           p.UserID,
			GroupID:      p.GroupID,
			Username:     p.Username,
			PasswordHash: p.PasswordHash,
			Role:         ownerRole,
			Now:          p.Now,
			ChangeSeq:    seq,
		}); err != nil {
			return fmt.Errorf("storage: create new group's owner: %w", err)
		}
		return nil
	})
}
