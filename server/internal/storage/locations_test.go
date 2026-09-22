package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func locationScope(t *testing.T) (scopeA Scope, scopeB Scope) {
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

func locationStorageOf(t *testing.T, s Scope) *Storage {
	t.Helper()
	gs, ok := s.(groupScope)
	if !ok {
		t.Fatalf("Scope is a %T, not groupScope -- newTestStorageFromScope needs updating alongside it", s)
	}
	return &Storage{store: gs.store}
}

func TestLocationCreateRootStoresNameAndNilParent(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-1", Name: "Garage", Now: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Garage" || created.ParentID.Valid || created.Version != 1 {
		t.Fatalf("created = %+v, want {Name:Garage ParentID:NULL Version:1}", created)
	}
}

func TestLocationCreateWithLiveParentInSameGroupSucceeds(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-parent", Name: "House", Now: now}); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: "loc-parent", Now: now})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}
	if !child.ParentID.Valid || child.ParentID.String != "loc-parent" {
		t.Fatalf("child.ParentID = %+v, want loc-parent", child.ParentID)
	}
}

func TestLocationCreateRejectsUnknownParent(t *testing.T) {
	scopeA, _ := locationScope(t)
	_, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-1", Name: "Garage", ParentID: "does-not-exist", Now: time.Now().UnixMilli()})
	if !errors.Is(err, ErrLocationParentNotFound) {
		t.Fatalf("Create with unknown parent: err = %v, want ErrLocationParentNotFound", err)
	}
}

func TestLocationCreateRejectsTombstonedParent(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-dead", Name: "Old shed", Now: now}); err != nil {
		t.Fatalf("Create loc-dead: %v", err)
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: "loc-dead", Now: now}); err != nil {
		t.Fatalf("Delete loc-dead: %v", err)
	}

	_, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-1", Name: "Garage", ParentID: "loc-dead", Now: now})
	if !errors.Is(err, ErrLocationParentNotFound) {
		t.Fatalf("Create under a tombstoned parent: err = %v, want ErrLocationParentNotFound", err)
	}
}

func TestLocationCreateRejectsAnotherGroupsParent(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	bParent, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b-parent", Name: "B's house", Now: now})
	if err != nil {
		t.Fatalf("Create in group B: %v", err)
	}

	_, err = scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a-1", Name: "Garage", ParentID: bParent.ID, Now: now})
	if !errors.Is(err, ErrLocationParentNotFound) {
		t.Fatalf("Create in group A with group B's location as parent: err = %v, want ErrLocationParentNotFound", err)
	}
}

func TestLocationCreateParentForeignKeyIsTheBackstop(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	s := locationStorageOf(t, scopeA)
	now := time.Now().UnixMilli()

	bParent, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b-parent", Name: "B's house", Now: now})
	if err != nil {
		t.Fatalf("Create in group B: %v", err)
	}

	err = s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(t.Context(),
			`INSERT INTO locations (id, group_id, name, parent_id, created_at, updated_at, version, deleted_at, change_seq)
			 VALUES (?, ?, ?, ?, ?, ?, 1, NULL, 0)`,
			"loc-a-bypass", "groupA", "Garage", bParent.ID, now, now)
		return execErr
	})
	if err == nil {
		t.Fatal("raw INSERT with a cross-group parent_id succeeded; the composite FOREIGN KEY did not reject it -- cross-group re-parenting would be representable at the storage layer with no service check in front of it")
	}
	if !isForeignKeyConstraintViolation(err) {
		t.Fatalf("raw INSERT with a cross-group parent_id failed with %v, want a FOREIGN KEY constraint violation specifically (some other failure would not prove the FK is what stopped it)", err)
	}
}

