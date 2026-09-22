package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var FS embed.FS

const dialect = goose.DialectSQLite3

func Apply(ctx context.Context, db *sql.DB) (int64, error) {
	provider, err := goose.NewProvider(dialect, db, FS)
	if err != nil {
		return 0, fmt.Errorf("migrations: build provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return 0, fmt.Errorf("migrations: apply: %w", err)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrations: read version: %w", err)
	}
	return version, nil
}

func Version(ctx context.Context, db *sql.DB) (int64, error) {
	provider, err := goose.NewProvider(dialect, db, FS)
	if err != nil {
		return 0, fmt.Errorf("migrations: build provider: %w", err)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrations: read version: %w", err)
	}
	return version, nil
}
