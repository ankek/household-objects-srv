package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"
)

func runReadOp(
	ctx context.Context,
	stmts *readStmts,
	ix *itemIndex,
	r *rand.Rand,
	cfg workloadConfig,
	s *samples,
	stats *workloadStats,
) error {
	switch n := r.Intn(100); {
	case n < 50:
		return timed(s, kindReadDetail, func() error { return readDetail(ctx, stmts, ix, r, cfg, stats) })
	case n < 80:
		return timed(s, kindReadList, func() error { return readList(ctx, stmts, r, cfg) })
	default:
		return timed(s, kindReadSearch, func() error { return readSearch(ctx, stmts, r, cfg, stats) })
	}
}

func timed(s *samples, kind string, fn func() error) error {
	started := time.Now()
	err := fn()
	if err != nil {
		return err
	}
	s.add(kind, time.Since(started))
	return nil
}

func readDetail(
	ctx context.Context,
	stmts *readStmts,
	ix *itemIndex,
	r *rand.Rand,
	cfg workloadConfig,
	stats *workloadStats,
) error {
	itemID, ok := ix.random(r)
	if !ok {
		return errors.New("item index is empty")
	}

	var (
		id, name, description, shortCode        string
		quantity, updatedAt, version, changeSeq int64
		locationName                            sql.NullString
	)
	err := stmts.detail.QueryRowContext(ctx, cfg.groupID, itemID).
		Scan(&id, &name, &description, &quantity, &shortCode, &updatedAt, &version, &changeSeq, &locationName)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		atomic.AddInt64(&stats.DetailMisses, 1)
		return nil
	case err != nil:
		return fmt.Errorf("detail row: %w", err)
	}

	idents, err := stmts.detailIdents.QueryContext(ctx, cfg.groupID, itemID)
	if err != nil {
		return fmt.Errorf("detail identifications: %w", err)
	}
	if err := drainPairs(idents); err != nil {
		return fmt.Errorf("detail identifications: %w", err)
	}

	labels, err := stmts.detailLabels.QueryContext(ctx, cfg.groupID, itemID)
	if err != nil {
		return fmt.Errorf("detail labels: %w", err)
	}
	if err := drainSingle(labels); err != nil {
		return fmt.Errorf("detail labels: %w", err)
	}
	return nil
}

func readList(ctx context.Context, stmts *readStmts, r *rand.Rand, cfg workloadConfig) error {
	offset := r.Intn(40) * cfg.pageSize

	var total int64
	if err := stmts.listCount.QueryRowContext(ctx, cfg.groupID).Scan(&total); err != nil {
		return fmt.Errorf("list count: %w", err)
	}

	rows, err := stmts.list.QueryContext(ctx, cfg.groupID, cfg.pageSize, offset)
	if err != nil {
		return fmt.Errorf("list page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, name     string
			quantity     int64
			updatedAt    int64
			locationName sql.NullString
		)
		if err := rows.Scan(&id, &name, &quantity, &updatedAt, &locationName); err != nil {
			return fmt.Errorf("list scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list rows: %w", err)
	}
	return nil
}

func readSearch(ctx context.Context, stmts *readStmts, r *rand.Rand, cfg workloadConfig, stats *workloadStats) error {
	rows, err := stmts.search.QueryContext(ctx, searchQuery(r), cfg.groupID, 25)
	if err != nil {
		return fmt.Errorf("fts5 search: %w", err)
	}
	defer rows.Close()
	var n int64
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return fmt.Errorf("fts5 search scan: %w", err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fts5 search rows: %w", err)
	}
	atomic.AddInt64(&stats.SearchHitRows, n)
	return nil
}

func drainPairs(rows *sql.Rows) error {
	defer rows.Close()
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return err
		}
	}
	return rows.Err()
}

func drainSingle(rows *sql.Rows) error {
	defer rows.Close()
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return err
		}
	}
	return rows.Err()
}

func runWriteOp(
	ctx context.Context,
	db *sql.DB,
	gen *uuidGen,
	ix *itemIndex,
	r *rand.Rand,
	cfg workloadConfig,
	stats *workloadStats,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin write tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed on the success path

	now := time.Now().UnixMilli()
	seq, err := nextChangeSeq(ctx, tx, cfg.groupID)
	if err != nil {
		return err
	}

	switch n := r.Intn(100); {
	case n < 40:
		err = opUpdateItem(ctx, tx, ix, r, cfg, now, seq)
	case n < 65:
		err = opAdjustStock(ctx, tx, gen, ix, r, cfg, now, seq)
	case n < 85:
		err = opInsertItem(ctx, tx, gen, ix, r, cfg, now, seq, stats)
	case n < 95:
		err = opUpsertPurchase(ctx, tx, gen, ix, r, cfg, now, seq)
	default:
		err = opSoftDeleteItem(ctx, tx, ix, r, cfg, now, seq, stats)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit write tx: %w", err)
	}
	return nil
}

