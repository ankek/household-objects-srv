package storage

import "testing"

const (
	clockIndepFarPastNow   int64 = 1
	clockIndepNormalNow    int64 = 1_700_000_000_000
	clockIndepFarFutureNow int64 = 4_102_444_800_000
)

type clockIndepPartialMergeResult struct {
	applied           bool
	conflictFields    []string
	outcomeVersion    int64
	itemName          string
	itemQuantity      int64
	itemVersion       int64
	itemChangeSeq     int64
	nameFieldVersion  int64
	quantityFVVersion int64
	quantityFVTracked bool
	conflictFieldName string
	conflictServer    string
	conflictLosing    string
}

func clockIndepRunPartialMerge(t *testing.T, now int64) clockIndepPartialMergeResult {
	t.Helper()
	commitA, _, scopeA, _, s := pushCommitScope(t)

	if _, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-clock-partial", Name: "Hammer", Quantity: 1, ShortCode: "SC-CLKP1", Now: 1,
	}); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-clock-partial", Name: "Hammer", Quantity: 9, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance item to version 2: %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-clock-partial",
		EntityType:  "item",
		EntityID:    "item-clock-partial",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"name": "Mallet", "quantity": 2}),
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-clock-partial")
	if err != nil {
		t.Fatalf("Get item: %v", err)
	}

	nameVersion, _ := fieldVersionRow(t, s, "groupA", "item", "item-clock-partial", "name")
	quantityVersion, quantityTracked := fieldVersionRow(t, s, "groupA", "item", "item-clock-partial", "quantity")

	conflicts := readConflictRows(t, s, "groupA", "item-clock-partial")
	if len(conflicts) != 1 {
		t.Fatalf("len(conflicts) = %d, want 1: %+v", len(conflicts), conflicts)
	}

	return clockIndepPartialMergeResult{
		applied:           outcome.Applied,
		conflictFields:    outcome.ConflictFields,
		outcomeVersion:    outcome.Version,
		itemName:          item.Name,
		itemQuantity:      item.Quantity,
		itemVersion:       item.Version,
		itemChangeSeq:     item.ChangeSeq,
		nameFieldVersion:  nameVersion,
		quantityFVVersion: quantityVersion,
		quantityFVTracked: quantityTracked,
		conflictFieldName: conflicts[0].fieldName,
		conflictServer:    conflicts[0].serverSnapshot.String,
		conflictLosing:    conflicts[0].losingSnapshot.String,
	}
}

func TestApplyMutationPartialMergeOutcomeIsClockIndependent(t *testing.T) {
	want := clockIndepPartialMergeResult{
		applied:           true,
		conflictFields:    []string{"quantity"},
		outcomeVersion:    3,
		itemName:          "Mallet",
		itemQuantity:      9,
		itemVersion:       3,
		itemChangeSeq:     3,
		nameFieldVersion:  3,
		quantityFVVersion: 2,
		quantityFVTracked: true,
		conflictFieldName: "quantity",
		conflictServer:    "9",
		conflictLosing:    "2",
	}

	tests := []struct {
		name string
		now  int64
	}{
		{"normal Now", clockIndepNormalNow},
		{"far-past Now (1970, near epoch)", clockIndepFarPastNow},
		{"far-future Now (2100)", clockIndepFarFutureNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clockIndepRunPartialMerge(t, tt.now)
			if got.applied != want.applied ||
				!equalStringSlices(got.conflictFields, want.conflictFields) ||
				got.outcomeVersion != want.outcomeVersion ||
				got.itemName != want.itemName ||
				got.itemQuantity != want.itemQuantity ||
				got.itemVersion != want.itemVersion ||
				got.itemChangeSeq != want.itemChangeSeq ||
				got.nameFieldVersion != want.nameFieldVersion ||
				got.quantityFVVersion != want.quantityFVVersion ||
				got.quantityFVTracked != want.quantityFVTracked ||
				got.conflictFieldName != want.conflictFieldName ||
				got.conflictServer != want.conflictServer ||
				got.conflictLosing != want.conflictLosing {
				t.Fatalf("Now=%d result = %+v, want %+v (result must be identical regardless of Now)", tt.now, got, want)
			}
		})
	}
}

