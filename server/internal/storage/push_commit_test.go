package storage

import (
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"testing"
)

func pushCommitScope(t *testing.T) (commitA, commitB PushCommitRepository, scopeA, scopeB Scope, s *Storage) {
	t.Helper()
	s = newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	seedGroupRow(t, s, "groupB")

	var err error
	commitA, err = s.ForGroupPushCommit(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroupPushCommit(groupA): %v", err)
	}
	commitB, err = s.ForGroupPushCommit(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroupPushCommit(groupB): %v", err)
	}
	scopeA, err = s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err = s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return commitA, commitB, scopeA, scopeB, s
}

func pushFields(t *testing.T, fields map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal field %q: %v", k, err)
		}
		out[k] = b
	}
	return out
}

func countRowsForGroup(t *testing.T, s *Storage, table, groupID string) int {
	t.Helper()
	var n int
	if err := s.store.Reader().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM "+table+" WHERE group_id = ?", groupID).Scan(&n); err != nil {
		t.Fatalf("count %s for %q: %v", table, groupID, err)
	}
	return n
}

func TestApplyMutationCreateAppliesAndRecordsLedger(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	m := PushMutation{
		MutationID:  "mut-create-1",
		EntityType:  "item",
		EntityID:    "item-create-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Drill", "quantity": 3}),
		Now:         1000,
	}

	outcome, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied || outcome.Skipped {
		t.Fatalf("outcome = %+v, want Applied=true Skipped=false", outcome)
	}
	if outcome.EntityID != "item-create-1" || outcome.Version != 1 {
		t.Fatalf("outcome = %+v, want EntityID=item-create-1 Version=1", outcome)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-create-1")
	if err != nil {
		t.Fatalf("Get created item: %v", err)
	}
	if item.Name != "Drill" || item.Quantity != 3 || item.Version != 1 {
		t.Fatalf("created item = %+v, want Name=Drill Quantity=3 Version=1", item)
	}

	q := gen.New(s.store.Reader())
	ledger, err := q.GetMutationByMutationID(t.Context(), gen.GetMutationByMutationIDParams{
		GroupID: "groupA", MutationID: "mut-create-1",
	})
	if err != nil {
		t.Fatalf("read back ledger row: %v", err)
	}
	if ledger.Outcome != string(MutationOutcomeApplied) || ledger.EntityID != "item-create-1" {
		t.Fatalf("ledger row = %+v, want Outcome=applied EntityID=item-create-1", ledger)
	}
}

func TestApplyMutationReplaySkipsWithNoNewRowsOrChangeSeq(t *testing.T) {
	commitA, _, _, _, s := pushCommitScope(t)

	m := PushMutation{
		MutationID:  "mut-replay-1",
		EntityType:  "item",
		EntityID:    "item-replay-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Ladder"}),
		Now:         1000,
	}

	first, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("first ApplyMutation: %v", err)
	}
	if !first.Applied {
		t.Fatalf("first outcome = %+v, want Applied=true", first)
	}
	watermarkAfterFirst, err := commitA.Watermark(t.Context())
	if err != nil {
		t.Fatalf("Watermark after first: %v", err)
	}

	second, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("second (replay) ApplyMutation: %v", err)
	}
	if !second.Skipped || second.Applied {
		t.Fatalf("replay outcome = %+v, want Skipped=true Applied=false", second)
	}

	watermarkAfterReplay, err := commitA.Watermark(t.Context())
	if err != nil {
		t.Fatalf("Watermark after replay: %v", err)
	}
	if watermarkAfterReplay != watermarkAfterFirst {
		t.Fatalf("watermark after replay = %d, want unchanged from %d (a skip must allocate no change_seq)",
			watermarkAfterReplay, watermarkAfterFirst)
	}

	if n := countRowsForGroup(t, s, "items", "groupA"); n != 1 {
		t.Fatalf("items row count = %d, want 1 (replay must not write a second row)", n)
	}
	if n := countRowsForGroup(t, s, "mutations", "groupA"); n != 1 {
		t.Fatalf("mutations row count = %d, want 1 (replay must not record a second ledger row)", n)
	}
}

