package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.tx(ctx, false, fn)
}

func (s *Store) ImportTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.tx(ctx, true, fn)
}

func (s *Store) tx(ctx context.Context, deferFK bool, fn func(*sql.Tx) error) (err error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err == nil {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("db: rollback: %w", rbErr))
		}
	}()

	if deferFK {
		if _, err = tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
			return fmt.Errorf("db: defer foreign keys: %w", err)
		}
	}

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("db: commit transaction: %w", err)
	}
	return nil
}