func TestApplyMutationFarFutureNowDoesNotBypassStaleBaseVersionConflict(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	if _, err := scopeA.Items().Create(t.Context(), CreateItemParams{
		ID: "item-clock-bypass", Name: "Saw", Quantity: 1, ShortCode: "SC-CLKB1", Now: 1,
	}); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := scopeA.Items().Update(t.Context(), UpdateItemParams{
		ItemID: "item-clock-bypass", Name: "Saw", Quantity: 9, ExpectedVersion: 1, Now: 2,
	}); err != nil {
		t.Fatalf("advance item to version 2 (tracks quantity=2): %v", err)
	}

	outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-clock-bypass",
		EntityType:  "item",
		EntityID:    "item-clock-bypass",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"quantity": 999}),
		Now:         clockIndepFarFutureNow,
	})
	if err != nil {
		t.Fatalf("ApplyMutation: %v", err)
	}
	if outcome.Applied {
		t.Fatalf("outcome.Applied = true, want false -- a far-future Now must not bypass the stale base_version conflict")
	}
	if !equalStringSlices(outcome.ConflictFields, []string{"quantity"}) {
		t.Fatalf("outcome.ConflictFields = %v, want [quantity]", outcome.ConflictFields)
	}

	item, err := scopeA.Items().Get(t.Context(), "item-clock-bypass")
	if err != nil {
		t.Fatalf("Get item: %v", err)
	}
	if item.Quantity != 9 || item.Version != 2 {
		t.Fatalf("item after rejected merge = %+v, want Quantity=9 Version=2 (server value untouched)", item)
	}

	fv, tracked := fieldVersionRow(t, s, "groupA", "item", "item-clock-bypass", "quantity")
	if !tracked || fv != 2 {
		t.Fatalf("field_versions[quantity] = (tracked=%v, version=%d), want (true, 2) -- untouched by the rejected merge", tracked, fv)
	}
}

func TestApplyMutationChangeSeqAllocationOrderIsServerTxOrderNotNow(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	batch := []struct {
		entityID string
		now      int64
	}{
		{"item-clock-seq-1", clockIndepFarFutureNow},
		{"item-clock-seq-2", clockIndepNormalNow},
		{"item-clock-seq-3", clockIndepFarPastNow},
		{"item-clock-seq-4", clockIndepFarFutureNow},
	}

	for i, m := range batch {
		outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
			MutationID:  "mut-clock-seq-" + m.entityID,
			EntityType:  "item",
			EntityID:    m.entityID,
			BaseVersion: 0,
			Fields:      pushFields(t, map[string]any{"name": "Item", "quantity": 1}),
			Now:         m.now,
		})
		if err != nil {
			t.Fatalf("ApplyMutation[%d] (%s): %v", i, m.entityID, err)
		}
		if !outcome.Applied {
			t.Fatalf("ApplyMutation[%d] (%s): outcome.Applied = false, want true", i, m.entityID)
		}
	}

	for i, m := range batch {
		item, err := scopeA.Items().Get(t.Context(), m.entityID)
		if err != nil {
			t.Fatalf("Get %s: %v", m.entityID, err)
		}
		wantSeq := int64(i + 1)
		if item.ChangeSeq != wantSeq {
			t.Fatalf("%s: ChangeSeq = %d, want %d (call order %d, Now=%d) -- change_seq must follow call order, not Now", m.entityID, item.ChangeSeq, wantSeq, i, m.now)
		}
	}

	watermark, err := commitA.Watermark(t.Context())
	if err != nil {
		t.Fatalf("Watermark: %v", err)
	}
	if watermark != int64(len(batch)) {
		t.Fatalf("Watermark = %d, want %d", watermark, len(batch))
	}
	_ = s
}