func opUpdateItem(ctx context.Context, tx *sql.Tx, ix *itemIndex, r *rand.Rand, cfg workloadConfig, now, seq int64) error {
	itemID, ok := ix.random(r)
	if !ok {
		return errors.New("item index is empty")
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE items
		    SET name = ?, description = ?, updated_at = ?, version = version + 1, change_seq = ?
		  WHERE group_id = ? AND id = ? AND deleted_at IS NULL`,
		itemName(r), itemDescription(r), now, seq, cfg.groupID, itemID)
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	return nil
}

func opAdjustStock(ctx context.Context, tx *sql.Tx, gen *uuidGen, ix *itemIndex, r *rand.Rand, cfg workloadConfig, now, seq int64) error {
	itemID, ok := ix.random(r)
	if !ok {
		return errors.New("item index is empty")
	}
	var quantity int64
	err := tx.QueryRowContext(ctx,
		`SELECT quantity FROM items WHERE group_id = ? AND id = ? AND deleted_at IS NULL`,
		cfg.groupID, itemID).Scan(&quantity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read quantity: %w", err)
	}

	delta := int64(r.Intn(7) - 3)
	if delta == 0 {
		delta = 1
	}
	resulting := quantity + delta
	if resulting < 0 {
		resulting = 0
		delta = -quantity
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO stock_adjustments (id, group_id, item_id, delta, reason, note, resulting_quantity, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, NULL, ?)`,
		gen.next(), cfg.groupID, itemID, delta, "spike", "", resulting, now, seq); err != nil {
		return fmt.Errorf("insert stock adjustment: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE items SET quantity = ?, updated_at = ?, version = version + 1, change_seq = ?
		  WHERE group_id = ? AND id = ? AND deleted_at IS NULL`,
		resulting, now, seq, cfg.groupID, itemID); err != nil {
		return fmt.Errorf("apply stock adjustment: %w", err)
	}
	return nil
}

func opInsertItem(ctx context.Context, tx *sql.Tx, gen *uuidGen, ix *itemIndex, r *rand.Rand, cfg workloadConfig, now, seq int64, stats *workloadStats) error {
	itemID := gen.next()
	code := shortCode(int(1_000_000 + r.Int63n(1_000_000_000)))
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO items (id, group_id, name, description, location_id, quantity, short_code, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, (SELECT id FROM locations WHERE group_id = ? AND deleted_at IS NULL LIMIT 1), ?, ?, ?, 1, NULL, ?)`,
		itemID, cfg.groupID, itemName(r), itemDescription(r), cfg.groupID, 1+r.Intn(5), code, now, seq); err != nil {
		return fmt.Errorf("insert item: %w", err)
	}
	kind := pick(r, identificationKinds)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO item_identifications (id, group_id, item_id, kind, value, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, ?, 1, NULL, ?)`,
		gen.next(), cfg.groupID, itemID, kind, identificationValue(r, kind), now, seq); err != nil {
		return fmt.Errorf("insert identification: %w", err)
	}
	ix.add(itemID)
	atomic.AddInt64(&stats.ItemsInserted, 1)
	return nil
}

func opUpsertPurchase(ctx context.Context, tx *sql.Tx, gen *uuidGen, ix *itemIndex, r *rand.Rand, cfg workloadConfig, now, seq int64) error {
	itemID, ok := ix.random(r)
	if !ok {
		return errors.New("item index is empty")
	}
	var exists int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM items WHERE group_id = ? AND id = ? AND deleted_at IS NULL`,
		cfg.groupID, itemID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check item before purchase upsert: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO item_purchase (id, group_id, item_id, vendor, purchased_on, purchase_price_minor, order_reference, notes, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, NULL, ?)
		 ON CONFLICT(item_id) DO UPDATE SET
		     notes = excluded.notes,
		     vendor = excluded.vendor,
		     updated_at = excluded.updated_at,
		     version = item_purchase.version + 1,
		     change_seq = excluded.change_seq`,
		gen.next(), cfg.groupID, itemID, pick(r, brands),
		fmt.Sprintf("2026-%02d-%02d", 1+r.Intn(12), 1+r.Intn(28)),
		int64(500+r.Intn(200000)), fmt.Sprintf("ORD-%07d", r.Intn(1e7)),
		purchaseNote(r), now, seq); err != nil {
		return fmt.Errorf("upsert purchase: %w", err)
	}
	return nil
}

func opSoftDeleteItem(ctx context.Context, tx *sql.Tx, ix *itemIndex, r *rand.Rand, cfg workloadConfig, now, seq int64, stats *workloadStats) error {
	itemID, ok := ix.random(r)
	if !ok {
		return errors.New("item index is empty")
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE items SET deleted_at = ?, updated_at = ?, version = version + 1, change_seq = ?
		  WHERE group_id = ? AND id = ? AND deleted_at IS NULL`,
		now, now, seq, cfg.groupID, itemID)
	if err != nil {
		return fmt.Errorf("soft delete item: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete rows affected: %w", err)
	}
	atomic.AddInt64(&stats.ItemsSoftDeleted, n)
	return nil
}
