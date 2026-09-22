package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/roles"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

var ErrRegistrationClosed = errors.New("storage: registration is closed; a group already exists")

func (s *Storage) HasAnyGroup(ctx context.Context) (bool, error) {
	if s == nil || s.store == nil {
		return false, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	q := gen.New(s.store.Reader())
	count, err := q.CountGroups(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: count groups: %w", err)
	}
	return count > 0, nil
}

const ownerRole = roles.Owner

type FirstUserParams struct {
	GroupID      string
	GroupName    string
	UserID       string
	Username     string
	PasswordHash string
	Now          int64
}

func (s *Storage) RegisterFirstUser(ctx context.Context, p FirstUserParams) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if err := p.validate(); err != nil {
		return err
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		rows, err := q.CreateGroupIfNone(ctx, gen.CreateGroupIfNoneParams{
			ID:   p.GroupID,
			Name: p.GroupName,
			Now:  p.Now,
		})
		if err != nil {
			return fmt.Errorf("storage: create first group: %w", err)
		}
		if rows == 0 {
			return ErrRegistrationClosed
		}

		seq, err := db.AllocChangeSeq(ctx, tx, p.GroupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for first user: %w", err)
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
			return fmt.Errorf("storage: create first user: %w", err)
		}
		return nil
	})
}

func (p FirstUserParams) validate() error {
	switch {
	case p.GroupID == "":
		return errors.New("storage: RegisterFirstUser: GroupID is empty")
	case p.GroupName == "":
		return errors.New("storage: RegisterFirstUser: GroupName is empty")
	case p.UserID == "":
		return errors.New("storage: RegisterFirstUser: UserID is empty")
	case p.Username == "":
		return errors.New("storage: RegisterFirstUser: Username is empty")
	case p.PasswordHash == "":
		return errors.New("storage: RegisterFirstUser: PasswordHash is empty")
	case p.Now <= 0:
		return errors.New("storage: RegisterFirstUser: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}
