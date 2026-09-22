package sync

import (
	"encoding/json"
	"errors"
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
