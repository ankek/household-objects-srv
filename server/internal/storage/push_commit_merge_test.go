package storage

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"sort"
	"testing"
)

type conflictRow struct {
	entityType, entityID, fieldName string
	serverSnapshot, losingSnapshot  sql.NullString
	mutationID                      string
}

func readConflictRows(t *testing.T, s *Storage, groupID, entityID string) []conflictRow {
	t.Helper()
	rows, err := s.store.Reader().QueryContext(t.Context(),
		`SELECT entity_type, entity_id, field_name, server_value_snapshot, losing_client_value_snapshot, mutation_id
		   FROM conflicts WHERE group_id = ? AND entity_id = ? ORDER BY field_name`,
		groupID, entityID)
	if err != nil {
		t.Fatalf("query conflicts: %v", err)
	}
	defer rows.Close()

	var out []conflictRow
	for rows.Next() {
		var c conflictRow
		if err := rows.Scan(&c.entityType, &c.entityID, &c.fieldName, &c.serverSnapshot, &c.losingSnapshot, &c.mutationID); err != nil {
			t.Fatalf("scan conflict row: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate conflicts: %v", err)
	}
	return out
}

func fieldVersionRow(t *testing.T, s *Storage, groupID, entityType, entityID, fieldName string) (version int64, exists bool) {
	t.Helper()
	err := s.store.Reader().QueryRowContext(t.Context(),
		`SELECT version FROM field_versions WHERE group_id = ? AND entity_type = ? AND entity_id = ? AND field_name = ?`,
		groupID, entityType, entityID, fieldName).Scan(&version)
	switch {
	case err == sql.ErrNoRows:
		return 0, false
	case err != nil:
		t.Fatalf("read field_versions[%s/%s/%s]: %v", entityType, entityID, fieldName, err)
	}
	return version, true
}

func mustLedgerID(t *testing.T, s *Storage, groupID, mutationID string) string {
	t.Helper()
	row, err := gen.New(s.store.Reader()).GetMutationByMutationID(t.Context(), gen.GetMutationByMutationIDParams{
		GroupID: groupID, MutationID: mutationID,
	})
	if err != nil {
		t.Fatalf("read back ledger row for %q: %v", mutationID, err)
	}
	return row.ID
}

func TestApplyMutationFieldLevelMergePartialConflictAppliesUntrackedFieldConflictsTrackedField(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-partial-1", Name: "Hammer", Quantity: 1, ShortCode: "SC-PART1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-partial-1", Name: "Hammer", Quantity: 9, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance item to version 2: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-partial-1",
		EntityType:  "item",
		EntityID:    "item-partial-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Mallet", "quantity": 2}),
		Now:         3000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied {
		t.Fatalf("outcome.Applied = false, want true -- name is untracked and must merge")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"quantity"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [quantity]", outcome.ConflictFields)
	}
	if outcome.Version != 3 {
		t.Fatalf("outcome.Version = %d, want 3", outcome.Version)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-partial-1")
	if err != nil {
		t.Fatalf("Get item: %v", err)
	}
	if item.Name != "Mallet" || item.Quantity != 9 || item.Version != 3 {
		t.Fatalf("item after partial merge = %+v, want Name=Mallet Quantity=9 Version=3", item)
	}

	if v, ok := fieldVersionRow(t, s, "groupA", "item", "item-partial-1", "name"); !ok || v != 3 {
		t.Fatalf("field_versions[item/item-partial-1/name] = (%d,%v), want (3,true)", v, ok)
	}
	if v, ok := fieldVersionRow(t, s, "groupA", "item", "item-partial-1", "quantity"); !ok || v != 2 {
		t.Fatalf("field_versions[item/item-partial-1/quantity] = (%d,%v), want (2,true) -- unchanged by the conflicting merge", v, ok)
	}

	rows := readConflictRows(t, s, "groupA", "item-partial-1")
	if len(rows) != 1 {
		t.Fatalf("conflicts rows = %+v, want exactly 1", rows)
	}
	c := rows[0]
	if c.fieldName != "quantity" || c.entityType != "item" {
		t.Fatalf("conflict row = %+v, want field_name=quantity entity_type=item", c)
	}
	if !c.serverSnapshot.Valid || c.serverSnapshot.String != "9" {
		t.Fatalf("conflict server_value_snapshot = %+v, want valid \"9\"", c.serverSnapshot)
	}
	if !c.losingSnapshot.Valid || c.losingSnapshot.String != "2" {
		t.Fatalf("conflict losing_client_value_snapshot = %+v, want valid \"2\"", c.losingSnapshot)
	}
	if c.mutationID != mustLedgerID(t, s, "groupA", "mut-partial-1") {
		t.Fatalf("conflict mutation_id = %q, want the ledger row's own id", c.mutationID)
	}
}

