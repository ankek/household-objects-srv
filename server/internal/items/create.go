package items

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

const maxShortCodeAttempts = 10

type CreateRequest struct {
	Name        string
	Description string
	LocationID  string
	Quantity    int64
}

func Create(ctx context.Context, repo storage.ItemRepository, req CreateRequest) (storage.Item, error) {
	if repo == nil {
		return storage.Item{}, errors.New("items: Create needs a non-nil storage.ItemRepository")
	}
	if req.Name == "" {
		return storage.Item{}, ErrNameRequired
	}

	id, err := newID()
	if err != nil {
		return storage.Item{}, err
	}
	now := time.Now().UnixMilli()

	for attempt := 0; attempt < maxShortCodeAttempts; attempt++ {
		code, err := newShortCode()
		if err != nil {
			return storage.Item{}, err
		}

		item, err := repo.Create(ctx, storage.CreateItemParams{
			ID:          id,
			Name:        req.Name,
			Description: req.Description,
			LocationID:  req.LocationID,
			Quantity:    req.Quantity,
			ShortCode:   code,
			Now:         now,
		})
		switch {
		case err == nil:
			return item, nil
		case errors.Is(err, storage.ErrShortCodeTaken):
			continue
		default:
			return storage.Item{}, err
		}
	}
	return storage.Item{}, fmt.Errorf("items: create: exhausted %d short-code attempts against a %d-symbol, %d-character alphabet -- this should not happen and points at a broken RNG or a corrupted index, not ordinary bad luck",
		maxShortCodeAttempts, len(shortCodeAlphabet), shortCodeLength)
}
