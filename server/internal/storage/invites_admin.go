package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type InviteInfo struct {
	ID                  string
	CreatedByUserID     string
	CreatedAtUnixMilli  int64
	UpdatedAtUnixMilli  int64
	ExpiresAtUnixMilli  int64
	Redeemed            bool
	RedeemedAtUnixMilli int64
	RedeemedByUserID    string
}

func (s *Storage) ListInvites(ctx context.Context, groupID string) ([]InviteInfo, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" {
		return nil, errors.New("storage: ListInvites needs a non-empty groupID")
	}

	q := gen.New(s.store.Reader())
	rows, err := q.ListInvites(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("storage: list invites: %w", err)
	}

	out := make([]InviteInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, InviteInfo{
			ID:                  r.ID,
			CreatedByUserID:     r.CreatedByUserID,
			CreatedAtUnixMilli:  r.CreatedAt,
			UpdatedAtUnixMilli:  r.UpdatedAt,
			ExpiresAtUnixMilli:  r.ExpiresAt,
			Redeemed:            r.RedeemedAt.Valid,
			RedeemedAtUnixMilli: r.RedeemedAt.Int64,
			RedeemedByUserID:    r.RedeemedByUserID.String,
		})
	}
	return out, nil
}

func (s *Storage) RevokeInvite(ctx context.Context, groupID, inviteID string, now int64) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || inviteID == "" {
		return errors.New("storage: RevokeInvite needs a non-empty groupID and inviteID")
	}
	if now <= 0 {
		return errors.New("storage: RevokeInvite needs a positive Unix-millisecond now")
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		current, err := q.GetInviteForRevoke(ctx, gen.GetInviteForRevokeParams{
			GroupID: groupID,
			ID:      inviteID,
		})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("storage: invite %q: %w", inviteID, ErrNotFound)
		case err != nil:
			return fmt.Errorf("storage: get invite for revoke: %w", err)
		}
		if current.RedeemedAt.Valid {
			return nil
		}
		if current.ExpiresAt <= now {
			return nil
		}

		seq, err := db.AllocChangeSeq(ctx, tx, groupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for invite revoke: %w", err)
		}

		if _, err := q.RevokeInvite(ctx, gen.RevokeInviteParams{
			Now:       now,
			ChangeSeq: seq,
			GroupID:   groupID,
			ID:        inviteID,
		}); err != nil {
			return fmt.Errorf("storage: revoke invite: %w", err)
		}
		return nil
	})
}
