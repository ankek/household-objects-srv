package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type ItemLabelAssignment = gen.ItemLabel

type SyncRepository interface {
	Items(ctx context.Context, since int64, limit int64) ([]Item, error)

	ItemsAtSeq(ctx context.Context, seq int64) ([]Item, error)

	Warranty(ctx context.Context, since int64, limit int64) ([]Warranty, error)

	WarrantyAtSeq(ctx context.Context, seq int64) ([]Warranty, error)

	Sale(ctx context.Context, since int64, limit int64) ([]Sale, error)

	SaleAtSeq(ctx context.Context, seq int64) ([]Sale, error)

	Purchase(ctx context.Context, since int64, limit int64) ([]Purchase, error)

	PurchaseAtSeq(ctx context.Context, seq int64) ([]Purchase, error)

	Identifications(ctx context.Context, since int64, limit int64) ([]Identification, error)

	IdentificationsAtSeq(ctx context.Context, seq int64) ([]Identification, error)

	ItemCustomFields(ctx context.Context, since int64, limit int64) ([]ItemCustomField, error)

	ItemCustomFieldsAtSeq(ctx context.Context, seq int64) ([]ItemCustomField, error)

	StockAdjustments(ctx context.Context, since int64, limit int64) ([]StockAdjustment, error)

	StockAdjustmentsAtSeq(ctx context.Context, seq int64) ([]StockAdjustment, error)

	Locations(ctx context.Context, since int64, limit int64) ([]Location, error)

	LocationsAtSeq(ctx context.Context, seq int64) ([]Location, error)

	Labels(ctx context.Context, since int64, limit int64) ([]Label, error)

	LabelsAtSeq(ctx context.Context, seq int64) ([]Label, error)

	ItemLabels(ctx context.Context, since int64, limit int64) ([]ItemLabelAssignment, error)

	ItemLabelsAtSeq(ctx context.Context, seq int64) ([]ItemLabelAssignment, error)

	Attachments(ctx context.Context, since int64, limit int64) ([]Attachment, error)

	AttachmentsAtSeq(ctx context.Context, seq int64) ([]Attachment, error)

	LowWatermark(ctx context.Context) (int64, error)

	SetLowWatermark(ctx context.Context, to int64) error
}

type syncRepository struct {
	binding
}

