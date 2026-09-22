package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/roles"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"modernc.org/sqlite"
)

var ErrInviteNotRedeemable = errors.New("storage: invite is not redeemable")

var ErrUsernameTaken = errors.New("storage: username is already taken")

const sqliteConstraintUniqueCode = 2067

func isUniqueConstraintViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.Code() == sqliteConstraintUniqueCode
}

func (s *Storage) InviteRedeemable(ctx context.Context, tokenHash string, now int64) (bool, error) {
	if s == nil || s.store == nil {
		return false, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if tokenHash == "" {
		return false, errors.New("storage: InviteRedeemable needs a non-empty tokenHash")
	}
	if now <= 0 {
		return false, errors.New("storage: InviteRedeemable needs a positive Unix-millisecond now")
	}

	q := gen.New(s.store.Reader())
	row, err := q.GetInviteForRedeem(ctx, tokenHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("storage: get invite for redeem: %w", err)
	}
	return !row.RedeemedAt.Valid && row.ExpiresAt > now, nil
}

type RedeemInviteParams struct {
	TokenHash    string
	UserID       string
	Username     string
	PasswordHash string
	Now          int64
}

func (p RedeemInviteParams) validate() error {
	switch {
	case p.TokenHash == "":
		return errors.New("storage: RedeemInviteParams: TokenHash is empty")
	case p.UserID == "":
		return errors.New("storage: RedeemInviteParams: UserID is empty")
	case p.Username == "":
		return errors.New("storage: RedeemInviteParams: Username is empty")
	case p.PasswordHash == "":
		return errors.New("storage: RedeemInviteParams: PasswordHash is empty")
	case p.Now <= 0:
		return errors.New("storage: RedeemInviteParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type RedeemedInvite struct {
	GroupID  string
	UserID   string
	Username string
}

func (s *Storage) RedeemInvite(ctx context.Context, p RedeemInviteParams) (RedeemedInvite, error) {
	if s == nil || s.store == nil {
		return RedeemedInvite{}, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if err := p.validate(); err != nil {
		return RedeemedInvite{}, err
	}

	var result RedeemedInvite
	err := s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		current, err := q.GetInviteForRedeem(ctx, p.TokenHash)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrInviteNotRedeemable
		case err != nil:
			return fmt.Errorf("storage: get invite for redeem: %w", err)
		}

		seq, err := db.AllocChangeSeq(ctx, tx, current.GroupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for invite redemption: %w", err)
		}

		if err := q.CreateUser(ctx, gen.CreateUserParams{
			ID:           p.UserID,
			GroupID:      current.GroupID,
			Username:     p.Username,
			PasswordHash: p.PasswordHash,
			Role:         roles.Member,
			Now:          p.Now,
			ChangeSeq:    seq,
		}); err != nil {
			if isUniqueConstraintViolation(err) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("storage: create invited user: %w", err)
		}

		rows, err := q.MarkInviteRedeemed(ctx, gen.MarkInviteRedeemedParams{
			Now:              sql.NullInt64{Int64: p.Now, Valid: true},
			RedeemedByUserID: sql.NullString{String: p.UserID, Valid: true},
			ChangeSeq:        seq,
			GroupID:          current.GroupID,
			TokenHash:        p.TokenHash,
		})
		if err != nil {
			return fmt.Errorf("storage: mark invite redeemed: %w", err)
		}
		if rows != 1 {
			return ErrInviteNotRedeemable
		}

		result = RedeemedInvite{GroupID: current.GroupID, UserID: p.UserID, Username: p.Username}
		return nil
	})
	if err != nil {
		return RedeemedInvite{}, err
	}
	return result, nil
}