func TestApplyMutationFieldLevelMergeDifferentFieldsBothSurvive(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-both-1", Name: "Drill", Description: "old desc", Quantity: 1, ShortCode: "SC-BOTH1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-both-1", Name: "Drill", Description: "old desc", Quantity: 5, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance item to version 2: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-both-1",
		EntityType:  "item",
		EntityID:    "item-both-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "New Name", "description": "new desc"}),
		Now:         3000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied || len(outcome.ConflictFields) != 0 {
		t.Fatalf("outcome = %+v, want Applied=true, no conflicts (both fields untracked since base_version)", outcome)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-both-1")
	if err != nil {
		t.Fatalf("Get item: %v", err)
	}
	if item.Name != "New Name" || item.Description != "new desc" || item.Quantity != 5 {
		t.Fatalf("item after merge = %+v, want Name=New Name Description=new desc Quantity=5", item)
	}
}

func TestApplyMutationFieldLevelMergeBootstrapDefaultAppliesUntrackedField(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-bootstrap-1", Name: "Ladder", Quantity: 1, ShortCode: "SC-BOOT1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if v, ok := fieldVersionRow(t, s, "groupA", "item", "item-bootstrap-1", "name"); ok {
		t.Fatalf("field_versions[item/item-bootstrap-1/name] = %d, want no row before any push", v)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-bootstrap-1", Name: "Ladder", Quantity: 2, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance to v2: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-bootstrap-1", Name: "Ladder", Quantity: 3, ExpectedVersion: 2, Now: 3,
	}); err != nil {
		t.Fatalf("advance to v3: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-bootstrap-1",
		EntityType:  "item",
		EntityID:    "item-bootstrap-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Step Ladder"}),
		Now:         4000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied || len(outcome.ConflictFields) != 0 {
		t.Fatalf("outcome = %+v, want Applied=true, no conflicts (bootstrap default merges)", outcome)
	}
	if outcome.Version != 4 {
		t.Fatalf("outcome.Version = %d, want 4", outcome.Version)
	}
	if v, ok := fieldVersionRow(t, s, "groupA", "item", "item-bootstrap-1", "name"); !ok || v != 4 {
		t.Fatalf("field_versions[item/item-bootstrap-1/name] = (%d,%v), want (4,true) after this merge", v, ok)
	}
}

func TestApplyMutationFieldLevelMergeSameFieldConflictWritesConflictRowWithSnapshots(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-samefield-1", Name: "Hammer", Quantity: 1, ShortCode: "SC-SAME1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-samefield-1", Name: "Mallet", Quantity: 1, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("REST-edit name to Mallet: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-samefield-1",
		EntityType:  "item",
		EntityID:    "item-samefield-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Rubber Mallet"}),
		Now:         3000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false -- name is the ONLY named field and it conflicts")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"name"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [name]", outcome.ConflictFields)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-samefield-1")
	if err != nil {
		t.Fatalf("Get item: %v", err)
	}
	if item.Name != "Mallet" || item.Version != 2 {
		t.Fatalf("item = %+v, want unchanged (Name=Mallet Version=2) -- server value wins", item)
	}

	rows := readConflictRows(t, s, "groupA", "item-samefield-1")
	if len(rows) != 1 {
		t.Fatalf("conflicts rows = %+v, want exactly 1", rows)
	}
	c := rows[0]
	if c.fieldName != "name" {
		t.Fatalf("conflict field_name = %q, want name", c.fieldName)
	}
	if !c.serverSnapshot.Valid || c.serverSnapshot.String != `"Mallet"` {
		t.Fatalf("conflict server_value_snapshot = %+v, want valid %q", c.serverSnapshot, `"Mallet"`)
	}
	if !c.losingSnapshot.Valid || c.losingSnapshot.String != `"Rubber Mallet"` {
		t.Fatalf("conflict losing_client_value_snapshot = %+v, want valid %q", c.losingSnapshot, `"Rubber Mallet"`)
	}
	if c.mutationID != mustLedgerID(t, s, "groupA", "mut-samefield-1") {
		t.Fatalf("conflict mutation_id = %q, want the ledger row's own id", c.mutationID)
	}
}

