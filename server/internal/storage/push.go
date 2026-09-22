package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type MutationLedgerEntry = gen.Mutation

type MutationOutcome string

const (
	MutationOutcomeApplied          MutationOutcome = "applied"
	MutationOutcomeSkippedDuplicate MutationOutcome = "skipped_duplicate"
	MutationOutcomeConflict         MutationOutcome = "conflict"
)

func (o MutationOutcome) valid() bool {
	switch o {
	case MutationOutcomeApplied, MutationOutcomeSkippedDuplicate, MutationOutcomeConflict:
		return true
	default:
		return false
	}
}

var ErrMutationAlreadyRecorded = errors.New("storage: mutation_id is already recorded for this group")

type InsertMutationLedgerEntryParams struct {
	ID         string
	MutationID string
	EntityType string
	EntityID   string
	Outcome    MutationOutcome
	Now        int64
}

func (p InsertMutationLedgerEntryParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: InsertMutationLedgerEntryParams: ID is empty")
	case p.MutationID == "":
		return errors.New("storage: InsertMutationLedgerEntryParams: MutationID is empty")
	case p.EntityType == "":
		return errors.New("storage: InsertMutationLedgerEntryParams: EntityType is empty")
	case p.EntityID == "":
		return errors.New("storage: InsertMutationLedgerEntryParams: EntityID is empty")
	case !p.Outcome.valid():
		return fmt.Errorf("storage: InsertMutationLedgerEntryParams: Outcome %q is not one of applied/skipped_duplicate/conflict", p.Outcome)
	case p.Now <= 0:
		return errors.New("storage: InsertMutationLedgerEntryParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type PushRepository interface {
	Lookup(ctx context.Context, mutationID string) (MutationLedgerEntry, error)

	Insert(ctx context.Context, p InsertMutationLedgerEntryParams) (MutationLedgerEntry, error)
}

type pushRepository struct {
	binding
}

func (r pushRepository) Lookup(ctx context.Context, mutationID string) (MutationLedgerEntry, error) {
	return lookupMutationLedgerEntryTx(ctx, r.queries(), r.group(), mutationID)
}

func (r pushRepository) Insert(ctx context.Context, p InsertMutationLedgerEntryParams) (MutationLedgerEntry, error) {
	if err := p.validate(); err != nil {
		return MutationLedgerEntry{}, err
	}

	var created MutationLedgerEntry
	err := r.writeTx(ctx, func(_ *sql.Tx, q *gen.Queries) error {
		row, err := insertMutationLedgerEntryTx(ctx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return MutationLedgerEntry{}, err
	}
	return created, nil
}

func lookupMutationLedgerEntryTx(ctx context.Context, q *gen.Queries, group, mutationID string) (MutationLedgerEntry, error) {
	row, err := q.GetMutationByMutationID(ctx, gen.GetMutationByMutationIDParams{
		GroupID: group, MutationID: mutationID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return MutationLedgerEntry{}, fmt.Errorf("storage: mutation %q: %w", mutationID, ErrNotFound)
	case err != nil:
		return MutationLedgerEntry{}, fmt.Errorf("storage: lookup mutation %q: %w", mutationID, err)
	}
	return row, nil
}

func insertMutationLedgerEntryTx(ctx context.Context, q *gen.Queries, group string, p InsertMutationLedgerEntryParams) (MutationLedgerEntry, error) {
	if err := q.CreateMutation(ctx, gen.CreateMutationParams{
		ID:         p.ID,
		GroupID:    group,
		MutationID: p.MutationID,
		EntityType: p.EntityType,
		EntityID:   p.EntityID,
		Now:        p.Now,
		Outcome:    string(p.Outcome),
	}); err != nil {
		if isUniqueConstraintViolation(err) {
			return MutationLedgerEntry{}, fmt.Errorf("storage: insert mutation %q: %w", p.MutationID, ErrMutationAlreadyRecorded)
		}
		return MutationLedgerEntry{}, fmt.Errorf("storage: insert mutation %q: %w", p.MutationID, err)
	}

	row, err := q.GetMutationByMutationID(ctx, gen.GetMutationByMutationIDParams{
		GroupID: group, MutationID: p.MutationID,
	})
	if err != nil {
		return MutationLedgerEntry{}, fmt.Errorf("storage: read back inserted mutation %q: %w", p.MutationID, err)
	}
	return row, nil
}

func (s *Storage) ForGroupPush(g GroupID) (PushRepository, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage is not open", ErrNoGroup)
	}
	if g.IsZero() {
		return nil, fmt.Errorf("%w: group id is the zero value", ErrNoGroup)
	}
	return pushRepository{binding{q: gen.New(s.store.Reader()), gid: g, store: s.store}}, nil
}