func TestLocationGetRejectsAnotherGroupsLocation(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A's garage", Now: now})
	if err != nil {
		t.Fatalf("Create in group A: %v", err)
	}

	if _, err := scopeB.Locations().Get(t.Context(), a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group B Get(group A's location): err = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.Locations().Get(t.Context(), a.ID); err != nil {
		t.Fatalf("group A Get(its own location): %v, want success", err)
	}
}

func TestLocationListExcludesOtherGroupsRows(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	for _, id := range []string{"loc-a1", "loc-a2", "loc-a3"} {
		if _, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: id, Name: id, Now: now}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	if _, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b1", Name: "loc-b1", Now: now}); err != nil {
		t.Fatalf("Create loc-b1: %v", err)
	}

	rows, err := scopeA.Locations().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("group A's list has %d rows, want exactly 3 -- either group B's row leaked in or one of group A's own is missing: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.ID == "loc-b1" {
			t.Fatalf("group A's list contains group B's location %q -- cross-tenant leak", r.ID)
		}
	}
}

func TestLocationUpdateRenameOnlyKeepsParent(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	parent, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-parent", Name: "House", Now: now})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: parent.ID, Now: now})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}

	updated, err := scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: child.ID, Name: "Garage (renamed)", ParentID: parent.ID, ExpectedVersion: 1, Now: now,
	})
	if err != nil {
		t.Fatalf("Update (rename): %v", err)
	}
	if updated.Name != "Garage (renamed)" || !updated.ParentID.Valid || updated.ParentID.String != parent.ID || updated.Version != 2 {
		t.Fatalf("updated = %+v, want {Name:'Garage (renamed)' ParentID:%s Version:2}", updated, parent.ID)
	}
}

func TestLocationUpdateMovesToNewParent(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	oldParent, _ := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-old", Name: "Old house", Now: now})
	newParent, _ := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-new", Name: "New house", Now: now})
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: oldParent.ID, Now: now})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}

	updated, err := scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: child.ID, Name: child.Name, ParentID: newParent.ID, ExpectedVersion: 1, Now: now,
	})
	if err != nil {
		t.Fatalf("Update (move): %v", err)
	}
	if !updated.ParentID.Valid || updated.ParentID.String != newParent.ID {
		t.Fatalf("updated.ParentID = %+v, want %s", updated.ParentID, newParent.ID)
	}
}

func TestLocationUpdateCanMoveToRoot(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	parent, _ := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-parent", Name: "House", Now: now})
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: parent.ID, Now: now})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}

	updated, err := scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: child.ID, Name: child.Name, ParentID: "", ExpectedVersion: 1, Now: now,
	})
	if err != nil {
		t.Fatalf("Update (move to root): %v", err)
	}
	if updated.ParentID.Valid {
		t.Fatalf("updated.ParentID = %+v, want NULL (root)", updated.ParentID)
	}
}

func TestLocationMoveRejectsSelfParent(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A", Now: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: a.ID, Name: a.Name, ParentID: a.ID, ExpectedVersion: 1, Now: now,
	})
	if !errors.Is(err, ErrLocationCycle) {
		t.Fatalf("self-parent move: err = %v, want ErrLocationCycle", err)
	}
}

func TestLocationMoveRejectsDeepCycle(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A", Now: now})
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	b, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b", Name: "B", ParentID: a.ID, Now: now})
	if err != nil {
		t.Fatalf("Create B (child of A): %v", err)
	}
	c, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-c", Name: "C", ParentID: b.ID, Now: now})
	if err != nil {
		t.Fatalf("Create C (child of B): %v", err)
	}

	_, err = scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: a.ID, Name: a.Name, ParentID: c.ID, ExpectedVersion: a.Version, Now: now,
	})
	if !errors.Is(err, ErrLocationCycle) {
		t.Fatalf("moving A under its own grandchild C (A->B->C): err = %v, want ErrLocationCycle", err)
	}

	after, err := scopeA.Locations().Get(t.Context(), a.ID)
	if err != nil {
		t.Fatalf("Get A after rejected move: %v", err)
	}
	if after.Version != 1 {
		t.Fatalf("A's version after a rejected cyclic move is %d, want 1 -- the rejected move landed anyway", after.Version)
	}
}

