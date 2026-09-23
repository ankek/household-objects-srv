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

func TestPushLookupFieldVersionsReturnsEmptyForAnUntrackedEntity(t *testing.T) {
	repoA, _, _ := pushScope(t)

	got, err := repoA.LookupFieldVersions(t.Context(), "item", "item-1")
	if err != nil {
		t.Fatalf("LookupFieldVersions: %v", err)
	}
	if got == nil {
		t.Error("LookupFieldVersions(untracked entity) = nil, want a non-nil empty slice (P-4: emit_empty_slices)")
	}
	if len(got) != 0 {
		t.Errorf("LookupFieldVersions(untracked entity) = %+v, want empty", got)
	}
}

func TestPushUpsertFieldVersionThenLookupReturnsTheRow(t *testing.T) {
	repoA, _, _ := pushScope(t)

	if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "item-1", FieldName: "name", Version: 3, Now: 1_000,
	}); err != nil {
		t.Fatalf("UpsertFieldVersion: %v", err)
	}

	got, err := repoA.LookupFieldVersions(t.Context(), "item", "item-1")
	if err != nil {
		t.Fatalf("LookupFieldVersions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LookupFieldVersions = %+v, want exactly one row", got)
	}
	row := got[0]
	if row.GroupID != "groupA" {
		t.Errorf("row.GroupID = %q, want %q", row.GroupID, "groupA")
	}
	if row.EntityType != "item" || row.EntityID != "item-1" || row.FieldName != "name" {
		t.Errorf("row.EntityType/EntityID/FieldName = %q/%q/%q, want %q/%q/%q",
			row.EntityType, row.EntityID, row.FieldName, "item", "item-1", "name")
	}
	if row.Version != 3 {
		t.Errorf("row.Version = %d, want %d", row.Version, 3)
	}
	if row.UpdatedAt != 1_000 {
		t.Errorf("row.UpdatedAt = %d, want %d", row.UpdatedAt, 1_000)
	}
}

func TestPushUpsertFieldVersionOverwritesRatherThanIncrements(t *testing.T) {
	repoA, _, _ := pushScope(t)

	if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "item-1", FieldName: "name", Version: 5, Now: 1_000,
	}); err != nil {
		t.Fatalf("first UpsertFieldVersion: %v", err)
	}
	if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "item-1", FieldName: "name", Version: 2, Now: 2_000,
	}); err != nil {
		t.Fatalf("second UpsertFieldVersion: %v", err)
	}

	got, err := repoA.LookupFieldVersions(t.Context(), "item", "item-1")
	if err != nil {
		t.Fatalf("LookupFieldVersions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LookupFieldVersions = %+v, want exactly one row (the upsert must replace, not add a second row)", got)
	}
	if got[0].Version != 2 {
		t.Errorf("row.Version after second upsert = %d, want %d (overwritten, not max()'d or incremented)", got[0].Version, 2)
	}
	if got[0].UpdatedAt != 2_000 {
		t.Errorf("row.UpdatedAt after second upsert = %d, want %d", got[0].UpdatedAt, 2_000)
	}
}

func TestPushLookupFieldVersionsBatchesMultipleFields(t *testing.T) {
	repoA, _, _ := pushScope(t)

	fields := []string{"name", "notes", "location_id"}
	for i, field := range fields {
		if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
			EntityType: "item", EntityID: "item-1", FieldName: field, Version: int64(i + 1), Now: 1_000,
		}); err != nil {
			t.Fatalf("UpsertFieldVersion(%q): %v", field, err)
		}
	}
	if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "item-2", FieldName: "name", Version: 9, Now: 1_000,
	}); err != nil {
		t.Fatalf("UpsertFieldVersion(item-2): %v", err)
	}

	got, err := repoA.LookupFieldVersions(t.Context(), "item", "item-1")
	if err != nil {
		t.Fatalf("LookupFieldVersions: %v", err)
	}
	if len(got) != len(fields) {
		t.Fatalf("LookupFieldVersions returned %d rows, want %d: %+v", len(got), len(fields), got)
	}
	seen := make(map[string]int64, len(got))
	for _, row := range got {
		if row.EntityID != "item-1" {
			t.Errorf("row.EntityID = %q, want %q (item-2's own field must not appear in item-1's batch)", row.EntityID, "item-1")
		}
		seen[row.FieldName] = row.Version
	}
	for i, field := range fields {
		if seen[field] != int64(i+1) {
			t.Errorf("field %q version = %d, want %d", field, seen[field], i+1)
		}
	}
}