func TestApplyMutationUpdateMatchingBaseVersionAppliesAndBumpsVersion(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-upd-1", Name: "Saw", Quantity: 1, ShortCode: "SC-UPD1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-upd-1",
		EntityType:  "item",
		EntityID:    "item-upd-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"quantity": 5}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied || len(outcome.ConflictFields) != 0 {
		t.Fatalf("outcome = %+v, want Applied=true, no conflicts", outcome)
	}
	if outcome.Version != 2 {
		t.Fatalf("outcome.Version = %d, want 2", outcome.Version)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-upd-1")
	if err != nil {
		t.Fatalf("Get updated item: %v", err)
	}
	if item.Quantity != 5 || item.Name != "Saw" || item.Version != 2 {
		t.Fatalf("updated item = %+v, want Quantity=5 Name=Saw Version=2", item)
	}
}

func TestApplyMutationUpdateAbsentEntityConflictsWithoutResurrection(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-absent-1",
		EntityType:  "item",
		EntityID:    "item-never-existed",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Ghost"}),
		Now:         4000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false for an absent entity")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{entityConflictField}) {
		t.Fatalf("outcome.ConflictFields = %v, want [%s]", outcome.ConflictFields, entityConflictField)
	}

	if _, err := scopeA.Items().Get(t.Context(), "item-never-existed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(item-never-existed) error = %v, want ErrNotFound -- a conflict must never resurrect the row", err)
	}
}

func TestApplyMutationIsAtomicAcrossEntityWriteAndLedgerInsert(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	injected := errors.New("injected failure after entity write")
	pushCommitFailAfterEntityWrite = func() error { return injected }
	t.Cleanup(func() { pushCommitFailAfterEntityWrite = nil })

	_, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-atomic-1",
		EntityType:  "item",
		EntityID:    "item-atomic-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Torn"}),
		Now:         5000,
	})
	if !errors.Is(err, injected) {
		t.Fatalf("ApplyMutation error = %v, want the injected failure", err)
	}

	if _, err := scopeA.Items().Get(t.Context(), "item-atomic-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(item-atomic-1) error = %v, want ErrNotFound -- the entity write must have rolled back", err)
	}
	if n := countRowsForGroup(t, s, "items", "groupA"); n != 0 {
		t.Fatalf("items row count = %d, want 0 (entity write must not survive the rollback)", n)
	}
	if n := countRowsForGroup(t, s, "mutations", "groupA"); n != 0 {
		t.Fatalf("mutations row count = %d, want 0 (no ledger row on a rolled-back mutation)", n)
	}
}

func TestApplyMutationCrossGroupCannotUpdateAnothersEntity(t *testing.T) {
	commitA, commitB, scopeA, _, _ := pushCommitScope(t)

	_, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-cross-create",
		EntityType:  "item",
		EntityID:    "item-cross-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "GroupA's Item"}),
		Now:         6000,
	})
	if err != nil {
		t.Fatalf("groupA create: %v", err)
	}

	outcome, err := commitB.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-cross-update",
		EntityType:  "item",
		EntityID:    "item-cross-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Stolen"}),
		Now:         7000,
	})
	if err != nil {
		t.Fatalf("groupB update attempt: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false -- groupB must not be able to write groupA's item")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{entityConflictField}) {
		t.Fatalf("outcome.ConflictFields = %v, want [%s]", outcome.ConflictFields, entityConflictField)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-cross-1")
	if err != nil {
		t.Fatalf("groupA Get after cross-group attempt: %v", err)
	}
	if item.Name != "GroupA's Item" || item.Version != 1 {
		t.Fatalf("groupA's item = %+v, want unchanged (Name=GroupA's Item Version=1)", item)
	}
}

