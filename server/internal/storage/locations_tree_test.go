package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func treeShape(nodes []*LocationNode) string {
	var sb strings.Builder
	var walk func(ns []*LocationNode, depth int)
	walk = func(ns []*LocationNode, depth int) {
		for _, n := range ns {
			fmt.Fprintf(&sb, "%s%s(%d/%d)\n", strings.Repeat("  ", depth), n.Name, n.ItemCount, n.TotalItemCount)
			walk(n.Children, depth+1)
		}
	}
	walk(nodes, 0)
	return sb.String()
}

func assertTree(t *testing.T, nodes []*LocationNode, want string) {
	t.Helper()
	got := strings.TrimSpace(treeShape(nodes))
	want = strings.TrimSpace(want)
	if got != want {
		t.Fatalf("tree mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestTreeNestsAndRollsUpCounts(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-house", "House", "")
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "loc-house")
	seedLocationRow(t, f.storage, "groupA", "loc-shelf", "Shelf", "loc-garage")
	seedLocationRow(t, f.storage, "groupA", "loc-attic", "Attic", "loc-house")

	seedFilterItem(t, f.storage, "groupA", "itm-1", "Boiler", "", "loc-house", 10)
	seedFilterItem(t, f.storage, "groupA", "itm-2", "Drill", "", "loc-garage", 10)
	seedFilterItem(t, f.storage, "groupA", "itm-3", "Screws", "", "loc-shelf", 10)
	seedFilterItem(t, f.storage, "groupA", "itm-4", "Nails", "", "loc-shelf", 10)
	seedFilterItem(t, f.storage, "groupA", "itm-5", "Umbrella", "", "", 10)

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	assertTree(t, tree, `
House(1/4)
  Attic(0/0)
  Garage(1/3)
    Shelf(2/2)`)
}

func TestTreeOrdersSiblingsByNameThenID(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-root", "Root", "")
	seedLocationRow(t, f.storage, "groupA", "loc-c", "Cellar", "loc-root")
	seedLocationRow(t, f.storage, "groupA", "loc-a", "Attic", "loc-root")
	seedLocationRow(t, f.storage, "groupA", "loc-b2", "Box", "loc-root")
	seedLocationRow(t, f.storage, "groupA", "loc-b1", "Box", "loc-root")

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	assertTree(t, tree, `
Root(0/0)
  Attic(0/0)
  Box(0/0)
  Box(0/0)
  Cellar(0/0)`)

	boxes := tree[0].Children[1:3]
	if boxes[0].ID != "loc-b1" || boxes[1].ID != "loc-b2" {
		t.Fatalf("same-named siblings ordered %s then %s, want loc-b1 then loc-b2 (id is the tiebreak)", boxes[0].ID, boxes[1].ID)
	}
}

func TestTreeExcludesTombstonedLocationsAndTheirItems(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-house", "House", "")
	seedLocationRow(t, f.storage, "groupA", "loc-shed", "Shed", "loc-house")
	seedFilterItem(t, f.storage, "groupA", "itm-1", "Mower", "", "loc-shed", 10)
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE locations SET deleted_at = 99 WHERE id = 'loc-shed'`)
		return err
	}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	assertTree(t, tree, `House(0/0)`)
}

func TestTreePromotesAnOrphanRatherThanDroppingIt(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-house", "House", "")
	seedLocationRow(t, f.storage, "groupA", "loc-garage", "Garage", "loc-house")
	seedLocationRow(t, f.storage, "groupA", "loc-shelf", "Shelf", "loc-garage")
	seedFilterItem(t, f.storage, "groupA", "itm-1", "Drill", "", "loc-garage", 10)
	seedFilterItem(t, f.storage, "groupA", "itm-2", "Screws", "", "loc-shelf", 10)
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE locations SET deleted_at = 99 WHERE id = 'loc-house'`)
		return err
	}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	assertTree(t, tree, `
Garage(1/2)
  Shelf(1/1)`)
}

func TestTreeExcludesAnotherGroupsLocations(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-a-house", "House", "")
	seedLocationRow(t, f.storage, "groupB", "loc-b-house", "House", "")
	seedFilterItem(t, f.storage, "groupA", "itm-a", "A's boiler", "", "loc-a-house", 10)
	seedFilterItem(t, f.storage, "groupB", "itm-b1", "B's boiler", "", "loc-b-house", 10)
	seedFilterItem(t, f.storage, "groupB", "itm-b2", "B's mower", "", "loc-b-house", 10)

	treeA, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree(A): %v", err)
	}
	assertTree(t, treeA, `House(1/1)`)
	if treeA[0].ID != "loc-a-house" {
		t.Fatalf("household A's root is %q, want loc-a-house", treeA[0].ID)
	}

	treeB, err := f.scopeB.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree(B): %v", err)
	}
	assertTree(t, treeB, `House(2/2)`)
}

func TestTreeCountsOnlyLiveItems(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-house", "House", "")
	seedFilterItem(t, f.storage, "groupA", "itm-live", "Boiler", "", "loc-house", 10)
	seedFilterItem(t, f.storage, "groupA", "itm-dead", "Old boiler", "", "loc-house", 10)
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE items SET deleted_at = 99 WHERE id = 'itm-dead'`)
		return err
	}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	assertTree(t, tree, `House(1/1)`)
}

func TestTreeTerminatesOnACyclicEdgeSet(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-solo", "Solo", "")
	seedLocationRow(t, f.storage, "groupA", "loc-a", "A", "")
	seedLocationRow(t, f.storage, "groupA", "loc-b", "B", "loc-a")
	if err := f.storage.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE locations SET parent_id = 'loc-b' WHERE id = 'loc-a'`)
		return err
	}); err != nil {
		t.Fatalf("close the cycle: %v", err)
	}

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	assertTree(t, tree, `Solo(0/0)`)
}

func TestTreeLeafChildrenAreEmptyNotNil(t *testing.T) {
	f := newFilterFixture(t)
	seedLocationRow(t, f.storage, "groupA", "loc-house", "House", "")

	tree, err := f.scopeA.Locations().Tree(t.Context())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tree[0].Children == nil {
		t.Fatal("a leaf's Children is nil; it must be an empty slice so JSON renders [] rather than null")
	}
}

func TestSortNodesOrdersByNameThenID(t *testing.T) {
	nodes := []*LocationNode{
		{Location: Location{ID: "b", Name: "Box"}},
		{Location: Location{ID: "a", Name: "Box"}},
		{Location: Location{ID: "c", Name: "Attic"}},
	}
	sortNodes(nodes)
	got := nodes[0].ID + nodes[1].ID + nodes[2].ID
	if got != "cab" {
		t.Fatalf("sortNodes produced %q, want \"cab\" (Attic first, then Box by id)", got)
	}
}
