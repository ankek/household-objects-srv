package reporting

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

const purchaseDateLayout = "2006-01-02"

var ErrDateRangeInvalid = errors.New("reporting: from and to must be real ISO-8601 calendar days (YYYY-MM-DD), with from no later than to")

type PurchaseRow struct {
	ItemID             string
	ItemName           string
	PurchasedOn        string
	Vendor             string
	PurchasePriceMinor int64
}

func PurchasesInRange(ctx context.Context, repo storage.ReportRepository, from, to string) ([]PurchaseRow, error) {
	if repo == nil {
		return nil, errors.New("reporting: PurchasesInRange needs a non-nil storage.ReportRepository")
	}

	fromDate, err := time.Parse(purchaseDateLayout, from)
	if err != nil {
		return nil, fmt.Errorf("%w: from %q: %v", ErrDateRangeInvalid, from, err)
	}
	toDate, err := time.Parse(purchaseDateLayout, to)
	if err != nil {
		return nil, fmt.Errorf("%w: to %q: %v", ErrDateRangeInvalid, to, err)
	}
	if toDate.Before(fromDate) {
		return nil, fmt.Errorf("%w: from %q is after to %q", ErrDateRangeInvalid, from, to)
	}

	rows, err := repo.PurchasesInRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("reporting: purchases in range: %w", err)
	}

	out := make([]PurchaseRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, PurchaseRow{
			ItemID:             r.ItemID,
			ItemName:           r.ItemName,
			PurchasedOn:        r.PurchasedOn,
			Vendor:             r.Vendor,
			PurchasePriceMinor: r.PurchasePriceMinor,
		})
	}
	return out, nil
}