func TestApplyMutationEntityConflictWritesConflictRowWithNullSnapshots(t *testing.T) {
	commitA, _, _, _, s := pushCommitScope(t)

	_, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-entity-null-1",
		EntityType:  "item",
		EntityID:    "item-never-existed-null",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Ghost"}),
		Now:         4000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}

	rows := readConflictRows(t, s, "groupA", "item-never-existed-null")
	if len(rows) != 1 {
		t.Fatalf("conflicts rows = %+v, want exactly 1", rows)
	}
	c := rows[0]
	if c.fieldName != entityConflictField {
		t.Fatalf("conflict field_name = %q, want %q", c.fieldName, entityConflictField)
	}
	if c.serverSnapshot.Valid || c.losingSnapshot.Valid {
		t.Fatalf("conflict snapshots = %+v, want both NULL for a whole-entity conflict", c)
	}
}

func TestApplyMutationA155ConflictStillWritesConflictRowAndLedgerRowAfterEntityWriteRollback(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "label-a155-existing", Name: "Garage", Color: "#123456", Now: 1,
	}); err != nil {
		t.Fatalf("seed existing label: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-a155-1",
		EntityType:  "label",
		EntityID:    "label-a155-loser",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Garage", "color": "#abcdef"}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}

	q := gen.New(s.store.Reader())
	ledger, err := q.GetMutationByMutationID(t.Context(), gen.GetMutationByMutationIDParams{
		GroupID: "groupA", MutationID: "mut-a155-1",
	})
	if err != nil {
		t.Fatalf("read back ledger row: %v", err)
	}
	if ledger.Outcome != string(MutationOutcomeConflict) {
		t.Fatalf("ledger.Outcome = %q, want conflict", ledger.Outcome)
	}

	rows := readConflictRows(t, s, "groupA", "label-a155-loser")
	if len(rows) != 1 {
		t.Fatalf("conflicts rows = %+v, want exactly 1", rows)
	}
	if rows[0].fieldName != "name" || rows[0].mutationID != ledger.ID {
		t.Fatalf("conflict row = %+v, want field_name=name mutation_id=%s", rows[0], ledger.ID)
	}
	if n := countRowsForGroup(t, s, "labels", "groupA"); n != 1 {
		t.Fatalf("labels row count = %d, want 1 (the losing create must never durably write)", n)
	}
}

func TestApplyMutationStockAdjustmentThenSameBatchItemUpdate(t *testing.T) {
	t.Run("name only: both mutations apply", func(t *testing.T) {
		commitA, _, scopeA, _, _ := pushCommitScope(t)

		item, err := scopeA.Items().Create(t.Context(), CreateItemParams{
			ID: "item-a158-1", Name: "Box", Quantity: 1, ShortCode: "SC-A1581", Now: 1,
		})
		if err != nil {
			t.Fatalf("seed item: %v", err)
		}
		baseVersion := item.Version

		stockOutcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-a158-stock-1",
			EntityType:  "stock_adjustment",
			EntityID:    "adj-a158-1",
			BaseVersion: 0,
			Fields:      pushFields(t, map[string]any{"item_id": item.ID, "delta": 5, "reason": "restock", "note": ""}),
			Now:         2000,
		})
		if err != nil || !stockOutcome.Applied {
			t.Fatalf("stock adjustment ApplyMutation = %+v, %v, want Applied=true", stockOutcome, err)
		}

		updateOutcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-a158-item-1",
			EntityType:  "item",
			EntityID:    item.ID,
			BaseVersion: baseVersion,
			Fields:      pushFields(t, map[string]any{"name": "Renamed Box"}),
			Now:         2001,
		})
		if err != nil {
			t.Fatalf("item update ApplyMutation: %v", err)
		}
		if !updateOutcome.Applied || len(updateOutcome.ConflictFields) != 0 {
			t.Fatalf("item update outcome = %+v, want Applied=true, no conflicts", updateOutcome)
		}

		got, err := scopeA.Items().Get(t.Context(), item.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != "Renamed Box" || got.Quantity != 6 {
			t.Fatalf("item = %+v, want Name=Renamed Box Quantity=6", got)
		}
	})

	t.Run("name and quantity: quantity conflicts, name still applies", func(t *testing.T) {
		commitA, _, scopeA, _, _ := pushCommitScope(t)

		item, err := scopeA.Items().Create(t.Context(), CreateItemParams{
			ID: "item-a158-2", Name: "Crate", Quantity: 1, ShortCode: "SC-A1582", Now: 1,
		})
		if err != nil {
			t.Fatalf("seed item: %v", err)
		}
		baseVersion := item.Version

		if _, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-a158-stock-2",
			EntityType:  "stock_adjustment",
			EntityID:    "adj-a158-2",
			BaseVersion: 0,
			Fields:      pushFields(t, map[string]any{"item_id": item.ID, "delta": 3, "reason": "restock", "note": ""}),
			Now:         2000,
		}); err != nil {
			t.Fatalf("stock adjustment ApplyMutation: %v", err)
		}

		updateOutcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-a158-item-2",
			EntityType:  "item",
			EntityID:    item.ID,
			BaseVersion: baseVersion,
			Fields:      pushFields(t, map[string]any{"name": "Renamed Crate", "quantity": 99}),
			Now:         2001,
		})
		if err != nil {
			t.Fatalf("item update ApplyMutation: %v", err)
		}
		if !updateOutcome.Applied {
			t.Fatalf("outcome.Applied = false, want true (name still applies)")
		}
		if !equalStringSlices(updateOutcome.ConflictFields, []string{"quantity"}) {
			t.Fatalf("outcome.ConflictFields = %v, want [quantity]", updateOutcome.ConflictFields)
		}

		got, err := scopeA.Items().Get(t.Context(), item.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != "Renamed Crate" || got.Quantity != 4 {
			t.Fatalf("item = %+v, want Name=Renamed Crate Quantity=4", got)
		}
	})
}

