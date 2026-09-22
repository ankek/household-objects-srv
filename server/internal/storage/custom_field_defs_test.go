package storage

import (
	"errors"
	"testing"
	"time"
)

func customFieldDefScope(t *testing.T) (scopeA Scope, scopeB Scope) {
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

func TestCustomFieldDefCreateStoresNameAndType(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Warranty length", FieldType: "text", DisplayOrder: 0, Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Warranty length" || created.FieldType != "text" {
		t.Errorf("got {name:%q field_type:%q}, want {\"Warranty length\" \"text\"}", created.Name, created.FieldType)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
	}
}

func TestCustomFieldDefCreateAllowsDuplicateNames(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Warranty length", FieldType: "text", Now: now,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-2", Name: "Warranty length", FieldType: "number", Now: now,
	}); err != nil {
		t.Fatalf("second Create (same name) = %v, want success -- A101.2 permits duplicate names", err)
	}

	rows, err := scopeA.CustomFieldDefs().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List = %d rows, want 2", len(rows))
	}
}

func TestCustomFieldDefCreateRejectsAnUnknownFieldTypeAtTheCheckConstraint(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)

	_, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Bad", FieldType: "not-a-real-type", Now: time.Now().UnixMilli(),
	})
	if err == nil {
		t.Fatal("Create with an unrecognised field_type succeeded; migration 0001's CHECK (field_type IN (...)) should have refused it")
	}
}

func TestCustomFieldDefListOrdersByDisplayOrderThenCreatedAtThenID(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-hi", Name: "High order", FieldType: "text", DisplayOrder: 10, Now: now,
	}); err != nil {
		t.Fatalf("Create cfd-hi: %v", err)
	}
	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-b", Name: "Tied b", FieldType: "text", DisplayOrder: 0, Now: now,
	}); err != nil {
		t.Fatalf("Create cfd-b: %v", err)
	}
	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-a", Name: "Tied a", FieldType: "text", DisplayOrder: 0, Now: now,
	}); err != nil {
		t.Fatalf("Create cfd-a: %v", err)
	}
	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-earlier", Name: "Earlier", FieldType: "text", DisplayOrder: 0, Now: now - 1000,
	}); err != nil {
		t.Fatalf("Create cfd-earlier: %v", err)
	}

	rows, err := scopeA.CustomFieldDefs().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var gotIDs []string
	for _, r := range rows {
		gotIDs = append(gotIDs, r.ID)
	}
	wantIDs := []string{"cfd-earlier", "cfd-a", "cfd-b", "cfd-hi"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("List returned %v, want %v", gotIDs, wantIDs)
	}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("List order = %v, want %v (display_order, then created_at, then id)", gotIDs, wantIDs)
		}
	}
}

func TestCustomFieldDefListOnAGroupWithNoDefinitionsIsAnEmptySlice(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)

	rows, err := scopeA.CustomFieldDefs().List(t.Context())
	if err != nil {
		t.Fatalf("List with no definitions = %v, want success", err)
	}
	if len(rows) != 0 {
		t.Errorf("List = %+v, want an empty slice", rows)
	}
}

func TestCustomFieldDefListExcludesAnotherGroupsRows(t *testing.T) {
	scopeA, scopeB := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-a", Name: "A's own def", FieldType: "text", Now: now,
	}); err != nil {
		t.Fatalf("Create on groupA: %v", err)
	}

	rows, err := scopeB.CustomFieldDefs().List(t.Context())
	if err != nil {
		t.Fatalf("groupB List: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("groupB List = %+v, want empty -- groupA's definition leaked into groupB's list", rows)
	}
}

func TestCustomFieldDefGetRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Warranty length", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.CustomFieldDefs().Get(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get of groupA's row = %v, want ErrNotFound", err)
	}
	own, err := scopeA.CustomFieldDefs().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("groupA Get of its OWN row = %v, want success; without this the miss above proves nothing about scope", err)
	}
	if own.Name != "Warranty length" {
		t.Errorf("name = %q, want %q", own.Name, "Warranty length")
	}
}

func TestCustomFieldDefUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Old name", FieldType: "text", DisplayOrder: 0, Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{
		ID: created.ID, Name: "New name", FieldType: "number", DisplayOrder: 5, ExpectedVersion: created.Version, Now: now + 1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}
	if updated.Name != "New name" || updated.FieldType != "number" || updated.DisplayOrder != 5 {
		t.Errorf("got {name:%q field_type:%q display_order:%d}, want {\"New name\" \"number\" 5}", updated.Name, updated.FieldType, updated.DisplayOrder)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestCustomFieldDefUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "alice", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{
		ID: created.ID, Name: "bob", FieldType: "text", ExpectedVersion: 1, Now: now + 1,
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err = scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{
		ID: created.ID, Name: "carol", FieldType: "text", ExpectedVersion: 1, Now: now + 2,
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale version = %v, want ErrVersionMismatch", err)
	}
	got, err := scopeA.CustomFieldDefs().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "bob" || got.Version != 2 {
		t.Errorf("after the rejected update the row is {name:%q version:%d}, want {\"bob\" 2} -- the conflicting write landed anyway", got.Name, got.Version)
	}
}

func TestCustomFieldDefUpdateRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "alice", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = scopeB.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{
		ID: created.ID, Name: "pwned", FieldType: "text", ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Update of groupA's row = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the row exists)", err)
	}

	got, err := scopeA.CustomFieldDefs().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's attempt: %v", err)
	}
	if got.Name != "alice" || got.Version != 1 {
		t.Errorf("groupA's row is now {name:%q version:%d}, want {\"alice\" 1} -- groupB's write landed despite the error", got.Name, got.Version)
	}
}

func TestCustomFieldDefDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "alice", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.CustomFieldDefs().Delete(t.Context(), created.ID, now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.CustomFieldDefs().Get(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.CustomFieldDefs().Delete(t.Context(), created.ID, now+2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-2", Name: "alice", FieldType: "text", Now: now + 3,
	}); err != nil {
		t.Fatalf("Create after Delete with the same name = %v, want success (A101.2: no uniqueness rule)", err)
	}
}

func TestCustomFieldDefDeleteRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "alice", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeB.CustomFieldDefs().Delete(t.Context(), created.ID, now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Delete of groupA's row = %v, want ErrNotFound", err)
	}
	got, err := scopeA.CustomFieldDefs().Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's delete attempt = %v, want the row still there", err)
	}
	if got.Name != "alice" || got.DeletedAt.Valid {
		t.Errorf("groupA's row is now {name:%q deleted_at:%+v}, want {\"alice\" NULL} -- groupB's delete landed despite the error", got.Name, got.DeletedAt)
	}
}

func TestCustomFieldDefDeleteRemovesExactlyOneRow(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	for _, id := range []string{"cfd-1", "cfd-2", "cfd-3"} {
		if _, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
			ID: id, Name: id, FieldType: "text", Now: now,
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	if err := scopeA.CustomFieldDefs().Delete(t.Context(), "cfd-1", now); err != nil {
		t.Fatalf("Delete cfd-1: %v", err)
	}

	rows, err := scopeA.CustomFieldDefs().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("deleting cfd-1 left %d of 3 definitions, want 2 -- the delete matched more than the row it named", len(rows))
	}
	for _, r := range rows {
		if r.ID == "cfd-1" {
			t.Errorf("cfd-1 survived its own delete")
		}
	}
}

func TestCustomFieldDefRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := customFieldDefScope(t)
	now := time.Now().UnixMilli()

	cases := []struct {
		name string
		call func() error
	}{
		{"create without an id", func() error {
			_, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{Name: "n", FieldType: "text", Now: now})
			return err
		}},
		{"create without a name", func() error {
			_, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{ID: "cfd-1", FieldType: "text", Now: now})
			return err
		}},
		{"create without a field type", func() error {
			_, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{ID: "cfd-1", Name: "n", Now: now})
			return err
		}},
		{"create without a timestamp", func() error {
			_, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{ID: "cfd-1", Name: "n", FieldType: "text"})
			return err
		}},
		{"update without an id", func() error {
			_, err := scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{Name: "n", FieldType: "text", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"update without an expected version", func() error {
			_, err := scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{ID: "cfd-1", Name: "n", FieldType: "text", Now: now})
			return err
		}},
		{"update without a name", func() error {
			_, err := scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{ID: "cfd-1", FieldType: "text", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"update without a field type", func() error {
			_, err := scopeA.CustomFieldDefs().Update(t.Context(), UpdateCustomFieldDefParams{ID: "cfd-1", Name: "n", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"delete without an id", func() error {
			return scopeA.CustomFieldDefs().Delete(t.Context(), "", now)
		}},
		{"delete without a timestamp", func() error {
			return scopeA.CustomFieldDefs().Delete(t.Context(), "cfd-1", 0)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("succeeded; an incomplete params struct must be refused before it reaches SQL")
			}
		})
	}
}
