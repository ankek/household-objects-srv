package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"testing"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := Open(t.Context(), Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func seedItem(t *testing.T, s *Storage, groupID, itemID string, updatedAt int64) {
	t.Helper()
	err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(),
			`INSERT OR IGNORE INTO groups (id, name, created_at, updated_at) VALUES (?, ?, 1, 1)`,
			groupID, groupID); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, short_code, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 1, ?)`,
			itemID, groupID, itemID+" name", itemID, updatedAt)
		return err
	})
	if err != nil {
		t.Fatalf("seed item %q in group %q: %v", itemID, groupID, err)
	}
}

func itemIDs(items []Item) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestScopedRepositoryNeverReachesAnotherGroup(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	seedItem(t, s, "groupA", "itemA2", 20)
	seedItem(t, s, "groupB", "itemB1", 30)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err := s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}

	listed, err := scopeA.Items().List(t.Context(), Page{Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := itemIDs(listed)
	if len(got) != 2 || got[0] != "itemA1" || got[1] != "itemA2" {
		t.Fatalf("group A listed %v; a scoped repository must return its own tenant's rows and nothing else", got)
	}

	if _, err := scopeA.Items().Get(t.Context(), "itemB1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group A reading group B's item: err = %v, want ErrNotFound; a cross-tenant read must be indistinguishable from a miss (404, never 403)", err)
	}

	own, err := scopeB.Items().Get(t.Context(), "itemB1")
	if err != nil {
		t.Fatalf("group B reading its own item: %v; without this the cross-tenant assertion above would pass against a row that simply does not exist", err)
	}
	if own.GroupID != "groupB" {
		t.Errorf("Get returned a row belonging to group %q", own.GroupID)
	}
}

func TestRepositoryCallSitesCarryNoGroupID(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)

	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	if scope.GroupID() != MustGroupID("groupA") {
		t.Fatalf("Scope.GroupID() = %v, want groupA", scope.GroupID())
	}

	repo := scope.Items()
	if _, err := repo.Get(t.Context(), "itemA1"); err != nil {
		t.Fatalf("Get supplied its own group and still failed: %v", err)
	}
	if _, err := repo.List(t.Context(), Page{Limit: 1}); err != nil {
		t.Fatalf("List supplied its own group and still failed: %v", err)
	}
}

func TestForGroupRefusesEveryUnscopedBinding(t *testing.T) {
	t.Run("zero group id", func(t *testing.T) {
		s := newTestStorage(t)
		scope, err := s.ForGroup(GroupID{})
		if !errors.Is(err, ErrNoGroup) {
			t.Fatalf("ForGroup(GroupID{}) error = %v, want ErrNoGroup", err)
		}
		if scope != nil {
			t.Error("ForGroup returned a Scope alongside its error; a caller that ignores the error must get nothing, not a repository bound to the empty string")
		}
	})

	t.Run("unopened storage", func(t *testing.T) {
		scope, err := (&Storage{}).ForGroup(MustGroupID("groupA"))
		if !errors.Is(err, ErrNoGroup) {
			t.Fatalf("ForGroup on a zero Storage: error = %v, want ErrNoGroup", err)
		}
		if scope != nil {
			t.Error("a zero Storage produced a Scope")
		}
	})

	t.Run("nil storage", func(t *testing.T) {
		var s *Storage
		if _, err := s.ForGroup(MustGroupID("groupA")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("ForGroup on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}

func TestZeroScopeYieldsNoRepositoryAtAll(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("calling Items() on a zero Scope returned a repository; the interface is what guarantees an unconstructed scope is nothing rather than something unscoped")
		}
	}()
	var scope Scope
	_ = scope.Items() //nolint:govet // deliberate nil-interface call; the panic is the guarantee
}

func TestListRefusesAnUnboundedWindow(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	for _, page := range []Page{{}, {Limit: -1}, {Limit: 10, Offset: -1}} {
		if _, err := scope.Items().List(t.Context(), page); err == nil {
			t.Errorf("List(%+v) succeeded; a non-positive limit or negative offset must be refused, not defaulted", page)
		}
	}
}

func TestStorageGroupIDsReturnsEveryCreatedGroup(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	seedItem(t, s, "groupA", "itemA2", 20)
	seedItem(t, s, "groupB", "itemB1", 30)
	seedItem(t, s, "groupC", "itemC1", 40)

	got, err := s.GroupIDs(t.Context())
	if err != nil {
		t.Fatalf("GroupIDs: %v", err)
	}

	ids := make([]string, 0, len(got))
	for _, g := range got {
		ids = append(ids, g.String())
	}
	sort.Strings(ids)
	want := []string{"groupA", "groupB", "groupC"}
	if len(ids) != len(want) {
		t.Fatalf("GroupIDs = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("GroupIDs = %v, want %v", ids, want)
		}
	}
}

func TestStorageGroupIDsOnAFreshDatabaseIsEmpty(t *testing.T) {
	s := newTestStorage(t)

	got, err := s.GroupIDs(t.Context())
	if err != nil {
		t.Fatalf("GroupIDs: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GroupIDs on a fresh database = %v, want empty", got)
	}
}

func TestStorageGroupIDsRefusesAnUnopenedStorage(t *testing.T) {
	t.Run("unopened storage", func(t *testing.T) {
		if _, err := (&Storage{}).GroupIDs(t.Context()); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("GroupIDs on a zero Storage: error = %v, want ErrNoGroup", err)
		}
	})

	t.Run("nil storage", func(t *testing.T) {
		var s *Storage
		if _, err := s.GroupIDs(t.Context()); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("GroupIDs on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}
