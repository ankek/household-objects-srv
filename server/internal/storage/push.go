package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
)

type MutationLedgerEntry = gen.Mutation

type FieldVersion = gen.FieldVersion

type ConflictRecord = gen.Conflict

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

type UpsertFieldVersionParams struct {
	EntityType string
	EntityID   string
	FieldName  string
	Version    int64
	Now        int64
}

func (p UpsertFieldVersionParams) validate() error {
	switch {
	case p.EntityType == "":
		return errors.New("storage: UpsertFieldVersionParams: EntityType is empty")
	case p.EntityID == "":
		return errors.New("storage: UpsertFieldVersionParams: EntityID is empty")
	case p.FieldName == "":
		return errors.New("storage: UpsertFieldVersionParams: FieldName is empty")
	case p.Version <= 0:
		return errors.New("storage: UpsertFieldVersionParams: Version must be positive (migration 0005's bootstrap rule treats an absent row as version 1, never 0)")
	case p.Now <= 0:
		return errors.New("storage: UpsertFieldVersionParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type InsertConflictParams struct {
	ID                        string
	EntityType                string
	EntityID                  string
	FieldName                 string
	ServerValueSnapshot       string
	LosingClientValueSnapshot string
	DetectedAt                int64
	MutationID                string
	Now                       int64
}

func (p InsertConflictParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: InsertConflictParams: ID is empty")
	case p.EntityType == "":
		return errors.New("storage: InsertConflictParams: EntityType is empty")
	case p.EntityID == "":
		return errors.New("storage: InsertConflictParams: EntityID is empty")
	case p.FieldName == "":
		return errors.New("storage: InsertConflictParams: FieldName is empty")
	case p.DetectedAt <= 0:
		return errors.New("storage: InsertConflictParams: DetectedAt must be a positive Unix-millisecond timestamp")
	case p.Now <= 0:
		return errors.New("storage: InsertConflictParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type PushRepository interface {
	Lookup(ctx context.Context, mutationID string) (MutationLedgerEntry, error)

	Insert(ctx context.Context, p InsertMutationLedgerEntryParams) (MutationLedgerEntry, error)

	LookupFieldVersions(ctx context.Context, entityType, entityID string) ([]FieldVersion, error)

	UpsertFieldVersion(ctx context.Context, p UpsertFieldVersionParams) error

	InsertConflict(ctx context.Context, p InsertConflictParams) (ConflictRecord, error)
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

func (r pushRepository) LookupFieldVersions(ctx context.Context, entityType, entityID string) ([]FieldVersion, error) {
	return lookupFieldVersionsTx(ctx, r.queries(), r.group(), entityType, entityID)
}

func (r pushRepository) UpsertFieldVersion(ctx context.Context, p UpsertFieldVersionParams) error {
	if err := p.validate(); err != nil {
		return err
	}
	return r.writeTx(ctx, func(_ *sql.Tx, q *gen.Queries) error {
		return upsertFieldVersionTx(ctx, q, r.group(), p)
	})
}

func (r pushRepository) InsertConflict(ctx context.Context, p InsertConflictParams) (ConflictRecord, error) {
	if err := p.validate(); err != nil {
		return ConflictRecord{}, err
	}

	var created ConflictRecord
	err := r.writeTx(ctx, func(_ *sql.Tx, q *gen.Queries) error {
		row, err := insertConflictTx(ctx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return ConflictRecord{}, err
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

func lookupFieldVersionsTx(ctx context.Context, q *gen.Queries, group, entityType, entityID string) ([]FieldVersion, error) {
	rows, err := q.ListFieldVersionsByEntity(ctx, gen.ListFieldVersionsByEntityParams{
		GroupID: group, EntityType: entityType, EntityID: entityID,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list field versions for %s %q: %w", entityType, entityID, err)
	}
	return rows, nil
}

func upsertFieldVersionTx(ctx context.Context, q *gen.Queries, group string, p UpsertFieldVersionParams) error {
	if err := q.UpsertFieldVersion(ctx, gen.UpsertFieldVersionParams{
		GroupID:    group,
		EntityType: p.EntityType,
		EntityID:   p.EntityID,
		FieldName:  p.FieldName,
		Version:    p.Version,
		Now:        p.Now,
	}); err != nil {
		return fmt.Errorf("storage: upsert field version %s/%s[%s]: %w", p.EntityType, p.EntityID, p.FieldName, err)
	}
	return nil
}

func insertConflictTx(ctx context.Context, q *gen.Queries, group string, p InsertConflictParams) (ConflictRecord, error) {
	serverSnapshot := nullString(p.ServerValueSnapshot)
	losingSnapshot := nullString(p.LosingClientValueSnapshot)
	mutationID := nullString(p.MutationID)

	if err := q.CreateConflict(ctx, gen.CreateConflictParams{
		ID:                        p.ID,
		GroupID:                   group,
		EntityType:                p.EntityType,
		EntityID:                  p.EntityID,
		FieldName:                 p.FieldName,
		ServerValueSnapshot:       serverSnapshot,
		LosingClientValueSnapshot: losingSnapshot,
		DetectedAt:                p.DetectedAt,
		MutationID:                mutationID,
		Now:                       p.Now,
	}); err != nil {
		return ConflictRecord{}, fmt.Errorf("storage: insert conflict %q: %w", p.ID, err)
	}

	return ConflictRecord{
		ID:                        p.ID,
		GroupID:                   group,
		EntityType:                p.EntityType,
		EntityID:                  p.EntityID,
		FieldName:                 p.FieldName,
		ServerValueSnapshot:       serverSnapshot,
		LosingClientValueSnapshot: losingSnapshot,
		DetectedAt:                p.DetectedAt,
		MutationID:                mutationID,
		CreatedAt:                 p.Now,
		UpdatedAt:                 p.Now,
	}, nil
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
