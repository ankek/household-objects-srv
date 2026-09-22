package storage

import (
	"errors"
	"sort"
	"testing"
	"time"
)

func itemLabelMatrix(t *testing.T) (scopeA Scope, scopeB Scope) {
	t.Helper()
	s := newTestStorage(t)
	for _, id := range []string{"itemA1", "itemA2", "itemA3"} {
		seedItem(t, s, "groupA", id, 10)
	}
	seedItem(t, s, "groupB", "itemB1", 20)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err = s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}

	now := time.Now().UnixMilli()
	for _, l := range []struct{ id, name string }{
		{"lblA1", "fragile"}, {"lblA2", "heavy"}, {"lblA3", "seasonal"},
	} {
		if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
			ID: l.id, Name: l.name, Color: "#888888", Now: now,
		}); err != nil {
			t.Fatalf("seed label %q: %v", l.id, err)
		}
	}
	if _, err := scopeB.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lblB1", Name: "b-only", Color: "#888888", Now: now,
	}); err != nil {
		t.Fatalf("seed group B label: %v", err)
	}

	for _, item := range []string{"itemA1", "itemA2", "itemA3"} {
		for _, label := range []string{"lblA1", "lblA2", "lblA3"} {
			if err := scopeA.ItemLabels().Attach(t.Context(), AttachLabelParams{
				ID: "edge-" + item + "-" + label, ItemID: item, LabelID: label, Now: now,
			}); err != nil {
				t.Fatalf("seed edge %s/%s: %v", item, label, err)
			}
		}
	}
	return scopeA, scopeB
}

func labelNames(rows []Label) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

func countLiveEdges(t *testing.T, scope Scope, itemIDs ...string) int {
	t.Helper()
	total := 0
	for _, id := range itemIDs {
		rows, err := scope.ItemLabels().ListForItem(t.Context(), id)
		if err != nil {
			t.Fatalf("ListForItem(%q): %v", id, err)
		}
		total += len(rows)
	}
	return total
}

func TestAttachThenListReturnsTheLabelsThemselves(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)

	rows, err := scopeA.ItemLabels().ListForItem(t.Context(), "itemA1")
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	got := labelNames(rows)
	want := []string{"fragile", "heavy", "seasonal"}
	if len(got) != len(want) {
		t.Fatalf("ListForItem = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListForItem = %v, want %v", got, want)
		}
	}
	if rows[0].Color == "" {
		t.Error("label rows carry no Color -- the join must return whole labels, not ids")
	}
}

func TestDetachRemovesExactlyOneEdge(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)

	if got := countLiveEdges(t, scopeA, "itemA1", "itemA2", "itemA3"); got != 9 {
		t.Fatalf("fixture holds %d live edges, want 9", got)
	}

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA2", "lblA2", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	if got := countLiveEdges(t, scopeA, "itemA1", "itemA2", "itemA3"); got != 8 {
		t.Fatalf("after one detach the matrix holds %d live edges, want exactly 8 -- fewer means the statement matched a whole row or column of the matrix, not one cell", got)
	}

	remaining := labelNames(mustList(t, scopeA, "itemA2"))
	if len(remaining) != 2 || remaining[0] != "fragile" || remaining[1] != "seasonal" {
		t.Errorf("itemA2 labels = %v, want [fragile seasonal] -- the detach took more than the one label it named", remaining)
	}

	for _, other := range []string{"itemA1", "itemA3"} {
		names := labelNames(mustList(t, scopeA, other))
		if len(names) != 3 {
			t.Errorf("%s labels = %v, want all three -- the detach reached past the item it named", other, names)
		}
	}
}

func mustList(t *testing.T, scope Scope, itemID string) []Label {
	t.Helper()
	rows, err := scope.ItemLabels().ListForItem(t.Context(), itemID)
	if err != nil {
		t.Fatalf("ListForItem(%q): %v", itemID, err)
	}
	return rows
}

func TestAttachIsIdempotent(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)

	if err := scopeA.ItemLabels().Attach(t.Context(), AttachLabelParams{
		ID: "edge-dup", ItemID: "itemA1", LabelID: "lblA1", Now: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("re-Attach of an already-attached label = %v, want nil (idempotent)", err)
	}
	if got := len(mustList(t, scopeA, "itemA1")); got != 3 {
		t.Errorf("itemA1 holds %d labels after a repeat attach, want 3 -- a duplicate edge was created", got)
	}
}

