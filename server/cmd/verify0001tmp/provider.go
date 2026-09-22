package main

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/migrations"
	"github.com/pressly/goose/v3"
)

func gooseProvider(db *sql.DB) (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectSQLite3, db, migrations.FS)
}
