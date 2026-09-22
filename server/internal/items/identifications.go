package items

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

var identificationKinds = map[string]bool{
	"serial":    true,
	"model":     true,
	"asset_tag": true,
	"barcode":   true,
	"other":     true,
}

var ErrIdentificationKindInvalid = errors.New("items: kind must be one of serial, model, asset_tag, barcode, other")

var ErrIdentificationValueRequired = errors.New("items: value is required")

func ValidateIdentificationKind(kind string) error {
	if !identificationKinds[kind] {
		return fmt.Errorf("%w (got %q)", ErrIdentificationKindInvalid, kind)
	}
	return nil
}

type CreateIdentificationRequest struct {
	ItemID string
	Kind   string
	Value  string
}

type UpdateIdentificationRequest struct {
	ItemID           string
	IdentificationID string
	Kind             string
	Value            string
	ExpectedVersion  int64
}

func CreateIdentification(ctx context.Context, repo storage.IdentificationRepository, req CreateIdentificationRequest) (storage.Identification, error) {
	if repo == nil {
		return storage.Identification{}, errors.New("items: CreateIdentification needs a non-nil storage.IdentificationRepository")
	}
	if err := ValidateIdentificationKind(req.Kind); err != nil {
		return storage.Identification{}, err
	}
	if req.Value == "" {
		return storage.Identification{}, ErrIdentificationValueRequired
	}

	id, err := newID()
	if err != nil {
		return storage.Identification{}, err
	}

	return repo.Create(ctx, storage.CreateIdentificationParams{
		ID:     id,
		ItemID: req.ItemID,
		Kind:   req.Kind,
		Value:  req.Value,
		Now:    time.Now().UnixMilli(),
	})
}

func UpdateIdentification(ctx context.Context, repo storage.IdentificationRepository, req UpdateIdentificationRequest) (storage.Identification, error) {
	if repo == nil {
		return storage.Identification{}, errors.New("items: UpdateIdentification needs a non-nil storage.IdentificationRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.Identification{}, ErrVersionRequired
	}
	if err := ValidateIdentificationKind(req.Kind); err != nil {
		return storage.Identification{}, err
	}
	if req.Value == "" {
		return storage.Identification{}, ErrIdentificationValueRequired
	}

	return repo.Update(ctx, storage.UpdateIdentificationParams{
		ItemID:          req.ItemID,
		ID:              req.IdentificationID,
		Kind:            req.Kind,
		Value:           req.Value,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}
