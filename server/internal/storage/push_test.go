package storage

import (
	"database/sql"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"testing"
)

func pushScope(t *testing.T) (repoA, repoB PushRepository, s *Storage) {
	t.Helper()
	s = newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	seedGroupRow(t, s, "groupB")

	var err error
	repoA, err = s.ForGroupPush(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroupPush(groupA): %v", err)
	}
	repoB, err = s.ForGroupPush(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroupPush(groupB): %v", err)
	}
	return repoA, repoB, s
}

func TestPushLookupIsNotFoundForAnUnknownMutationID(t *testing.T) {
	repoA, _, _ := pushScope(t)

	if _, err := repoA.Lookup(t.Context(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestPushInsertThenLookupReturnsTheRecordedRow(t *testing.T) {
	repoA, _, _ := pushScope(t)

	const now int64 = 1_000_000
	created, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID:         "ledger-1",
		MutationID: "mutation-1",
		EntityType: "item",
		EntityID:   "item-1",
		Outcome:    MutationOutcomeApplied,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if created.ID != "ledger-1" {
		t.Errorf("created.ID = %q, want %q", created.ID, "ledger-1")
	}
	if created.GroupID != "groupA" {
		t.Errorf("created.GroupID = %q, want %q", created.GroupID, "groupA")
	}
	if created.MutationID != "mutation-1" {
		t.Errorf("created.MutationID = %q, want %q", created.MutationID, "mutation-1")
	}
	if created.EntityType != "item" || created.EntityID != "item-1" {
		t.Errorf("created.EntityType/EntityID = %q/%q, want %q/%q", created.EntityType, created.EntityID, "item", "item-1")
	}
	if created.Outcome != string(MutationOutcomeApplied) {
		t.Errorf("created.Outcome = %q, want %q", created.Outcome, MutationOutcomeApplied)
	}
	if created.AppliedAt != now || created.CreatedAt != now || created.UpdatedAt != now {
		t.Errorf("created.AppliedAt/CreatedAt/UpdatedAt = %d/%d/%d, want all %d", created.AppliedAt, created.CreatedAt, created.UpdatedAt, now)
	}

	got, err := repoA.Lookup(t.Context(), "mutation-1")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != created {
		t.Errorf("Lookup = %+v, want %+v (Insert's own return value)", got, created)
	}
}

func TestPushSameMutationIDInDifferentGroupsIsIndependent(t *testing.T) {
	repoA, repoB, _ := pushScope(t)

	createdA, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-a", MutationID: "shared-mutation", EntityType: "item", EntityID: "item-a",
		Outcome: MutationOutcomeApplied, Now: 1,
	})
	if err != nil {
		t.Fatalf("Insert (groupA): %v", err)
	}

	if _, err := repoB.Lookup(t.Context(), "shared-mutation"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Lookup(%q) before its own Insert, error = %v, want ErrNotFound; "+
			"a mutation_id recorded for groupA must be invisible to groupB (P-3)", "shared-mutation", err)
	}

	createdB, err := repoB.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-b", MutationID: "shared-mutation", EntityType: "item", EntityID: "item-b",
		Outcome: MutationOutcomeConflict, Now: 2,
	})
	if err != nil {
		t.Fatalf("Insert (groupB), same mutation_id as groupA: %v", err)
	}
	if createdB.ID == createdA.ID {
		t.Fatalf("groupB's own row id %q collided with groupA's %q", createdB.ID, createdA.ID)
	}

	gotA, err := repoA.Lookup(t.Context(), "shared-mutation")
	if err != nil {
		t.Fatalf("groupA Lookup after groupB's own Insert: %v", err)
	}
	if gotA != createdA {
		t.Errorf("groupA Lookup = %+v, want %+v (groupB's own Insert must not have overwritten it)", gotA, createdA)
	}

	gotB, err := repoB.Lookup(t.Context(), "shared-mutation")
	if err != nil {
		t.Fatalf("groupB Lookup: %v", err)
	}
	if gotB != createdB {
		t.Errorf("groupB Lookup = %+v, want %+v", gotB, createdB)
	}
}

func TestPushInsertRejectsADuplicateMutationIDInTheSameGroup(t *testing.T) {
	repoA, _, _ := pushScope(t)

	if _, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-1", MutationID: "mutation-1", EntityType: "item", EntityID: "item-1",
		Outcome: MutationOutcomeApplied, Now: 1,
	}); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	_, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-2", MutationID: "mutation-1", EntityType: "item", EntityID: "item-1",
		Outcome: MutationOutcomeApplied, Now: 2,
	})
	if !errors.Is(err, ErrMutationAlreadyRecorded) {
		t.Fatalf("second Insert (same group, same mutation_id) error = %v, want ErrMutationAlreadyRecorded", err)
	}

	got, err := repoA.Lookup(t.Context(), "mutation-1")
	if err != nil {
		t.Fatalf("Lookup after rejected duplicate Insert: %v", err)
	}
	if got.ID != "ledger-1" {
		t.Errorf("Lookup.ID after rejected duplicate = %q, want %q (the first row, untouched)", got.ID, "ledger-1")
	}
}

func TestPushInsertValidatesRequiredFields(t *testing.T) {
	base := InsertMutationLedgerEntryParams{
		ID: "ledger-1", MutationID: "mutation-1", EntityType: "item", EntityID: "item-1",
		Outcome: MutationOutcomeApplied, Now: 1,
	}

	tests := []struct {
		name    string
		mutate  func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams
		wantErr bool
	}{
		{"valid", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { return p }, false},
		{"empty ID", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { p.ID = ""; return p }, true},
		{"empty MutationID", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { p.MutationID = ""; return p }, true},
		{"empty EntityType", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { p.EntityType = ""; return p }, true},
		{"empty EntityID", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { p.EntityID = ""; return p }, true},
		{"invalid Outcome", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams {
			p.Outcome = "partial"
			return p
		}, true},
		{"zero Now", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { p.Now = 0; return p }, true},
		{"negative Now", func(p InsertMutationLedgerEntryParams) InsertMutationLedgerEntryParams { p.Now = -1; return p }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoA, _, _ := pushScope(t)
			_, err := repoA.Insert(t.Context(), tt.mutate(base))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Insert error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPushLookupIsScopedToItsOwnGroupAtTheGeneratedQueryLevel(t *testing.T) {
	repoA, _, s := pushScope(t)

	created, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-cross-tenant", MutationID: "mutation-cross-tenant", EntityType: "item", EntityID: "item-1",
		Outcome: MutationOutcomeApplied, Now: 1,
	})
	if err != nil {
		t.Fatalf("Insert (groupA): %v", err)
	}

	q := gen.New(s.store.Reader())

	rowA, err := q.GetMutationByMutationID(t.Context(), gen.GetMutationByMutationIDParams{GroupID: "groupA", MutationID: created.MutationID})
	if err != nil {
		t.Fatalf("GetMutationByMutationID(groupA, %q): %v", created.MutationID, err)
	}
	if rowA.ID != created.ID {
		t.Fatalf("GetMutationByMutationID(groupA) returned id %q, want %q", rowA.ID, created.ID)
	}

	_, err = q.GetMutationByMutationID(t.Context(), gen.GetMutationByMutationIDParams{GroupID: "groupB", MutationID: created.MutationID})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetMutationByMutationID(groupB, %q) error = %v, want sql.ErrNoRows; a mutation "+
			"recorded for groupA must be invisible to groupB (P-3)", created.MutationID, err)
	}
}
