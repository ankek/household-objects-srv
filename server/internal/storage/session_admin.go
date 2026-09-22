package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type SessionInfo struct {
	ID                 string
	UserAgent          string
	CreatedFromIP      string
	CreatedAtUnixMilli int64
	UpdatedAtUnixMilli int64
	ExpiresAtUnixMilli int64
	Revoked            bool
	RevokedAtUnixMilli int64
}

func (s *Storage) ListSessions(ctx context.Context, groupID, userID string) ([]SessionInfo, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || userID == "" {
		return nil, errors.New("storage: ListSessions needs a non-empty groupID and userID")
	}

	q := gen.New(s.store.Reader())
	rows, err := q.ListSessionsForUser(ctx, gen.ListSessionsForUserParams{GroupID: groupID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("storage: list sessions: %w", err)
	}

	out := make([]SessionInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionInfo{
			ID:                 r.ID,
			UserAgent:          r.UserAgent,
			CreatedFromIP:      r.CreatedFromIp,
			CreatedAtUnixMilli: r.CreatedAt,
			UpdatedAtUnixMilli: r.UpdatedAt,
			ExpiresAtUnixMilli: r.ExpiresAt,
			Revoked:            r.RevokedAt.Valid,
			RevokedAtUnixMilli: r.RevokedAt.Int64,
		})
	}
	return out, nil
}

func (s *Storage) RevokeSession(ctx context.Context, groupID, userID, sessionID string, now int64) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || userID == "" || sessionID == "" {
		return errors.New("storage: RevokeSession needs a non-empty groupID, userID and sessionID")
	}
	if now <= 0 {
		return errors.New("storage: RevokeSession needs a positive Unix-millisecond now")
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		revokedAt, err := q.GetSessionForRevoke(ctx, gen.GetSessionForRevokeParams{
			GroupID: groupID,
			UserID:  userID,
			ID:      sessionID,
		})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("storage: session %q: %w", sessionID, ErrNotFound)
		case err != nil:
			return fmt.Errorf("storage: get session for revoke: %w", err)
		}
		if revokedAt.Valid {
			return nil
		}

		seq, err := db.AllocChangeSeq(ctx, tx, groupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for session revoke: %w", err)
		}

		if _, err := q.RevokeSession(ctx, gen.RevokeSessionParams{
			Now:       sql.NullInt64{Int64: now, Valid: true},
			ChangeSeq: seq,
			GroupID:   groupID,
			UserID:    userID,
			ID:        sessionID,
		}); err != nil {
			return fmt.Errorf("storage: revoke session: %w", err)
		}
		return nil
	})
}

func (s *Storage) RevokeCallingSession(ctx context.Context, groupID, userID, tokenHash string, now int64) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if groupID == "" || userID == "" || tokenHash == "" {
		return errors.New("storage: RevokeCallingSession needs a non-empty groupID, userID and tokenHash")
	}
	if now <= 0 {
		return errors.New("storage: RevokeCallingSession needs a positive Unix-millisecond now")
	}

	return s.store.Tx(ctx, func(tx *sql.Tx) error {
		q := gen.New(tx)

		seq, err := db.AllocChangeSeq(ctx, tx, groupID)
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for logout: %w", err)
		}

		if _, err := q.RevokeSessionByTokenHash(ctx, gen.RevokeSessionByTokenHashParams{
			Now:       sql.NullInt64{Int64: now, Valid: true},
			ChangeSeq: seq,
			GroupID:   groupID,
			UserID:    userID,
			TokenHash: tokenHash,
		}); err != nil {
			return fmt.Errorf("storage: revoke calling session: %w", err)
		}
		return nil
	})
}