func TestApplyMutationUnknownFieldIsRejected(t *testing.T) {
	commitA, _, _, _, _ := pushCommitScope(t)

	_, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-unknown-field",
		EntityType:  "item",
		EntityID:    "item-unknown-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Drill", "not_a_real_field": true}),
		Now:         8000,
	})
	if !errors.Is(err, ErrPushUnknownField) {
		t.Fatalf("ApplyMutation error = %v, want ErrPushUnknownField", err)
	}
}

func TestApplyMutationAttachmentIsAlwaysRejected(t *testing.T) {
	commitA, _, _, _, _ := pushCommitScope(t)

	_, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-attachment-1",
		EntityType:  "attachment",
		EntityID:    "attachment-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{}),
		Now:         9000,
	})
	if !errors.Is(err, ErrPushAttachmentRejected) {
		t.Fatalf("ApplyMutation error = %v, want ErrPushAttachmentRejected", err)
	}
}

func TestApplyMutationCreationOnlyEntityRejectsNonZeroBaseVersion(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-for-stock-1", Name: "Box", Quantity: 1, ShortCode: "SC-STOCK1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}

	_, err = commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-stock-bad-version",
		EntityType:  "stock_adjustment",
		EntityID:    "adj-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"item_id": "item-for-stock-1", "delta": 1, "reason": "recount", "note": ""}),
		Now:         10000,
	})
	if !errors.Is(err, ErrPushCreationOnly) {
		t.Fatalf("ApplyMutation error = %v, want ErrPushCreationOnly", err)
	}
}

func TestApplyMutationWarrantyCreateOnExistingBlockIsConflict(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-warr-1", Name: "Fridge", ShortCode: "SC-WARR1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Warranty().Create(t.Context(), CreateWarrantyParams{
		ID: "warr-existing", ItemID: "item-warr-1", Holder: "Acme", Now: 1,
	}); err != nil {
		t.Fatalf("seed existing warranty: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-warr-conflict",
		EntityType:  "warranty_block",
		EntityID:    "warr-second-device",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"item_id": "item-warr-1", "holder": "Other Co"}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{entityConflictField}) {
		t.Fatalf("outcome.ConflictFields = %v, want [%s]", outcome.ConflictFields, entityConflictField)
	}

	q := gen.New(s.store.Reader())
	ledger, err := q.GetMutationByMutationID(t.Context(), gen.GetMutationByMutationIDParams{
		GroupID: "groupA", MutationID: "mut-warr-conflict",
	})
	if err != nil {
		t.Fatalf("read back ledger row: %v", err)
	}
	if ledger.Outcome != string(MutationOutcomeConflict) {
		t.Fatalf("ledger.Outcome = %q, want conflict", ledger.Outcome)
	}
	if n := countRowsForGroup(t, s, "item_warranty", "groupA"); n != 1 {
		t.Fatalf("item_warranty row count = %d, want 1 (the losing create must write nothing)", n)
	}
}

func TestApplyMutationLabelCreateNameConflictIsRecorded(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "label-existing", Name: "Garage", Color: "#123456", Now: 1,
	}); err != nil {
		t.Fatalf("seed existing label: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-label-conflict",
		EntityType:  "label",
		EntityID:    "label-second-device",
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
	if !equalStringSlices(outcome.ConflictFields, []string{"name"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [name]", outcome.ConflictFields)
	}
	if n := countRowsForGroup(t, s, "labels", "groupA"); n != 1 {
		t.Fatalf("labels row count = %d, want 1", n)
	}
}

func TestApplyMutationItemCreateShortCodeExhaustionIsConflict(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	if _, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-taken-code", Name: "Ladder", ShortCode: "FIXEDCODE", Now: 1,
	}); err != nil {
		t.Fatalf("seed item with the fixed short code: %v", err)
	}

	prev := pushNewItemShortCode
	pushNewItemShortCode = func() (string, error) { return "FIXEDCODE", nil }
	t.Cleanup(func() { pushNewItemShortCode = prev })

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-shortcode-exhausted",
		EntityType:  "item",
		EntityID:    "item-new-id",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Second Ladder"}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"short_code"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [short_code]", outcome.ConflictFields)
	}
}

