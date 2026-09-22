-- name: GetStockAdjustment :one
SELECT * FROM stock_adjustments
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListStockAdjustments :many
SELECT * FROM stock_adjustments
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: CreateStockAdjustment :exec
INSERT INTO stock_adjustments (
    id, group_id, item_id, delta, reason, note, resulting_quantity,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(delta), sqlc.arg(reason), sqlc.arg(note), sqlc.arg(resulting_quantity),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: AdjustItemQuantity :execrows
UPDATE items
SET quantity   = sqlc.arg(quantity),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListStockAdjustmentChangesSince :many
SELECT * FROM stock_adjustments
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListStockAdjustmentChangesAtSeq :many
SELECT * FROM stock_adjustments
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

