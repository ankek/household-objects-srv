package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Label = gen.Label

var ErrLabelNameConflict = errors.New("storage: a label with this name already exists in this group")

type CreateLabelParams struct {
	ID    string
	Name  string
	Color string
	Now   int64
}

func (p CreateLabelParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateLabelParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: CreateLabelParams: Name is empty")
	case p.Color == "":
		return errors.New("storage: CreateLabelParams: Color is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateLabelParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateLabelParams struct {
	ID              string
	Name            string
	Color           string
	ExpectedVersion int64
	Now             int64
}

func (p UpdateLabelParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: UpdateLabelParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: UpdateLabelParams: Name is empty")
	case p.Color == "":
		return errors.New("storage: UpdateLabelParams: Color is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateLabelParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateLabelParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type LabelRepository interface {
	Get(ctx context.Context, id string) (Label, error)

	List(ctx context.Context) ([]Label, error)

	Create(ctx context.Context, p CreateLabelParams) (Label, error)

	Update(ctx context.Context, p UpdateLabelParams) (Label, error)

	Delete(ctx context.Context, id string, now int64) error
}

type labelRepository struct {
	binding
}

func (r labelRepository) Get(ctx context.Context, id string) (Label, error) {
	row, err := r.queries().GetLabel(ctx, gen.GetLabelParams{
		GroupID: r.group(),
		ID:      id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Label{}, fmt.Errorf("storage: label %q: %w", id, ErrNotFound)
	case err != nil:
		return Label{}, fmt.Errorf("storage: get label %q: %w", id, err)
	}
	return row, nil
}

func (r labelRepository) List(ctx context.Context) ([]Label, error) {
	rows, err := r.queries().ListLabels(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list labels for group %q: %w", r.group(), err)
	}
	return rows, nil
}

func (r labelRepository) Create(ctx context.Context, p CreateLabelParams) (Label, error) {
	if err := p.validate(); err != nil {
		return Label{}, err
	}

	var created Label
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createLabelTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Label{}, err
	}
	return created, nil
}

func createLabelTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateLabelParams) (Label, error) {
	switch _, err := q.GetLabelByName(ctx, gen.GetLabelByNameParams{GroupID: group, Name: p.Name}); {
	case err == nil:
		return Label{}, ErrLabelNameConflict
	case errors.Is(err, sql.ErrNoRows):
	default:
		return Label{}, fmt.Errorf("storage: check label name conflict for %q: %w", p.Name, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Label{}, fmt.Errorf("storage: allocate change_seq for label: %w", err)
	}

	if err := q.CreateLabel(ctx, gen.CreateLabelParams{
		ID:        p.ID,
		GroupID:   group,
		Name:      p.Name,
		Color:     p.Color,
		Now:       p.Now,
		ChangeSeq: seq,
	}); err != nil {
		return Label{}, fmt.Errorf("storage: create label: %w", err)
	}

	created, err := q.GetLabel(ctx, gen.GetLabelParams{GroupID: group, ID: p.ID})
	if err != nil {
		return Label{}, fmt.Errorf("storage: read back created label %q: %w", p.ID, err)
	}
	return created, nil
}

func (r labelRepository) Update(ctx context.Context, p UpdateLabelParams) (Label, error) {
	if err := p.validate(); err != nil {
		return Label{}, err
	}

	var updated Label
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateLabelTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Label{}, err
	}
	return updated, nil
}

func updateLabelTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateLabelParams) (Label, error) {
	current, err := q.GetLabel(ctx, gen.GetLabelParams{GroupID: group, ID: p.ID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Label{}, fmt.Errorf("storage: label %q: %w", p.ID, ErrNotFound)
	case err != nil:
		return Label{}, fmt.Errorf("storage: get label %q for update: %w", p.ID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Label{}, ErrVersionMismatch
	}

	if conflict, err := q.GetLabelByName(ctx, gen.GetLabelByNameParams{GroupID: group, Name: p.Name}); err == nil {
		if conflict.ID != p.ID {
			return Label{}, ErrLabelNameConflict
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Label{}, fmt.Errorf("storage: check label name conflict for %q: %w", p.Name, err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Label{}, fmt.Errorf("storage: allocate change_seq for label update: %w", err)
	}

	rows, err := q.UpdateLabel(ctx, gen.UpdateLabelParams{
		Name:            p.Name,
		Color:           p.Color,
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		ID:              p.ID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return Label{}, fmt.Errorf("storage: update label %q: %w", p.ID, err)
	}
	if rows != 1 {
		return Label{}, fmt.Errorf("storage: update label %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ID, rows, current.Version)
	}

	updated, err := q.GetLabel(ctx, gen.GetLabelParams{GroupID: group, ID: p.ID})
	if err != nil {
		return Label{}, fmt.Errorf("storage: read back updated label %q: %w", p.ID, err)
	}
	return updated, nil
}

func (r labelRepository) Delete(ctx context.Context, id string, now int64) error {
	switch {
	case id == "":
		return errors.New("storage: Delete label: id is empty")
	case now <= 0:
		return errors.New("storage: Delete label: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for label delete: %w", err)
		}

		rows, err := q.DeleteLabel(ctx, gen.DeleteLabelParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ID:        id,
		})
		if err != nil {
			return fmt.Errorf("storage: delete label %q: %w", id, err)
		}
		switch {
		case rows == 0:
			return fmt.Errorf("storage: label %q: %w", id, ErrNotFound)
		case rows != 1:
			return fmt.Errorf("storage: delete label %q: matched %d rows, want 1; this should be unreachable given ux_labels_group_id_id's uniqueness on (group_id, id) -- see this method's own doc for why the guard is written this way regardless",
				id, rows)
		}
		return nil
	})
}
