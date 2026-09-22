package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type CustomFieldDef = gen.CustomFieldDef

type CreateCustomFieldDefParams struct {
	ID           string
	Name         string
	FieldType    string
	DisplayOrder int64
	Now          int64
}

func (p CreateCustomFieldDefParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateCustomFieldDefParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: CreateCustomFieldDefParams: Name is empty")
	case p.FieldType == "":
		return errors.New("storage: CreateCustomFieldDefParams: FieldType is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateCustomFieldDefParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateCustomFieldDefParams struct {
	ID              string
	Name            string
	FieldType       string
	DisplayOrder    int64
	ExpectedVersion int64
	Now             int64
}

func (p UpdateCustomFieldDefParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: UpdateCustomFieldDefParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: UpdateCustomFieldDefParams: Name is empty")
	case p.FieldType == "":
		return errors.New("storage: UpdateCustomFieldDefParams: FieldType is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateCustomFieldDefParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateCustomFieldDefParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type CustomFieldDefRepository interface {
	Get(ctx context.Context, id string) (CustomFieldDef, error)

	List(ctx context.Context) ([]CustomFieldDef, error)

	Create(ctx context.Context, p CreateCustomFieldDefParams) (CustomFieldDef, error)

	Update(ctx context.Context, p UpdateCustomFieldDefParams) (CustomFieldDef, error)

	Delete(ctx context.Context, id string, now int64) error
}

type customFieldDefRepository struct {
	binding
}

func (r customFieldDefRepository) Get(ctx context.Context, id string) (CustomFieldDef, error) {
	row, err := r.queries().GetCustomFieldDef(ctx, gen.GetCustomFieldDefParams{
		GroupID: r.group(),
		ID:      id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return CustomFieldDef{}, fmt.Errorf("storage: custom field def %q: %w", id, ErrNotFound)
	case err != nil:
		return CustomFieldDef{}, fmt.Errorf("storage: get custom field def %q: %w", id, err)
	}
	return row, nil
}

func (r customFieldDefRepository) List(ctx context.Context) ([]CustomFieldDef, error) {
	rows, err := r.queries().ListCustomFieldDefs(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list custom field defs for group %q: %w", r.group(), err)
	}
	return rows, nil
}

func (r customFieldDefRepository) Create(ctx context.Context, p CreateCustomFieldDefParams) (CustomFieldDef, error) {
	if err := p.validate(); err != nil {
		return CustomFieldDef{}, err
	}

	var created CustomFieldDef
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for custom field def: %w", err)
		}

		if err := q.CreateCustomFieldDef(ctx, gen.CreateCustomFieldDefParams{
			ID:           p.ID,
			GroupID:      r.group(),
			Name:         p.Name,
			FieldType:    p.FieldType,
			DisplayOrder: p.DisplayOrder,
			Now:          p.Now,
			ChangeSeq:    seq,
		}); err != nil {
			return fmt.Errorf("storage: create custom field def: %w", err)
		}

		created, err = q.GetCustomFieldDef(ctx, gen.GetCustomFieldDefParams{GroupID: r.group(), ID: p.ID})
		if err != nil {
			return fmt.Errorf("storage: read back created custom field def %q: %w", p.ID, err)
		}
		return nil
	})
	if err != nil {
		return CustomFieldDef{}, err
	}
	return created, nil
}

func (r customFieldDefRepository) Update(ctx context.Context, p UpdateCustomFieldDefParams) (CustomFieldDef, error) {
	if err := p.validate(); err != nil {
		return CustomFieldDef{}, err
	}

	var updated CustomFieldDef
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		current, err := q.GetCustomFieldDef(ctx, gen.GetCustomFieldDefParams{GroupID: r.group(), ID: p.ID})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("storage: custom field def %q: %w", p.ID, ErrNotFound)
		case err != nil:
			return fmt.Errorf("storage: get custom field def %q for update: %w", p.ID, err)
		}
		if current.Version != p.ExpectedVersion {
			return ErrVersionMismatch
		}

		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for custom field def update: %w", err)
		}

		rows, err := q.UpdateCustomFieldDef(ctx, gen.UpdateCustomFieldDefParams{
			Name:            p.Name,
			FieldType:       p.FieldType,
			DisplayOrder:    p.DisplayOrder,
			Now:             p.Now,
			ChangeSeq:       seq,
			GroupID:         r.group(),
			ID:              p.ID,
			ExpectedVersion: p.ExpectedVersion,
		})
		if err != nil {
			return fmt.Errorf("storage: update custom field def %q: %w", p.ID, err)
		}
		if rows != 1 {
			return fmt.Errorf("storage: update custom field def %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
				p.ID, rows, current.Version)
		}

		updated, err = q.GetCustomFieldDef(ctx, gen.GetCustomFieldDefParams{GroupID: r.group(), ID: p.ID})
		if err != nil {
			return fmt.Errorf("storage: read back updated custom field def %q: %w", p.ID, err)
		}
		return nil
	})
	if err != nil {
		return CustomFieldDef{}, err
	}
	return updated, nil
}

func (r customFieldDefRepository) Delete(ctx context.Context, id string, now int64) error {
	switch {
	case id == "":
		return errors.New("storage: Delete custom field def: id is empty")
	case now <= 0:
		return errors.New("storage: Delete custom field def: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for custom field def delete: %w", err)
		}

		rows, err := q.DeleteCustomFieldDef(ctx, gen.DeleteCustomFieldDefParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ID:        id,
		})
		if err != nil {
			return fmt.Errorf("storage: delete custom field def %q: %w", id, err)
		}
		switch {
		case rows == 0:
			return fmt.Errorf("storage: custom field def %q: %w", id, ErrNotFound)
		case rows != 1:
			return fmt.Errorf("storage: delete custom field def %q: matched %d rows, want 1; this should be unreachable given ux_custom_field_defs_group_id_id's uniqueness on (group_id, id) -- see this method's own doc for why the guard is written this way regardless",
				id, rows)
		}
		return nil
	})
}
