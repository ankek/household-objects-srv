package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	kindReadDetail  = "read_detail"
	kindReadList    = "read_list"
	kindReadSearch  = "read_search"
	kindReadOverall = "read_overall"
	kindWrite       = "write_tx"
)

var readKinds = []string{kindReadDetail, kindReadList, kindReadSearch}

const seedBaseMillis int64 = 1767225600000

type itemIndex struct {
	mu  sync.RWMutex
	ids []string
}

func (ix *itemIndex) add(id string) {
	ix.mu.Lock()
	ix.ids = append(ix.ids, id)
	ix.mu.Unlock()
}

func (ix *itemIndex) random(r *rand.Rand) (string, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if len(ix.ids) == 0 {
		return "", false
	}
	return ix.ids[r.Intn(len(ix.ids))], true
}

func (ix *itemIndex) size() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.ids)
}

type seedStats struct {
	Groups          int     `json:"groups"`
	Locations       int     `json:"locations"`
	Labels          int     `json:"labels"`
	Items           int     `json:"items"`
	ItemLabels      int     `json:"item_labels"`
	Identifications int     `json:"identifications"`
	Purchases       int     `json:"purchases"`
	DurationSec     float64 `json:"duration_sec"`
	ItemsPerSec     float64 `json:"items_per_sec"`
}

type seedConfig struct {
	groupID    string
	items      int
	locations  int
	labels     int
	batchSize  int
	rngSeed    int64
	shortCodes *atomic.Int64
}

func nextChangeSeqBlock(ctx context.Context, tx *sql.Tx, groupID string, n int64) (int64, error) {
	var end int64
	err := tx.QueryRowContext(ctx,
		`UPDATE groups SET change_seq_counter = change_seq_counter + ? WHERE id = ? RETURNING change_seq_counter`,
		n, groupID).Scan(&end)
	if err != nil {
		return 0, fmt.Errorf("allocate change_seq block of %d: %w", n, err)
	}
	return end - n + 1, nil
}

func nextChangeSeq(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	return nextChangeSeqBlock(ctx, tx, groupID, 1)
}

func seed(ctx context.Context, db *sql.DB, gen *uuidGen, ix *itemIndex, cfg seedConfig) (*seedStats, error) {
	start := time.Now()
	r := rand.New(rand.NewSource(cfg.rngSeed))
	stats := &seedStats{Groups: 1}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO groups (id, name, currency_code, registration_enabled, change_seq_counter, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, 0, 0, ?, 1, NULL, 0)`,
		cfg.groupID, "Spike Household", "EUR", seedBaseMillis); err != nil {
		return nil, fmt.Errorf("insert group: %w", err)
	}

	locationIDs, err := seedLocations(ctx, db, gen, r, cfg)
	if err != nil {
		return nil, err
	}
	stats.Locations = len(locationIDs)

	labelIDs, err := seedLabels(ctx, db, gen, r, cfg)
	if err != nil {
		return nil, err
	}
	stats.Labels = len(labelIDs)

	if err := seedItems(ctx, db, gen, r, ix, cfg, locationIDs, labelIDs, stats); err != nil {
		return nil, err
	}

	stats.DurationSec = time.Since(start).Seconds()
	if stats.DurationSec > 0 {
		stats.ItemsPerSec = float64(stats.Items) / stats.DurationSec
	}
	return stats, nil
}

func seedLocations(ctx context.Context, db *sql.DB, gen *uuidGen, r *rand.Rand, cfg seedConfig) ([]string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin locations tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed below; rollback is the failure path

	seqStart, err := nextChangeSeqBlock(ctx, tx, cfg.groupID, int64(cfg.locations))
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, cfg.locations)
	roots := make([]string, 0, len(roomNames))
	for i := 0; i < cfg.locations; i++ {
		id := gen.nextAt(seedBaseMillis + int64(i))
		var name string
		var parent any
		if i < len(roomNames) {
			name = roomNames[i]
			parent = nil
			roots = append(roots, id)
		} else {
			name = fmt.Sprintf("%s %d", pick(r, containerNames), i)
			if len(roots) > 0 {
				parent = roots[r.Intn(len(roots))]
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO locations (id, group_id, name, parent_id, updated_at, version, deleted_at, change_seq)
			 VALUES (?, ?, ?, ?, ?, 1, NULL, ?)`,
			id, cfg.groupID, name, parent, seedBaseMillis+int64(i), seqStart+int64(i)); err != nil {
			return nil, fmt.Errorf("insert location %d: %w", i, err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit locations: %w", err)
	}
	return ids, nil
}

