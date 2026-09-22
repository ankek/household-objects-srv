package labels

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

func AttachLabel(ctx context.Context, repo storage.ItemLabelRepository, itemID, labelID string) error {
	switch {
	case repo == nil:
		return errors.New("labels: AttachLabel needs a non-nil storage.ItemLabelRepository")
	case itemID == "":
		return errors.New("labels: AttachLabel: itemID is empty")
	case labelID == "":
		return errors.New("labels: AttachLabel: labelID is empty")
	}

	id, err := newID()
	if err != nil {
		return err
	}

	return repo.Attach(ctx, storage.AttachLabelParams{
		ID:      id,
		ItemID:  itemID,
		LabelID: labelID,
		Now:     time.Now().UnixMilli(),
	})
}

func DetachLabel(ctx context.Context, repo storage.ItemLabelRepository, itemID, labelID string) error {
	switch {
	case repo == nil:
		return errors.New("labels: DetachLabel needs a non-nil storage.ItemLabelRepository")
	case itemID == "":
		return errors.New("labels: DetachLabel: itemID is empty")
	case labelID == "":
		return errors.New("labels: DetachLabel: labelID is empty")
	}

	return repo.Detach(ctx, itemID, labelID, time.Now().UnixMilli())
}