func TestApplyMutationFieldVersionEqualsEntityVersionNeverDerivedFromNow(t *testing.T) {
	tests := []struct {
		name string
		now  int64
	}{
		{"normal Now", clockIndepNormalNow},
		{"far-past Now", clockIndepFarPastNow},
		{"far-future Now", clockIndepFarFutureNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commitA, _, scopeA, _, s := pushCommitScope(t)

			if _, err := scopeA.Items().Create(t.Context(), CreateItemParams{
				ID: "item-clock-fv", Name: "Drill", Quantity: 1, ShortCode: "SC-CLKFV", Now: 1,
			}); err != nil {
				t.Fatalf("seed item: %v", err)
			}

			outcome, err := commitA.ApplyMutation(t.Context(), PushMutation{
				MutationID:  "mut-clock-fv",
				EntityType:  "item",
				EntityID:    "item-clock-fv",
				BaseVersion: 1,
				Fields:      pushFields(t, map[string]any{"quantity": 42}),
				Now:         tt.now,
			})
			if err != nil {
				t.Fatalf("ApplyMutation: %v", err)
			}
			if !outcome.Applied || outcome.Version != 2 {
				t.Fatalf("outcome = %+v, want Applied=true Version=2", outcome)
			}

			item, err := scopeA.Items().Get(t.Context(), "item-clock-fv")
			if err != nil {
				t.Fatalf("Get item: %v", err)
			}
			if item.Version != 2 {
				t.Fatalf("item.Version = %d, want 2", item.Version)
			}

			fv, tracked := fieldVersionRow(t, s, "groupA", "item", "item-clock-fv", "quantity")
			if !tracked || fv != item.Version {
				t.Fatalf("field_versions[quantity] = (tracked=%v, version=%d), want (true, %d) == item.Version, regardless of Now=%d", tracked, fv, item.Version, tt.now)
			}
		})
	}
}

func TestApplyMutationFarPastNowDoesNotSortBehindWatermark(t *testing.T) {
	commitA, _, scopeA, _, s := pushCommitScope(t)

	if _, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-clock-watermark-1",
		EntityType:  "item",
		EntityID:    "item-clock-watermark-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Wrench", "quantity": 1}),
		Now:         clockIndepNormalNow,
	}); err != nil {
		t.Fatalf("ApplyMutation[1]: %v", err)
	}

	watermarkBefore, err := commitA.Watermark(t.Context())
	if err != nil {
		t.Fatalf("Watermark (before): %v", err)
	}

	outcome2, err := commitA.ApplyMutation(t.Context(), PushMutation{
		MutationID:  "mut-clock-watermark-2",
		EntityType:  "item",
		EntityID:    "item-clock-watermark-2",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Plane", "quantity": 1}),
		Now:         clockIndepFarPastNow,
	})
	if err != nil {
		t.Fatalf("ApplyMutation[2] (far-past Now): %v", err)
	}
	if !outcome2.Applied {
		t.Fatalf("outcome2.Applied = false, want true")
	}

	item2, err := scopeA.Items().Get(t.Context(), "item-clock-watermark-2")
	if err != nil {
		t.Fatalf("Get item-clock-watermark-2: %v", err)
	}
	if item2.ChangeSeq <= watermarkBefore {
		t.Fatalf("item-clock-watermark-2.ChangeSeq = %d, want > watermarkBefore=%d -- a far-past Now must not sort the row behind an already-published watermark", item2.ChangeSeq, watermarkBefore)
	}

	watermarkAfter, err := commitA.Watermark(t.Context())
	if err != nil {
		t.Fatalf("Watermark (after): %v", err)
	}
	if watermarkAfter != item2.ChangeSeq {
		t.Fatalf("watermarkAfter = %d, want %d (== the far-past-Now mutation's own change_seq)", watermarkAfter, item2.ChangeSeq)
	}
	_ = s
}

func TestApplyMutationRejectsNonPositiveNow(t *testing.T) {
	commitA, _, _, _, _ := pushCommitScope(t)

	for _, now := range []int64{0, -1, -4_102_444_800_000} {
		m := PushMutation{
			MutationID:  "mut-clock-nonpositive",
			EntityType:  "item",
			EntityID:    "item-clock-nonpositive",
			BaseVersion: 0,
			Fields:      pushFields(t, map[string]any{"name": "X", "quantity": 1}),
			Now:         now,
		}
		if _, err := commitA.ApplyMutation(t.Context(), m); err == nil {
			t.Fatalf("ApplyMutation with Now=%d: want error, got nil", now)
		}
	}
}