func TestReAttachRevivesTheTombstoneRatherThanInsertingASecondRow(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)
	now := time.Now().UnixMilli()

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA1", "lblA1", now); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if err := scopeA.ItemLabels().Attach(t.Context(), AttachLabelParams{
		ID: "edge-fresh", ItemID: "itemA1", LabelID: "lblA1", Now: now,
	}); err != nil {
		t.Fatalf("re-Attach after detach: %v", err)
	}

	rows := mustList(t, scopeA, "itemA1")
	if len(rows) != 3 {
		t.Fatalf("itemA1 holds %d labels after detach+re-attach, want 3", len(rows))
	}

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA1", "lblA1", now); err != nil {
		t.Fatalf("second Detach after revive = %v, want nil -- more than one live edge exists for this pair", err)
	}
}

func TestDetachOfAnUnattachedLabelIsNotFound(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)
	now := time.Now().UnixMilli()

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA1", "lblA1", now); err != nil {
		t.Fatalf("first Detach: %v", err)
	}
	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA1", "lblA1", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Detach = %v, want ErrNotFound", err)
	}
}

func TestAttachRejectsAnotherGroupsItem(t *testing.T) {
	_, scopeB := itemLabelMatrix(t)

	err := scopeB.ItemLabels().Attach(t.Context(), AttachLabelParams{
		ID: "edge-x", ItemID: "itemA1", LabelID: "lblB1", Now: time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Attach(group A's item, group B's own label) = %v, want ErrNotFound", err)
	}
}

func TestAttachRejectsAnotherGroupsLabel(t *testing.T) {
	_, scopeB := itemLabelMatrix(t)

	err := scopeB.ItemLabels().Attach(t.Context(), AttachLabelParams{
		ID: "edge-x", ItemID: "itemB1", LabelID: "lblA1", Now: time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Attach(group B's own item, group A's label) = %v, want ErrNotFound", err)
	}
}

func TestListForItemRejectsAnotherGroupsItem(t *testing.T) {
	_, scopeB := itemLabelMatrix(t)

	if _, err := scopeB.ItemLabels().ListForItem(t.Context(), "itemA1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListForItem(group A's item) = %v, want ErrNotFound -- never an empty list", err)
	}
}

func TestDetachRejectsAnotherGroupsEdge(t *testing.T) {
	scopeA, scopeB := itemLabelMatrix(t)

	err := scopeB.ItemLabels().Detach(t.Context(), "itemA1", "lblA1", time.Now().UnixMilli())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Detach(group A's edge) = %v, want ErrNotFound", err)
	}
	if got := countLiveEdges(t, scopeA, "itemA1", "itemA2", "itemA3"); got != 9 {
		t.Fatalf("group A holds %d live edges after group B's rejected detach, want 9 untouched", got)
	}
}

func TestTombstonedLabelDisappearsFromItsItemsWithoutCascading(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)

	if err := scopeA.Labels().Delete(t.Context(), "lblA2", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete label: %v", err)
	}

	for _, item := range []string{"itemA1", "itemA2", "itemA3"} {
		names := labelNames(mustList(t, scopeA, item))
		if len(names) != 2 {
			t.Errorf("%s labels = %v, want 2 -- a tombstoned label must not appear in an item's tag list", item, names)
		}
		for _, n := range names {
			if n == "heavy" {
				t.Errorf("%s still lists the tombstoned label", item)
			}
		}
	}

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA1", "lblA2", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Detach of an edge whose label was tombstoned = %v, want nil -- the label delete cascaded when A110 says it must not", err)
	}
}

func TestItemDeleteCascadesToItsLabelAssignments(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)

	if err := scopeA.Items().Delete(t.Context(), "itemA1", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete item: %v", err)
	}

	if got := countLiveEdges(t, scopeA, "itemA2", "itemA3"); got != 6 {
		t.Fatalf("surviving items hold %d live edges, want 6 -- the item cascade reached past the item it named", got)
	}

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA1", "lblA1", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Detach on a deleted item's edge = %v, want ErrNotFound -- the item delete did not cascade to item_labels", err)
	}
}

func TestItemIDsForLabelReturnsEveryTaggedItem(t *testing.T) {
	scopeA, _ := itemLabelMatrix(t)

	ids, err := scopeA.ItemLabels().ItemIDsForLabel(t.Context(), "lblA1")
	if err != nil {
		t.Fatalf("ItemIDsForLabel: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("ItemIDsForLabel = %v, want all three items", ids)
	}

	if err := scopeA.ItemLabels().Detach(t.Context(), "itemA2", "lblA1", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	ids, err = scopeA.ItemLabels().ItemIDsForLabel(t.Context(), "lblA1")
	if err != nil {
		t.Fatalf("ItemIDsForLabel after detach: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ItemIDsForLabel = %v, want 2 after one detach", ids)
	}
}

func TestItemIDsForLabelRejectsAnotherGroupsLabel(t *testing.T) {
	_, scopeB := itemLabelMatrix(t)

	if _, err := scopeB.ItemLabels().ItemIDsForLabel(t.Context(), "lblA1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ItemIDsForLabel(group A's label) = %v, want ErrNotFound", err)
	}
}
