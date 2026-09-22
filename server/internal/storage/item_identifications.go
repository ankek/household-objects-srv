package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Identification = gen.ItemIdentification

type CreateIdentificationParams struct {
	ID     string
	ItemID string
	Kind   string
	Value  string
	Now    int64
}

func (p CreateIdentificationParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateIdentificationParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreateIdentificationParams: ItemID is empty")
	case p.Kind == "":
		return errors.New("storage: CreateIdentificationParams: Kind is empty")
	case p.Value == "":
		return errors.New("storage: CreateIdentificationParams: Value is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateIdentificationParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateIdentificationParams struct {
	ItemID          string
	ID              string
	Kind            string
	Value           string
	ExpectedVersion int64
	Now             int64
}

func (p UpdateIdentificationParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("storage: UpdateIdentificationParams: ItemID is empty")
	case p.ID == "":
		return errors.New("storage: UpdateIdentificationParams: ID is empty")
	case p.Kind == "":
		return errors.New("storage: UpdateIdentificationParams: Kind is empty")
	case p.Value == "":
		return errors.New("storage: UpdateIdentificationParams: Value is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateIdentificationParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateIdentificationParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type IdentificationRepository interface {
	Get(ctx context.Context, itemID, id string) (Identification, error)

	List(ctx context.Context, itemID string) ([]Identification, error)

	Create(ctx context.Context, p CreateIdentificationParams) (Identification, error)

	Update(ctx context.Context, p UpdateIdentificationParams) (Identification, error)

	Delete(ctx context.Context, itemID, id string, now int64) error
}

type identificationRepository struct {
	binding
}

func (r identificationRepository) Get(ctx context.Context, itemID, id string) (Identification, error) {
	row, err := r.queries().GetItemIdentification(ctx, gen.GetItemIdentificationParams{
		GroupID: r.group(),
		ItemID:  itemID,
		ID:      id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Identification{}, fmt.Errorf("storage: identification %q on item %q: %w", id, itemID, ErrNotFound)
	case err != nil:
		return Identification{}, fmt.Errorf("storage: get identification %q on item %q: %w", id, itemID, err)
	}
	return row, nil
}

func (r identificationRepository) List(ctx context.Context, itemID string) ([]Identification, error) {
	if _, err := r.queries().GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: itemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
		}
		return nil, fmt.Errorf("storage: get item %q for identification list: %w", itemID, err)
	}

	rows, err := r.queries().ListItemIdentifications(ctx, gen.ListItemIdentificationsParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list identifications for item %q: %w", itemID, err)
	}
	return rows, nil
}

func (r identificationRepository) Create(ctx context.Context, p CreateIdentificationParams) (Identification, error) {
	if err := p.validate(); err != nil {
		return Identification{}, err
	}

	var created Identification
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createIdentificationTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Identification{}, err
	}
	return created, nil
}

func createIdentificationTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateIdentificationParams) (Identification, error) {
	if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Identification{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return Identification{}, fmt.Errorf("storage: get item %q for identification create: %w", p.ItemID, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Identification{}, fmt.Errorf("storage: allocate change_seq for identification: %w", err)
	}

	if err := q.CreateItemIdentification(ctx, gen.CreateItemIdentificationParams{
		ID:        p.ID,
		GroupID:   group,
		ItemID:    p.ItemID,
		Kind:      p.Kind,
		Value:     p.Value,
		Now:       p.Now,
		ChangeSeq: seq,
	}); err != nil {
		return Identification{}, fmt.Errorf("storage: create identification for item %q: %w", p.ItemID, err)
	}

	created, err := q.GetItemIdentification(ctx, gen.GetItemIdentificationParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	if err != nil {
		return Identification{}, fmt.Errorf("storage: read back created identification %q for item %q: %w", p.ID, p.ItemID, err)
	}
	return created, nil
}

func (r identificationRepository) Update(ctx context.Context, p UpdateIdentificationParams) (Identification, error) {
	if err := p.validate(); err != nil {
		return Identification{}, err
	}

	var updated Identification
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateIdentificationTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Identification{}, err
	}
	return updated, nil
}

func updateIdentificationTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateIdentificationParams) (Identification, error) {
	current, err := q.GetItemIdentification(ctx, gen.GetItemIdentificationParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Identification{}, fmt.Errorf("storage: identification %q on item %q: %w", p.ID, p.ItemID, ErrNotFound)
	case err != nil:
		return Identification{}, fmt.Errorf("storage: get identification %q on item %q for update: %w", p.ID, p.ItemID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Identification{}, ErrVersionMismatch
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Identification{}, fmt.Errorf("storage: allocate change_seq for identification update: %w", err)
	}

	rows, err := q.UpdateItemIdentification(ctx, gen.UpdateItemIdentificationParams{
		Kind:            p.Kind,
		Value:           p.Value,
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		ItemID:          p.ItemID,
		ID:              p.ID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return Identification{}, fmt.Errorf("storage: update identification %q on item %q: %w", p.ID, p.ItemID, err)
	}
	if rows != 1 {
		return Identification{}, fmt.Errorf("storage: update identification %q on item %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ID, p.ItemID, rows, current.Version)
	}

	updated, err := q.GetItemIdentification(ctx, gen.GetItemIdentificationParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	if err != nil {
		return Identification{}, fmt.Errorf("storage: read back updated identification %q on item %q: %w", p.ID, p.ItemID, err)
	}
	return updated, nil
}

func (r identificationRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete identification: itemID is empty")
	case id == "":
		return errors.New("storage: Delete identification: id is empty")
	case now <= 0:
		return errors.New("storage: Delete identification: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for identification delete: %w", err)
		}

		rows, err := q.DeleteItemIdentification(ctx, gen.DeleteItemIdentificationParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
			ID:        id,
		})
		if err != nil {
			return fmt.Errorf("storage: delete identification %q on item %q: %w", id, itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: identification %q on item %q: %w", id, itemID, ErrNotFound)
		}
		return nil
	})
}
