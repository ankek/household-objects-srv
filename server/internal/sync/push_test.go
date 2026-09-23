package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

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

func pushCommitFor(t *testing.T, s *storage.Storage, groupID string) storage.PushCommitRepository {
	t.Helper()
	commit, err := s.ForGroupPushCommit(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupPushCommit(%s): %v", groupID, err)
	}
	return commit
}

func TestPushCreateAndUpdateBatchClassifiesEachEntry(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	createMutation := PushMutation{
		MutationID:  "mut-create",
		EntityType:  "item",
		EntityID:    "item-1",
		BaseVersion: 0,
		Fields:      pushFields(t, map[string]any{"name": "Drill"}),
	}
	updateMutation := PushMutation{
		MutationID:  "mut-update",
		EntityType:  "item",
		EntityID:    "item-1",
		BaseVersion: 1,
		Fields:      pushFields(t, map[string]any{"quantity": 4}),
	}

	result, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{createMutation, updateMutation},
		Now:       1000,
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(result.Applied) != 2 {
		t.Fatalf("len(Applied) = %d, want 2: %+v", len(result.Applied), result.Applied)
	}
	if result.Applied[0].MutationID != "mut-create" || result.Applied[0].Version != 1 {
		t.Fatalf("Applied[0] = %+v, want MutationID=mut-create Version=1", result.Applied[0])
	}
	if result.Applied[1].MutationID != "mut-update" || result.Applied[1].Version != 2 {
		t.Fatalf("Applied[1] = %+v, want MutationID=mut-update Version=2", result.Applied[1])
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("Conflicts = %+v, want empty", result.Conflicts)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %+v, want empty", result.Skipped)
	}
	if result.NewWatermark <= 0 {
		t.Fatalf("NewWatermark = %d, want > 0 after two applied writes", result.NewWatermark)
	}

	replay, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{createMutation},
		Now:       2000,
	})
	if err != nil {
		t.Fatalf("Push (replay): %v", err)
	}
	if len(replay.Applied) != 0 || len(replay.Conflicts) != 0 {
		t.Fatalf("replay result = %+v, want only Skipped populated", replay)
	}
	if len(replay.Skipped) != 1 || replay.Skipped[0].MutationID != "mut-create" {
		t.Fatalf("replay.Skipped = %+v, want one entry for mut-create", replay.Skipped)
	}
	if replay.NewWatermark != result.NewWatermark {
		t.Fatalf("replay.NewWatermark = %d, want unchanged %d", replay.NewWatermark, result.NewWatermark)
	}
}

func TestPushWholeEntityConflictProducesOneConflictsEntry(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	result, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{{
			MutationID:  "mut-absent",
			EntityType:  "item",
			EntityID:    "item-never-existed",
			BaseVersion: 1,
			Fields:      pushFields(t, map[string]any{"name": "Ghost"}),
		}},
		Now: 1000,
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("Applied = %+v, want empty", result.Applied)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("len(Conflicts) = %d, want 1: %+v", len(result.Conflicts), result.Conflicts)
	}
	c := result.Conflicts[0]
	if c.MutationID != "mut-absent" || c.EntityType != "item" || c.EntityID != "item-never-existed" || c.FieldName != "_entity" {
		t.Fatalf("Conflicts[0] = %+v, want {mut-absent item item-never-existed _entity}", c)
	}
}

func TestPushStopsTheBatchAtTheFirstStructurallyInvalidMutation(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	_, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{
			{MutationID: "mut-ok", EntityType: "item", EntityID: "item-1", BaseVersion: 0, Fields: pushFields(t, map[string]any{"name": "Drill"})},
			{MutationID: "mut-bad", EntityType: "attachment", EntityID: "attach-1", BaseVersion: 0, Fields: pushFields(t, map[string]any{})},
		},
		Now: 1000,
	})
	if !errors.Is(err, storage.ErrPushAttachmentRejected) {
		t.Fatalf("Push error = %v, want storage.ErrPushAttachmentRejected", err)
	}

	scope, err := s.ForGroup(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	if _, err := scope.Items().Get(t.Context(), "item-1"); err != nil {
		t.Fatalf("Get(item-1) after the batch stopped = %v, want the earlier mutation's own commit to have survived", err)
	}
}

func TestPushNilCommitRepositoryIsRejected(t *testing.T) {
	_, err := Push(t.Context(), nil, PushBatch{})
	if !errors.Is(err, ErrNoRepository) {
		t.Fatalf("Push(nil) error = %v, want ErrNoRepository", err)
	}
}

func TestPushStructuralErrorIsAPushMutationErrorNamingIndexAndMutationID(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	_, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{
			{MutationID: "mut-ok", EntityType: "item", EntityID: "item-1", BaseVersion: 0, Fields: pushFields(t, map[string]any{"name": "Drill"})},
			{MutationID: "mut-bad", EntityType: "attachment", EntityID: "attach-1", BaseVersion: 0, Fields: pushFields(t, map[string]any{})},
		},
		Now: 1000,
	})

	var mutErr *PushMutationError
	if !errors.As(err, &mutErr) {
		t.Fatalf("Push error = %v (%T), want *PushMutationError", err, err)
	}
	if mutErr.Index != 1 || mutErr.MutationID != "mut-bad" {
		t.Fatalf("PushMutationError = %+v, want Index=1 MutationID=mut-bad", mutErr)
	}
	if !errors.Is(err, storage.ErrPushAttachmentRejected) {
		t.Fatalf("errors.Is(err, storage.ErrPushAttachmentRejected) = false, want true (Unwrap must reach the sentinel)")
	}
}

