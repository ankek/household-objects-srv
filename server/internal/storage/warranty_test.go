package storage

import (
	"errors"
	"testing"
	"time"
)

func warrantyScope(t *testing.T) (Scope, Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupB", "itemB", 20)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err := s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return scopeA, scopeB
}

func TestWarrantyCreateSucceedsWithNothingButAnItem(t *testing.T) {
	scopeA, _ := warrantyScope(t)

	created, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID:     "war-1",
		ItemID: "itemA",
		Now:    time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create(item only) = %v, want success (FR-011/FR-012: the block is opt-in and no field of it is required)", err)
	}
	if created.Holder != "" || created.Provider != "" || created.Notes != "" {
		t.Errorf("got {holder:%q provider:%q notes:%q}, want all \"\" (schema defaults)", created.Holder, created.Provider, created.Notes)
	}
	if created.StartsOn.Valid || created.ExpiresOn.Valid {
		t.Errorf("got {starts_on:%+v expires_on:%+v}, want both NULL", created.StartsOn, created.ExpiresOn)
	}
	if created.IsLifetime != 0 {
		t.Errorf("IsLifetime = %d, want 0 (schema default)", created.IsLifetime)
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

func TestWarrantyCreateStoresEveryFR012Field(t *testing.T) {
	scopeA, _ := warrantyScope(t)

	created, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID:         "war-full",
		ItemID:     "itemA",
		Holder:     "Alice Example",
		Provider:   "Acme Warranties",
		StartsOn:   "2026-01-15",
		ExpiresOn:  "2029-01-14",
		IsLifetime: true,
		Notes:      "receipt in the drawer",
		Now:        time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := scopeA.Warranty().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	for _, c := range []struct{ what, got, want string }{
		{"holder", got.Holder, "Alice Example"},
		{"provider", got.Provider, "Acme Warranties"},
		{"starts_on", got.StartsOn.String, "2026-01-15"},
		{"expires_on", got.ExpiresOn.String, "2029-01-14"},
		{"notes", got.Notes, "receipt in the drawer"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
	if got.IsLifetime != 1 {
		t.Errorf("is_lifetime = %d, want 1", got.IsLifetime)
	}
	if got.ID != created.ID {
		t.Errorf("Get returned id %q, want %q", got.ID, created.ID)
	}
}

func TestWarrantyCreateRejectsASecondBlock(t *testing.T) {
	scopeA, _ := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Now: now}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-2", ItemID: "itemA", Now: now})
	if !errors.Is(err, ErrWarrantyExists) {
		t.Fatalf("second Create = %v, want ErrWarrantyExists (1:0..1, ux_item_warranty_group_item)", err)
	}
}

func TestWarrantyCreateAgainAfterDeleteSucceeds(t *testing.T) {
	scopeA, _ := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Holder: "first", Now: now}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := scopeA.Warranty().Delete(t.Context(), "itemA", now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	created, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-2", ItemID: "itemA", Holder: "second", Now: now + 2})
	if err != nil {
		t.Fatalf("Create after Delete = %v, want success -- ux_item_warranty_group_item is partial on deleted_at IS NULL", err)
	}
	if created.Holder != "second" || created.Version != 1 {
		t.Errorf("recreated block = {holder:%q version:%d}, want {holder:%q version:1} -- a fresh row, not the tombstone revived", created.Holder, created.Version, "second")
	}
}

func TestWarrantyCreateRejectsAnItemFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := warrantyScope(t)
	now := time.Now().UnixMilli()

	_, err := scopeB.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-x", ItemID: "itemA", Now: now})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Create on groupA's item = %v, want ErrNotFound (P-3: a foreign id must be indistinguishable from an unknown one)", err)
	}

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-a", ItemID: "itemA", Now: now}); err != nil {
		t.Fatalf("groupA Create on its OWN item = %v, want success; without this the refusal above proves nothing about scope", err)
	}
}

func TestWarrantyGetRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Holder: "alice", Now: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.Warranty().Get(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get of groupA's warranty = %v, want ErrNotFound", err)
	}
	own, err := scopeA.Warranty().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("groupA Get of its OWN warranty = %v, want success; without this the miss above proves nothing about scope", err)
	}
	if own.Holder != "alice" {
		t.Errorf("holder = %q, want %q", own.Holder, "alice")
	}
}

func TestWarrantyGetOnAnItemWithNoBlockIsNotFound(t *testing.T) {
	scopeA, _ := warrantyScope(t)

	byNoBlock := scopeA.Warranty().Get
	if _, err := byNoBlock(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on an item with no warranty = %v, want ErrNotFound", err)
	}
	if _, err := byNoBlock(t.Context(), "no-such-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on an unknown item = %v, want ErrNotFound (the two must be indistinguishable)", err)
	}
}

func TestWarrantyUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := warrantyScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Holder: "alice", Notes: "old", Now: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scopeA.Warranty().Update(t.Context(), UpdateWarrantyParams{
		ItemID:          "itemA",
		Holder:          "bob",
		ExpiresOn:       "2030-06-01",
		IsLifetime:      true,
		ExpectedVersion: created.Version,
		Now:             now + 1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}
	if updated.Holder != "bob" || updated.ExpiresOn.String != "2030-06-01" || updated.IsLifetime != 1 {
		t.Errorf("got {holder:%q expires_on:%q is_lifetime:%d}, want {\"bob\" \"2030-06-01\" 1}", updated.Holder, updated.ExpiresOn.String, updated.IsLifetime)
	}
	if updated.Notes != "" {
		t.Errorf("Notes = %q, want \"\" -- Update is a whole-block replace and the request omitted Notes", updated.Notes)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestWarrantyUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Holder: "alice", Now: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Warranty().Update(t.Context(), UpdateWarrantyParams{ItemID: "itemA", Holder: "bob", ExpectedVersion: 1, Now: now + 1}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err := scopeA.Warranty().Update(t.Context(), UpdateWarrantyParams{ItemID: "itemA", Holder: "carol", ExpectedVersion: 1, Now: now + 2})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale version = %v, want ErrVersionMismatch", err)
	}
	got, err := scopeA.Warranty().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Holder != "bob" || got.Version != 2 {
		t.Errorf("after the rejected update the row is {holder:%q version:%d}, want {\"bob\" 2} -- the conflicting write landed anyway", got.Holder, got.Version)
	}
}

func TestWarrantyUpdateRejectsAnotherGroupsBlock(t *testing.T) {
	scopeA, scopeB := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Holder: "alice", Now: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := scopeB.Warranty().Update(t.Context(), UpdateWarrantyParams{ItemID: "itemA", Holder: "pwned", ExpectedVersion: 1, Now: now + 1})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Update of groupA's warranty = %v, want ErrNotFound (never ErrVersionMismatch, which would confirm the row exists)", err)
	}

	got, err := scopeA.Warranty().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("groupA Get after groupB's attempt: %v", err)
	}
	if got.Holder != "alice" || got.Version != 1 {
		t.Errorf("groupA's block is now {holder:%q version:%d}, want {\"alice\" 1} -- groupB's write landed despite the error", got.Holder, got.Version)
	}
}

func TestWarrantyDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Now: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.Warranty().Delete(t.Context(), "itemA", now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Warranty().Get(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Warranty().Delete(t.Context(), "itemA", now+2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound -- see warrantyDeleteHandler's doc on why this is not idempotent the way a session revoke is", err)
	}
}

func TestWarrantyDeleteRejectsAnotherGroupsBlock(t *testing.T) {
	scopeA, scopeB := warrantyScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "war-1", ItemID: "itemA", Holder: "alice", Now: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeB.Warranty().Delete(t.Context(), "itemA", now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Delete of groupA's warranty = %v, want ErrNotFound", err)
	}
	got, err := scopeA.Warranty().Get(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("groupA Get after groupB's delete attempt = %v, want the block still there", err)
	}
	if got.Holder != "alice" || got.DeletedAt.Valid {
		t.Errorf("groupA's block is now {holder:%q deleted_at:%+v}, want {\"alice\" NULL} -- groupB's delete landed despite the error", got.Holder, got.DeletedAt)
	}
}

func TestWarrantyRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := warrantyScope(t)
	now := time.Now().UnixMilli()

	cases := []struct {
		name string
		call func() error
	}{
		{"create without an id", func() error {
			_, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ItemID: "itemA", Now: now})
			return err
		}},
		{"create without an item", func() error {
			_, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "w", Now: now})
			return err
		}},
		{"create without a timestamp", func() error {
			_, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{ID: "w", ItemID: "itemA"})
			return err
		}},
		{"update without an expected version", func() error {
			_, err := scopeA.Warranty().Update(t.Context(), UpdateWarrantyParams{ItemID: "itemA", Now: now})
			return err
		}},
		{"delete without an item", func() error {
			return scopeA.Warranty().Delete(t.Context(), "", now)
		}},
		{"delete without a timestamp", func() error {
			return scopeA.Warranty().Delete(t.Context(), "itemA", 0)
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