func TestLocationMoveRejectsCycleThroughATombstonedNode(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A", Now: now})
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	b, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b", Name: "B", ParentID: a.ID, Now: now})
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}
	c, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-c", Name: "C", ParentID: b.ID, Now: now})
	if err != nil {
		t.Fatalf("Create C: %v", err)
	}
	d, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-d", Name: "D", ParentID: c.ID, Now: now})
	if err != nil {
		t.Fatalf("Create D: %v", err)
	}
	sA := locationStorageOf(t, scopeA)
	if err := sA.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(t.Context(),
			`UPDATE locations SET deleted_at = ?, updated_at = ?, version = version + 1
			 WHERE group_id = ? AND id = ?`,
			now, now, "groupA", c.ID)
		return execErr
	}); err != nil {
		t.Fatalf("raw tombstone of C: %v", err)
	}
	dAfter, err := scopeA.Locations().Get(t.Context(), d.ID)
	if err != nil {
		t.Fatalf("Get D after C's delete: %v (D must still be live and readable -- A108's own plain-delete contract)", err)
	}
	if !dAfter.ParentID.Valid || dAfter.ParentID.String != c.ID {
		t.Fatalf("D.ParentID after C's delete = %+v, want still %q -- A108 says a plain delete never rewrites a child's parent_id", dAfter.ParentID, c.ID)
	}

	_, err = scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: a.ID, Name: a.Name, ParentID: d.ID, ExpectedVersion: a.Version, Now: now,
	})
	if !errors.Is(err, ErrLocationCycle) {
		t.Fatalf("moving A under D (whose chain runs A->B->C(dead)->D): err = %v, want ErrLocationCycle -- a cycle through a tombstoned node was not caught", err)
	}
}

func TestLocationUpdateRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	b, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b", Name: "B's garage", Now: now})
	if err != nil {
		t.Fatalf("Create in group B: %v", err)
	}

	_, err = scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: b.ID, Name: "renamed", ExpectedVersion: 1, Now: now,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("group A Update(group B's location): err = %v, want ErrNotFound", err)
	}
	after, err := scopeB.Locations().Get(t.Context(), b.ID)
	if err != nil {
		t.Fatalf("Get B's own location afterwards: %v", err)
	}
	if after.Name != "B's garage" || after.Version != 1 {
		t.Fatalf("B's location is now %+v, want unchanged {Name:\"B's garage\" Version:1} -- group A's cross-group Update answered 404 but landed anyway", after)
	}
}

func TestLocationUpdateRejectsAnotherGroupsParent(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A", Now: now})
	if err != nil {
		t.Fatalf("Create in group A: %v", err)
	}
	bParent, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b-parent", Name: "B's house", Now: now})
	if err != nil {
		t.Fatalf("Create in group B: %v", err)
	}

	_, err = scopeA.Locations().Update(t.Context(), UpdateLocationParams{
		LocationID: a.ID, Name: a.Name, ParentID: bParent.ID, ExpectedVersion: 1, Now: now,
	})
	if !errors.Is(err, ErrLocationParentNotFound) {
		t.Fatalf("moving A under group B's location: err = %v, want ErrLocationParentNotFound", err)
	}
}

func TestLocationUpdateRejectsStaleVersion(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A", Now: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Locations().Update(t.Context(), UpdateLocationParams{LocationID: a.ID, Name: "A renamed", ExpectedVersion: 1, Now: now}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err = scopeA.Locations().Update(t.Context(), UpdateLocationParams{LocationID: a.ID, Name: "A renamed again", ExpectedVersion: 1, Now: now})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale version: err = %v, want ErrVersionMismatch", err)
	}
}

func TestLocationDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	a, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-a", Name: "A", Now: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: a.ID, Now: now}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Locations().Get(t.Context(), a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: err = %v, want ErrNotFound", err)
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: a.ID, Now: now}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeat Delete: err = %v, want ErrNotFound (A93: a second delete is 404, not 204)", err)
	}
}