func TestApplyMutationLocationCreateMissingParentIsConflict(t *testing.T) {
	commitA, _, _, _, _ := pushCommitScope(t)

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-loc-parent-missing",
		EntityType:  "location",
		EntityID:    "loc-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Shed", "parent_id": "does-not-exist"}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"parent_id"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [parent_id]", outcome.ConflictFields)
	}
}

func TestApplyMutationLocationUpdateCycleIsConflict(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	root, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-root", Name: "House", Now: 1})
	if err != nil {
		t.Fatalf("seed root location: %v", err)
	}
	child, err := scopeA.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-child", Name: "Garage", ParentID: root.ID, Now: 1})
	if err != nil {
		t.Fatalf("seed child location: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-loc-cycle",
		EntityType:  "location",
		EntityID:    root.ID,
		BaseVersion: root.Version,
		Fields:      pushFields(t, map[string]any{"name": "House", "parent_id": child.ID}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"parent_id"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [parent_id]", outcome.ConflictFields)
	}
}

func TestApplyMutationCreateClientEntityIDAlreadyExistsIsConflict(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	existing, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-collide", Name: "Original", ShortCode: "SC-ORIG", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-id-collide",
		EntityType:  "item",
		EntityID:    existing.ID,
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Impostor"}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{entityConflictField}) {
		t.Fatalf("outcome.ConflictFields = %v, want [%s]", outcome.ConflictFields, entityConflictField)
	}

	item, err := scopeA.Items().Get(t.Context(), existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if item.Name != "Original" {
		t.Fatalf("item.Name = %q, want unchanged Original", item.Name)
	}
}

func TestApplyMutationWarrantyCreateMissingItemIsConflict(t *testing.T) {
	commitA, _, _, _, _ := pushCommitScope(t)

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-warr-no-item",
		EntityType:  "warranty_block",
		EntityID:    "warr-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"item_id": "item-does-not-exist", "holder": "Acme"}),
		Now:         2000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"item_id"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [item_id]", outcome.ConflictFields)
	}
}

func TestApplyMutationItemLabelCreateMissingItemOrLabelIsConflict(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	item, err := scopeA.Items().Create(t.Context(), CreateItemParams{ID: "item-for-edge", Name: "Box", ShortCode: "SC-EDGE1", Now: 1})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	label, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{ID: "label-for-edge", Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("seed label: %v", err)
	}

	t.Run("missing item_id", func(t *testing.T) {
		outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-edge-no-item",
			EntityType:  "item_label",
			EntityID:    "edge-1",
			BaseVersion: 0,
			Fields:      pushFields(t, map[string]any{"item_id": "does-not-exist", "label_id": label.ID}),
			Now:         2000,
		})
		if err != nil {
			t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
		}
		if !equalStringSlices(outcome.ConflictFields, []string{"item_id"}) {
			t.Fatalf("outcome.ConflictFields = %v, want [item_id]", outcome.ConflictFields)
		}
	})

	t.Run("missing label_id", func(t *testing.T) {
		outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-edge-no-label",
			EntityType:  "item_label",
			EntityID:    "edge-2",
			BaseVersion: 0,
			Fields:      pushFields(t, map[string]any{"item_id": item.ID, "label_id": "does-not-exist"}),
			Now:         2000,
		})
		if err != nil {
			t.Fatalf("ApplyMutation returned an error, want a recorded conflict: %v", err)
		}
		if !equalStringSlices(outcome.ConflictFields, []string{"label_id"}) {
			t.Fatalf("outcome.ConflictFields = %v, want [label_id]", outcome.ConflictFields)
		}
	})
}

