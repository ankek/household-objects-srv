package storage

import (
	"errors"
	"testing"
	"time"
)

func TestItemCreateSucceedsWithNameOnly(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seed-only-to-create-the-group-row", 1)

	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	now := time.Now().UnixMilli()
	created, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID:        "item-name-only",
		Name:      "A Lamp",
		ShortCode: "CODE0001",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("Create(name only) = %v, want success (FR-010)", err)
	}
	if created.Name != "A Lamp" {
		t.Errorf("Name = %q, want %q", created.Name, "A Lamp")
	}
	if created.Description != "" {
		t.Errorf("Description = %q, want \"\" (schema default)", created.Description)
	}
	if created.LocationID.Valid {
		t.Errorf("LocationID = %+v, want NULL", created.LocationID)
	}
	if created.Quantity != 0 {
		t.Errorf("Quantity = %d, want 0 (schema default)", created.Quantity)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence", created.ChangeSeq)
	}

	got, err := scope.Items().Get(t.Context(), "item-name-only")
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Get returned id %q, want %q", got.ID, created.ID)
	}
}

func TestItemCreateRejectsEmptyName(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seed", 1)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	_, err = scope.Items().Create(t.Context(), CreateItemParams{
		ID:        "item-no-name",
		ShortCode: "CODE0002",
		Now:       time.Now().UnixMilli(),
	})
	if err == nil {
		t.Fatal("Create with empty Name succeeded; FR-010 names Name as the one mandatory field")
	}
}

func TestItemCreateReportsShortCodeCollision(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seed", 1)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	now := time.Now().UnixMilli()
	if _, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "First", ShortCode: "DUPE0001", Now: now,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-2", Name: "Second", ShortCode: "DUPE0001", Now: now,
	})
	if !errors.Is(err, ErrShortCodeTaken) {
		t.Fatalf("second Create with the same short_code: err = %v, want ErrShortCodeTaken", err)
	}

	if _, err := scope.Items().Get(t.Context(), "item-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(item-2) after a failed collide-on-short_code Create: err = %v, want ErrNotFound (the failed insert's transaction must have rolled back)", err)
	}
}

func TestItemCreateAllowsSameShortCodeInDifferentGroups(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seedA", 1)
	seedItem(t, s, "groupB", "seedB", 1)
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err := s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}

	now := time.Now().UnixMilli()
	if _, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "itemA", Name: "In A", ShortCode: "SAME0001", Now: now,
	}); err != nil {
		t.Fatalf("Create in group A: %v", err)
	}
	if _, err := scopeB.Items().Create(t.Context(), CreateItemParams{
		ID: "itemB", Name: "In B", ShortCode: "SAME0001", Now: now,
	}); err != nil {
		t.Fatalf("Create in group B with the same short_code as group A: %v, want success -- FR-073's uniqueness is per group", err)
	}
}

func TestItemUpdateAppliesUnderMatchingVersion(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seed", 1)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	created, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "Old Name", ShortCode: "UPD00001", Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scope.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-1", Name: "New Name", Description: "now has a description",
		Quantity: 3, ExpectedVersion: created.Version, Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "New Name" || updated.Description != "now has a description" || updated.Quantity != 3 {
		t.Errorf("Update result = %+v, want the new field values applied", updated)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("Version = %d, want %d (incremented by exactly one)", updated.Version, created.Version+1)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want > %d (a fresh allocation)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestItemUpdateRejectsStaleVersion(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seed", 1)
	scope, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	created, err := scope.Items().Create(t.Context(), CreateItemParams{
		ID: "item-1", Name: "Original", ShortCode: "VER00001", Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scope.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-1", Name: "Winner", ExpectedVersion: created.Version, Now: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err = scope.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-1", Name: "Loser", ExpectedVersion: created.Version, Now: time.Now().UnixMilli(),
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale-version Update: err = %v, want ErrVersionMismatch", err)
	}

	current, err := scope.Items().Get(t.Context(), "item-1")
	if err != nil {
		t.Fatalf("Get after rejected update: %v", err)
	}
	if current.Name != "Winner" {
		t.Errorf("current Name = %q, want %q; the losing update must not have applied", current.Name, "Winner")
	}
}

func TestItemUpdateReportsNotFoundForUnknownAndForeignIDs(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seedA", 1)
	seedItem(t, s, "groupB", "itemB1", 1)
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}

	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "does-not-exist", Name: "x", ExpectedVersion: 1, Now: time.Now().UnixMilli(),
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(unknown id): err = %v, want ErrNotFound", err)
	}

	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "itemB1", Name: "x", ExpectedVersion: 1, Now: time.Now().UnixMilli(),
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(group B's id, as group A): err = %v, want ErrNotFound (never a version-mismatch leak that would confirm the row exists)", err)
	}
}
