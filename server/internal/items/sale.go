package items

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

const saleDateLayout = warrantyDateLayout

var ErrSaleDateInvalid = errors.New("items: sale date must be an ISO-8601 calendar day (YYYY-MM-DD), or empty")

var ErrSalePriceNegative = errors.New("items: SalePriceMinor must not be negative")

type CreateSaleRequest struct {
	ItemID         string
	BuyerName      string
	Notes          string
	SoldOn         string
	SalePriceMinor int64
}

type UpdateSaleRequest struct {
	ItemID          string
	BuyerName       string
	SoldOn          string
	SalePriceMinor  int64
	Notes           string
	ExpectedVersion int64
}

func CreateSale(ctx context.Context, repo storage.SaleRepository, req CreateSaleRequest) (storage.Sale, error) {
	if repo == nil {
		return storage.Sale{}, errors.New("items: CreateSale needs a non-nil storage.SaleRepository")
	}
	if err := validateSaleDate(req.SoldOn); err != nil {
		return storage.Sale{}, err
	}
	if req.SalePriceMinor < 0 {
		return storage.Sale{}, fmt.Errorf("%w (got %d)", ErrSalePriceNegative, req.SalePriceMinor)
	}

	id, err := newID()
	if err != nil {
		return storage.Sale{}, err
	}

	return repo.Create(ctx, storage.CreateSaleParams{
		ID:             id,
		ItemID:         req.ItemID,
		BuyerName:      req.BuyerName,
		SoldOn:         req.SoldOn,
		SalePriceMinor: req.SalePriceMinor,
		Notes:          req.Notes,
		Now:            time.Now().UnixMilli(),
	})
}

func UpdateSale(ctx context.Context, repo storage.SaleRepository, req UpdateSaleRequest) (storage.Sale, error) {
	if repo == nil {
		return storage.Sale{}, errors.New("items: UpdateSale needs a non-nil storage.SaleRepository")
	}
	if req.ExpectedVersion <= 0 {
		return storage.Sale{}, ErrVersionRequired
	}
	if err := validateSaleDate(req.SoldOn); err != nil {
		return storage.Sale{}, err
	}
	if req.SalePriceMinor < 0 {
		return storage.Sale{}, fmt.Errorf("%w (got %d)", ErrSalePriceNegative, req.SalePriceMinor)
	}

	return repo.Update(ctx, storage.UpdateSaleParams{
		ItemID:          req.ItemID,
		BuyerName:       req.BuyerName,
		SoldOn:          req.SoldOn,
		SalePriceMinor:  req.SalePriceMinor,
		Notes:           req.Notes,
		ExpectedVersion: req.ExpectedVersion,
		Now:             time.Now().UnixMilli(),
	})
}

func ValidateSaleDate(soldOn string) error {
	return validateSaleDate(soldOn)
}

func validateSaleDate(soldOn string) error {
	if soldOn == "" {
		return nil
	}
	parsed, err := time.Parse(saleDateLayout, soldOn)
	if err != nil {
		return fmt.Errorf("%w (SoldOn = %q)", ErrSaleDateInvalid, soldOn)
	}
	if parsed.Format(saleDateLayout) != soldOn {
		return fmt.Errorf("%w (SoldOn = %q is not zero-padded; want %q)", ErrSaleDateInvalid, soldOn, parsed.Format(saleDateLayout))
	}
	return nil
}