func TestLocationDeleteRejectsAnotherGroupsLocation(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	b, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b", Name: "B", Now: now})
	if err != nil {
		t.Fatalf("Create in group B: %v", err)
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: b.ID, Now: now}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group A Delete(group B's location): err = %v, want ErrNotFound", err)
	}
	after, err := scopeB.Locations().Get(t.Context(), b.ID)
	if err != nil {
		t.Fatalf("Get B's own location afterwards: %v -- group A's cross-group Delete answered 404 but landed anyway", err)
	}
	if after.Version != 1 {
		t.Fatalf("B's location version = %d, want 1 unchanged", after.Version)
	}
}

func TestLocationDeleteRemovesExactlyOneRow(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	for _, id := range []string{"loc-1", "loc-2", "loc-3"} {
		if _, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: id, Name: id, Now: now}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: "loc-1", Now: now}); err != nil {
		t.Fatalf("Delete loc-1: %v", err)
	}

	rows, err := scopeA.Locations().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("deleting loc-1 left %d of 3 locations, want 2 -- the delete matched more than the row it named", len(rows))
	}
	for _, r := range rows {
		if r.ID == "loc-1" {
			t.Errorf("loc-1 survived its own delete")
		}
	}
}

func TestLocationDeleteDoesNotCascadeToLiveChildren(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	parent, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-parent", Name: "House", Now: now})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: parent.ID, Now: now})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}

	err = scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: parent.ID, Now: now})
	var notEmpty *LocationNotEmptyError
	if !errors.As(err, &notEmpty) {
		t.Fatalf("Delete of a parent with a live child: err = %v, want a *LocationNotEmptyError", err)
	}
	if notEmpty.ChildCount != 1 || notEmpty.ItemCount != 0 {
		t.Fatalf("refusal counts = %d children, %d items; want 1 and 0", notEmpty.ChildCount, notEmpty.ItemCount)
	}
	if untouched, getErr := scopeA.Locations().Get(t.Context(), parent.ID); getErr != nil {
		t.Fatalf("Get parent after a refused delete: %v -- a refusal must write nothing", getErr)
	} else if untouched.Version != 1 {
		t.Fatalf("parent.Version after a refused delete = %d, want 1 -- the refusal bumped a row", untouched.Version)
	}

	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{
		LocationID: parent.ID, Reassign: true, ReassignTo: "", Now: now,
	}); err != nil {
		t.Fatalf("Delete parent with reassignment to the root: %v", err)
	}

	childAfter, err := scopeA.Locations().Get(t.Context(), child.ID)
	if err != nil {
		t.Fatalf("Get child after parent's delete: %v -- the child must still be live and readable (A108: plain delete, no cascade)", err)
	}
	if childAfter.Version != 2 {
		t.Fatalf("child.Version after a reassigning delete = %d, want 2 -- the child's parent_id changed, so its version MUST bump or an offline client never learns it moved (P-2, FR-131)", childAfter.Version)
	}
	if childAfter.ParentID.Valid {
		t.Fatalf("child.ParentID after reassignment to the root = %+v, want NULL -- an empty ReassignTo makes children roots", childAfter.ParentID)
	}

	if _, err := scopeA.Locations().Get(t.Context(), parent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get tombstoned parent: err = %v, want ErrNotFound", err)
	}
	rows, err := scopeA.Locations().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, r := range rows {
		if r.ID == parent.ID {
			t.Fatalf("List still contains the tombstoned parent %q", parent.ID)
		}
	}
}

func TestLocationRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	cases := []struct {
		name string
		call func() error
	}{
		{"create without an id", func() error {
			_, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{Name: "x", Now: now})
			return err
		}},
		{"create without a name", func() error {
			_, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-x", Now: now})
			return err
		}},
		{"update without an expected version", func() error {
			_, err := scopeA.Locations().Update(t.Context(), UpdateLocationParams{LocationID: "loc-x", Name: "x", Now: now})
			return err
		}},
		{"update without a name", func() error {
			_, err := scopeA.Locations().Update(t.Context(), UpdateLocationParams{LocationID: "loc-x", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"delete without an id", func() error {
			return scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: "", Now: now})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("want a validation error, got nil")
			}
		})
	}
}

