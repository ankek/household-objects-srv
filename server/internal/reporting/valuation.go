package reporting

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
)

type GroupBy string

const (
	GroupByLocation GroupBy = "location"
	GroupByLabel    GroupBy = "label"
)

const unassignedLocationLabel = "Unassigned"

var ErrGroupByInvalid = errors.New(`reporting: group_by must be "location" or "label"`)

func ParseGroupBy(raw string) (GroupBy, error) {
	switch GroupBy(raw) {
	case GroupByLocation:
		return GroupByLocation, nil
	case GroupByLabel:
		return GroupByLabel, nil
	default:
		return "", fmt.Errorf("%w (got %q)", ErrGroupByInvalid, raw)
	}
}

type ValuationRow struct {
	GroupKey        string
	GroupLabel      string
	ItemCount       int64
	TotalValueMinor int64
}

func Valuation(ctx context.Context, repo storage.ReportRepository, groupBy GroupBy) ([]ValuationRow, error) {
	if repo == nil {
		return nil, errors.New("reporting: Valuation needs a non-nil storage.ReportRepository")
	}

	switch groupBy {
	case GroupByLocation:
		rows, err := repo.ValuationByLocation(ctx)
		if err != nil {
			return nil, fmt.Errorf("reporting: valuation by location: %w", err)
		}
		out := make([]ValuationRow, 0, len(rows))
		for _, r := range rows {
			label := r.LocationName
			if r.LocationID == "" {
				label = unassignedLocationLabel
			}
			out = append(out, ValuationRow{
				GroupKey:        r.LocationID,
				GroupLabel:      label,
				ItemCount:       r.ItemCount,
				TotalValueMinor: r.TotalValueMinor,
			})
		}
		return out, nil

	case GroupByLabel:
		rows, err := repo.ValuationByLabel(ctx)
		if err != nil {
			return nil, fmt.Errorf("reporting: valuation by label: %w", err)
		}
		out := make([]ValuationRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, ValuationRow{
				GroupKey:        r.LabelID,
				GroupLabel:      r.LabelName,
				ItemCount:       r.ItemCount,
				TotalValueMinor: r.TotalValueMinor,
			})
		}
		return out, nil

	default:
		return nil, fmt.Errorf("%w (got %q)", ErrGroupByInvalid, groupBy)
	}
}
