package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/migrations"
	"os"
	"path/filepath"
	"time"
)

const (
	nItems = 3000
	nIdent = 2
)

func measure(ctx context.Context, dir string) {
	fmt.Printf("  %-46s %10s %12s\n", "scenario", "elapsed", "items/s")
	for _, sc := range []struct {
		name           string
		dropTriggers   bool
		satellitesLast bool
		deferFK        bool
	}{
		{"parent-first, FTS triggers ON (naive import)", false, true, false},
		{"satellites-first, FTS triggers ON (fast path)", false, false, true},
		{"FTS triggers DROPPED (cost attribution only)", true, true, false},
	} {
		d := runScenario(ctx, filepath.Join(dir, fmt.Sprintf("m-%t-%t.db", sc.dropTriggers, sc.satellitesLast)), sc.dropTriggers, sc.satellitesLast, sc.deferFK)
		fmt.Printf("  %-46s %10s %12.0f\n", sc.name, d.Round(time.Millisecond), float64(nItems)/d.Seconds())
	}
}

func runScenario(ctx context.Context, path string, dropTriggers, satellitesLast, deferFK bool) time.Duration {
	db := open(path)
	defer func() { db.Close(); os.Remove(path) }()
	if _, err := migrations.Apply(ctx, db); err != nil {
		panic(err)
	}
	if dropTriggers {
		rows, err := db.Query(`SELECT name FROM sqlite_schema WHERE type='trigger'`)
		must(err)
		var names []string
		for rows.Next() {
			var n string
			must(rows.Scan(&n))
			names = append(names, n)
		}
		rows.Close()
		for _, n := range names {
			must(exec(db, "DROP TRIGGER "+n))
		}
	}
	must(exec(db, `INSERT INTO groups(id,name,created_at,updated_at) VALUES('G','G',1,1)`))

	tx, err := db.Begin()
	must(err)
	if deferFK {
		_, err = tx.Exec(`PRAGMA defer_foreign_keys = ON`)
		must(err)
	}
	insItem, err := tx.PrepareContext(ctx, `INSERT INTO items(id,group_id,name,description,short_code,created_at,updated_at,change_seq) VALUES(?,'G',?,?,?,1,1,?)`)
	must(err)
	insID, err := tx.PrepareContext(ctx, `INSERT INTO item_identifications(id,group_id,item_id,kind,value,created_at,updated_at,change_seq) VALUES(?,'G',?,'serial',?,1,1,?)`)
	must(err)

	start := time.Now()
	for i := 0; i < nItems; i++ {
		itemID := fmt.Sprintf("item-%06d", i)
		writeItem := func() {
			_, err := insItem.ExecContext(ctx, itemID,
				fmt.Sprintf("Cordless hammer drill model %d", i),
				fmt.Sprintf("A reasonably wordy description for item number %d in the corpus", i),
				fmt.Sprintf("SC%06d", i), i)
			must(err)
		}
		writeSats := func() {
			for j := 0; j < nIdent; j++ {
				_, err := insID.ExecContext(ctx, fmt.Sprintf("id-%06d-%d", i, j), itemID,
					fmt.Sprintf("SN-%06d-%d", i, j), i)
				must(err)
			}
		}
		if satellitesLast {
			writeItem()
			writeSats()
		} else {
			writeSats()
			writeItem()
		}
	}
	must(tx.Commit())
	elapsed := time.Since(start)

	var n int
	must(db.QueryRow(`SELECT count(*) FROM items_fts`).Scan(&n))
	if !dropTriggers && n != nItems {
		panic(fmt.Sprintf("FTS row count %d != %d", n, nItems))
	}
	var hit int
	if !dropTriggers {
		must(db.QueryRow(`SELECT count(*) FROM items_fts WHERE items_fts MATCH '"SN-002999-1"'`).Scan(&hit))
		if hit != 1 {
			panic(fmt.Sprintf("identifier not indexed: %d", hit))
		}
	}
	return elapsed
}

var _ = sql.ErrNoRows
