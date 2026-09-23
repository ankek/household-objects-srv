package items

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

const warrantyDateLayout = "2006-01-02"

var ErrWarrantyDateInvalid = errors.New("items: warranty dates must be ISO-8601 calendar days (YYYY-MM-DD), or empty")

type CreateWarrantyRequest struct {
	ItemID     string
	Holder     string
	Provider   string
	Notes      string
	StartsOn   string
	ExpiresOn  string
	IsLifetime bool
}

type UpdateWarrantyRequest struct {
	ItemID          string
	Holder          string
	Provider        string
	StartsOn        string
	ExpiresOn       string
	Notes           string
	IsLifetime      bool
	ExpectedVersion int64
}

func CreateWarranty(ctx context.Context, repo storage.WarrantyRepository, req CreateWarrantyRequest) (storage.Warranty, error) {
	if repo == nil {
		return storage.Warranty{}, errors.New("items: CreateWarranty needs a non-nil storage.WarrantyRepository")
	}
	if err := validateWarrantyDates(req.StartsOn, req.ExpiresOn); err != nil {
		return storage.Warranty{}, err
	}

	id, err := newID()
	if err != nil {
		return storage.Warranty{}, err
	}

	return repo.Create(ctx, storage.CreateWarrantyParams{
		ID:         id,
		ItemID:     req.ItemID,
		Holder:     req.Holder,
		Provider:   req.Provider,
		StartsOn:   req.StartsOn,
		ExpiresOn:  req.ExpiresOn,
		IsLifetime: req.IsLifetime,
		Notes:      req.Notes,
		Now:        time.Now().UnixMilli(),
	})
}

func UpdateWarranty(ctx context.Context, repo storage.WarrantyRepository, req UpdateWarrantyRequest) (storage.Warranty, error) {
	if repo == nil {
		return storage.Warranty{}, errors.New("items: UpdateWarranty needs a non-nil storage.WarrantyRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.Warranty{}, ErrVersionRequired
	}
	if err := validateWarrantyDates(req.StartsOn, req.ExpiresOn); err != nil {
		return storage.Warranty{}, err
	}

	return repo.Update(ctx, storage.UpdateWarrantyParams{
		ItemID:          req.ItemID,
		Holder:          req.Holder,
		Provider:        req.Provider,
		StartsOn:        req.StartsOn,
		ExpiresOn:       req.ExpiresOn,
		IsLifetime:      req.IsLifetime,
		Notes:           req.Notes,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}

func ValidateWarrantyDates(startsOn, expiresOn string) error {
	return validateWarrantyDates(startsOn, expiresOn)
}

func validateWarrantyDates(startsOn, expiresOn string) error {
	for _, d := range []struct{ field, value string }{
		{"StartsOn", startsOn},
		{"ExpiresOn", expiresOn},
	} {
		if d.value == "" {
			continue
		}
		parsed, err := time.Parse(warrantyDateLayout, d.value)
		if err != nil {
			return fmt.Errorf("%w (%s = %q)", ErrWarrantyDateInvalid, d.field, d.value)
		}
		if parsed.Format(warrantyDateLayout) != d.value {
			return fmt.Errorf("%w (%s = %q is not zero-padded; want %q)", ErrWarrantyDateInvalid, d.field, d.value, parsed.Format(warrantyDateLayout))
		}
	}
	return nil
}
