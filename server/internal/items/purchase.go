package items

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

const purchaseDateLayout = warrantyDateLayout

var ErrPurchaseDateInvalid = errors.New("items: purchase date must be an ISO-8601 calendar day (YYYY-MM-DD), or empty")

var ErrPurchasePriceNegative = errors.New("items: PurchasePriceMinor must not be negative")

type CreatePurchaseRequest struct {
	ItemID             string
	Vendor             string
	OrderReference     string
	Notes              string
	PurchasedOn        string
	PurchasePriceMinor int64
}

type UpdatePurchaseRequest struct {
	ItemID             string
	Vendor             string
	PurchasedOn        string
	PurchasePriceMinor int64
	OrderReference     string
	Notes              string
	ExpectedVersion    int64
}

func CreatePurchase(ctx context.Context, repo storage.PurchaseRepository, req CreatePurchaseRequest) (storage.Purchase, error) {
	if repo == nil {
		return storage.Purchase{}, errors.New("items: CreatePurchase needs a non-nil storage.PurchaseRepository")
	}
	if err := validatePurchaseDate(req.PurchasedOn); err != nil {
		return storage.Purchase{}, err
	}
	if req.PurchasePriceMinor < 0 {
		return storage.Purchase{}, fmt.Errorf("%w (got %d)", ErrPurchasePriceNegative, req.PurchasePriceMinor)
	}

	id, err := newID()
	if err != nil {
		return storage.Purchase{}, err
	}

	return repo.Create(ctx, storage.CreatePurchaseParams{
		ID:                 id,
		ItemID:             req.ItemID,
		Vendor:             req.Vendor,
		PurchasedOn:        req.PurchasedOn,
		PurchasePriceMinor: req.PurchasePriceMinor,
		OrderReference:     req.OrderReference,
		Notes:              req.Notes,
		Now:                time.Now().UnixMilli(),
	})
}

func UpdatePurchase(ctx context.Context, repo storage.PurchaseRepository, req UpdatePurchaseRequest) (storage.Purchase, error) {
	if repo == nil {
		return storage.Purchase{}, errors.New("items: UpdatePurchase needs a non-nil storage.PurchaseRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.Purchase{}, ErrVersionRequired
	}
	if err := validatePurchaseDate(req.PurchasedOn); err != nil {
		return storage.Purchase{}, err
	}
	if req.PurchasePriceMinor < 0 {
		return storage.Purchase{}, fmt.Errorf("%w (got %d)", ErrPurchasePriceNegative, req.PurchasePriceMinor)
	}

	return repo.Update(ctx, storage.UpdatePurchaseParams{
		ItemID:             req.ItemID,
		Vendor:             req.Vendor,
		PurchasedOn:        req.PurchasedOn,
		PurchasePriceMinor: req.PurchasePriceMinor,
		OrderReference:     req.OrderReference,
		Notes:              req.Notes,
		ExpectedVersion:    req.ExpectedVersion,
		Now:                time.Now().UnixMilli(),
	})
}

func validatePurchaseDate(purchasedOn string) error {
	if purchasedOn == "" {
		return nil
	}
	parsed, err := time.Parse(purchaseDateLayout, purchasedOn)
	if err != nil {
		return fmt.Errorf("%w (PurchasedOn = %q)", ErrPurchaseDateInvalid, purchasedOn)
	}
	if parsed.Format(purchaseDateLayout) != purchasedOn {
		return fmt.Errorf("%w (PurchasedOn = %q is not zero-padded; want %q)", ErrPurchaseDateInvalid, purchasedOn, parsed.Format(purchaseDateLayout))
	}
	return nil
}
