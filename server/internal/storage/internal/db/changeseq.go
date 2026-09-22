package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrGroupNotFound = errors.New("db: group not found")

func AllocChangeSeq(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	var seq int64
	err := tx.QueryRowContext(ctx,
		`UPDATE groups SET change_seq_counter = change_seq_counter + 1
		 WHERE id = ? RETURNING change_seq_counter`, groupID).Scan(&seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("db: allocate change_seq for group %q: %w", groupID, ErrGroupNotFound)
	case err != nil:
		return 0, fmt.Errorf("db: allocate change_seq for group %q: %w", groupID, err)
	}
	return seq, nil
}