func TestPushUpsertFieldVersionValidatesRequiredFields(t *testing.T) {
	base := UpsertFieldVersionParams{EntityType: "item", EntityID: "item-1", FieldName: "name", Version: 1, Now: 1}

	tests := []struct {
		name    string
		mutate  func(p UpsertFieldVersionParams) UpsertFieldVersionParams
		wantErr bool
	}{
		{"valid", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { return p }, false},
		{"empty EntityType", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.EntityType = ""; return p }, true},
		{"empty EntityID", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.EntityID = ""; return p }, true},
		{"empty FieldName", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.FieldName = ""; return p }, true},
		{"zero Version", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.Version = 0; return p }, true},
		{"negative Version", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.Version = -1; return p }, true},
		{"zero Now", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.Now = 0; return p }, true},
		{"negative Now", func(p UpsertFieldVersionParams) UpsertFieldVersionParams { p.Now = -1; return p }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoA, _, _ := pushScope(t)
			err := repoA.UpsertFieldVersion(t.Context(), tt.mutate(base))
			if (err != nil) != tt.wantErr {
				t.Fatalf("UpsertFieldVersion error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPushFieldVersionsAreScopedToTheirOwnGroup(t *testing.T) {
	repoA, repoB, _ := pushScope(t)

	if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "shared-item", FieldName: "name", Version: 1, Now: 1,
	}); err != nil {
		t.Fatalf("UpsertFieldVersion (groupA): %v", err)
	}

	gotB, err := repoB.LookupFieldVersions(t.Context(), "item", "shared-item")
	if err != nil {
		t.Fatalf("groupB LookupFieldVersions before its own Upsert: %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("groupB LookupFieldVersions before its own Upsert = %+v, want empty; a field tracked "+
			"for groupA must be invisible to groupB (P-3)", gotB)
	}

	if err := repoB.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "shared-item", FieldName: "name", Version: 7, Now: 2,
	}); err != nil {
		t.Fatalf("UpsertFieldVersion (groupB), same tuple as groupA: %v", err)
	}

	gotA, err := repoA.LookupFieldVersions(t.Context(), "item", "shared-item")
	if err != nil {
		t.Fatalf("groupA LookupFieldVersions after groupB's own Upsert: %v", err)
	}
	if len(gotA) != 1 || gotA[0].Version != 1 {
		t.Fatalf("groupA LookupFieldVersions after groupB's own Upsert = %+v, want exactly one row at version 1 (groupB's own Upsert must not have overwritten it)", gotA)
	}

	gotB, err = repoB.LookupFieldVersions(t.Context(), "item", "shared-item")
	if err != nil {
		t.Fatalf("groupB LookupFieldVersions: %v", err)
	}
	if len(gotB) != 1 || gotB[0].Version != 7 {
		t.Fatalf("groupB LookupFieldVersions = %+v, want exactly one row at version 7", gotB)
	}
}

func TestPushFieldVersionsAreScopedToTheirOwnGroupAtTheGeneratedQueryLevel(t *testing.T) {
	repoA, _, s := pushScope(t)

	if err := repoA.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: "item-cross-tenant", FieldName: "name", Version: 1, Now: 1,
	}); err != nil {
		t.Fatalf("UpsertFieldVersion (groupA): %v", err)
	}

	q := gen.New(s.store.Reader())

	rowsA, err := q.ListFieldVersionsByEntity(t.Context(), gen.ListFieldVersionsByEntityParams{
		GroupID: "groupA", EntityType: "item", EntityID: "item-cross-tenant",
	})
	if err != nil {
		t.Fatalf("ListFieldVersionsByEntity(groupA): %v", err)
	}
	if len(rowsA) != 1 {
		t.Fatalf("ListFieldVersionsByEntity(groupA) = %+v, want exactly one row", rowsA)
	}

	rowsB, err := q.ListFieldVersionsByEntity(t.Context(), gen.ListFieldVersionsByEntityParams{
		GroupID: "groupB", EntityType: "item", EntityID: "item-cross-tenant",
	})
	if err != nil {
		t.Fatalf("ListFieldVersionsByEntity(groupB): %v", err)
	}
	if len(rowsB) != 0 {
		t.Fatalf("ListFieldVersionsByEntity(groupB) = %+v, want empty; a field tracked for groupA "+
			"must be invisible to groupB (P-3)", rowsB)
	}
}

