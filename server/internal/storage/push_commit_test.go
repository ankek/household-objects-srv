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

func TestApplyMutationStaleBaseVersionConflictsEveryNamedFieldAndWritesNothing(t *testing.T) {
	commitA, _, scopeA, _, _ := pushCommitScope(t)

	_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-stale-1", Name: "Hammer", Quantity: 1, ShortCode: "SC-STALE1", Now: 1,
	})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-stale-1", Name: "Hammer", Quantity: 9, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance item to version 2: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-stale-1",
		EntityType:  "item",
		EntityID:    "item-stale-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Mallet", "quantity": 2}),
		Now:         3000,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false for a stale base_version")
	}
	wantConflicts := []string{"name", "quantity"}
	if !equalStringSlices(outcome.ConflictFields, wantConflicts) {
		t.Fatalf("outcome.ConflictFields = %v, want %v", outcome.ConflictFields, wantConflicts)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-stale-1")
	if err != nil {
		t.Fatalf("Get item: %v", err)
	}
	if item.Name != "Hammer" || item.Quantity != 9 || item.Version != 2 {
		t.Fatalf("item after conflicting push = %+v, want unchanged (Name=Hammer Quantity=9 Version=2)", item)
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
