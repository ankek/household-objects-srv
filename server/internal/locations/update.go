package locations

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

type UpdateRequest struct {
	LocationID      string
	Name            string
	ParentID        string
	ExpectedVersion int64
}

func Update(ctx context.Context, repo storage.LocationRepository, req UpdateRequest) (storage.Location, error) {
	if repo == nil {
		return storage.Location{}, errors.New("locations: Update needs a non-nil storage.LocationRepository")
	}
	if req.Name == "" {
		return storage.Location{}, ErrNameRequired
	}
	if req.ExpectedVersion <= 0 {
		return storage.Location{}, ErrVersionRequired
	}

	return repo.Update(ctx, storage.UpdateLocationParams{
		LocationID:      req.LocationID,
		Name:            req.Name,
		ParentID:        req.ParentID,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