func TestApplyMutationConflictedMutationReplaysAsSkipped(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	if _, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{
		ID: "label-existing-2", Name: "Garage", Color: "#123456", Now: 1,
	}); err != nil {
		t.Fatalf("seed existing label: %v", err)
	}

	m := PushMutation{
		MutationID:  "mut-label-conflict-replay",
		EntityType:  "label",
		EntityID:    "label-second-device-2",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Garage", "color": "#abcdef"}),
		Now:         2000,
	}

	first, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("first ApplyMutation: %v", err)
	}
	if first.Applied || first.Skipped {
		t.Fatalf("first outcome = %+v, want a conflict (Applied=false Skipped=false)", first)
	}

	second, err := commitA.ApplyMutation(t.Context(), m)
	if err != nil {
		t.Fatalf("second (replay) ApplyMutation: %v", err)
	}
	if !second.Skipped {
		t.Fatalf("replay outcome = %+v, want Skipped=true", second)
	}
}

func TestApplyMutationItemLabelDeleteTombstonesLiveEdge(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	item, err := scopeA.Items().Create(t.Context(), CreateItemParams{ID: "item-del-edge", Name: "Box", ShortCode: "SC-DEL1", Now: 1})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	label, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{ID: "label-del-edge", Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("seed label: %v", err)
	}
	if err := scopeA.ItemLabels().Attach(t.Context(), AttachLabelParams{ID: "edge-del-1", ItemID: item.ID, LabelID: label.ID, Now: 1}); err != nil {
		t.Fatalf("seed attach: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID: "mut-edge-delete",
		EntityType: "item_label",
		EntityID:   "edge-del-1",
		Fields:     pushFields(t, map[string]any{"item_id": item.ID, "label_id": label.ID}),
		Now:        2000,
		Op:         PushOpDelete,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if !outcome.Applied {
		t.Fatalf("outcome.Applied = false, want true")
	}
	if outcome.Version != 2 {
		t.Fatalf("outcome.Version = %d, want 2 (attach=1, detach=2)", outcome.Version)
	}

	labels, err := scopeA.ItemLabels().ListForItem(t.Context(), item.ID)
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("ListForItem = %+v, want empty after delete", labels)
	}
}

func TestApplyMutationItemLabelDeleteOnAbsentEdgeIsEntityConflict(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	item, err := scopeA.Items().Create(t.Context(), CreateItemParams{ID: "item-del-absent", Name: "Box", ShortCode: "SC-DEL2", Now: 1})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	label, err := scopeA.Labels().Create(t.Context(), CreateLabelParams{ID: "label-del-absent", Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("seed label: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID: "mut-edge-delete-absent",
		EntityType: "item_label",
		EntityID:   "edge-never-attached",
		Fields:     pushFields(t, map[string]any{"item_id": item.ID, "label_id": label.ID}),
		Now:        2000,
		Op:         PushOpDelete,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{entityConflictField}) {
		t.Fatalf("outcome.ConflictFields = %v, want [%s]", outcome.ConflictFields, entityConflictField)
	}
}

func TestApplyMutationDeleteOnUnsupportedEntityTypeIsStructuralError(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	item, err := scopeA.Items().Create(t.Context(), CreateItemParams{ID: "item-del-unsupported", Name: "Box", ShortCode: "SC-DEL3", Now: 1})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}

	_, err = commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-item-delete-unsupported",
		EntityType:  "item",
		EntityID:    item.ID,
		BaseVersion: item.Version,
		Fields:      pushFields(t, map[string]any{}),
		Now:         2000,
		Op:          PushOpDelete,
	})
	if !errors.Is(err, ErrPushDeleteNotSupported) {
		t.Fatalf("ApplyMutation error = %v, want ErrPushDeleteNotSupported", err)
	}
}

func TestApplyMutationUnknownOpIsStructuralError(t *testing.T) {
	commitA, _, _, _, _ := pushCommitScope(t)

	_, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-bad-op",
		EntityType:  "item",
		EntityID:    "item-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "X"}),
		Now:         2000,
		Op:          "bogus",
	})
	if !errors.Is(err, ErrPushUnknownOp) {
		t.Fatalf("ApplyMutation error = %v, want ErrPushUnknownOp", err)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
