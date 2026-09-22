package labels

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

type CreateLabelRequest struct {
	Name  string
	Color string
}

type UpdateLabelRequest struct {
	LabelID         string
	Name            string
	Color           string
	ExpectedVersion int64
}

func CreateLabel(ctx context.Context, repo storage.LabelRepository, req CreateLabelRequest) (storage.Label, error) {
	if repo == nil {
		return storage.Label{}, errors.New("labels: CreateLabel needs a non-nil storage.LabelRepository")
	}
	if req.Name == "" {
		return storage.Label{}, ErrNameRequired
	}
	if err := ValidateColor(req.Color); err != nil {
		return storage.Label{}, err
	}

	id, err := newID()
	if err != nil {
		return storage.Label{}, err
	}

	return repo.Create(ctx, storage.CreateLabelParams{
		ID:    id,
		Name:  req.Name,
		Color: req.Color,
		Now:   time.Now().UnixMilli(),
	})
}

func UpdateLabel(ctx context.Context, repo storage.LabelRepository, req UpdateLabelRequest) (storage.Label, error) {
	if repo == nil {
		return storage.Label{}, errors.New("labels: UpdateLabel needs a non-nil storage.LabelRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.Label{}, ErrVersionRequired
	}
	if req.Name == "" {
		return storage.Label{}, ErrNameRequired
	}
	if err := ValidateColor(req.Color); err != nil {
		return storage.Label{}, err
	}

	return repo.Update(ctx, storage.UpdateLabelParams{
		ID:              req.LabelID,
		Name:            req.Name,
		Color:           req.Color,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
