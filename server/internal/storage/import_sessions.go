package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"time"
)

type ImportSession = gen.ImportSession

const ImportSessionTTL = time.Hour

type ImportSessionRepository interface {
	Create(ctx context.Context, p CreateImportSessionParams) (ImportSession, error)

	Get(ctx context.Context, id string) (ImportSession, error)

	Delete(ctx context.Context, id string) error
}

type CreateImportSessionParams struct {
	ID     string
	Source string
	Now    int64
}

func (p CreateImportSessionParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateImportSessionParams: ID is empty")
	case p.Source == "":
		return errors.New("storage: CreateImportSessionParams: Source is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateImportSessionParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type importSessionRepository struct {
	binding
}

func (r importSessionRepository) Create(ctx context.Context, p CreateImportSessionParams) (ImportSession, error) {
	if err := p.validate(); err != nil {
		return ImportSession{}, err
	}

	expiresAt := p.Now + ImportSessionTTL.Milliseconds()

	var created ImportSession
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		if err := q.CreateImportSession(ctx, gen.CreateImportSessionParams{
			ID:        p.ID,
			GroupID:   r.group(),
			Source:    p.Source,
			CreatedAt: p.Now,
			ExpiresAt: expiresAt,
		}); err != nil {
			return fmt.Errorf("storage: create import session %q: %w", p.ID, err)
		}

		row, err := q.GetImportSession(ctx, gen.GetImportSessionParams{GroupID: r.group(), ID: p.ID})
		if err != nil {
			return fmt.Errorf("storage: read back created import session %q: %w", p.ID, err)
		}
		created = row
		return nil
	})
	if err != nil {
		return ImportSession{}, err
	}
	return created, nil
}

func (r importSessionRepository) Get(ctx context.Context, id string) (ImportSession, error) {
	row, err := r.queries().GetImportSession(ctx, gen.GetImportSessionParams{GroupID: r.group(), ID: id})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ImportSession{}, fmt.Errorf("storage: import session %q: %w", id, ErrNotFound)
	case err != nil:
		return ImportSession{}, fmt.Errorf("storage: get import session %q: %w", id, err)
	}
	return row, nil
}

func (r importSessionRepository) Delete(ctx context.Context, id string) error {
	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		if err := q.DeleteImportSession(ctx, gen.DeleteImportSessionParams{GroupID: r.group(), ID: id}); err != nil {
			return fmt.Errorf("storage: delete import session %q: %w", id, err)
		}
		return nil
	})
}
