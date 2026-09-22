package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Sale = gen.ItemSale

var ErrSaleExists = errors.New("storage: this item already has a sold-to block")

type CreateSaleParams struct {
	ID             string
	ItemID         string
	BuyerName      string
	Notes          string
	SoldOn         string
	SalePriceMinor int64
	Now            int64
}

func (p CreateSaleParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateSaleParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreateSaleParams: ItemID is empty")
	case p.SalePriceMinor < 0:
		return errors.New("storage: CreateSaleParams: SalePriceMinor must not be negative")
	case p.Now <= 0:
		return errors.New("storage: CreateSaleParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateSaleParams struct {
	ItemID          string
	BuyerName       string
	SoldOn          string
	SalePriceMinor  int64
	Notes           string
	ExpectedVersion int64
	Now             int64
}

func (p UpdateSaleParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("storage: UpdateSaleParams: ItemID is empty")
	case p.SalePriceMinor < 0:
		return errors.New("storage: UpdateSaleParams: SalePriceMinor must not be negative")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateSaleParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateSaleParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type SaleRepository interface {
	Get(ctx context.Context, itemID string) (Sale, error)

	Create(ctx context.Context, p CreateSaleParams) (Sale, error)

	Update(ctx context.Context, p UpdateSaleParams) (Sale, error)

	Delete(ctx context.Context, itemID string, now int64) error
}

type saleRepository struct {
	binding
}

func (r saleRepository) Get(ctx context.Context, itemID string) (Sale, error) {
	s, err := r.queries().GetItemSale(ctx, gen.GetItemSaleParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Sale{}, fmt.Errorf("storage: sale for item %q: %w", itemID, ErrNotFound)
	case err != nil:
		return Sale{}, fmt.Errorf("storage: get sale for item %q: %w", itemID, err)
	}
	return s, nil
}

func (r saleRepository) Create(ctx context.Context, p CreateSaleParams) (Sale, error) {
	if err := p.validate(); err != nil {
		return Sale{}, err
	}

	var created Sale
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createSaleTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Sale{}, err
	}
	return created, nil
}

func createSaleTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateSaleParams) (Sale, error) {
	if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Sale{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return Sale{}, fmt.Errorf("storage: get item %q for sale create: %w", p.ItemID, err)
	}

	switch _, err := q.GetItemSale(ctx, gen.GetItemSaleParams{GroupID: group, ItemID: p.ItemID}); {
	case err == nil:
		return Sale{}, ErrSaleExists
	case !errors.Is(err, sql.ErrNoRows):
		return Sale{}, fmt.Errorf("storage: check existing sale for item %q: %w", p.ItemID, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Sale{}, fmt.Errorf("storage: allocate change_seq for sale: %w", err)
	}

	if err := q.CreateItemSale(ctx, gen.CreateItemSaleParams{
		ID:             p.ID,
		GroupID:        group,
		ItemID:         p.ItemID,
		BuyerName:      p.BuyerName,
		SoldOn:         nullString(p.SoldOn),
		SalePriceMinor: p.SalePriceMinor,
		Notes:          p.Notes,
		Now:            p.Now,
		ChangeSeq:      seq,
	}); err != nil {
		if isUniqueConstraintViolation(err) {
			return Sale{}, ErrSaleExists
		}
		return Sale{}, fmt.Errorf("storage: create sale for item %q: %w", p.ItemID, err)
	}

	created, err := q.GetItemSale(ctx, gen.GetItemSaleParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Sale{}, fmt.Errorf("storage: read back created sale for item %q: %w", p.ItemID, err)
	}
	return created, nil
}

func (r saleRepository) Update(ctx context.Context, p UpdateSaleParams) (Sale, error) {
	if err := p.validate(); err != nil {
		return Sale{}, err
	}

	var updated Sale
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateSaleTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Sale{}, err
	}
	return updated, nil
}

func updateSaleTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateSaleParams) (Sale, error) {
	current, err := q.GetItemSale(ctx, gen.GetItemSaleParams{GroupID: group, ItemID: p.ItemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Sale{}, fmt.Errorf("storage: sale for item %q: %w", p.ItemID, ErrNotFound)
	case err != nil:
		return Sale{}, fmt.Errorf("storage: get sale for item %q for update: %w", p.ItemID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Sale{}, ErrVersionMismatch
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Sale{}, fmt.Errorf("storage: allocate change_seq for sale update: %w", err)
	}

	rows, err := q.UpdateItemSale(ctx, gen.UpdateItemSaleParams{
		BuyerName:       p.BuyerName,
		SoldOn:          nullString(p.SoldOn),
		SalePriceMinor:  p.SalePriceMinor,
		Notes:           p.Notes,
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		ItemID:          p.ItemID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return Sale{}, fmt.Errorf("storage: update sale for item %q: %w", p.ItemID, err)
	}
	if rows != 1 {
		return Sale{}, fmt.Errorf("storage: update sale for item %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ItemID, rows, current.Version)
	}

	updated, err := q.GetItemSale(ctx, gen.GetItemSaleParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Sale{}, fmt.Errorf("storage: read back updated sale for item %q: %w", p.ItemID, err)
	}
	return updated, nil
}

func (r saleRepository) Delete(ctx context.Context, itemID string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete sale: itemID is empty")
	case now <= 0:
		return errors.New("storage: Delete sale: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for sale delete: %w", err)
		}

		rows, err := q.DeleteItemSale(ctx, gen.DeleteItemSaleParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
		})
		if err != nil {
			return fmt.Errorf("storage: delete sale for item %q: %w", itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: sale for item %q: %w", itemID, ErrNotFound)
		}
		return nil
	})
}
