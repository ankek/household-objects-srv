package locations

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

type CreateRequest struct {
	Name     string
	ParentID string
}

func Create(ctx context.Context, repo storage.LocationRepository, req CreateRequest) (storage.Location, error) {
	if repo == nil {
		return storage.Location{}, errors.New("locations: Create needs a non-nil storage.LocationRepository")
	}
	if req.Name == "" {
		return storage.Location{}, ErrNameRequired
	}

	id, err := newID()
	if err != nil {
		return storage.Location{}, err
	}

	return repo.Create(ctx, storage.CreateLocationParams{
		ID:       id,
		Name:     req.Name,
		ParentID: req.ParentID,
		Now:      time.Now().UnixMilli(),
	})
}
