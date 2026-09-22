package reporting

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
)

type LocationItemCountRow struct {
	LocationID   string
	LocationName string
	ItemCount    int64
}

func ItemCountByLocation(ctx context.Context, locations storage.LocationRepository) ([]LocationItemCountRow, error) {
	if locations == nil {
		return nil, errors.New("reporting: ItemCountByLocation needs a non-nil storage.LocationRepository")
	}

	tree, err := locations.Tree(ctx)
	if err != nil {
		return nil, fmt.Errorf("reporting: item count by location: %w", err)
	}

	out := make([]LocationItemCountRow, 0, len(tree))
	var walk func(nodes []*storage.LocationNode)
	walk = func(nodes []*storage.LocationNode) {
		for _, n := range nodes {
			out = append(out, LocationItemCountRow{
				LocationID:   n.ID,
				LocationName: n.Name,
				ItemCount:    n.TotalItemCount,
			})
			walk(n.Children)
		}
	}
	walk(tree)
	return out, nil
}