func seedItemInLocation(t *testing.T, s *Storage, groupID, itemID, locationID string) {
	t.Helper()
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`INSERT INTO items (id, group_id, name, short_code, location_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 1, 1)`,
			itemID, groupID, itemID+" name", itemID, locationID)
		return err
	}); err != nil {
		t.Fatalf("seed item %q in location %q: %v", itemID, locationID, err)
	}
}

func itemLocationOf(t *testing.T, s *Storage, groupID, itemID string) sql.NullString {
	t.Helper()
	var loc sql.NullString
	if err := s.store.Reader().QueryRowContext(t.Context(),
		`SELECT location_id FROM items WHERE group_id = ? AND id = ?`, groupID, itemID,
	).Scan(&loc); err != nil {
		t.Fatalf("read location_id of item %q: %v", itemID, err)
	}
	return loc
}

func TestLocationDeleteRefusesALocationHoldingItems(t *testing.T) {
	scopeA, _ := locationScope(t)
	s := locationStorageOf(t, scopeA)
	now := time.Now().UnixMilli()

	garage, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-garage", Name: "Garage", Now: now})
	if err != nil {
		t.Fatalf("Create Garage: %v", err)
	}
	seedItemInLocation(t, s, "groupA", "itm-drill", garage.ID)

	err = scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: garage.ID, Now: now})
	if !errors.Is(err, ErrLocationNotEmpty) {
		t.Fatalf("Delete of a location holding an item: err = %v, want ErrLocationNotEmpty", err)
	}
	var notEmpty *LocationNotEmptyError
	if !errors.As(err, &notEmpty) {
		t.Fatalf("err does not unwrap to *LocationNotEmptyError: %v -- the HTTP layer needs the counts, not just the verdict", err)
	}
	if notEmpty.ItemCount != 1 || notEmpty.ChildCount != 0 {
		t.Errorf("refusal counts = %d children, %d items; want 0 and 1", notEmpty.ChildCount, notEmpty.ItemCount)
	}

	if _, err := scopeA.Locations().Get(t.Context(), garage.ID); err != nil {
		t.Errorf("Garage after a refused delete: %v -- a refusal must leave the location live", err)
	}
	if got := itemLocationOf(t, s, "groupA", "itm-drill"); !got.Valid || got.String != garage.ID {
		t.Errorf("the item's location_id after a refused delete = %+v, want still %q", got, garage.ID)
	}
}

func TestLocationDeleteAllowsAnEmptyLocation(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	empty, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-empty", Name: "Empty shelf", Now: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: empty.ID, Now: now}); err != nil {
		t.Fatalf("Delete of an empty location: %v -- the guard must not require a reassignment for a location holding nothing", err)
	}
}

func TestLocationDeleteIgnoresTombstonedContents(t *testing.T) {
	scopeA, _ := locationScope(t)
	s := locationStorageOf(t, scopeA)
	now := time.Now().UnixMilli()

	parent, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-parent", Name: "House", Now: now})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: parent.ID, Now: now})
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}
	seedItemInLocation(t, s, "groupA", "itm-dead", parent.ID)

	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: child.ID, Now: now}); err != nil {
		t.Fatalf("Delete the (empty) child first: %v", err)
	}
	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(t.Context(),
			`UPDATE items SET deleted_at = ? WHERE group_id = ? AND id = ?`, now, "groupA", "itm-dead")
		return execErr
	}); err != nil {
		t.Fatalf("tombstone the item: %v", err)
	}

	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: parent.ID, Now: now}); err != nil {
		t.Fatalf("Delete of a location holding only TOMBSTONED contents: %v -- the counts must filter on deleted_at, or a household could never clean up a branch it already emptied", err)
	}
}

