package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type DeviceTokenInfo struct {
	ID                 string
	DeviceLabel        string
	CreatedAtUnixMilli int64
	UpdatedAtUnixMilli int64
	Revoked            bool
	RevokedAtUnixMilli int64
}

func (s *Storage) ListDeviceTokens(ctx context.Context, groupID, userID string) ([]DeviceTokenInfo, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || userID == "" {
		return nil, errors.New("storage: ListDeviceTokens needs a non-empty groupID and userID")
	}

	q := gen.New(s.store.Reader())
	rows, err := q.ListDeviceTokensForUser(ctx, gen.ListDeviceTokensForUserParams{GroupID: groupID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("storage: list device tokens: %w", err)
	}

	out := make([]DeviceTokenInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, DeviceTokenInfo{
			ID:                 r.ID,
			DeviceLabel:        r.DeviceLabel,
			CreatedAtUnixMilli: r.CreatedAt,
			UpdatedAtUnixMilli: r.UpdatedAt,
			Revoked:            r.RevokedAt.Valid,
			RevokedAtUnixMilli: r.RevokedAt.Int64,
		})
	}
	return out, nil
}

func (s *Storage) RevokeDeviceToken(ctx context.Context, groupID, userID, deviceTokenID string, now int64) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || userID == "" || deviceTokenID == "" {
		return errors.New("storage: RevokeDeviceToken needs a non-empty groupID, userID and deviceTokenID")
	}
	if now <= 0 {
		return errors.New("storage: RevokeDeviceToken needs a positive Unix-millisecond now")
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		revokedAt, err := q.GetDeviceTokenForRevoke(ctx, gen.GetDeviceTokenForRevokeParams{
			GroupID: groupID,
			UserID:  userID,
			ID:      deviceTokenID,
		})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("storage: device token %q: %w", deviceTokenID, ErrNotFound)
		case err != nil:
			return fmt.Errorf("storage: get device token for revoke: %w", err)
		}
		if revokedAt.Valid {
			return nil
		}

		seq, err := db.AllocChangeSeq(ctx, tx, groupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for device token revoke: %w", err)
		}

		if _, err := q.RevokeDeviceToken(ctx, gen.RevokeDeviceTokenParams{
			Now:       sql.NullInt64{Int64: now, Valid: true},
			ChangeSeq: seq,
			GroupID:   groupID,
			UserID:    userID,
			ID:        deviceTokenID,
		}); err != nil {
			return fmt.Errorf("storage: revoke device token: %w", err)
		}
		return nil
	})
}
