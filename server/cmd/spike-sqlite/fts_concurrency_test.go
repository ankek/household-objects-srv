package main

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFTS5ConsistencyUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "fts-race.db")

	writeDB, _, err := openPool(poolConfig{path: dbPath, maxOpen: 1, roleName: "write"})
	if err != nil {
		t.Fatalf("open write pool: %v", err)
	}
	defer writeDB.Close()

	readDB, _, err := openPool(poolConfig{path: dbPath, maxOpen: 4, roleName: "read"})
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	defer readDB.Close()

	if err := assertFTS5(ctx, writeDB); err != nil {
		t.Fatalf("FTS5 not available in this build: %v", err)
	}

	if _, err := writeDB.ExecContext(ctx, schemaDDL); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := writeDB.ExecContext(ctx, ftsTriggerDDL); err != nil {
		t.Fatalf("create FTS5 triggers: %v", err)
	}

	gen := newUUIDGen(42)
	ix := &itemIndex{}
	var shortCodes atomic.Int64

	groupID := gen.nextAt(seedBaseMillis)
	seedStat, err := seed(ctx, writeDB, gen, ix, seedConfig{
		groupID:    groupID,
		items:      200,
		locations:  10,
		labels:     8,
		batchSize:  50,
		rngSeed:    42,
		shortCodes: &shortCodes,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if seedStat.Items != 200 {
		t.Fatalf("expected 200 seeded items, got %d", seedStat.Items)
	}

	const canaryToken = "raceflagcanary"
	canaryID := gen.next()
	canaryName := fmt.Sprintf("Zaraffe %s special", canaryToken)
	if _, err := writeDB.ExecContext(ctx,
		`INSERT INTO items (id, group_id, name, description, location_id, quantity, short_code, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, '', NULL, 1, 'CANARY01', ?, 1, NULL, 999999)`,
		canaryID, groupID, canaryName, time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert canary item: %v", err)
	}
	ix.add(canaryID)

	const writers = 6
	const opsPerWriter = 120
	cfg := workloadConfig{groupID: groupID, pageSize: 25}

	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for w := range writers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(1000 + id)))
			var stats workloadStats
			for i := range opsPerWriter {
				if err := runWriteOp(ctx, writeDB, gen, ix, r, cfg, &stats); err != nil {
					errCh <- fmt.Errorf("writer %d op %d: %w", id, i, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent write failed (this is a real finding, not flakiness): %v", err)
	}

	fts, err := checkFTSConsistency(ctx, readDB)
	if err != nil {
		t.Fatalf("fts consistency check: %v", err)
	}
	if fts.Delta != 0 {
		t.Errorf("FTS5 index drift under concurrent writes: live_items=%d fts_rows=%d delta=%d",
			fts.LiveItems, fts.FTSRows, fts.Delta)
	}

	var canaryLive bool
	var canaryNameNow string
	if err := writeDB.QueryRowContext(ctx,
		`SELECT deleted_at IS NULL, name FROM items WHERE id = ?`, canaryID,
	).Scan(&canaryLive, &canaryNameNow); err != nil {
		t.Fatalf("read canary row: %v", err)
	}
	if canaryLive && canaryNameNow == canaryName {
		var n int
		if err := readDB.QueryRowContext(ctx,
			`SELECT count(*) FROM items_fts WHERE item_id = ? AND items_fts MATCH ?`,
			canaryID, canaryToken).Scan(&n); err != nil {
			t.Fatalf("canary FTS5 lookup: %v", err)
		}
		if n == 0 {
			t.Errorf("canary item %s is live with its original name but NOT findable by FTS5 search on %q — index drift under concurrency", canaryID, canaryToken)
		}
	} else {
		t.Logf("canary item was mutated by a concurrent writer (live=%v, name changed=%v) — skipping the deterministic findability assertion, this is expected under the randomised write mix",
			canaryLive, canaryNameNow != canaryName)
	}

	if fts.LiveItems > 0 && fts.SampleMatchRows == 0 {
		t.Errorf("no live item is findable via FTS5 search on common vocabulary (live_items=%d) — index present but not searchable", fts.LiveItems)
	}

	t.Logf("fts consistency after %d concurrent writers x %d ops (seed=%d items): live_items=%d fts_rows=%d delta=%d sample_match_rows=%d",
		writers, opsPerWriter, seedStat.Items, fts.LiveItems, fts.FTSRows, fts.Delta, fts.SampleMatchRows)
}