func TestLocationDeleteReassignsChildrenAndItemsToATarget(t *testing.T) {
	scopeA, _ := locationScope(t)
	s := locationStorageOf(t, scopeA)
	now := time.Now().UnixMilli()

	create := func(id, name, parent string) Location {
		t.Helper()
		loc, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: id, Name: name, ParentID: parent, Now: now})
		if err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		return loc
	}
	garage := create("loc-garage", "Garage", "")
	shelf := create("loc-shelf", "Shelf", garage.ID)
	basement := create("loc-basement", "Basement", "")
	attic := create("loc-attic", "Attic", "")
	atticBox := create("loc-attic-box", "Attic box", attic.ID)
	seedItemInLocation(t, s, "groupA", "itm-drill", garage.ID)
	seedItemInLocation(t, s, "groupA", "itm-skis", attic.ID)

	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{
		LocationID: garage.ID, Reassign: true, ReassignTo: basement.ID, Now: now,
	}); err != nil {
		t.Fatalf("reassigning Delete: %v", err)
	}

	shelfAfter, err := scopeA.Locations().Get(t.Context(), shelf.ID)
	if err != nil {
		t.Fatalf("Get the moved child: %v", err)
	}
	if !shelfAfter.ParentID.Valid || shelfAfter.ParentID.String != basement.ID {
		t.Errorf("the child's parent after reassignment = %+v, want %q", shelfAfter.ParentID, basement.ID)
	}
	if got := itemLocationOf(t, s, "groupA", "itm-drill"); !got.Valid || got.String != basement.ID {
		t.Errorf("the moved item's location = %+v, want %q", got, basement.ID)
	}
	if _, err := scopeA.Locations().Get(t.Context(), garage.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get the deleted location: err = %v, want ErrNotFound", err)
	}

	boxAfter, err := scopeA.Locations().Get(t.Context(), atticBox.ID)
	if err != nil {
		t.Fatalf("Get the control child: %v", err)
	}
	if !boxAfter.ParentID.Valid || boxAfter.ParentID.String != attic.ID {
		t.Errorf("an unrelated location's parent changed: %+v, want still %q -- the reassignment moved rows it did not hold", boxAfter.ParentID, attic.ID)
	}
	if got := itemLocationOf(t, s, "groupA", "itm-skis"); !got.Valid || got.String != attic.ID {
		t.Errorf("an unrelated item moved: %+v, want still %q", got, attic.ID)
	}
}

func TestLocationDeleteReassignmentIsShallow(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	create := func(id, name, parent string) Location {
		t.Helper()
		loc, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: id, Name: name, ParentID: parent, Now: now})
		if err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		return loc
	}
	house := create("loc-house", "House", "")
	garage := create("loc-garage", "Garage", house.ID)
	shelf := create("loc-shelf", "Shelf", garage.ID)
	box := create("loc-box", "Box", shelf.ID)

	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{
		LocationID: garage.ID, Reassign: true, ReassignTo: house.ID, Now: now,
	}); err != nil {
		t.Fatalf("reassigning Delete: %v", err)
	}

	shelfAfter, err := scopeA.Locations().Get(t.Context(), shelf.ID)
	if err != nil {
		t.Fatalf("Get shelf: %v", err)
	}
	if !shelfAfter.ParentID.Valid || shelfAfter.ParentID.String != house.ID {
		t.Errorf("the direct child moved to %+v, want %q", shelfAfter.ParentID, house.ID)
	}
	boxAfter, err := scopeA.Locations().Get(t.Context(), box.ID)
	if err != nil {
		t.Fatalf("Get box: %v", err)
	}
	if !boxAfter.ParentID.Valid || boxAfter.ParentID.String != shelf.ID {
		t.Errorf("the GRANDCHILD's parent = %+v, want still %q -- reassignment is shallow; a grandchild rides along with its own parent rather than being re-parented itself", boxAfter.ParentID, shelf.ID)
	}
	if boxAfter.Version != 1 {
		t.Errorf("the grandchild's version = %d, want 1 -- it did not change, so nothing should have been written to it", boxAfter.Version)
	}
}

