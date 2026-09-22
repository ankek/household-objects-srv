package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

type CreateStockAdjustmentRequest struct {
	ItemID string
	Delta  int64
	Reason string
	Note   string
}

func CreateStockAdjustment(ctx context.Context, repo storage.StockAdjustmentRepository, req CreateStockAdjustmentRequest) (storage.StockAdjustment, error) {
	if repo == nil {
		return storage.StockAdjustment{}, errors.New("items: CreateStockAdjustment needs a non-nil storage.StockAdjustmentRepository")
	}

	id, err := newID()
	if err != nil {
		return storage.StockAdjustment{}, err
	}

	return repo.Create(ctx, storage.CreateStockAdjustmentParams{
		ID:     id,
		ItemID: req.ItemID,
		Delta:  req.Delta,
		Reason: req.Reason,
		Note:   req.Note,
		Now:    time.Now().UnixMilli(),
	})
}