func seedLabels(ctx context.Context, db *sql.DB, gen *uuidGen, r *rand.Rand, cfg seedConfig) ([]string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin labels tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed below; rollback is the failure path

	seqStart, err := nextChangeSeqBlock(ctx, tx, cfg.groupID, int64(cfg.labels))
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, cfg.labels)
	for i := 0; i < cfg.labels; i++ {
		id := gen.nextAt(seedBaseMillis + int64(1000+i))
		name := labelNames[i%len(labelNames)]
		if i >= len(labelNames) {
			name = fmt.Sprintf("%s %d", name, i)
		}
		color := fmt.Sprintf("#%06x", r.Intn(1<<24))
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO labels (id, group_id, name, color, updated_at, version, deleted_at, change_seq)
			 VALUES (?, ?, ?, ?, ?, 1, NULL, ?)`,
			id, cfg.groupID, name, color, seedBaseMillis+int64(1000+i), seqStart+int64(i)); err != nil {
			return nil, fmt.Errorf("insert label %d: %w", i, err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit labels: %w", err)
	}
	return ids, nil
}

func seedItems(
	ctx context.Context,
	db *sql.DB,
	gen *uuidGen,
	r *rand.Rand,
	ix *itemIndex,
	cfg seedConfig,
	locationIDs, labelIDs []string,
	stats *seedStats,
) error {
	for start := 0; start < cfg.items; start += cfg.batchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := cfg.batchSize
		if start+n > cfg.items {
			n = cfg.items - start
		}
		if err := seedItemBatch(ctx, db, gen, r, ix, cfg, locationIDs, labelIDs, stats, start, n); err != nil {
			return err
		}
	}
	return nil
}

func seedItemBatch(
	ctx context.Context,
	db *sql.DB,
	gen *uuidGen,
	r *rand.Rand,
	ix *itemIndex,
	cfg seedConfig,
	locationIDs, labelIDs []string,
	stats *seedStats,
	start, n int,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin item batch tx at %d: %w", start, err)
	}
	defer tx.Rollback() //nolint:errcheck // committed below; rollback is the failure path

	seq, err := nextChangeSeqBlock(ctx, tx, cfg.groupID, int64(n)*6)
	if err != nil {
		return err
	}

	for i := 0; i < n; i++ {
		idx := start + i
		ts := seedBaseMillis + int64(10000+idx)
		itemID := gen.nextAt(ts)

		var locID any
		if len(locationIDs) > 0 {
			locID = locationIDs[r.Intn(len(locationIDs))]
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO items (id, group_id, name, description, location_id, quantity, short_code, updated_at, version, deleted_at, change_seq)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, NULL, ?)`,
			itemID, cfg.groupID, itemName(r), itemDescription(r), locID,
			1+r.Intn(12), shortCode(int(cfg.shortCodes.Add(1))), ts, seq); err != nil {
			return fmt.Errorf("insert item %d: %w", idx, err)
		}
		seq++
		stats.Items++
		ix.add(itemID)

		for j := 0; j < 1+r.Intn(2); j++ {
			kind := pick(r, identificationKinds)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO item_identifications (id, group_id, item_id, kind, value, updated_at, version, deleted_at, change_seq)
				 VALUES (?, ?, ?, ?, ?, ?, 1, NULL, ?)`,
				gen.nextAt(ts), cfg.groupID, itemID, kind, identificationValue(r, kind), ts, seq); err != nil {
				return fmt.Errorf("insert identification for item %d: %w", idx, err)
			}
			seq++
			stats.Identifications++
		}

		if r.Intn(100) < 40 {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO item_purchase (id, group_id, item_id, vendor, purchased_on, purchase_price_minor, order_reference, notes, updated_at, version, deleted_at, change_seq)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, NULL, ?)`,
				gen.nextAt(ts), cfg.groupID, itemID, pick(r, brands),
				fmt.Sprintf("2025-%02d-%02d", 1+r.Intn(12), 1+r.Intn(28)),
				int64(500+r.Intn(200000)), fmt.Sprintf("ORD-%07d", r.Intn(1e7)),
				purchaseNote(r), ts, seq); err != nil {
				return fmt.Errorf("insert purchase for item %d: %w", idx, err)
			}
			seq++
			stats.Purchases++
		}

		if len(labelIDs) > 0 {
			used := make(map[string]bool, 3)
			for j := 0; j < r.Intn(4); j++ {
				labelID := labelIDs[r.Intn(len(labelIDs))]
				if used[labelID] {
					continue
				}
				used[labelID] = true
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO item_labels (id, group_id, item_id, label_id, updated_at, version, deleted_at, change_seq)
					 VALUES (?, ?, ?, ?, ?, 1, NULL, ?)`,
					gen.nextAt(ts), cfg.groupID, itemID, labelID, ts, seq); err != nil {
					return fmt.Errorf("insert item_label for item %d: %w", idx, err)
				}
				seq++
				stats.ItemLabels++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit item batch at %d: %w", start, err)
	}
	return nil
}

type readStmts struct {
	detail       *sql.Stmt
	detailIdents *sql.Stmt
	detailLabels *sql.Stmt
	list         *sql.Stmt
	listCount    *sql.Stmt
	search       *sql.Stmt
}

func prepareReads(ctx context.Context, db *sql.DB) (*readStmts, error) {
	prep := func(name, query string) (*sql.Stmt, error) {
		st, err := db.PrepareContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("prepare %s: %w", name, err)
		}
		return st, nil
	}
	var (
		s   readStmts
		err error
	)
	if s.detail, err = prep("detail", `
		SELECT i.id, i.name, i.description, i.quantity, i.short_code, i.updated_at, i.version, i.change_seq, l.name
		  FROM items i
		  LEFT JOIN locations l ON l.id = i.location_id AND l.group_id = i.group_id
		 WHERE i.group_id = ? AND i.id = ? AND i.deleted_at IS NULL`); err != nil {
		return nil, err
	}
	if s.detailIdents, err = prep("detail_identifications", `
		SELECT kind, value FROM item_identifications
		 WHERE group_id = ? AND item_id = ? AND deleted_at IS NULL`); err != nil {
		return nil, err
	}
	if s.detailLabels, err = prep("detail_labels", `
		SELECT lb.name FROM item_labels il
		  JOIN labels lb ON lb.id = il.label_id AND lb.group_id = il.group_id
		 WHERE il.group_id = ? AND il.item_id = ? AND il.deleted_at IS NULL`); err != nil {
		return nil, err
	}
	if s.list, err = prep("list", `
		SELECT i.id, i.name, i.quantity, i.updated_at, l.name
		  FROM items i
		  LEFT JOIN locations l ON l.id = i.location_id AND l.group_id = i.group_id
		 WHERE i.group_id = ? AND i.deleted_at IS NULL
		 ORDER BY i.updated_at DESC, i.id
		 LIMIT ? OFFSET ?`); err != nil {
		return nil, err
	}
	if s.listCount, err = prep("list_count", `
		SELECT count(*) FROM items WHERE group_id = ? AND deleted_at IS NULL`); err != nil {
		return nil, err
	}
	if s.search, err = prep("search", `
		SELECT i.id, i.name
		  FROM items_fts
		  JOIN items i ON i.id = items_fts.item_id
		 WHERE items_fts MATCH ? AND i.group_id = ? AND i.deleted_at IS NULL
		 ORDER BY rank
		 LIMIT ?`); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *readStmts) close() {
	for _, st := range []*sql.Stmt{s.detail, s.detailIdents, s.detailLabels, s.list, s.listCount, s.search} {
		if st != nil {
			_ = st.Close()
		}
	}
}

type workloadConfig struct {
	groupID   string
	duration  time.Duration
	writers   int
	readers   int
	writeRate float64
	readRate  float64
	pageSize  int
	rngSeed   int64
}

type workloadStats struct {
	Reads            int64 `json:"reads"`
	Writes           int64 `json:"writes"`
	SearchHitRows    int64 `json:"search_hit_rows"`
	DetailMisses     int64 `json:"detail_misses"`
	ItemsInserted    int64 `json:"items_inserted"`
	ItemsSoftDeleted int64 `json:"items_soft_deleted"`
}

func runWorkload(
	ctx context.Context,
	writeDB, readDB *sql.DB,
	gen *uuidGen,
	ix *itemIndex,
	coll *collector,
	cfg workloadConfig,
) (*workloadStats, error) {
	stmts, err := prepareReads(ctx, readDB)
	if err != nil {
		return nil, err
	}
	defer stmts.close()

	runCtx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	var (
		stats    workloadStats
		errOnce  sync.Once
		firstErr error
		wg       sync.WaitGroup
	)
	fail := func(err error) {
		if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	for w := 0; w < cfg.writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s := newSamples()
			defer coll.merge(s)
			r := rand.New(rand.NewSource(cfg.rngSeed + int64(1_000_000+id)))
			throttle := newThrottle(cfg.writeRate)
			defer throttle.stop()
			for {
				if !throttle.wait(runCtx) {
					return
				}
				started := time.Now()
				if err := runWriteOp(runCtx, writeDB, gen, ix, r, cfg, &stats); err != nil {
					fail(fmt.Errorf("writer %d: %w", id, err))
					return
				}
				s.add(kindWrite, time.Since(started))
				atomic.AddInt64(&stats.Writes, 1)
			}
		}(w)
	}

	for rd := 0; rd < cfg.readers; rd++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s := newSamples()
			defer coll.merge(s)
			r := rand.New(rand.NewSource(cfg.rngSeed + int64(2_000_000+id)))
			throttle := newThrottle(cfg.readRate)
			defer throttle.stop()
			for {
				if !throttle.wait(runCtx) {
					return
				}
				if err := runReadOp(runCtx, stmts, ix, r, cfg, s, &stats); err != nil {
					fail(fmt.Errorf("reader %d: %w", id, err))
					return
				}
				atomic.AddInt64(&stats.Reads, 1)
			}
		}(rd)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return &stats, nil
}

type throttle struct {
	ticker *time.Ticker
}

func newThrottle(ratePerSec float64) *throttle {
	if ratePerSec <= 0 {
		return &throttle{}
	}
	interval := time.Duration(float64(time.Second) / ratePerSec)
	if interval <= 0 {
		return &throttle{}
	}
	return &throttle{ticker: time.NewTicker(interval)}
}

func (t *throttle) wait(ctx context.Context) bool {
	if t.ticker == nil {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-t.ticker.C:
		return true
	}
}

func (t *throttle) stop() {
	if t.ticker != nil {
		t.ticker.Stop()
	}
}

func searchQuery(r *rand.Rand) string {
	term := pick(r, searchTerms)
	switch r.Intn(10) {
	case 0, 1, 2:
		if len(term) > 4 {
			term = term[:4]
		}
		return term + "*"
	case 3, 4:
		return term + " " + pick(r, searchTerms)
	default:
		return term
	}
}
