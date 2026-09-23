package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type StockAdjustment = gen.StockAdjustment

type CreateStockAdjustmentParams struct {
	ID     string
	ItemID string
	Delta  int64
	Reason string
	Note   string
	Now    int64
}

func (p CreateStockAdjustmentParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateStockAdjustmentParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreateStockAdjustmentParams: ItemID is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateStockAdjustmentParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type StockAdjustmentRepository interface {
	List(ctx context.Context, itemID string) ([]StockAdjustment, error)

	Create(ctx context.Context, p CreateStockAdjustmentParams) (StockAdjustment, error)
}

type stockAdjustmentRepository struct {
	binding
}

func (r stockAdjustmentRepository) List(ctx context.Context, itemID string) ([]StockAdjustment, error) {
	if _, err := r.queries().GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: itemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
		}
		return nil, fmt.Errorf("storage: get item %q for stock adjustment list: %w", itemID, err)
	}

	rows, err := r.queries().ListStockAdjustments(ctx, gen.ListStockAdjustmentsParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list stock adjustments for item %q: %w", itemID, err)
	}
	return rows, nil
}

func (r stockAdjustmentRepository) Create(ctx context.Context, p CreateStockAdjustmentParams) (StockAdjustment, error) {
	if err := p.validate(); err != nil {
		return StockAdjustment{}, err
	}

	var created StockAdjustment
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		before, beforeErr := q.GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: p.ItemID})

		row, err := createStockAdjustmentTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row

		if beforeErr != nil {
			return fmt.Errorf("storage: item %q: field-version diff pre-read: %w", p.ItemID, beforeErr)
		}
		after, err := q.GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: p.ItemID})
		if err != nil {
			return fmt.Errorf("storage: item %q: field-version diff post-read: %w", p.ItemID, err)
		}
		return recordFieldVersionsTx(ctx, q, r.group(), "item", after.ID, after.Version, p.Now, diffItemFieldVersions(before, after))
	})
	if err != nil {
		return StockAdjustment{}, err
	}
	return created, nil
}

func createStockAdjustmentTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateStockAdjustmentParams) (StockAdjustment, error) {
	item, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StockAdjustment{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return StockAdjustment{}, fmt.Errorf("storage: get item %q for stock adjustment create: %w", p.ItemID, err)
	}

	resultingQuantity := item.Quantity + p.Delta

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return StockAdjustment{}, fmt.Errorf("storage: allocate change_seq for stock adjustment: %w", err)
	}

	if err := q.CreateStockAdjustment(ctx, gen.CreateStockAdjustmentParams{
		ID:                p.ID,
		GroupID:           group,
		ItemID:            p.ItemID,
		Delta:             p.Delta,
		Reason:            p.Reason,
		Note:              p.Note,
		ResultingQuantity: resultingQuantity,
		Now:               p.Now,
		ChangeSeq:         seq,
	}); err != nil {
		return StockAdjustment{}, fmt.Errorf("storage: create stock adjustment for item %q: %w", p.ItemID, err)
	}

	rows, err := q.AdjustItemQuantity(ctx, gen.AdjustItemQuantityParams{
		Quantity:  resultingQuantity,
		Now:       p.Now,
		ChangeSeq: seq,
		GroupID:   group,
		ItemID:    p.ItemID,
	})
	if err != nil {
		return StockAdjustment{}, fmt.Errorf("storage: apply stock adjustment to item %q: %w", p.ItemID, err)
	}
	if rows != 1 {
		return StockAdjustment{}, fmt.Errorf("storage: apply stock adjustment to item %q: matched %d rows, want 1 (pre-check read the item live inside this same transaction); this should be unreachable under this server's single-writer transaction model",
			p.ItemID, rows)
	}

	created, err := q.GetStockAdjustment(ctx, gen.GetStockAdjustmentParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	if err != nil {
		return StockAdjustment{}, fmt.Errorf("storage: read back created stock adjustment %q for item %q: %w", p.ID, p.ItemID, err)
	}
	return created, nil
}
