package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/migrations"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

const driverName = "sqlite"

const DefaultReadConns = 4

const busyTimeoutMS = 5000

var connPragmas = []string{
	fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS),
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(ON)",
	"cache_size(-2000)",
}

type Config struct {
	Path string

	ReadConns int
}

type Store struct {
	read    *sql.DB
	write   *sql.DB
	version int64
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, errors.New("db: no database path configured")
	}
	readConns := cfg.ReadConns
	if readConns <= 0 {
		readConns = DefaultReadConns
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o750); err != nil {
		return nil, fmt.Errorf("db: create database directory: %w", err)
	}

	write, err := openPool(ctx, dataSourceName(cfg.Path, true), 1)
	if err != nil {
		return nil, fmt.Errorf("db: open write pool: %w", err)
	}

	version, err := migrations.Apply(ctx, write)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("db: migrate: %w", err), write.Close())
	}

	read, err := openPool(ctx, dataSourceName(cfg.Path, false), readConns)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("db: open read pool: %w", err), write.Close())
	}

	return &Store{read: read, write: write, version: version}, nil
}

func (s *Store) Reader() *sql.DB { return s.read }

func (s *Store) Writer() *sql.DB { return s.write }

func (s *Store) SchemaVersion() int64 { return s.version }

func (s *Store) Close() error {
	return errors.Join(s.read.Close(), s.write.Close())
}

func dataSourceName(path string, immediate bool) string {
	q := url.Values{}
	for _, p := range connPragmas {
		q.Add("_pragma", p)
	}
	if immediate {
		q.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + q.Encode()
}

func openPool(ctx context.Context, dsn string, maxConns int) (*sql.DB, error) {
	pool, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(maxConns)
	pool.SetMaxIdleConns(maxConns)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)

	if err := pool.PingContext(ctx); err != nil {
		return nil, errors.Join(err, pool.Close())
	}
	if err := assertPragmas(ctx, pool); err != nil {
		return nil, errors.Join(err, pool.Close())
	}
	return pool, nil
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func assertPragmas(ctx context.Context, q rowQuerier) error {
	var journalMode string
	if err := q.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("read back journal_mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("journal_mode is %q, want \"wal\" (NFR-025)", journalMode)
	}

	var foreignKeys int
	if err := q.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fmt.Errorf("read back foreign_keys: %w", err)
	}
	if foreignKeys != 1 {
		return errors.New("foreign_keys is off; P-3 tenant isolation would be unenforced")
	}
	return nil
}