func TestLocationDeleteRejectsAReassignTargetInsideItsOwnSubtree(t *testing.T) {
	scopeA, _ := locationScope(t)
	now := time.Now().UnixMilli()

	create := func(id, name, parent string) Location {
		t.Helper()
		loc, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: id, Name: name, ParentID: parent, Now: now})
		if err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		return loc
	}
	garage := create("loc-garage", "Garage", "")
	shelf := create("loc-shelf", "Shelf", garage.ID)
	box := create("loc-box", "Box", shelf.ID)

	for name, target := range map[string]string{
		"the location being deleted itself": garage.ID,
		"a direct child":                    shelf.ID,
		"a grandchild":                      box.ID,
	} {
		t.Run(name, func(t *testing.T) {
			err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{
				LocationID: garage.ID, Reassign: true, ReassignTo: target, Now: now,
			})
			if !errors.Is(err, ErrLocationReassignTarget) {
				t.Fatalf("reassigning to %s: err = %v, want ErrLocationReassignTarget", name, err)
			}
			if _, getErr := scopeA.Locations().Get(t.Context(), garage.ID); getErr != nil {
				t.Fatalf("Garage after a rejected reassignment: %v -- a rejection must write nothing", getErr)
			}
		})
	}
}

func TestLocationDeleteRejectsAnUnresolvableReassignTarget(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	now := time.Now().UnixMilli()

	garage, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-garage", Name: "Garage", Now: now})
	if err != nil {
		t.Fatalf("Create Garage: %v", err)
	}
	if _, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-shelf", Name: "Shelf", ParentID: garage.ID, Now: now}); err != nil {
		t.Fatalf("Create Shelf: %v", err)
	}
	dead, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-dead", Name: "Old shed", Now: now})
	if err != nil {
		t.Fatalf("Create the shed: %v", err)
	}
	if err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: dead.ID, Now: now}); err != nil {
		t.Fatalf("tombstone the shed: %v", err)
	}
	foreign, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-b", Name: "B's garage", Now: now})
	if err != nil {
		t.Fatalf("Create in group B: %v", err)
	}

	for name, target := range map[string]string{
		"an id that never existed": "loc-nowhere",
		"a tombstoned location":    dead.ID,
		"another household's":      foreign.ID,
	} {
		t.Run(name, func(t *testing.T) {
			err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{
				LocationID: garage.ID, Reassign: true, ReassignTo: target, Now: now,
			})
			if !errors.Is(err, ErrLocationParentNotFound) {
				t.Fatalf("reassigning to %s: err = %v, want ErrLocationParentNotFound", name, err)
			}
		})
	}

	if after, err := scopeB.Locations().Get(t.Context(), foreign.ID); err != nil {
		t.Fatalf("Get B's location afterwards: %v", err)
	} else if after.Version != 1 {
		t.Fatalf("B's location version = %d, want 1 -- household A's rejected reassignment wrote to it", after.Version)
	}
}

func TestLocationDeleteRefusalPrecedesNothingForAForeignID(t *testing.T) {
	scopeA, scopeB := locationScope(t)
	s := locationStorageOf(t, scopeA)
	now := time.Now().UnixMilli()

	garage, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-garage", Name: "Garage", Now: now})
	if err != nil {
		t.Fatalf("Create in group A: %v", err)
	}
	seedItemInLocation(t, s, "groupA", "itm-drill", garage.ID)

	err = scopeB.Locations().Delete(t.Context(), DeleteLocationParams{LocationID: garage.ID, Now: now})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("household B deleting household A's non-empty location: err = %v, want ErrNotFound", err)
	}
	if errors.Is(err, ErrLocationNotEmpty) {
		t.Fatal("household B learned that household A's location is non-empty; the guard must run AFTER the tenant boundary, never before it")
	}
}

func TestLocationDeleteRejectsAReassignToWithoutReassign(t *testing.T) {
	scopeA, _ := locationScope(t)
	err := scopeA.Locations().Delete(t.Context(), DeleteLocationParams{
		LocationID: "loc-x", Reassign: false, ReassignTo: "loc-y", Now: time.Now().UnixMilli(),
	})
	if err == nil {
		t.Fatal("Delete with ReassignTo set and Reassign false returned nil; want an error rather than a silent choice between two opposite readings")
	}
}
