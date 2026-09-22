package storage

import (
	"context"
	"fmt"
	"sort"
)

type LocationNode struct {
	Location

	ItemCount int64

	TotalItemCount int64

	Children []*LocationNode
}

func (r locationRepository) Tree(ctx context.Context) ([]*LocationNode, error) {
	rows, err := r.queries().ListLocations(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list locations for tree: %w", err)
	}

	counts, err := r.queries().CountLiveItemsPerLocation(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: count items per location: %w", err)
	}
	direct := make(map[string]int64, len(counts))
	for _, c := range counts {
		if c.LocationID.Valid {
			direct[c.LocationID.String] = c.ItemCount
		}
	}

	nodes := make(map[string]*LocationNode, len(rows))
	order := make([]*LocationNode, 0, len(rows))
	for _, row := range rows {
		node := &LocationNode{Location: row, ItemCount: direct[row.ID], Children: []*LocationNode{}}
		nodes[row.ID] = node
		order = append(order, node)
	}

	roots := make([]*LocationNode, 0, len(order))
	for _, node := range order {
		parent, ok := nodes[node.ParentID.String]
		if !node.ParentID.Valid || !ok {
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}

	for _, node := range roots {
		rollUp(node, make(map[*LocationNode]struct{}, len(order)))
	}
	return roots, nil
}

func rollUp(node *LocationNode, visited map[*LocationNode]struct{}) int64 {
	if _, seen := visited[node]; seen {
		return 0
	}
	visited[node] = struct{}{}

	total := node.ItemCount
	for _, child := range node.Children {
		total += rollUp(child, visited)
	}
	node.TotalItemCount = total
	return total
}

func sortNodes(nodes []*LocationNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID < nodes[j].ID
	})
}
