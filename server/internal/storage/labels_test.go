package storage

import (
	"errors"
	"testing"
	"time"
)

func labelScope(t *testing.T) (scopeA Scope, scopeB Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupB", "itemB", 20)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err = s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return scopeA, scopeB
}

func TestLabelCreateStoresNameAndColor(t *testing.T) {
	scopeA, _ := labelScope(t)

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Kitchen", Color: "#FF8800", Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Kitchen" || created.Color != "#FF8800" {
		t.Errorf("got {name:%q color:%q}, want {\"Kitchen\" \"#FF8800\"}", created.Name, created.Color)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
	}
}

func TestLabelCreateRejectsADuplicateNameInTheSameGroup(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Kitchen", Color: "#888888", Now: now,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-2", Name: "Kitchen", Color: "#00FF00", Now: now,
	}); !errors.Is(err, ErrLabelNameConflict) {
		t.Fatalf("second Create (same name) = %v, want ErrLabelNameConflict (A109)", err)
	}

	rows, err := scopeA.Labels().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List = %d rows, want 1 -- the rejected create must not have landed", len(rows))
	}
}

func TestLabelCreateAllowsTheSameNameInDifferentGroups(t *testing.T) {
	scopeA, scopeB := labelScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-a", Name: "Kitchen", Color: "#888888", Now: now,
	}); err != nil {
		t.Fatalf("Create on groupA: %v", err)
	}
	if _, err := scopeB.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-b", Name: "Kitchen", Color: "#888888", Now: now,
	}); err != nil {
		t.Fatalf("Create on groupB (same name, different group) = %v, want success", err)
	}
}

func TestLabelCreateAllowsReusingATombstonedName(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Kitchen", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.Labels().Delete(t.Context(), created.ID, now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-2", Name: "Kitchen", Color: "#00FF00", Now: now + 2,
	}); err != nil {
		t.Fatalf("Create reusing a tombstoned name = %v, want success (A109)", err)
	}
}

func TestLabelListOrdersByNameThenID(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	for _, l := range []struct{ id, name string }{
		{"lbl-z", "Zzz"}, {"lbl-a", "Aaa"}, {"lbl-m", "Mmm"},
	} {
		if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
			ID: l.id, Name: l.name, Color: "#888888", Now: now,
		}); err != nil {
			t.Fatalf("Create %s: %v", l.id, err)
		}
	}

	rows, err := scopeA.Labels().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "Aaa" || rows[1].Name != "Mmm" || rows[2].Name != "Zzz" {
		t.Fatalf("List order = %+v, want Aaa, Mmm, Zzz", rows)
	}
}

func TestLabelListOnAGroupWithNoLabelsIsAnEmptySlice(t *testing.T) {
	scopeA, _ := labelScope(t)

	rows, err := scopeA.Labels().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("List = %#v, want a non-nil empty slice (sqlc emit_empty_slices)", rows)
	}
}

func TestLabelListExcludesAnotherGroupsRows(t *testing.T) {
	scopeA, scopeB := labelScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-a", Name: "A's own label", Color: "#888888", Now: now,
	}); err != nil {
		t.Fatalf("Create on groupA: %v", err)
	}

	rows, err := scopeB.Labels().List(t.Context())
	if err != nil {
		t.Fatalf("groupB List: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("groupB List = %+v, want empty -- groupA's label leaked into groupB's list", rows)
	}
}

func TestLabelGetRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Kitchen", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.Labels().Get(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get of groupA's row = %v, want ErrNotFound", err)
	}
	own, err := scopeA.Labels().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("groupA Get of its OWN row = %v, want success; without this the miss above proves nothing about scope", err)
	}
	if own.Name != "Kitchen" {
		t.Errorf("name = %q, want %q", own.Name, "Kitchen")
	}
}

func TestLabelUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Old name", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scopeA.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: "New name", Color: "#00FF00", ExpectedVersion: created.Version, Now: now + 1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}
	if updated.Name != "New name" || updated.Color != "#00FF00" {
		t.Errorf("got {name:%q color:%q}, want {\"New name\" \"#00FF00\"}", updated.Name, updated.Color)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestLabelUpdateToItsOwnCurrentNameIsNotAConflict(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Kitchen", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scopeA.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: "Kitchen", Color: "#00FF00", ExpectedVersion: 1, Now: now + 1,
	})
	if err != nil {
		t.Fatalf("Update (recolour only, same name) = %v, want success -- a label must not conflict with itself", err)
	}
	if updated.Color != "#00FF00" {
		t.Errorf("Color = %q, want #00FF00", updated.Color)
	}
}

