package main

import (
	"errors"
	"modernc.org/sqlite"
)

const sqliteBusyCode = 5

func isDatabaseLocked(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.Code() == sqliteBusyCode
}
