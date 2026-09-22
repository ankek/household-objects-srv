package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeStockAdjustmentRepository struct {
	listFn   func(ctx context.Context, itemID string) ([]storage.StockAdjustment, error)
	createFn func(ctx context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error)
}

func (f fakeStockAdjustmentRepository) List(ctx context.Context, itemID string) ([]storage.StockAdjustment, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeStockAdjustmentRepository) Create(ctx context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
	return f.createFn(ctx, p)
}

func TestCreateStockAdjustmentRejectsANilRepository(t *testing.T) {
	_, err := CreateStockAdjustment(context.Background(), nil, CreateStockAdjustmentRequest{ItemID: "itm-1", Delta: 1})
	if err == nil {
		t.Fatal("CreateStockAdjustment(nil repo, ...) = nil error, want a non-nil error")
	}
}

func TestCreateStockAdjustmentMintsAnIDAndStampsNow(t *testing.T) {
	var got storage.CreateStockAdjustmentParams
	repo := fakeStockAdjustmentRepository{
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			got = p
			return storage.StockAdjustment{ID: p.ID, ItemID: p.ItemID, Delta: p.Delta, Reason: p.Reason, Note: p.Note, ResultingQuantity: p.Delta, Version: 1}, nil
		},
	}
	_, err := CreateStockAdjustment(context.Background(), repo, CreateStockAdjustmentRequest{
		ItemID: "itm-1", Delta: 5, Reason: "restock", Note: "found a spare box",
	})
	if err != nil {
		t.Fatalf("CreateStockAdjustment: %v", err)
	}
	if got.ID == "" {
		t.Error("CreateStockAdjustment did not mint an ID before calling storage.CreateStockAdjustmentParams")
	}
	if got.Now <= 0 {
		t.Errorf("CreateStockAdjustment.Now = %d, want a positive Unix-millisecond timestamp", got.Now)
	}
	if got.ItemID != "itm-1" || got.Delta != 5 || got.Reason != "restock" || got.Note != "found a spare box" {
		t.Errorf("got %+v, want {ItemID:itm-1 Delta:5 Reason:restock Note:\"found a spare box\"}", got)
	}
}

func TestCreateStockAdjustmentAcceptsAZeroDelta(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			return storage.StockAdjustment{ID: p.ID, ItemID: p.ItemID, Delta: 0, Reason: p.Reason, ResultingQuantity: 5, Version: 1}, nil
		},
	}
	_, err := CreateStockAdjustment(context.Background(), repo, CreateStockAdjustmentRequest{
		ItemID: "itm-1", Delta: 0, Reason: "recounted, no change",
	})
	if err != nil {
		t.Fatalf("CreateStockAdjustment with a zero delta: %v, want nil error", err)
	}
}

func TestCreateStockAdjustmentAcceptsBlankReasonAndNote(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			return storage.StockAdjustment{ID: p.ID, ItemID: p.ItemID, Delta: p.Delta, ResultingQuantity: p.Delta, Version: 1}, nil
		},
	}
	_, err := CreateStockAdjustment(context.Background(), repo, CreateStockAdjustmentRequest{ItemID: "itm-1", Delta: 1})
	if err != nil {
		t.Fatalf("CreateStockAdjustment with a blank reason and note: %v, want nil error", err)
	}
}

func TestCreateStockAdjustmentPropagatesStorageErrors(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		createFn: func(context.Context, storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			return storage.StockAdjustment{}, storage.ErrNotFound
		},
	}
	_, err := CreateStockAdjustment(context.Background(), repo, CreateStockAdjustmentRequest{ItemID: "does-not-exist", Delta: 1})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("CreateStockAdjustment = %v, want storage.ErrNotFound", err)
	}
}