func TestApplyMutationRESTEditThenStalePushConflictsTouchedFieldMergesUntouched(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-a153-1", Name: "Old Name", Description: "Old Description", Quantity: 1, ShortCode: "SC-A1531", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-a153-1", Name: "REST Name", Description: "Old Description", Quantity: 1, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("REST-edit name: %v", err)
	}

	t.Run("stale push touching name conflicts, REST value wins", func(t *testing.T) {
		outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-a153-name",
			EntityType:  "item",
			EntityID:    "item-a153-1",
			BaseVersion: 1,
			Fields:      pushFields(t, map[string]any{"name": "Offline Name"}),
			Now:         3000,
		})
		if err != nil {
			t.Fatalf("ApplyMutation: %v", err)
		}
		if outcome.Applied {
			t.Fatalf("outcome.Applied = true, want false")
		}
		if !equalStringSlices(outcome.ConflictFields, []string{"name"}) {
			t.Fatalf("outcome.ConflictFields = %v, want [name]", outcome.ConflictFields)
		}
		item, err := scopeA.Items().Get(t.Context(), "item-a153-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if item.Name != "REST Name" {
			t.Fatalf("item.Name = %q, want REST Name (server/REST value wins)", item.Name)
		}
	})

	t.Run("same stale push touching description only merges", func(t *testing.T) {
		outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-a153-description",
			EntityType:  "item",
			EntityID:    "item-a153-1",
			BaseVersion: 1,
			Fields:      pushFields(t, map[string]any{"description": "Offline Description"}),
			Now:         3001,
		})
		if err != nil {
			t.Fatalf("ApplyMutation: %v", err)
		}
		if !outcome.Applied || len(outcome.ConflictFields) != 0 {
			t.Fatalf("outcome = %+v, want Applied=true, no conflicts", outcome)
		}
		item, err := scopeA.Items().Get(t.Context(), "item-a153-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if item.Description != "Offline Description" {
			t.Fatalf("item.Description = %q, want Offline Description", item.Description)
		}
	})
}

