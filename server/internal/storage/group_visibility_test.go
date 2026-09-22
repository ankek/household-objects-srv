package storage

import (
	"errors"
	"testing"
	"time"
)

func groupVisibilityScope(t *testing.T) (Scope, Scope) {
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

func TestGroupVisibilityGetDefaultsToHidden(t *testing.T) {
	scopeA, _ := groupVisibilityScope(t)

	got, err := scopeA.Visibility().Get(t.Context())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.WarrantyVisible || got.SaleVisible || got.PurchaseVisible {
		t.Errorf("fresh group visibility = %+v, want all three hidden (FR-011's opt-in default)", got)
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
}

func TestGroupVisibilityUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := groupVisibilityScope(t)
	now := time.Now().UnixMilli()

	updated, err := scopeA.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{
		WarrantyVisible: true,
		SaleVisible:     true,
		PurchaseVisible: false,
		ExpectedVersion: 1,
		Now:             now,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.WarrantyVisible || !updated.SaleVisible || updated.PurchaseVisible {
		t.Errorf("updated = %+v, want {warranty:true sale:true purchase:false}", updated)
	}
	if updated.Version != 2 {
		t.Errorf("version after one update = %d, want 2", updated.Version)
	}

	got, err := scopeA.Visibility().Get(t.Context())
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got != updated {
		t.Errorf("Get after Update = %+v, want %+v (Update's own return value)", got, updated)
	}
}

func TestGroupVisibilityUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := groupVisibilityScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{
		WarrantyVisible: true, ExpectedVersion: 1, Now: now,
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err := scopeA.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{
		SaleVisible: true, ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("second Update with the now-stale version 1 = %v, want ErrVersionMismatch", err)
	}
}

func TestGroupVisibilityGetIsScopedToItsOwnGroup(t *testing.T) {
	scopeA, scopeB := groupVisibilityScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{
		WarrantyVisible: true, ExpectedVersion: 1, Now: now,
	}); err != nil {
		t.Fatalf("groupA Update: %v", err)
	}

	got, err := scopeB.Visibility().Get(t.Context())
	if err != nil {
		t.Fatalf("groupB Get: %v", err)
	}
	if got.WarrantyVisible || got.Version != 1 {
		t.Errorf("groupB's Get returned %+v, want its own untouched default (all hidden, version 1) -- groupA's change leaked across the scope", got)
	}
}

func TestGroupVisibilityUpdateDoesNotAffectAnotherGroup(t *testing.T) {
	scopeA, scopeB := groupVisibilityScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeB.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{
		WarrantyVisible: true, ExpectedVersion: 1, Now: now,
	}); err != nil {
		t.Fatalf("groupB Update its own visibility: %v", err)
	}

	got, err := scopeA.Visibility().Get(t.Context())
	if err != nil {
		t.Fatalf("groupA Get after groupB's own-group update: %v", err)
	}
	if got.WarrantyVisible || got.Version != 1 {
		t.Errorf("groupA's visibility is now %+v, want unchanged (hidden, version 1) -- groupB's own-group update bled into groupA", got)
	}
}

func TestGroupVisibilityRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := groupVisibilityScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{Now: now}); err == nil {
		t.Error("Update with ExpectedVersion 0 = nil error, want a validation error")
	}
	if _, err := scopeA.Visibility().Update(t.Context(), UpdateGroupVisibilityParams{ExpectedVersion: 1}); err == nil {
		t.Error("Update with Now 0 = nil error, want a validation error")
	}
}
