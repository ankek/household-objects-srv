package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type ItemLabelRepository interface {
	ListForItem(ctx context.Context, itemID string) ([]Label, error)

	ItemIDsForLabel(ctx context.Context, labelID string) ([]string, error)

	Attach(ctx context.Context, p AttachLabelParams) error

	Detach(ctx context.Context, itemID, labelID string, now int64) error
}

type AttachLabelParams struct {
	ID      string
	ItemID  string
	LabelID string
	Now     int64
}

func (p AttachLabelParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: AttachLabelParams: ID is empty")
	case p.ItemID == "":
		return errors.New("storage: AttachLabelParams: ItemID is empty")
	case p.LabelID == "":
		return errors.New("storage: AttachLabelParams: LabelID is empty")
	case p.Now <= 0:
		return errors.New("storage: AttachLabelParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type itemLabelRepository struct {
	binding
}

func (r itemLabelRepository) ListForItem(ctx context.Context, itemID string) ([]Label, error) {
	if itemID == "" {
		return nil, errors.New("storage: ListForItem: itemID is empty")
	}

	if _, err := r.queries().GetItem(ctx, gen.GetItemParams{GroupID: r.group(), ItemID: itemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
		}
		return nil, fmt.Errorf("storage: get item %q for label list: %w", itemID, err)
	}

	rows, err := r.queries().ListItemLabels(ctx, gen.ListItemLabelsParams{GroupID: r.group(), ItemID: itemID})
	if err != nil {
		return nil, fmt.Errorf("storage: list labels for item %q: %w", itemID, err)
	}
	return rows, nil
}

func (r itemLabelRepository) ItemIDsForLabel(ctx context.Context, labelID string) ([]string, error) {
	if labelID == "" {
		return nil, errors.New("storage: ItemIDsForLabel: labelID is empty")
	}

	if _, err := r.queries().GetLabel(ctx, gen.GetLabelParams{GroupID: r.group(), ID: labelID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("storage: label %q: %w", labelID, ErrNotFound)
		}
		return nil, fmt.Errorf("storage: get label %q for item list: %w", labelID, err)
	}

	rows, err := r.queries().ListItemIDsByLabel(ctx, gen.ListItemIDsByLabelParams{GroupID: r.group(), LabelID: labelID})
	if err != nil {
		return nil, fmt.Errorf("storage: list items for label %q: %w", labelID, err)
	}
	return rows, nil
}

func (r itemLabelRepository) Attach(ctx context.Context, p AttachLabelParams) error {
	if err := p.validate(); err != nil {
		return err
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		_, err := attachItemLabelTx(ctx, tx, q, r.group(), p)
		return err
	})
}

func attachItemLabelTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p AttachLabelParams) (ItemLabelAssignment, error) {
	if _, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ItemLabelAssignment{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
		}
		return ItemLabelAssignment{}, fmt.Errorf("storage: get item %q for attach: %w", p.ItemID, err)
	}
	if _, err := q.GetLabel(ctx, gen.GetLabelParams{GroupID: group, ID: p.LabelID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ItemLabelAssignment{}, fmt.Errorf("storage: label %q: %w", p.LabelID, ErrNotFound)
		}
		return ItemLabelAssignment{}, fmt.Errorf("storage: get label %q for attach: %w", p.LabelID, err)
	}

	switch existing, err := q.GetItemLabel(ctx, gen.GetItemLabelParams{
		GroupID: group, ItemID: p.ItemID, LabelID: p.LabelID,
	}); {
	case err == nil:
		return existing, nil
	case errors.Is(err, sql.ErrNoRows):
	default:
		return ItemLabelAssignment{}, fmt.Errorf("storage: check existing item_label edge: %w", err)
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return ItemLabelAssignment{}, fmt.Errorf("storage: allocate change_seq for label attach: %w", err)
	}

	revived, err := q.ReviveItemLabel(ctx, gen.ReviveItemLabelParams{
		Now:       p.Now,
		ChangeSeq: seq,
		GroupID:   group,
		ItemID:    p.ItemID,
		LabelID:   p.LabelID,
	})
	if err != nil {
		return ItemLabelAssignment{}, fmt.Errorf("storage: revive item_label edge: %w", err)
	}
	if revived == 0 {
		if err := q.CreateItemLabel(ctx, gen.CreateItemLabelParams{
			ID:        p.ID,
			GroupID:   group,
			ItemID:    p.ItemID,
			LabelID:   p.LabelID,
			Now:       p.Now,
			ChangeSeq: seq,
		}); err != nil {
			return ItemLabelAssignment{}, fmt.Errorf("storage: create item_label edge: %w", err)
		}
	}

	row, err := q.GetItemLabel(ctx, gen.GetItemLabelParams{GroupID: group, ItemID: p.ItemID, LabelID: p.LabelID})
	if err != nil {
		return ItemLabelAssignment{}, fmt.Errorf("storage: read back attached item_label edge: %w", err)
	}
	return row, nil
}

func (r itemLabelRepository) Detach(ctx context.Context, itemID, labelID string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Detach: itemID is empty")
	case labelID == "":
		return errors.New("storage: Detach: labelID is empty")
	case now <= 0:
		return errors.New("storage: Detach: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for label detach: %w", err)
		}

		rows, err := q.DeleteItemLabel(ctx, gen.DeleteItemLabelParams{
			DeletedAt: sql.NullInt64{Int64: now, Valid: true},
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
			LabelID:   labelID,
		})
		if err != nil {
			return fmt.Errorf("storage: detach label %q from item %q: %w", labelID, itemID, err)
		}
		switch {
		case rows == 0:
			return fmt.Errorf("storage: label %q on item %q: %w", labelID, itemID, ErrNotFound)
		case rows != 1:
			return fmt.Errorf("storage: detach label %q from item %q: matched %d rows, want 1; ux_item_labels_group_item_label makes (group_id, item_id, label_id) unique among live rows, so this is reachable only if a predicate was lost from DeleteItemLabel -- see this method's own doc",
				labelID, itemID, rows)
		}
		return nil
	})
}
