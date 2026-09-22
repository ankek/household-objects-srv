package main

import (
	"database/sql"
	"errors"
	"fmt"
	"modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestIsDatabaseLockedClassifiesARealSQLiteBusyError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock-test.db")
	dsn := func(immediate bool) string {
		s := "file:" + path + "?_pragma=busy_timeout(50)"
		if immediate {
			s += "&_txlock=immediate"
		}
		return s
	}

	writer, err := sql.Open("sqlite", dsn(true))
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	holder, err := sql.Open("sqlite", dsn(true))
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer func() { _ = holder.Close() }()

	tx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder.Begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	contender, err := sql.Open("sqlite", dsn(true))
	if err != nil {
		t.Fatalf("open contender: %v", err)
	}
	defer func() { _ = contender.Close() }()

	_, err = contender.Exec(`INSERT INTO t DEFAULT VALUES`)
	if err == nil {
		t.Fatal("contending write succeeded while the holder transaction was open; the fixture does not exhibit lock contention, so this test proves nothing")
	}

	if !isDatabaseLocked(err) {
		t.Fatalf("isDatabaseLocked(%v) = false, want true for a real SQLITE_BUSY condition", err)
	}

	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		t.Fatalf("the fixture's error %v does not even unwrap to *sqlite.Error; test setup is wrong, not the code under test", err)
	}
	if sqliteErr.Code() != sqliteBusyCode {
		t.Fatalf("fixture error code = %d, want SQLITE_BUSY (%d) -- this test's own contention setup did not produce the condition it claims to", sqliteErr.Code(), sqliteBusyCode)
	}
}

func TestIsDatabaseLockedIsFalseForOrdinaryErrors(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("some other failure"),
		fmt.Errorf("wrapped: %w", errors.New("still not sqlite")),
		sql.ErrNoRows,
	} {
		if isDatabaseLocked(err) {
			t.Errorf("isDatabaseLocked(%v) = true, want false", err)
		}
	}
}
