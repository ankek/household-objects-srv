package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Purchase = gen.ItemPurchase

var ErrPurchaseExists = errors.New("storage: this item already has a purchased-from block")

type CreatePurchaseParams struct {
	ID                 string
	ItemID             string
	Vendor             string
	OrderReference     string
	Notes              string
	PurchasedOn        string
	PurchasePriceMinor int64
	Now                int64
}

func (p CreatePurchaseParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreatePurchaseParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreatePurchaseParams: ItemID is empty")
	case p.PurchasePriceMinor < 0:
		return errors.New("storage: CreatePurchaseParams: PurchasePriceMinor must not be negative")
	case p.Now <= 0:
		return errors.New("storage: CreatePurchaseParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdatePurchaseParams struct {
	ItemID             string
	Vendor             string
	PurchasedOn        string
	PurchasePriceMinor int64
	OrderReference     string
	Notes              string
	ExpectedVersion    int64
	Now                int64
}

func (p UpdatePurchaseParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("storage: UpdatePurchaseParams: ItemID is empty")
	case p.PurchasePriceMinor < 0:
		return errors.New("storage: UpdatePurchaseParams: PurchasePriceMinor must not be negative")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdatePurchaseParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdatePurchaseParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type PurchaseRepository interface {
	Get(ctx context.Context, itemID string) (Purchase, error)

	Create(ctx context.Context, p CreatePurchaseParams) (Purchase, error)

	Update(ctx context.Context, p UpdatePurchaseParams) (Purchase, error)

	Delete(ctx context.Context, itemID string, now int64) error
}

type purchaseRepository struct {
	binding
}

func (r purchaseRepository) Get(ctx context.Context, itemID string) (Purchase, error) {
	p, err := r.queries().GetItemPurchase(ctx, gen.GetItemPurchaseParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Purchase{}, fmt.Errorf("storage: purchase for item %q: %w", itemID, ErrNotFound)
	case err != nil:
		return Purchase{}, fmt.Errorf("storage: get purchase for item %q: %w", itemID, err)
	}
	return p, nil
}

func (r purchaseRepository) Create(ctx context.Context, p CreatePurchaseParams) (Purchase, error) {
	if err := p.validate(); err != nil {
		return Purchase{}, err
	}

	var created Purchase
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createPurchaseTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Purchase{}, err
	}
	return created, nil
}

func createPurchaseTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreatePurchaseParams) (Purchase, error) {
	if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Purchase{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return Purchase{}, fmt.Errorf("storage: get item %q for purchase create: %w", p.ItemID, err)
	}

	switch _, err := q.GetItemPurchase(ctx, gen.GetItemPurchaseParams{GroupID: group, ItemID: p.ItemID}); {
	case err == nil:
		return Purchase{}, ErrPurchaseExists
	case !errors.Is(err, sql.ErrNoRows):
		return Purchase{}, fmt.Errorf("storage: check existing purchase for item %q: %w", p.ItemID, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Purchase{}, fmt.Errorf("storage: allocate change_seq for purchase: %w", err)
	}

	if err := q.CreateItemPurchase(ctx, gen.CreateItemPurchaseParams{
		ID:                 p.ID,
		GroupID:            group,
		ItemID:             p.ItemID,
		Vendor:             p.Vendor,
		PurchasedOn:        nullString(p.PurchasedOn),
		PurchasePriceMinor: p.PurchasePriceMinor,
		OrderReference:     p.OrderReference,
		Notes:              p.Notes,
		Now:                p.Now,
		ChangeSeq:          seq,
	}); err != nil {
		if isUniqueConstraintViolation(err) {
			return Purchase{}, ErrPurchaseExists
		}
		return Purchase{}, fmt.Errorf("storage: create purchase for item %q: %w", p.ItemID, err)
	}

	created, err := q.GetItemPurchase(ctx, gen.GetItemPurchaseParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Purchase{}, fmt.Errorf("storage: read back created purchase for item %q: %w", p.ItemID, err)
	}
	return created, nil
}

func (r purchaseRepository) Update(ctx context.Context, p UpdatePurchaseParams) (Purchase, error) {
	if err := p.validate(); err != nil {
		return Purchase{}, err
	}

	var updated Purchase
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updatePurchaseTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Purchase{}, err
	}
	return updated, nil
}

func updatePurchaseTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdatePurchaseParams) (Purchase, error) {
	current, err := q.GetItemPurchase(ctx, gen.GetItemPurchaseParams{GroupID: group, ItemID: p.ItemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Purchase{}, fmt.Errorf("storage: purchase for item %q: %w", p.ItemID, ErrNotFound)
	case err != nil:
		return Purchase{}, fmt.Errorf("storage: get purchase for item %q for update: %w", p.ItemID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Purchase{}, ErrVersionMismatch
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Purchase{}, fmt.Errorf("storage: allocate change_seq for purchase update: %w", err)
	}

	rows, err := q.UpdateItemPurchase(ctx, gen.UpdateItemPurchaseParams{
		Vendor:             p.Vendor,
		PurchasedOn:        nullString(p.PurchasedOn),
		PurchasePriceMinor: p.PurchasePriceMinor,
		OrderReference:     p.OrderReference,
		Notes:              p.Notes,
		Now:                p.Now,
		ChangeSeq:          seq,
		GroupID:            group,
		ItemID:             p.ItemID,
		ExpectedVersion:    p.ExpectedVersion,
	})
	if err != nil {
		return Purchase{}, fmt.Errorf("storage: update purchase for item %q: %w", p.ItemID, err)
	}
	if rows != 1 {
		return Purchase{}, fmt.Errorf("storage: update purchase for item %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ItemID, rows, current.Version)
	}

	updated, err := q.GetItemPurchase(ctx, gen.GetItemPurchaseParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Purchase{}, fmt.Errorf("storage: read back updated purchase for item %q: %w", p.ItemID, err)
	}
	return updated, nil
}

func (r purchaseRepository) Delete(ctx context.Context, itemID string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete purchase: itemID is empty")
	case now <= 0:
		return errors.New("storage: Delete purchase: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for purchase delete: %w", err)
		}

		rows, err := q.DeleteItemPurchase(ctx, gen.DeleteItemPurchaseParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
		})
		if err != nil {
			return fmt.Errorf("storage: delete purchase for item %q: %w", itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: purchase for item %q: %w", itemID, ErrNotFound)
		}
		return nil
	})
}
