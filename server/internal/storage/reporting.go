package storage

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type LocationValuation struct {
	LocationID      string
	LocationName    string
	ItemCount       int64
	TotalValueMinor int64
}

type LabelValuation struct {
	LabelID         string
	LabelName       string
	ItemCount       int64
	TotalValueMinor int64
}

type WarrantyExpiring struct {
	ItemID    string
	ItemName  string
	ExpiresOn string
}

type PurchaseInRange struct {
	ItemID             string
	ItemName           string
	PurchasedOn        string
	Vendor             string
	PurchasePriceMinor int64
}

type ReportRepository interface {
	ValuationByLocation(ctx context.Context) ([]LocationValuation, error)

	ValuationByLabel(ctx context.Context) ([]LabelValuation, error)

	WarrantyExpiring(ctx context.Context, fromDate, toDate string) ([]WarrantyExpiring, error)

	PurchasesInRange(ctx context.Context, fromDate, toDate string) ([]PurchaseInRange, error)
}

type reportRepository struct {
	binding
}

func (r reportRepository) ValuationByLocation(ctx context.Context) ([]LocationValuation, error) {
	rows, err := r.queries().ValuationByLocation(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: valuation by location for group %q: %w", r.group(), err)
	}
	out := make([]LocationValuation, 0, len(rows))
	for _, row := range rows {
		out = append(out, LocationValuation{
			LocationID:      row.LocationID.String,
			LocationName:    row.LocationName.String,
			ItemCount:       row.ItemCount,
			TotalValueMinor: row.TotalValueMinor,
		})
	}
	return out, nil
}

func (r reportRepository) ValuationByLabel(ctx context.Context) ([]LabelValuation, error) {
	rows, err := r.queries().ValuationByLabel(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: valuation by label for group %q: %w", r.group(), err)
	}
	out := make([]LabelValuation, 0, len(rows))
	for _, row := range rows {
		out = append(out, LabelValuation{
			LabelID:         row.LabelID,
			LabelName:       row.LabelName,
			ItemCount:       row.ItemCount,
			TotalValueMinor: row.TotalValueMinor,
		})
	}
	return out, nil
}

func (r reportRepository) WarrantyExpiring(ctx context.Context, fromDate, toDate string) ([]WarrantyExpiring, error) {
	if fromDate == "" || toDate == "" {
		return nil, fmt.Errorf("storage: WarrantyExpiring: fromDate and toDate are both required")
	}
	rows, err := r.queries().ListWarrantyExpiring(ctx, gen.ListWarrantyExpiringParams{
		GroupID:  r.group(),
		FromDate: sql.NullString{String: fromDate, Valid: true},
		ToDate:   sql.NullString{String: toDate, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("storage: warranty expiring for group %q: %w", r.group(), err)
	}
	out := make([]WarrantyExpiring, 0, len(rows))
	for _, row := range rows {
		out = append(out, WarrantyExpiring{
			ItemID:    row.ItemID,
			ItemName:  row.ItemName,
			ExpiresOn: row.ExpiresOn.String,
		})
	}
	return out, nil
}

func (r reportRepository) PurchasesInRange(ctx context.Context, fromDate, toDate string) ([]PurchaseInRange, error) {
	if fromDate == "" || toDate == "" {
		return nil, fmt.Errorf("storage: PurchasesInRange: fromDate and toDate are both required")
	}
	rows, err := r.queries().ListPurchasesInRange(ctx, gen.ListPurchasesInRangeParams{
		GroupID:  r.group(),
		FromDate: sql.NullString{String: fromDate, Valid: true},
		ToDate:   sql.NullString{String: toDate, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("storage: purchases in range for group %q: %w", r.group(), err)
	}
	out := make([]PurchaseInRange, 0, len(rows))
	for _, row := range rows {
		out = append(out, PurchaseInRange{
			ItemID:             row.ItemID,
			ItemName:           row.ItemName,
			PurchasedOn:        row.PurchasedOn.String,
			Vendor:             row.Vendor,
			PurchasePriceMinor: row.PurchasePriceMinor,
		})
	}
	return out, nil
}
