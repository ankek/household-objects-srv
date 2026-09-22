package storage

import (
	"errors"
	"testing"
	"time"
)

func identificationScope(t *testing.T) (scopeA Scope, scopeB Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupA", "itemA2", 11)
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

func TestIdentificationCreateStoresKindAndValue(t *testing.T) {
	scopeA, _ := identificationScope(t)

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "SN-12345", Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Kind != "serial" || created.Value != "SN-12345" {
		t.Errorf("got {kind:%q value:%q}, want {\"serial\" \"SN-12345\"}", created.Kind, created.Value)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
	}
	if created.ItemID != "itemA" {
		t.Errorf("ItemID = %q, want %q", created.ItemID, "itemA")
	}
}

func TestIdentificationCreateAllowsMultipleRowsOfTheSameKind(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "barcode", Value: "0000000001", Now: now,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-2", ItemID: "itemA", Kind: "barcode", Value: "0000000002", Now: now,
	}); err != nil {
		t.Fatalf("second Create (same kind, different value) = %v, want success -- FR-015 is 0..N, not 1:0..1", err)
	}

	rows, err := scopeA.Identifications().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List = %d rows, want 2", len(rows))
	}
}

func TestIdentificationCreateRejectsAnItemFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := identificationScope(t)
	now := time.Now().UnixMilli()

	_, err := scopeB.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-x", ItemID: "itemA", Kind: "serial", Value: "pwned", Now: now,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Create on groupA's item = %v, want ErrNotFound (P-3: a foreign id must be indistinguishable from an unknown one)", err)
	}

	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-a", ItemID: "itemA", Kind: "serial", Value: "SN-1", Now: now,
	}); err != nil {
		t.Fatalf("groupA Create on its OWN item = %v, want success; without this the refusal above proves nothing about scope", err)
	}
}

func TestIdentificationListRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := identificationScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "SN-1", Now: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.Identifications().List(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB List of groupA's item = %v, want ErrNotFound", err)
	}
	rows, err := scopeA.Identifications().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("groupA List of its OWN item = %v, want success; without this the miss above proves nothing about scope", err)
	}
	if len(rows) != 1 || rows[0].Value != "SN-1" {
		t.Errorf("groupA's own list = %+v, want exactly one row {value:\"SN-1\"}", rows)
	}
}

func TestIdentificationListOnAnItemWithNoRowsIsAnEmptySlice(t *testing.T) {
	scopeA, _ := identificationScope(t)

	rows, err := scopeA.Identifications().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List on an item with no identifications = %v, want success", err)
	}
	if len(rows) != 0 {
		t.Errorf("List = %+v, want an empty slice", rows)
	}

	if _, err := scopeA.Identifications().List(t.Context(), "no-such-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("List on an unknown item = %v, want ErrNotFound", err)
	}
}

func TestIdentificationListExcludesOtherItemsRowsInTheSameGroup(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "itemA's own row", Now: now,
	}); err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}
	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-2", ItemID: "itemA2", Kind: "serial", Value: "itemA2's row", Now: now,
	}); err != nil {
		t.Fatalf("Create on itemA2: %v", err)
	}

	rows, err := scopeA.Identifications().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List(itemA): %v", err)
	}
	if len(rows) != 1 || rows[0].Value != "itemA's own row" {
		t.Fatalf("List(itemA) = %+v, want exactly one row {value:\"itemA's own row\"} -- itemA2's row leaked into itemA's list", rows)
	}
}

func TestIdentificationGetRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "SN-1", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.Identifications().Get(t.Context(), "itemA", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get of groupA's row = %v, want ErrNotFound", err)
	}
	own, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get of its OWN row = %v, want success; without this the miss above proves nothing about scope", err)
	}
	if own.Value != "SN-1" {
		t.Errorf("value = %q, want %q", own.Value, "SN-1")
	}
}

func TestIdentificationGetRejectsAnotherItemsRowInTheSameGroup(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "SN-1", Now: now,
	})
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	if _, err := scopeA.Identifications().Get(t.Context(), "itemA2", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Get of itemA's row by NAMING itemA2 = %v, want ErrNotFound -- the row belongs to a different item in the SAME group", err)
	}
	if _, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID); err != nil {
		t.Fatalf("Get naming the correct item = %v, want success", err)
	}
}

func TestIdentificationUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "old", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA", ID: created.ID, Kind: "model", Value: "new", ExpectedVersion: created.Version, Now: now + 1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}
	if updated.Kind != "model" || updated.Value != "new" {
		t.Errorf("got {kind:%q value:%q}, want {\"model\" \"new\"}", updated.Kind, updated.Value)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestIdentificationUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA", ID: created.ID, Kind: "serial", Value: "bob", ExpectedVersion: 1, Now: now + 1,
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err = scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA", ID: created.ID, Kind: "serial", Value: "carol", ExpectedVersion: 1, Now: now + 2,
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale version = %v, want ErrVersionMismatch", err)
	}
	got, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Value != "bob" || got.Version != 2 {
		t.Errorf("after the rejected update the row is {value:%q version:%d}, want {\"bob\" 2} -- the conflicting write landed anyway", got.Value, got.Version)
	}
}

func TestIdentificationUpdateRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = scopeB.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA", ID: created.ID, Kind: "serial", Value: "pwned", ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Update of groupA's row = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the row exists)", err)
	}

	got, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's attempt: %v", err)
	}
	if got.Value != "alice" || got.Version != 1 {
		t.Errorf("groupA's row is now {value:%q version:%d}, want {\"alice\" 1} -- groupB's write landed despite the error", got.Value, got.Version)
	}
}

func TestIdentificationUpdateRejectsAnotherItemsRowInTheSameGroup(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now,
	})
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	_, err = scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{
		ItemID: "itemA2", ID: created.ID, Kind: "serial", Value: "pwned by itemA2", ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Update naming itemA2 for itemA's row = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the row exists under that item)", err)
	}

	got, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get (naming the correct item) after the cross-item attempt: %v", err)
	}
	if got.Value != "alice" || got.Version != 1 {
		t.Errorf("itemA's row is now {value:%q version:%d}, want {\"alice\" 1} -- the cross-item write landed despite the error", got.Value, got.Version)
	}
}

func TestIdentificationDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.Identifications().Delete(t.Context(), "itemA", created.ID, now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Identifications().Delete(t.Context(), "itemA", created.ID, now+2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-2", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now + 3,
	}); err != nil {
		t.Fatalf("Create after Delete with the same kind/value = %v, want success (0..N, no uniqueness rule)", err)
	}
}

func TestIdentificationDeleteRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeB.Identifications().Delete(t.Context(), "itemA", created.ID, now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Delete of groupA's row = %v, want ErrNotFound", err)
	}
	got, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's delete attempt = %v, want the row still there", err)
	}
	if got.Value != "alice" || got.DeletedAt.Valid {
		t.Errorf("groupA's row is now {value:%q deleted_at:%+v}, want {\"alice\" NULL} -- groupB's delete landed despite the error", got.Value, got.DeletedAt)
	}
}

func TestIdentificationDeleteRejectsAnotherItemsRowInTheSameGroup(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "alice", Now: now,
	})
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	if err := scopeA.Identifications().Delete(t.Context(), "itemA2", created.ID, now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Delete naming itemA2 for itemA's row = %v, want ErrNotFound", err)
	}
	got, err := scopeA.Identifications().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get (naming the correct item) after the cross-item delete attempt: %v", err)
	}
	if got.Value != "alice" || got.DeletedAt.Valid {
		t.Errorf("itemA's row is now {value:%q deleted_at:%+v}, want {\"alice\" NULL} -- the cross-item delete landed despite the error", got.Value, got.DeletedAt)
	}
}

func TestIdentificationDeleteRemovesExactlyOneRow(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	for _, id := range []string{"id-1", "id-2", "id-3"} {
		if _, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
			ID: id, ItemID: "itemA", Kind: "serial", Value: id, Now: now,
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	if err := scopeA.Identifications().Delete(t.Context(), "itemA", "id-1", now); err != nil {
		t.Fatalf("Delete id-1: %v", err)
	}

	rows, err := scopeA.Identifications().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("deleting id-1 left %d of 3 identifications, want 2 -- the delete matched more than the row it named", len(rows))
	}
	for _, r := range rows {
		if r.ID == "id-1" {
			t.Errorf("id-1 survived its own delete")
		}
	}
}

func TestIdentificationRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := identificationScope(t)
	now := time.Now().UnixMilli()

	cases := []struct {
		name string
		call func() error
	}{
		{"create without an id", func() error {
			_, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{ItemID: "itemA", Kind: "serial", Value: "v", Now: now})
			return err
		}},
		{"create without an item", func() error {
			_, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{ID: "id-1", Kind: "serial", Value: "v", Now: now})
			return err
		}},
		{"create without a kind", func() error {
			_, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{ID: "id-1", ItemID: "itemA", Value: "v", Now: now})
			return err
		}},
		{"create without a value", func() error {
			_, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{ID: "id-1", ItemID: "itemA", Kind: "serial", Now: now})
			return err
		}},
		{"create without a timestamp", func() error {
			_, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{ID: "id-1", ItemID: "itemA", Kind: "serial", Value: "v"})
			return err
		}},
		{"update without an expected version", func() error {
			_, err := scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{ItemID: "itemA", ID: "id-1", Kind: "serial", Value: "v", Now: now})
			return err
		}},
		{"update without a kind", func() error {
			_, err := scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{ItemID: "itemA", ID: "id-1", Value: "v", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"update without a value", func() error {
			_, err := scopeA.Identifications().Update(t.Context(), UpdateIdentificationParams{ItemID: "itemA", ID: "id-1", Kind: "serial", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"delete without an item", func() error {
			return scopeA.Identifications().Delete(t.Context(), "", "id-1", now)
		}},
		{"delete without an id", func() error {
			return scopeA.Identifications().Delete(t.Context(), "itemA", "", now)
		}},
		{"delete without a timestamp", func() error {
			return scopeA.Identifications().Delete(t.Context(), "itemA", "id-1", 0)
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

func TestIdentificationCreateRejectsAnUnknownKindAtTheCheckConstraint(t *testing.T) {
	scopeA, _ := identificationScope(t)

	_, err := scopeA.Identifications().Create(t.Context(), CreateIdentificationParams{
		ID: "id-1", ItemID: "itemA", Kind: "not-a-real-kind", Value: "v", Now: time.Now().UnixMilli(),
	})
	if err == nil {
		t.Fatal("Create with an unrecognised kind succeeded; migration 0001's CHECK (kind IN (...)) should have refused it")
	}
}