func TestApplyMutationCustomFieldValueUnionRuleConflictsWholeUnionTogether(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	item, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-union-1", Name: "Box", Quantity: 1, ShortCode: "SC-UNION1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	cf, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-union-1", ItemID: item.ID, Name: "Weight", FieldType: "text", TextValue: strPtr("light"), Now: 1,
	})
	if err != nil {
		t.Fatalf("seed custom field: %v", err)
	}

	setup, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-union-setup",
		EntityType:  "item_custom_field",
		EntityID:    cf.ID,
		BaseVersion: cf.Version,
		Fields: pushFields(t, map[string]any{
			"item_id": item.ID, "field_type": "number", "text_value": nil, "number_value": 42.0,
			"bool_value": nil, "date_value": nil,
		}),
		Now: 2000,
	})
	if err != nil || !setup.Applied {
		t.Fatalf("setup ApplyMutation = %+v, %v, want Applied=true", setup, err)
	}

	row, err := scopeA.ItemCustomFields().Get(t.Context(), item.ID, cf.ID)
	if err != nil {
		t.Fatalf("Get after setup: %v", err)
	}
	baseVersion := int64(1)
	if row.Version != 2 {
		t.Fatalf("row.Version after setup = %d, want 2", row.Version)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-union-conflict",
		EntityType:  "item_custom_field",
		EntityID:    cf.ID,
		BaseVersion: baseVersion,
		Fields: pushFields(t, map[string]any{
			"item_id": item.ID, "field_type": "boolean", "bool_value": true, "name": "Renamed Weight",
		}),
		Now: 3000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied {
		t.Fatalf("outcome.Applied = false, want true -- name still merges")
	}
	wantConflicts := []string{"bool_value", "field_type"}
	got := append([]string(nil), outcome.ConflictFields...)
	sort.Strings(got)
	if !equalStringSlices(got, wantConflicts) {
		t.Fatalf("outcome.ConflictFields = %v, want %v (the whole present union, together)", outcome.ConflictFields, wantConflicts)
	}

	final, err := scopeA.ItemCustomFields().Get(t.Context(), item.ID, cf.ID)
	if err != nil {
		t.Fatalf("Get after conflicting merge: %v", err)
	}
	if final.Name != "Renamed Weight" {
		t.Fatalf("final.Name = %q, want Renamed Weight (outside the union, merges independently)", final.Name)
	}
	if final.FieldType != "number" {
		t.Fatalf("final.FieldType = %q, want number (union kept untouched)", final.FieldType)
	}
	if !final.NumberValue.Valid || final.NumberValue.Float64 != 42 {
		t.Fatalf("final.NumberValue = %+v, want valid 42", final.NumberValue)
	}
	if final.BoolValue.Valid || final.TextValue.Valid || final.DateValue.Valid {
		t.Fatalf("final value columns = %+v, want ONLY NumberValue set (A103.2)", final)
	}

	rows := readConflictRows(t, s, "groupA", cf.ID)
	if len(rows) != 2 {
		t.Fatalf("conflicts rows = %+v, want exactly 2 (field_type, bool_value)", rows)
	}
}

