-- name: GetItemSale :one
SELECT * FROM item_sale
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: CreateItemSale :exec
INSERT INTO item_sale (
    id, group_id, item_id, buyer_name, sold_on, sale_price_minor, notes,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(buyer_name), sqlc.arg(sold_on),
    sqlc.arg(sale_price_minor), sqlc.arg(notes), sqlc.arg(now), sqlc.arg(now), 1, NULL,
    sqlc.arg(change_seq)
);

-- name: UpdateItemSale :execrows
UPDATE item_sale
SET buyer_name       = sqlc.arg(buyer_name),
    sold_on          = sqlc.arg(sold_on),
    sale_price_minor = sqlc.arg(sale_price_minor),
    notes            = sqlc.arg(notes),
    updated_at       = sqlc.arg(now),
    version          = version + 1,
    change_seq       = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteItemSale :execrows
UPDATE item_sale
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemSaleChangesSince :many
SELECT * FROM item_sale
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemSaleChangesAtSeq :many
SELECT * FROM item_sale
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