func (s syncRepository) Items(ctx context.Context, since, limit int64) ([]Item, error) {
	rows, err := s.queries().ListItemChangesSince(ctx, gen.ListItemChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) ItemsAtSeq(ctx context.Context, seq int64) ([]Item, error) {
	rows, err := s.queries().ListItemChangesAtSeq(ctx, gen.ListItemChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Warranty(ctx context.Context, since, limit int64) ([]Warranty, error) {
	rows, err := s.queries().ListItemWarrantyChangesSince(ctx, gen.ListItemWarrantyChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_warranty changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) WarrantyAtSeq(ctx context.Context, seq int64) ([]Warranty, error) {
	rows, err := s.queries().ListItemWarrantyChangesAtSeq(ctx, gen.ListItemWarrantyChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_warranty changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Sale(ctx context.Context, since, limit int64) ([]Sale, error) {
	rows, err := s.queries().ListItemSaleChangesSince(ctx, gen.ListItemSaleChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_sale changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) SaleAtSeq(ctx context.Context, seq int64) ([]Sale, error) {
	rows, err := s.queries().ListItemSaleChangesAtSeq(ctx, gen.ListItemSaleChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_sale changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Purchase(ctx context.Context, since, limit int64) ([]Purchase, error) {
	rows, err := s.queries().ListItemPurchaseChangesSince(ctx, gen.ListItemPurchaseChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_purchase changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) PurchaseAtSeq(ctx context.Context, seq int64) ([]Purchase, error) {
	rows, err := s.queries().ListItemPurchaseChangesAtSeq(ctx, gen.ListItemPurchaseChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_purchase changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Identifications(ctx context.Context, since, limit int64) ([]Identification, error) {
	rows, err := s.queries().ListItemIdentificationChangesSince(ctx, gen.ListItemIdentificationChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_identifications changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) IdentificationsAtSeq(ctx context.Context, seq int64) ([]Identification, error) {
	rows, err := s.queries().ListItemIdentificationChangesAtSeq(ctx, gen.ListItemIdentificationChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_identifications changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) ItemCustomFields(ctx context.Context, since, limit int64) ([]ItemCustomField, error) {
	rows, err := s.queries().ListItemCustomFieldChangesSince(ctx, gen.ListItemCustomFieldChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_custom_fields changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) ItemCustomFieldsAtSeq(ctx context.Context, seq int64) ([]ItemCustomField, error) {
	rows, err := s.queries().ListItemCustomFieldChangesAtSeq(ctx, gen.ListItemCustomFieldChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_custom_fields changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) StockAdjustments(ctx context.Context, since, limit int64) ([]StockAdjustment, error) {
	rows, err := s.queries().ListStockAdjustmentChangesSince(ctx, gen.ListStockAdjustmentChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list stock_adjustments changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) StockAdjustmentsAtSeq(ctx context.Context, seq int64) ([]StockAdjustment, error) {
	rows, err := s.queries().ListStockAdjustmentChangesAtSeq(ctx, gen.ListStockAdjustmentChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list stock_adjustments changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Locations(ctx context.Context, since, limit int64) ([]Location, error) {
	rows, err := s.queries().ListLocationChangesSince(ctx, gen.ListLocationChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list locations changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) LocationsAtSeq(ctx context.Context, seq int64) ([]Location, error) {
	rows, err := s.queries().ListLocationChangesAtSeq(ctx, gen.ListLocationChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list locations changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Labels(ctx context.Context, since, limit int64) ([]Label, error) {
	rows, err := s.queries().ListLabelChangesSince(ctx, gen.ListLabelChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list labels changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) LabelsAtSeq(ctx context.Context, seq int64) ([]Label, error) {
	rows, err := s.queries().ListLabelChangesAtSeq(ctx, gen.ListLabelChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list labels changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) ItemLabels(ctx context.Context, since, limit int64) ([]ItemLabelAssignment, error) {
	rows, err := s.queries().ListItemLabelChangesSince(ctx, gen.ListItemLabelChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_labels changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) ItemLabelsAtSeq(ctx context.Context, seq int64) ([]ItemLabelAssignment, error) {
	rows, err := s.queries().ListItemLabelChangesAtSeq(ctx, gen.ListItemLabelChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list item_labels changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) Attachments(ctx context.Context, since, limit int64) ([]Attachment, error) {
	rows, err := s.queries().ListAttachmentChangesSince(ctx, gen.ListAttachmentChangesSinceParams{
		GroupID: s.group(), Since: since, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list attachments changes since %d: %w", since, err)
	}
	return rows, nil
}

func (s syncRepository) AttachmentsAtSeq(ctx context.Context, seq int64) ([]Attachment, error) {
	rows, err := s.queries().ListAttachmentChangesAtSeq(ctx, gen.ListAttachmentChangesAtSeqParams{
		GroupID: s.group(), Seq: seq,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list attachments changes at seq %d: %w", seq, err)
	}
	return rows, nil
}

func (s syncRepository) LowWatermark(ctx context.Context) (int64, error) {
	watermark, err := s.queries().GetGroupTombstoneLowWatermark(ctx, s.group())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("storage: tombstone low watermark for group %q: %w", s.group(), ErrNotFound)
	case err != nil:
		return 0, fmt.Errorf("storage: get tombstone low watermark: %w", err)
	}
	return watermark, nil
}

func (s syncRepository) SetLowWatermark(ctx context.Context, to int64) error {
	if to < 0 {
		return fmt.Errorf("storage: SetLowWatermark: to must not be negative, got %d", to)
	}
	return s.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		rows, err := q.SetGroupTombstoneLowWatermark(ctx, gen.SetGroupTombstoneLowWatermarkParams{
			TombstoneLowWatermark: to,
			GroupID:               s.group(),
		})
		if err != nil {
			return fmt.Errorf("storage: set tombstone low watermark: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("storage: set tombstone low watermark for group %q: %w", s.group(), ErrNotFound)
		}
		return nil
	})
}

func (s *Storage) ForGroupSync(g GroupID) (SyncRepository, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return syncRepository{binding{q: gen.New(s.store.Reader()), gid: g, store: s.store}}, nil
}
