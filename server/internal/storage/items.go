package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type Item = gen.Item

type Page struct {
	Limit  int64
	Offset int64
}

var ErrShortCodeTaken = errors.New("storage: short code is already taken in this group")

var ErrVersionMismatch = errors.New("storage: item version does not match; it changed since it was last read")

type CreateItemParams struct {
	ID          string
	Name        string
	Description string
	LocationID  string
	Quantity    int64
	ShortCode   string
	Now         int64
}

func (p CreateItemParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateItemParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: CreateItemParams: Name is empty")
	case p.ShortCode == "":
		return errors.New("storage: CreateItemParams: ShortCode is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateItemParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateItemParams struct {
	ItemID          string
	Name            string
	Description     string
	LocationID      string
	Quantity        int64
	ExpectedVersion int64
	Now             int64
}

func (p UpdateItemParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("storage: UpdateItemParams: ItemID is empty")
	case p.Name == "":
		return errors.New("storage: UpdateItemParams: Name is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateItemParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateItemParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type ItemRepository interface {
	Get(ctx context.Context, itemID string) (Item, error)

	GetByShortCode(ctx context.Context, shortCode string) (Item, error)

	GetByIDs(ctx context.Context, ids []string) ([]Item, error)

	List(ctx context.Context, page Page) ([]Item, error)

	ListFiltered(ctx context.Context, f ItemFilter, page Page) ([]Item, error)

	Create(ctx context.Context, p CreateItemParams) (Item, error)

	Update(ctx context.Context, p UpdateItemParams) (Item, error)

	Delete(ctx context.Context, itemID string, now int64) error
}

type itemRepository struct {
	binding
}

func (r itemRepository) Get(ctx context.Context, itemID string) (Item, error) {
	item, err := r.queries().GetItem(ctx, gen.GetItemParams{
		GroupID: r.group(),
		ItemID:  itemID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Item{}, fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
	case err != nil:
		return Item{}, fmt.Errorf("storage: get item %q: %w", itemID, err)
	}
	return item, nil
}

func (r itemRepository) GetByShortCode(ctx context.Context, shortCode string) (Item, error) {
	item, err := r.queries().GetItemByShortCode(ctx, gen.GetItemByShortCodeParams{
		GroupID:   r.group(),
		ShortCode: shortCode,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Item{}, fmt.Errorf("storage: item with short code %q: %w", shortCode, ErrNotFound)
	case err != nil:
		return Item{}, fmt.Errorf("storage: get item by short code %q: %w", shortCode, err)
	}
	return item, nil
}

func (r itemRepository) GetByIDs(ctx context.Context, ids []string) ([]Item, error) {
	if len(ids) == 0 {
		return nil, errors.New("storage: get items by ids: ids must not be empty")
	}

	items, err := r.queries().GetItemsByIDs(ctx, gen.GetItemsByIDsParams{
		GroupID: r.group(),
		ItemIds: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: get items by ids: %w", err)
	}
	return items, nil
}

func (r itemRepository) List(ctx context.Context, page Page) ([]Item, error) {
	if page.Limit <= 0 {
		return nil, fmt.Errorf("storage: list items: limit must be positive, got %d", page.Limit)
	}
	if page.Offset < 0 {
		return nil, fmt.Errorf("storage: list items: offset must not be negative, got %d", page.Offset)
	}

	items, err := r.queries().ListItems(ctx, gen.ListItemsParams{
		GroupID:   r.group(),
		RowLimit:  page.Limit,
		RowOffset: page.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list items: %w", err)
	}
	return items, nil
}

func (r itemRepository) Create(ctx context.Context, p CreateItemParams) (Item, error) {
	if err := p.validate(); err != nil {
		return Item{}, err
	}

	var created Item
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createItemTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Item{}, err
	}
	return created, nil
}

func createItemTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateItemParams) (Item, error) {
	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Item{}, fmt.Errorf("storage: allocate change_seq for item: %w", err)
	}

	if err := q.CreateItem(ctx, gen.CreateItemParams{
		ID:          p.ID,
		GroupID:     group,
		Name:        p.Name,
		Description: p.Description,
		LocationID:  nullString(p.LocationID),
		Quantity:    p.Quantity,
		ShortCode:   p.ShortCode,
		Now:         p.Now,
		ChangeSeq:   seq,
	}); err != nil {
		if isUniqueConstraintViolation(err) {
			return Item{}, ErrShortCodeTaken
		}
		return Item{}, fmt.Errorf("storage: create item: %w", err)
	}

	created, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ID})
	if err != nil {
		return Item{}, fmt.Errorf("storage: read back created item %q: %w", p.ID, err)
	}
	return created, nil
}

func (r itemRepository) Update(ctx context.Context, p UpdateItemParams) (Item, error) {
	if err := p.validate(); err != nil {
		return Item{}, err
	}

	var updated Item
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateItemTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Item{}, err
	}
	return updated, nil
}

func updateItemTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateItemParams) (Item, error) {
	current, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Item{}, fmt.Errorf("storage: item %q: %w", p.ItemID, ErrNotFound)
	case err != nil:
		return Item{}, fmt.Errorf("storage: get item %q for update: %w", p.ItemID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Item{}, ErrVersionMismatch
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Item{}, fmt.Errorf("storage: allocate change_seq for item update: %w", err)
	}

	rows, err := q.UpdateItem(ctx, gen.UpdateItemParams{
		Name:            p.Name,
		Description:     p.Description,
		LocationID:      nullString(p.LocationID),
		Quantity:        p.Quantity,
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		ItemID:          p.ItemID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		return Item{}, fmt.Errorf("storage: update item %q: %w", p.ItemID, err)
	}
	if rows != 1 {
		return Item{}, fmt.Errorf("storage: update item %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.ItemID, rows, current.Version)
	}

	updated, err := q.GetItem(ctx, gen.GetItemParams{GroupID: group, ItemID: p.ItemID})
	if err != nil {
		return Item{}, fmt.Errorf("storage: read back updated item %q: %w", p.ItemID, err)
	}
	return updated, nil
}

func (r itemRepository) Delete(ctx context.Context, itemID string, now int64) error {
	switch {
	case itemID == "":
		return errors.New("storage: Delete item: itemID is empty")
	case now <= 0:
		return errors.New("storage: Delete item: now must be a positive Unix-millisecond timestamp")
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for item delete: %w", err)
		}
		deletedAt := sql.NullInt64{Int64: now, Valid: true}

		rows, err := q.DeleteItem(ctx, gen.DeleteItemParams{
			DeletedAt: deletedAt,
			Now:       now,
			ChangeSeq: seq,
			GroupID:   r.group(),
			ItemID:    itemID,
		})
		if err != nil {
			return fmt.Errorf("storage: delete item %q: %w", itemID, err)
		}
		switch {
		case rows == 0:
			return fmt.Errorf("storage: item %q: %w", itemID, ErrNotFound)
		case rows != 1:
			return fmt.Errorf("storage: delete item %q: matched %d rows, want 1; this should be unreachable given items' own primary key uniqueness -- see DeleteItem's own SQL doc for why the guard is written this way regardless",
				itemID, rows)
		}

		if _, err := q.DeleteItemWarranty(ctx, gen.DeleteItemWarrantyParams{
			DeletedAt: deletedAt, Now: now, ChangeSeq: seq, GroupID: r.group(), ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("storage: cascade-tombstone warranty for item %q: %w", itemID, err)
		}
		if _, err := q.DeleteItemSale(ctx, gen.DeleteItemSaleParams{
			DeletedAt: deletedAt, Now: now, ChangeSeq: seq, GroupID: r.group(), ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("storage: cascade-tombstone sale for item %q: %w", itemID, err)
		}
		if _, err := q.DeleteItemPurchase(ctx, gen.DeleteItemPurchaseParams{
			DeletedAt: deletedAt, Now: now, ChangeSeq: seq, GroupID: r.group(), ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("storage: cascade-tombstone purchase for item %q: %w", itemID, err)
		}

		if _, err := q.CascadeDeleteItemIdentifications(ctx, gen.CascadeDeleteItemIdentificationsParams{
			DeletedAt: deletedAt, Now: now, ChangeSeq: seq, GroupID: r.group(), ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("storage: cascade-tombstone identifications for item %q: %w", itemID, err)
		}
		if _, err := q.CascadeDeleteItemCustomFields(ctx, gen.CascadeDeleteItemCustomFieldsParams{
			DeletedAt: deletedAt, Now: now, ChangeSeq: seq, GroupID: r.group(), ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("storage: cascade-tombstone custom fields for item %q: %w", itemID, err)
		}
		if _, err := q.CascadeDeleteItemLabels(ctx, gen.CascadeDeleteItemLabelsParams{
			DeletedAt: deletedAt, Now: now, ChangeSeq: seq, GroupID: r.group(), ItemID: itemID,
		}); err != nil {
			return fmt.Errorf("storage: cascade-tombstone label assignments for item %q: %w", itemID, err)
		}

		return nil
	})
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