func TestPushInsertConflictThenReadBackFromTheDatabase(t *testing.T) {
	repoA, _, s := pushScope(t)

	ledger, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-1", MutationID: "mutation-1", EntityType: "item", EntityID: "item-1",
		Outcome: MutationOutcomeConflict, Now: 500,
	})
	if err != nil {
		t.Fatalf("Insert (ledger row for the FK): %v", err)
	}

	created, err := repoA.InsertConflict(t.Context(), InsertConflictParams{
		ID: "conflict-1", EntityType: "item", EntityID: "item-1", FieldName: "name",
		ServerValueSnapshot: `"server value"`, LosingClientValueSnapshot: `"client value"`,
		DetectedAt: 1_000, MutationID: ledger.ID, Now: 1_000,
	})
	if err != nil {
		t.Fatalf("InsertConflict: %v", err)
	}

	if created.ID != "conflict-1" {
		t.Errorf("created.ID = %q, want %q", created.ID, "conflict-1")
	}
	if created.GroupID != "groupA" {
		t.Errorf("created.GroupID = %q, want %q", created.GroupID, "groupA")
	}
	if created.EntityType != "item" || created.EntityID != "item-1" || created.FieldName != "name" {
		t.Errorf("created.EntityType/EntityID/FieldName = %q/%q/%q, want %q/%q/%q",
			created.EntityType, created.EntityID, created.FieldName, "item", "item-1", "name")
	}
	if !created.ServerValueSnapshot.Valid || created.ServerValueSnapshot.String != `"server value"` {
		t.Errorf("created.ServerValueSnapshot = %+v, want valid %q", created.ServerValueSnapshot, `"server value"`)
	}
	if !created.LosingClientValueSnapshot.Valid || created.LosingClientValueSnapshot.String != `"client value"` {
		t.Errorf("created.LosingClientValueSnapshot = %+v, want valid %q", created.LosingClientValueSnapshot, `"client value"`)
	}
	if !created.MutationID.Valid || created.MutationID.String != ledger.ID {
		t.Errorf("created.MutationID = %+v, want valid %q", created.MutationID, ledger.ID)
	}
	if created.DetectedAt != 1_000 || created.CreatedAt != 1_000 || created.UpdatedAt != 1_000 {
		t.Errorf("created.DetectedAt/CreatedAt/UpdatedAt = %d/%d/%d, want all %d",
			created.DetectedAt, created.CreatedAt, created.UpdatedAt, 1_000)
	}

	var (
		groupID, entityType, entityID, fieldName string
		serverSnap, losingSnap, mutationID       sql.NullString
		detectedAt, createdAt, updatedAt         int64
	)
	err = s.store.Reader().QueryRowContext(t.Context(), `
		SELECT group_id, entity_type, entity_id, field_name,
		       server_value_snapshot, losing_client_value_snapshot,
		       detected_at, mutation_id, created_at, updated_at
		FROM conflicts WHERE id = ?`, created.ID).
		Scan(&groupID, &entityType, &entityID, &fieldName, &serverSnap, &losingSnap, &detectedAt, &mutationID, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("read back conflict row %q: %v", created.ID, err)
	}
	if groupID != "groupA" || entityType != "item" || entityID != "item-1" || fieldName != "name" {
		t.Errorf("persisted group_id/entity_type/entity_id/field_name = %q/%q/%q/%q, want %q/%q/%q/%q",
			groupID, entityType, entityID, fieldName, "groupA", "item", "item-1", "name")
	}
	if !serverSnap.Valid || serverSnap.String != `"server value"` {
		t.Errorf("persisted server_value_snapshot = %+v, want valid %q", serverSnap, `"server value"`)
	}
	if !losingSnap.Valid || losingSnap.String != `"client value"` {
		t.Errorf("persisted losing_client_value_snapshot = %+v, want valid %q", losingSnap, `"client value"`)
	}
	if !mutationID.Valid || mutationID.String != ledger.ID {
		t.Errorf("persisted mutation_id = %+v, want valid %q", mutationID, ledger.ID)
	}
	if detectedAt != 1_000 || createdAt != 1_000 || updatedAt != 1_000 {
		t.Errorf("persisted detected_at/created_at/updated_at = %d/%d/%d, want all %d", detectedAt, createdAt, updatedAt, 1_000)
	}
}

