package reporting

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"time"
)

const warrantyDateLayout = "2006-01-02"

const DefaultWithinDays = 30

var ErrWithinDaysInvalid = errors.New("reporting: within_days must be zero or positive")

type WarrantyExpiringRow struct {
	ItemID        string
	ItemName      string
	ExpiresOn     string
	DaysRemaining int
}

func WarrantyExpiring(ctx context.Context, repo storage.ReportRepository, now time.Time, withinDays int) ([]WarrantyExpiringRow, error) {
	if repo == nil {
		return nil, errors.New("reporting: WarrantyExpiring needs a non-nil storage.ReportRepository")
	}
	if now.IsZero() {
		return nil, errors.New("reporting: WarrantyExpiring: now must not be the zero time")
	}
	if withinDays < 0 {
		return nil, ErrWithinDaysInvalid
	}

	asOfText := now.Format(warrantyDateLayout)
	asOf, err := time.Parse(warrantyDateLayout, asOfText)
	if err != nil {
		return nil, fmt.Errorf("reporting: WarrantyExpiring: format now as a calendar day: %w", err)
	}
	toText := asOf.AddDate(0, 0, withinDays).Format(warrantyDateLayout)

	rows, err := repo.WarrantyExpiring(ctx, asOfText, toText)
	if err != nil {
		return nil, fmt.Errorf("reporting: warranty expiring: %w", err)
	}

	out := make([]WarrantyExpiringRow, 0, len(rows))
	for _, r := range rows {
		expires, err := time.Parse(warrantyDateLayout, r.ExpiresOn)
		if err != nil {
			return nil, fmt.Errorf("reporting: warranty expiring: item %q: expires_on %q is not a real ISO-8601 calendar day: %w", r.ItemID, r.ExpiresOn, err)
		}
		out = append(out, WarrantyExpiringRow{
			ItemID:        r.ItemID,
			ItemName:      r.ItemName,
			ExpiresOn:     r.ExpiresOn,
			DaysRemaining: int(expires.Sub(asOf) / (24 * time.Hour)),
		})
	}
	return out, nil
}
