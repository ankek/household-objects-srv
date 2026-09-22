package storage

import (
	"reflect"
	"testing"
	"time"
)

func TestItemGetByIDsReturnsOnlyThisGroupsLiveItems(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	seedItem(t, s, "groupA", "itemA2", 20)
	seedItem(t, s, "groupB", "itemB1", 30)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}

	got, err := scopeA.Items().GetByIDs(t.Context(), []string{
		"itemA1", "itemA2", "itemB1", "does-not-exist-anywhere",
	})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}

	want := []string{"itemA1", "itemA2"}
	if got := itemIDs(got); !reflect.DeepEqual(got, want) {
		t.Errorf("GetByIDs returned ids %v, want %v (only groupA's own live items -- itemB1 and the "+
			"nonexistent id must both be silently absent)", got, want)
	}
}

func TestItemGetByIDsExcludesTombstonedItems(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	seedItem(t, s, "groupA", "itemA2", 20)

	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	if err := scope.Items().Delete(t.Context(), "itemA1", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete(itemA1): %v", err)
	}

	got, err := scope.Items().GetByIDs(t.Context(), []string{"itemA1", "itemA2"})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if want := []string{"itemA2"}; !reflect.DeepEqual(itemIDs(got), want) {
		t.Errorf("GetByIDs returned ids %v, want %v (the tombstoned item must be excluded)", itemIDs(got), want)
	}
}

func TestItemGetByIDsRejectsEmptyIDs(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	if _, err := scope.Items().GetByIDs(t.Context(), nil); err == nil {
		t.Fatal("GetByIDs(nil) succeeded, want an error")
	}
	if _, err := scope.Items().GetByIDs(t.Context(), []string{}); err == nil {
		t.Fatal("GetByIDs([]string{}) succeeded, want an error")
	}
}

func TestItemGetByIDsToleratesDuplicateAndUnknownIDsMixed(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	got, err := scope.Items().GetByIDs(t.Context(), []string{"itemA1", "itemA1", "unknown-1", "unknown-2"})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if want := []string{"itemA1"}; !reflect.DeepEqual(itemIDs(got), want) {
		t.Errorf("GetByIDs returned ids %v, want %v (one row per distinct matching id)", itemIDs(got), want)
	}
}
