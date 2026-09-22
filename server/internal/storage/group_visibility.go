package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type GroupVisibility struct {
	WarrantyVisible bool
	SaleVisible     bool
	PurchaseVisible bool
	Version         int64
}

func toGroupVisibility(row gen.GetGroupVisibilityRow) GroupVisibility {
	return GroupVisibility{
		WarrantyVisible: row.WarrantyVisible != 0,
		SaleVisible:     row.SaleVisible != 0,
		PurchaseVisible: row.PurchaseVisible != 0,
		Version:         row.Version,
	}
}

type UpdateGroupVisibilityParams struct {
	WarrantyVisible bool
	SaleVisible     bool
	PurchaseVisible bool
	ExpectedVersion int64
	Now             int64
}

func (p UpdateGroupVisibilityParams) validate() error {
	switch {
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateGroupVisibilityParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateGroupVisibilityParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type GroupVisibilityRepository interface {
	Get(ctx context.Context) (GroupVisibility, error)

	Update(ctx context.Context, p UpdateGroupVisibilityParams) (GroupVisibility, error)
}

type groupVisibilityRepository struct {
	binding
}

func (r groupVisibilityRepository) Get(ctx context.Context) (GroupVisibility, error) {
	row, err := r.queries().GetGroupVisibility(ctx, r.group())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return GroupVisibility{}, fmt.Errorf("storage: group visibility for group %q: %w", r.group(), ErrNotFound)
	case err != nil:
		return GroupVisibility{}, fmt.Errorf("storage: get group visibility: %w", err)
	}
	return toGroupVisibility(row), nil
}

func (r groupVisibilityRepository) Update(ctx context.Context, p UpdateGroupVisibilityParams) (GroupVisibility, error) {
	if err := p.validate(); err != nil {
		return GroupVisibility{}, err
	}

	var updated GroupVisibility
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		current, err := q.GetGroupVisibility(ctx, r.group())
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("storage: group visibility for group %q: %w", r.group(), ErrNotFound)
		case err != nil:
			return fmt.Errorf("storage: get group visibility for update: %w", err)
		}
		if current.Version != p.ExpectedVersion {
			return ErrVersionMismatch
		}

		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for group visibility update: %w", err)
		}

		rows, err := q.UpdateGroupVisibility(ctx, gen.UpdateGroupVisibilityParams{
			WarrantyVisible: boolToInt(p.WarrantyVisible),
			SaleVisible:     boolToInt(p.SaleVisible),
			PurchaseVisible: boolToInt(p.PurchaseVisible),
			Now:             p.Now,
			ChangeSeq:       seq,
			GroupID:         r.group(),
			ExpectedVersion: p.ExpectedVersion,
		})
		if err != nil {
			return fmt.Errorf("storage: update group visibility: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("storage: update group visibility: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
				rows, current.Version)
		}

		row, err := q.GetGroupVisibility(ctx, r.group())
		if err != nil {
			return fmt.Errorf("storage: read back updated group visibility: %w", err)
		}
		updated = toGroupVisibility(row)
		return nil
	})
	if err != nil {
		return GroupVisibility{}, err
	}
	return updated, nil
}