func TestApplyMutationFieldLevelMergeIsScopedToItsOwnGroup(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	item, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-scope-1", Name: "Saw", Quantity: 1, ShortCode: "SC-SCOPE1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: item.ID, Name: "Saw", Quantity: 2, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance groupA item: %v", err)
	}

	pushB, err := s.ForGroupPush(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroupPush(groupB): %v", err)
	}
	if err := pushB.UpsertFieldVersion(t.Context(), UpsertFieldVersionParams{
		EntityType: "item", EntityID: item.ID, FieldName: "name", Version: 999, Now: 1,
	}); err != nil {
		t.Fatalf("poison groupB field_versions: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-scope-1",
		EntityType:  "item",
		EntityID:    item.ID,
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Circular Saw"}),
		Now:         3000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied || len(outcome.ConflictFields) != 0 {
		t.Fatalf("outcome = %+v, want Applied=true, no conflicts -- groupB's poisoned row must not leak into groupA's merge", outcome)
	}

	rows := readConflictRows(t, s, "groupB", item.ID)
	if len(rows) != 0 {
		t.Fatalf("groupB conflicts rows = %+v, want none (groupA's push must never write groupB's ledger)", rows)
	}
	if v, ok := fieldVersionRow(t, s, "groupA", "item", item.ID, "name"); !ok || v != 3 {
		t.Fatalf("field_versions[groupA/item/%s/name] = (%d,%v), want (3,true)", item.ID, v, ok)
	}
}

func TestApplyMutationReplayOfPartiallyAppliedMutationAnswersSkippedWithNoSecondWrite(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-replay-partial-1", Name: "Hammer", Quantity: 1, ShortCode: "SC-REPPART1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-replay-partial-1", Name: "Hammer", Quantity: 9, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance to v2: %v", err)
	}

	m := PushMutation{
		MutationID:  "mut-replay-partial-1",
		EntityType:  "item",
		EntityID:    "item-replay-partial-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Mallet", "quantity": 2}),
		Now:         3000,
	}

	first, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("first ApplyMutation: %v", err)
	}
	if !first.Applied || !equalStringSlices(first.ConflictFields, []string{"quantity"}) {
		t.Fatalf("first outcome = %+v, want Applied=true ConflictFields=[quantity]", first)
	}

	itemAfterFirst, err := scopeA.Items().Get(t.Context(), "item-replay-partial-1")
	if err != nil {
		t.Fatalf("Get after first: %v", err)
	}
	conflictsAfterFirst := readConflictRows(t, s, "groupA", "item-replay-partial-1")
	fieldVersionAfterFirst, _ := fieldVersionRow(t, s, "groupA", "item", "item-replay-partial-1", "name")

	second, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("second (replay) ApplyMutation: %v", err)
	}
	if !second.Skipped || second.Applied {
		t.Fatalf("replay outcome = %+v, want Skipped=true Applied=false", second)
	}

	itemAfterReplay, err := scopeA.Items().Get(t.Context(), "item-replay-partial-1")
	if err != nil {
		t.Fatalf("Get after replay: %v", err)
	}
	if itemAfterReplay.Version != itemAfterFirst.Version || itemAfterReplay.Name != itemAfterFirst.Name {
		t.Fatalf("item changed by replay: before=%+v after=%+v", itemAfterFirst, itemAfterReplay)
	}
	conflictsAfterReplay := readConflictRows(t, s, "groupA", "item-replay-partial-1")
	if len(conflictsAfterReplay) != len(conflictsAfterFirst) {
		t.Fatalf("conflicts row count changed by replay: before=%d after=%d", len(conflictsAfterFirst), len(conflictsAfterReplay))
	}
	fieldVersionAfterReplay, _ := fieldVersionRow(t, s, "groupA", "item", "item-replay-partial-1", "name")
	if fieldVersionAfterReplay != fieldVersionAfterFirst {
		t.Fatalf("field_versions[name] changed by replay: before=%d after=%d", fieldVersionAfterFirst, fieldVersionAfterReplay)
	}
	if n := countRowsForGroup(t, s, "mutations", "groupA"); n != 1 {
		t.Fatalf("mutations row count = %d, want 1 (replay must not record a second ledger row)", n)
	}
}
