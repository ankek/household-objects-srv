package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Warranty = gen.ItemWarranty

var ErrWarrantyExists = errors.New("storage: this item already has a warranty block")

type CreateWarrantyParams struct {
	ID         string
	ItemID     string
	Holder     string
	Provider   string
	Notes      string
	StartsOn   string
	ExpiresOn  string
	IsLifetime bool
	Now        int64
}

func (p CreateWarrantyParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateWarrantyParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreateWarrantyParams: ItemID is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateWarrantyParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateWarrantyParams struct {
	ItemID          string
	Holder          string
	Provider        string
	StartsOn        string
	ExpiresOn       string
	IsLifetime      bool
	Notes           string
	ExpectedVersion int64
	Now             int64
}

func (p UpdateWarrantyParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("storage: UpdateWarrantyParams: ItemID is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateWarrantyParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateWarrantyParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type WarrantyRepository interface {
	Get(ctx context.Context, itemID string) (Warranty, error)

	Create(ctx context.Context, p CreateWarrantyParams) (Warranty, error)

	Update(ctx context.Context, p UpdateWarrantyParams) (Warranty, error)

	Delete(ctx context.Context, itemID string, now int64) error
}

type warrantyRepository struct {
	binding
}

func (r warrantyRepository) Get(ctx context.Context, itemID string) (Warranty, error) {
	w, err := r.queries().GetItemWarranty(ctx, gen.GetItemWarrantyParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Warranty{}, fmt.Errorf("storage: warranty for item %q: %w", itemID, ErrNotFound)
	case err != nil:
		return Warranty{}, fmt.Errorf("storage: get warranty for item %q: %w", itemID, err)
	}
	return w, nil
}

func (r warrantyRepository) Create(ctx context.Context, p CreateWarrantyParams) (Warranty, error) {
	if err := p.validate(); err != nil {
		return Warranty{}, err
	}

	var created Warranty
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createWarrantyTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Warranty{}, err
	}
	return created, nil
}

func createWarrantyTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateWarrantyParams) (Warranty, error) {
	if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Warranty{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return Warranty{}, fmt.Errorf("storage: get item %q for warranty create: %w", p.ItemID, err)
	}

	switch _, err := q.GetItemWarranty(ctx, gen.GetItemWarrantyParams{GroupID: group, ItemID: p.ItemID}); {
	case err == nil:
		return Warranty{}, ErrWarrantyExists
	case !errors.Is(err, sql.ErrNoRows):
		return Warranty{}, fmt.Errorf("storage: check existing warranty for item %q: %w", p.ItemID, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Warranty{}, fmt.Errorf("storage: allocate change_seq for warranty: %w", err)
	}

	if err := q.CreateItemWarranty(ctx, gen.CreateItemWarrantyParams{
		ID:         p.ID,
		GroupID:    group,
		ItemID:     p.ItemID,
		Holder:     p.Holder,
		Provider:   p.Provider,
		StartsOn:   nullString(p.StartsOn),
		ExpiresOn:  nullString(p.ExpiresOn),
		IsLifetime: boolToInt(p.IsLifetime),
		Notes:      p.Notes,
		Now:        p.Now,
		ChangeSeq:  seq,
	}); err != nil {
		if isUniqueConstraintViolation(err) {
			return Warranty{}, ErrWarrantyExists
		}
		return Warranty{}, fmt.Errorf("storage: create warranty for item %q: %w", p.ItemID, err)
	}

	created, err := q.GetItemWarranty(ctx, gen.GetItemWarrantyParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Warranty{}, fmt.Errorf("storage: read back created warranty for item %q: %w", p.ItemID, err)
	}
	return created, nil
}

func (r warrantyRepository) Update(ctx context.Context, p UpdateWarrantyParams) (Warranty, error) {
	if err := p.validate(); err != nil {
		return Warranty{}, err
	}

	var updated Warranty
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateWarrantyTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Warranty{}, err
	}
	return updated, nil
}

func updateWarrantyTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateWarrantyParams) (Warranty, error) {
	current, err := q.GetItemWarranty(ctx, gen.GetItemWarrantyParams{GroupID: group, ItemID: p.ItemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Warranty{}, fmt.Errorf("storage: warranty for item %q: %w", p.ItemID, ErrNotFound)
	case err != nil:
		return Warranty{}, fmt.Errorf("storage: get warranty for item %q for update: %w", p.ItemID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Warranty{}, ErrVersionMismatch
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Warranty{}, fmt.Errorf("storage: allocate change_seq for warranty update: %w", err)
	}

	rows, err := q.UpdateItemWarranty(ctx, gen.UpdateItemWarrantyParams{
		Holder:          p.Holder,
		Provider:        p.Provider,
		StartsOn:        nullString(p.StartsOn),
		ExpiresOn:       nullString(p.ExpiresOn),
		IsLifetime:      boolToInt(p.IsLifetime),
		Notes:           p.Notes,
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		ItemID:          p.ItemID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return Warranty{}, fmt.Errorf("storage: update warranty for item %q: %w", p.ItemID, err)
	}
	if rows != 1 {
		return Warranty{}, fmt.Errorf("storage: update warranty for item %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ItemID, rows, current.Version)
	}

	updated, err := q.GetItemWarranty(ctx, gen.GetItemWarrantyParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Warranty{}, fmt.Errorf("storage: read back updated warranty for item %q: %w", p.ItemID, err)
	}
	return updated, nil
}

func (r warrantyRepository) Delete(ctx context.Context, itemID string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete warranty: itemID is empty")
	case now <= 0:
		return errors.New("storage: Delete warranty: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for warranty delete: %w", err)
		}

		rows, err := q.DeleteItemWarranty(ctx, gen.DeleteItemWarrantyParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
		})
		if err != nil {
			return fmt.Errorf("storage: delete warranty for item %q: %w", itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: warranty for item %q: %w", itemID, ErrNotFound)
		}
		return nil
	})
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
