package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type ItemCustomField = gen.ItemCustomField

type CreateItemCustomFieldParams struct {
	ID          string
	ItemID      string
	FieldDefID  string
	Name        string
	FieldType   string
	TextValue   *string
	NumberValue *float64
	BoolValue   *bool
	DateValue   *string
	Now         int64
}

func (p CreateItemCustomFieldParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateItemCustomFieldParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: CreateItemCustomFieldParams: ItemID is empty")
	case p.Name == "":
		return errors.New("storage: CreateItemCustomFieldParams: Name is empty")
	case p.FieldType == "":
		return errors.New("storage: CreateItemCustomFieldParams: FieldType is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateItemCustomFieldParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateItemCustomFieldParams struct {
	ItemID string
	ID     string

	FieldDefID      string
	Name            string
	FieldType       string
	TextValue       *string
	NumberValue     *float64
	BoolValue       *bool
	DateValue       *string
	ExpectedVersion int64
	Now             int64
}

func (p UpdateItemCustomFieldParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("storage: UpdateItemCustomFieldParams: ItemID is empty")
	case p.ID == "":
		return errors.New("storage: UpdateItemCustomFieldParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: UpdateItemCustomFieldParams: Name is empty")
	case p.FieldType == "":
		return errors.New("storage: UpdateItemCustomFieldParams: FieldType is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateItemCustomFieldParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateItemCustomFieldParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type ItemCustomFieldRepository interface {
	Get(ctx context.Context, itemID, id string) (ItemCustomField, error)

	List(ctx context.Context, itemID string) ([]ItemCustomField, error)

	Create(ctx context.Context, p CreateItemCustomFieldParams) (ItemCustomField, error)

	Update(ctx context.Context, p UpdateItemCustomFieldParams) (ItemCustomField, error)

	Delete(ctx context.Context, itemID, id string, now int64) error
}

type itemCustomFieldRepository struct {
	binding
}

func (r itemCustomFieldRepository) Get(ctx context.Context, itemID, id string) (ItemCustomField, error) {
	row, err := r.queries().GetItemCustomField(ctx, gen.GetItemCustomFieldParams{
		GroupID: r.group(),
		ItemID:  itemID,
		ID:      id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ItemCustomField{}, fmt.Errorf("storage: custom field %q on item %q: %w", id, itemID, ErrNotFound)
	case err != nil:
		return ItemCustomField{}, fmt.Errorf("storage: get custom field %q on item %q: %w", id, itemID, err)
	}
	return row, nil
}

func (r itemCustomFieldRepository) List(ctx context.Context, itemID string) ([]ItemCustomField, error) {
	if _, err := r.queries().GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: itemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
		}
		return nil, fmt.Errorf("storage: get item %q for custom field list: %w", itemID, err)
	}

	rows, err := r.queries().ListItemCustomFields(ctx, gen.ListItemCustomFieldsParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list custom fields for item %q: %w", itemID, err)
	}
	return rows, nil
}

func (r itemCustomFieldRepository) Create(ctx context.Context, p CreateItemCustomFieldParams) (ItemCustomField, error) {
	if err := p.validate(); err != nil {
		return ItemCustomField{}, err
	}

	var created ItemCustomField
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createItemCustomFieldTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return ItemCustomField{}, err
	}
	return created, nil
}

func createItemCustomFieldTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateItemCustomFieldParams) (ItemCustomField, error) {
	if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ItemCustomField{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return ItemCustomField{}, fmt.Errorf("storage: get item %q for custom field create: %w", p.ItemID, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return ItemCustomField{}, fmt.Errorf("storage: allocate change_seq for custom field: %w", err)
	}

	if err := q.CreateItemCustomField(ctx, gen.CreateItemCustomFieldParams{
		ID:          p.ID,
		GroupID:     group,
		ItemID:      p.ItemID,
		FieldDefID:  nullString(p.FieldDefID),
		Name:        p.Name,
		FieldType:   p.FieldType,
		TextValue:   nullStringPtr(p.TextValue),
		NumberValue: nullFloat64Ptr(p.NumberValue),
		BoolValue:   nullBoolPtr(p.BoolValue),
		DateValue:   nullStringPtr(p.DateValue),
		Now:         p.Now,
		ChangeSeq:   seq,
	}); err != nil {
		return ItemCustomField{}, fmt.Errorf("storage: create custom field for item %q: %w", p.ItemID, err)
	}

	created, err := q.GetItemCustomField(ctx, gen.GetItemCustomFieldParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	if err != nil {
		return ItemCustomField{}, fmt.Errorf("storage: read back created custom field %q for item %q: %w", p.ID, p.ItemID, err)
	}
	return created, nil
}

func (r itemCustomFieldRepository) Update(ctx context.Context, p UpdateItemCustomFieldParams) (ItemCustomField, error) {
	if err := p.validate(); err != nil {
		return ItemCustomField{}, err
	}

	var updated ItemCustomField
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateItemCustomFieldTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return ItemCustomField{}, err
	}
	return updated, nil
}

func updateItemCustomFieldTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateItemCustomFieldParams) (ItemCustomField, error) {
	current, err := q.GetItemCustomField(ctx, gen.GetItemCustomFieldParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ItemCustomField{}, fmt.Errorf("storage: custom field %q on item %q: %w", p.ID, p.ItemID, ErrNotFound)
	case err != nil:
		return ItemCustomField{}, fmt.Errorf("storage: get custom field %q on item %q for update: %w", p.ID, p.ItemID, err)
	}
	if current.Version != p.ExpectedVersion {
		return ItemCustomField{}, ErrVersionMismatch
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return ItemCustomField{}, fmt.Errorf("storage: allocate change_seq for custom field update: %w", err)
	}

	rows, err := q.UpdateItemCustomField(ctx, gen.UpdateItemCustomFieldParams{
		FieldDefID:      nullString(p.FieldDefID),
		Name:            p.Name,
		FieldType:       p.FieldType,
		TextValue:       nullStringPtr(p.TextValue),
		NumberValue:     nullFloat64Ptr(p.NumberValue),
		BoolValue:       nullBoolPtr(p.BoolValue),
		DateValue:       nullStringPtr(p.DateValue),
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		ItemID:          p.ItemID,
		ID:              p.ID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return ItemCustomField{}, fmt.Errorf("storage: update custom field %q on item %q: %w", p.ID, p.ItemID, err)
	}
	if rows != 1 {
		return ItemCustomField{}, fmt.Errorf("storage: update custom field %q on item %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ID, p.ItemID, rows, current.Version)
	}

	updated, err := q.GetItemCustomField(ctx, gen.GetItemCustomFieldParams{GroupID: group, ItemID: p.ItemID, ID: p.ID})
	if err != nil {
		return ItemCustomField{}, fmt.Errorf("storage: read back updated custom field %q on item %q: %w", p.ID, p.ItemID, err)
	}
	return updated, nil
}

func (r itemCustomFieldRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete custom field: itemID is empty")
	case id == "":
		return errors.New("storage: Delete custom field: id is empty")
	case now <= 0:
		return errors.New("storage: Delete custom field: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for custom field delete: %w", err)
		}

		rows, err := q.DeleteItemCustomField(ctx, gen.DeleteItemCustomFieldParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
			ID:        id,
		})
		if err != nil {
			return fmt.Errorf("storage: delete custom field %q on item %q: %w", id, itemID, err)
		}
		if rows == 0 {
			return fmt.Errorf("storage: custom field %q on item %q: %w", id, itemID, ErrNotFound)
		}
		return nil
	})
}

func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullFloat64Ptr(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func nullBoolPtr(b *bool) sql.NullInt64 {
	if b == nil {
		return sql.NullInt64{}
	}
	v := int64(0)
	if *b {
		v = 1
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