func TestPushInsertConflictLeavesOptionalSnapshotsAndMutationIDNull(t *testing.T) {
	repoA, _, _ := pushScope(t)

	created, err := repoA.InsertConflict(t.Context(), InsertConflictParams{
		ID: "conflict-no-mutation", EntityType: "item", EntityID: "item-1", FieldName: "_entity",
		DetectedAt: 1_000, Now: 1_000,
	})
	if err != nil {
		t.Fatalf("InsertConflict: %v", err)
	}
	if created.ServerValueSnapshot.Valid {
		t.Errorf("created.ServerValueSnapshot = %+v, want NULL (Valid=false)", created.ServerValueSnapshot)
	}
	if created.LosingClientValueSnapshot.Valid {
		t.Errorf("created.LosingClientValueSnapshot = %+v, want NULL (Valid=false)", created.LosingClientValueSnapshot)
	}
	if created.MutationID.Valid {
		t.Errorf("created.MutationID = %+v, want NULL (Valid=false) -- migration 0001's own comment: a conflict can be detected outside a push", created.MutationID)
	}
}

func TestPushInsertConflictValidatesRequiredFields(t *testing.T) {
	base := InsertConflictParams{
		ID: "conflict-1", EntityType: "item", EntityID: "item-1", FieldName: "name",
		DetectedAt: 1, Now: 1,
	}

	tests := []struct {
		name    string
		mutate  func(p InsertConflictParams) InsertConflictParams
		wantErr bool
	}{
		{"valid", func(p InsertConflictParams) InsertConflictParams { return p }, false},
		{"empty ID", func(p InsertConflictParams) InsertConflictParams { p.ID = ""; return p }, true},
		{"empty EntityType", func(p InsertConflictParams) InsertConflictParams { p.EntityType = ""; return p }, true},
		{"empty EntityID", func(p InsertConflictParams) InsertConflictParams { p.EntityID = ""; return p }, true},
		{"empty FieldName", func(p InsertConflictParams) InsertConflictParams { p.FieldName = ""; return p }, true},
		{"zero DetectedAt", func(p InsertConflictParams) InsertConflictParams { p.DetectedAt = 0; return p }, true},
		{"negative DetectedAt", func(p InsertConflictParams) InsertConflictParams { p.DetectedAt = -1; return p }, true},
		{"zero Now", func(p InsertConflictParams) InsertConflictParams { p.Now = 0; return p }, true},
		{"negative Now", func(p InsertConflictParams) InsertConflictParams { p.Now = -1; return p }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoA, _, _ := pushScope(t)
			_, err := repoA.InsertConflict(t.Context(), tt.mutate(base))
			if (err != nil) != tt.wantErr {
				t.Fatalf("InsertConflict error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPushInsertConflictIsScopedToItsOwnGroup(t *testing.T) {
	repoA, repoB, s := pushScope(t)

	createdA, err := repoA.InsertConflict(t.Context(), InsertConflictParams{
		ID: "conflict-a", EntityType: "item", EntityID: "shared-item", FieldName: "name",
		DetectedAt: 1, Now: 1,
	})
	if err != nil {
		t.Fatalf("InsertConflict (groupA): %v", err)
	}
	createdB, err := repoB.InsertConflict(t.Context(), InsertConflictParams{
		ID: "conflict-b", EntityType: "item", EntityID: "shared-item", FieldName: "name",
		DetectedAt: 2, Now: 2,
	})
	if err != nil {
		t.Fatalf("InsertConflict (groupB): %v", err)
	}

	rows, err := s.store.Reader().QueryContext(t.Context(), `SELECT id, group_id FROM conflicts WHERE id IN (?, ?) ORDER BY id`, createdA.ID, createdB.ID)
	if err != nil {
		t.Fatalf("query persisted conflicts: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var id, groupID string
		if err := rows.Scan(&id, &groupID); err != nil {
			t.Fatalf("scan persisted conflict: %v", err)
		}
		got[id] = groupID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate persisted conflicts: %v", err)
	}

	if got["conflict-a"] != "groupA" {
		t.Errorf("persisted group_id for conflict-a = %q, want %q", got["conflict-a"], "groupA")
	}
	if got["conflict-b"] != "groupB" {
		t.Errorf("persisted group_id for conflict-b = %q, want %q", got["conflict-b"], "groupB")
	}
}
