package main

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"time"
)

const driverName = "sqlite"

var walPragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(ON)",
}

const txLock = "immediate"

type poolConfig struct {
	path     string
	maxOpen  int
	roleName string
}

func openPool(cfg poolConfig) (*sql.DB, string, error) {
	q := url.Values{}
	for _, p := range walPragmas {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", txLock)
	dsn := "file:" + cfg.path + "?" + q.Encode()

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, dsn, fmt.Errorf("open %s pool: %w", cfg.roleName, err)
	}
	db.SetMaxOpenConns(cfg.maxOpen)
	db.SetMaxIdleConns(cfg.maxOpen)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, dsn, fmt.Errorf("ping %s pool: %w", cfg.roleName, err)
	}
	return db, dsn, nil
}

var pragmaReadback = []string{
	"journal_mode",
	"synchronous",
	"busy_timeout",
	"foreign_keys",
	"wal_autocheckpoint",
	"journal_size_limit",
	"page_size",
	"cache_size",
	"temp_store",
	"mmap_size",
	"locking_mode",
	"auto_vacuum",
	"threads",
}

func readPragmas(ctx context.Context, db *sql.DB) (map[string]string, error) {
	out := make(map[string]string, len(pragmaReadback)+1)
	for _, name := range pragmaReadback {
		var v sql.NullString
		if err := db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&v); err != nil {
			return nil, fmt.Errorf("read back PRAGMA %s: %w", name, err)
		}
		if v.Valid {
			out[name] = v.String
		} else {
			out[name] = ""
		}
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return nil, fmt.Errorf("read sqlite_version(): %w", err)
	}
	out["sqlite_version"] = version
	return out, nil
}

func assertFTS5(ctx context.Context, db *sql.DB) error {
	var n int
	err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM pragma_compile_options WHERE compile_options LIKE 'ENABLE_FTS5%'").Scan(&n)
	if err != nil {
		return fmt.Errorf("query compile options: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("driver was built without ENABLE_FTS5; the spike cannot measure the real workload")
	}
	return nil
}
