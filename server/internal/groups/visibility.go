package groups

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

var ErrVersionRequired = errors.New("groups: ExpectedVersion is required")

type UpdateVisibilityRequest struct {
	WarrantyVisible bool
	SaleVisible     bool
	PurchaseVisible bool
	ExpectedVersion int64
}

func UpdateVisibility(ctx context.Context, repo storage.GroupVisibilityRepository, req UpdateVisibilityRequest) (storage.GroupVisibility, error) {
	if repo == nil {
		return storage.GroupVisibility{}, errors.New("groups: UpdateVisibility needs a non-nil storage.GroupVisibilityRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.GroupVisibility{}, ErrVersionRequired
	}

	return repo.Update(ctx, storage.UpdateGroupVisibilityParams{
		WarrantyVisible: req.WarrantyVisible,
		SaleVisible:     req.SaleVisible,
		PurchaseVisible: req.PurchaseVisible,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