func TestLabelUpdateRejectsRenamingOntoADifferentLiveLabel(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "Kitchen", Color: "#888888", Now: now,
	}); err != nil {
		t.Fatalf("Create lbl-1: %v", err)
	}
	garage, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-2", Name: "Garage", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create lbl-2: %v", err)
	}

	if _, err := scopeA.Labels().Update(t.Context(), UpdateLabelParams{
		ID: garage.ID, Name: "Kitchen", Color: "#888888", ExpectedVersion: 1, Now: now + 1,
	}); !errors.Is(err, ErrLabelNameConflict) {
		t.Fatalf("Update renaming lbl-2 onto lbl-1's name = %v, want ErrLabelNameConflict", err)
	}

	got, err := scopeA.Labels().Get(t.Context(), garage.ID)
	if err != nil {
		t.Fatalf("Get lbl-2 after the rejected rename: %v", err)
	}
	if got.Name != "Garage" || got.Version != 1 {
		t.Errorf("lbl-2 is now {name:%q version:%d}, want {\"Garage\" 1} -- the conflicting rename landed anyway", got.Name, got.Version)
	}
}

func TestLabelUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "alice", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: "bob", Color: "#888888", ExpectedVersion: 1, Now: now + 1,
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err = scopeA.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: "carol", Color: "#888888", ExpectedVersion: 1, Now: now + 2,
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale version = %v, want ErrVersionMismatch", err)
	}
	got, err := scopeA.Labels().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "bob" || got.Version != 2 {
		t.Errorf("after the rejected update the row is {name:%q version:%d}, want {\"bob\" 2} -- the conflicting write landed anyway", got.Name, got.Version)
	}
}

func TestLabelUpdateRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "alice", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = scopeB.Labels().Update(t.Context(), UpdateLabelParams{
		ID: created.ID, Name: "pwned", Color: "#000000", ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Update of groupA's row = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the row exists)", err)
	}

	got, err := scopeA.Labels().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's attempt: %v", err)
	}
	if got.Name != "alice" || got.Version != 1 {
		t.Errorf("groupA's row is now {name:%q version:%d}, want {\"alice\" 1} -- groupB's write landed despite the error", got.Name, got.Version)
	}
}

func TestLabelDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "alice", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.Labels().Delete(t.Context(), created.ID, now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Labels().Get(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Labels().Delete(t.Context(), created.ID, now+2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound (A93: not idempotent)", err)
	}
}

func TestLabelDeleteRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := labelScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "lbl-1", Name: "alice", Color: "#888888", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeB.Labels().Delete(t.Context(), created.ID, now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Delete of groupA's row = %v, want ErrNotFound", err)
	}
	got, err := scopeA.Labels().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's delete attempt = %v, want the row still there", err)
	}
	if got.Name != "alice" || got.DeletedAt.Valid {
		t.Errorf("groupA's row is now {name:%q deleted_at:%+v}, want {\"alice\" NULL} -- groupB's delete landed despite the error", got.Name, got.DeletedAt)
	}
}

func TestLabelDeleteRemovesExactlyOneRow(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	for _, l := range []struct{ id, name string }{
		{"lbl-1", "Kitchen"}, {"lbl-2", "Garage"}, {"lbl-3", "Attic"},
	} {
		if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
			ID: l.id, Name: l.name, Color: "#888888", Now: now,
		}); err != nil {
			t.Fatalf("Create %s: %v", l.id, err)
		}
	}

	if err := scopeA.Labels().Delete(t.Context(), "lbl-1", now); err != nil {
		t.Fatalf("Delete lbl-1: %v", err)
	}

	rows, err := scopeA.Labels().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("deleting lbl-1 left %d of 3 labels, want 2 -- the delete matched more than the row it named", len(rows))
	}
	for _, r := range rows {
		if r.ID == "lbl-1" {
			t.Errorf("lbl-1 survived its own delete")
		}
	}
}

func TestLabelRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := labelScope(t)
	now := time.Now().UnixMilli()

	cases := []struct {
		name string
		call func() error
	}{
		{"create without an id", func() error {
			_, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{Name: "x", Color: "#888888", Now: now})
			return err
		}},
		{"create without a name", func() error {
			_, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{ID: "lbl-x", Color: "#888888", Now: now})
			return err
		}},
		{"create without a color", func() error {
			_, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{ID: "lbl-x", Name: "x", Now: now})
			return err
		}},
		{"create without now", func() error {
			_, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{ID: "lbl-x", Name: "x", Color: "#888888"})
			return err
		}},
		{"update without an expected version", func() error {
			_, err := scopeA.Labels().Update(t.Context(), UpdateLabelParams{ID: "lbl-x", Name: "x", Color: "#888888", Now: now})
			return err
		}},
		{"delete without an id", func() error {
			return scopeA.Labels().Delete(t.Context(), "", now)
		}},
		{"delete without now", func() error {
			return scopeA.Labels().Delete(t.Context(), "lbl-x", 0)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s: got nil error, want a validation error", tc.name)
			}
		})
	}
}
