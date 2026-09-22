package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

var ErrVersionRequired = errors.New("items: ExpectedVersion is required")

type UpdateRequest struct {
	ItemID          string
	Name            string
	Description     string
	LocationID      string
	Quantity        int64
	ExpectedVersion int64
}

func Update(ctx context.Context, repo storage.ItemRepository, req UpdateRequest) (storage.Item, error) {
	if repo == nil {
		return storage.Item{}, errors.New("items: Update needs a non-nil storage.ItemRepository")
	}
	if req.Name == "" {
		return storage.Item{}, ErrNameRequired
	}
	if req.ExpectedVersion <= 0 {
		return storage.Item{}, ErrVersionRequired
	}

	return repo.Update(ctx, storage.UpdateItemParams{
		ItemID:          req.ItemID,
		Name:            req.Name,
		Description:     req.Description,
		LocationID:      req.LocationID,
		Quantity:        req.Quantity,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