func TestPushStateDependentConflictDoesNotAbortTheBatch(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	scope, err := s.ForGroup(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	if _, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{
		ID: "label-existing", Name: "Garage", Color: "#123456", Now: 1,
	}); err != nil {
		t.Fatalf("seed existing label: %v", err)
	}

	result, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{
			{MutationID: "mut-conflict", EntityType: "label", EntityID: "label-loser", BaseVersion: 0,
				Fields: pushFields(t, map[string]any{"name": "Garage", "color": "#abcdef"})},
			{MutationID: "mut-after-conflict", EntityType: "item", EntityID: "item-after", BaseVersion: 0,
				Fields: pushFields(t, map[string]any{"name": "Drill"})},
		},
		Now: 1000,
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].MutationID != "mut-conflict" || result.Conflicts[0].FieldName != "name" {
		t.Fatalf("Conflicts = %+v, want one entry for mut-conflict field_name=name", result.Conflicts)
	}
	if len(result.Applied) != 1 || result.Applied[0].MutationID != "mut-after-conflict" {
		t.Fatalf("Applied = %+v, want one entry for mut-after-conflict -- the conflict must not abort the batch", result.Applied)
	}
}

func TestPushItemLabelDeleteOp(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")
	scope, err := s.ForGroup(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	item, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: "item-1", Name: "Box", ShortCode: "SC-1", Now: 1})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	label, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{ID: "label-1", Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("seed label: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: "edge-1", ItemID: item.ID, LabelID: label.ID, Now: 1}); err != nil {
		t.Fatalf("seed attach: %v", err)
	}

	result, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{
			{MutationID: "mut-delete-edge", EntityType: "item_label", EntityID: "edge-1",
				Fields: pushFields(t, map[string]any{"item_id": item.ID, "label_id": label.ID}), Op: "delete"},
		},
		Now: 2000,
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(result.Applied) != 1 || result.Applied[0].MutationID != "mut-delete-edge" {
		t.Fatalf("Applied = %+v, want one entry for mut-delete-edge", result.Applied)
	}

	labels, err := scope.ItemLabels().ListForItem(t.Context(), item.ID)
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("ListForItem = %+v, want empty after delete", labels)
	}
}

type fakePushCommit struct {
	applyErr error
}

func (f fakePushCommit) ApplyMutation(context.Context, storage.PushMutation) (storage.PushOutcome, error) {
	return storage.PushOutcome{}, f.applyErr
}

func (f fakePushCommit) Watermark(context.Context) (int64, error) { return 0, nil }

func TestPushClassifiesApplyMutationErrors(t *testing.T) {
	tests := []struct {
		name              string
		applyErr          error
		wantMutationErr   bool
		wantErrIsSentinel error
	}{
		{
			name:            "structural error becomes a *PushMutationError",
			applyErr:        storage.ErrPushAttachmentRejected,
			wantMutationErr: true,
		},
		{
			name:            "transient ledger failure is NOT a *PushMutationError",
			applyErr:        errors.New("storage: push: check mutation ledger for \"mut-1\": disk I/O error"),
			wantMutationErr: false,
		},
		{
			name:              "ErrVersionMismatch backstop is NOT a *PushMutationError",
			applyErr:          fmt.Errorf("storage: item: update: %w", storage.ErrVersionMismatch),
			wantMutationErr:   false,
			wantErrIsSentinel: storage.ErrVersionMismatch,
		},
		{
			name:              "validate()'s missing-field error IS a *PushMutationError",
			applyErr:          fmt.Errorf("%w: %q", storage.ErrPushMissingField, "entity_id"),
			wantMutationErr:   true,
			wantErrIsSentinel: storage.ErrPushMissingField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commit := fakePushCommit{applyErr: tt.applyErr}

			_, err := Push(t.Context(), commit, PushBatch{
				Mutations: []PushMutation{{MutationID: "mut-1", EntityType: "item", EntityID: "item-1", BaseVersion: 0, Fields: pushFields(t, map[string]any{"name": "Drill"})}},
				Now:       1000,
			})
			if err == nil {
				t.Fatalf("Push: got nil error, want non-nil")
			}

			var mutErr *PushMutationError
			gotMutationErr := errors.As(err, &mutErr)
			if gotMutationErr != tt.wantMutationErr {
				t.Fatalf("errors.As(err, *PushMutationError) = %v, want %v (err = %v, %T)", gotMutationErr, tt.wantMutationErr, err, err)
			}
			if tt.wantErrIsSentinel != nil && !errors.Is(err, tt.wantErrIsSentinel) {
				t.Fatalf("errors.Is(err, %v) = false, want true (err = %v)", tt.wantErrIsSentinel, err)
			}
		})
	}
}

func TestPushClientMalformedMutationIsAPushMutationError(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	_, err := Push(t.Context(), commit, PushBatch{
		Mutations: []PushMutation{
			{MutationID: "mut-no-entity-id", EntityType: "item", EntityID: "", BaseVersion: 0, Fields: pushFields(t, map[string]any{"name": "Drill"})},
		},
		Now: 1000,
	})

	var mutErr *PushMutationError
	if !errors.As(err, &mutErr) {
		t.Fatalf("Push error = %v (%T), want *PushMutationError", err, err)
	}
	if mutErr.Index != 0 || mutErr.MutationID != "mut-no-entity-id" {
		t.Fatalf("PushMutationError = %+v, want Index=0 MutationID=mut-no-entity-id", mutErr)
	}
	if !errors.Is(err, storage.ErrPushMissingField) {
		t.Fatalf("errors.Is(err, storage.ErrPushMissingField) = false, want true (err = %v)", err)
	}
}
