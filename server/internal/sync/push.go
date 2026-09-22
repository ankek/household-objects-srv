package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
)

type PushMutation struct {
	MutationID  string
	EntityType  string
	EntityID    string
	BaseVersion int64
	Fields      map[string]json.RawMessage
	Op          string
}

type PushBatch struct {
	Mutations []PushMutation
	Now       int64
}

type PushAppliedEntry struct {
	MutationID string
	EntityType string
	EntityID   string
	Version    int64
}

type PushSkippedEntry struct {
	MutationID string
}

type PushConflictEntry struct {
	MutationID string
	EntityType string
	EntityID   string
	FieldName  string
}

type PushResult struct {
	Applied      []PushAppliedEntry
	Skipped      []PushSkippedEntry
	Conflicts    []PushConflictEntry
	NewWatermark int64
}

type PushMutationError struct {
	Index      int
	MutationID string
	Err        error
}

func (e *PushMutationError) Error() string {
	return fmt.Sprintf("sync: push: mutation[%d] %q: %v", e.Index, e.MutationID, e.Err)
}

func (e *PushMutationError) Unwrap() error { return e.Err }

func Push(ctx context.Context, commit storage.PushCommitRepository, batch PushBatch) (PushResult, error) {
	if commit == nil {
		return PushResult{}, fmt.Errorf("%w", ErrNoRepository)
	}

	var result PushResult
	for i, m := range batch.Mutations {
		outcome, err := commit.ApplyMutation(ctx, storage.PushMutation{
			MutationID:  m.MutationID,
			EntityType:  m.EntityType,
			EntityID:    m.EntityID,
			BaseVersion: m.BaseVersion,
			Fields:      m.Fields,
			Now:         batch.Now,
			Op:          storage.PushOp(m.Op),
		})
		if err != nil {
			return PushResult{}, &PushMutationError{Index: i, MutationID: m.MutationID, Err: err}
		}

		if outcome.Skipped {
			result.Skipped = append(result.Skipped, PushSkippedEntry{MutationID: m.MutationID})
			continue
		}

		if outcome.Applied {
			result.Applied = append(result.Applied, PushAppliedEntry{
				MutationID: m.MutationID,
				EntityType: outcome.EntityType,
				EntityID:   outcome.EntityID,
				Version:    outcome.Version,
			})
		}
		for _, field := range outcome.ConflictFields {
			result.Conflicts = append(result.Conflicts, PushConflictEntry{
				MutationID: m.MutationID,
				EntityType: outcome.EntityType,
				EntityID:   outcome.EntityID,
				FieldName:  field,
			})
		}
	}

	watermark, err := commit.Watermark(ctx)
	if err != nil {
		return PushResult{}, fmt.Errorf("sync: push: read watermark: %w", err)
	}
	result.NewWatermark = watermark
	return result, nil
}
