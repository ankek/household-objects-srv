package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type ResetPasswordResult struct {
	UserID              string
	GroupID             string
	SessionsRevoked     int64
	DeviceTokensRevoked int64
}

func (s *Storage) ResetPassword(ctx context.Context, username, newPasswordHash string, now int64) (ResetPasswordResult, error) {
	if s == nil || s.store == nil {
		return ResetPasswordResult{}, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if username == "" {
		return ResetPasswordResult{}, errors.New("storage: ResetPassword needs a non-empty username")
	}
	if newPasswordHash == "" {
		return ResetPasswordResult{}, errors.New("storage: ResetPassword needs a non-empty newPasswordHash")
	}
	if now <= 0 {
		return ResetPasswordResult{}, errors.New("storage: ResetPassword needs a positive Unix-millisecond now")
	}

	target, err := s.UserForLogin(ctx, username)
	if err != nil {
		return ResetPasswordResult{}, err
	}

	var result ResetPasswordResult
	err = s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		seq, err := db.AllocChangeSeq(ctx, tx, target.GroupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for password reset: %w", err)
		}

		rows, err := q.UpdateUserPasswordHash(ctx, gen.UpdateUserPasswordHashParams{
			PasswordHash: newPasswordHash,
			Now:          now,
			ChangeSeq:    seq,
			GroupID:      target.GroupID,
			ID:           target.ID,
		})
		if err != nil {
			return fmt.Errorf("storage: reset password hash: %w", err)
		}
		if rows == 0 {
			return ErrUserNotFound
		}

		sessionsRevoked, err := q.RevokeAllSessionsForUser(ctx, gen.RevokeAllSessionsForUserParams{
			Now:       sql.NullInt64{Int64: now, Valid: true},
			ChangeSeq: seq,
			GroupID:   target.GroupID,
			UserID:    target.ID,
		})
		if err != nil {
			return fmt.Errorf("storage: revoke sessions for password reset: %w", err)
		}

		deviceTokensRevoked, err := q.RevokeAllDeviceTokensForUser(ctx, gen.RevokeAllDeviceTokensForUserParams{
			Now:       sql.NullInt64{Int64: now, Valid: true},
			ChangeSeq: seq,
			GroupID:   target.GroupID,
			UserID:    target.ID,
		})
		if err != nil {
			return fmt.Errorf("storage: revoke device tokens for password reset: %w", err)
		}

		result = ResetPasswordResult{
			UserID:              target.ID,
			GroupID:             target.GroupID,
			SessionsRevoked:     sessionsRevoked,
			DeviceTokensRevoked: deviceTokensRevoked,
		}
		return nil
	})
	if err != nil {
		return ResetPasswordResult{}, err
	}
	return result, nil
}
