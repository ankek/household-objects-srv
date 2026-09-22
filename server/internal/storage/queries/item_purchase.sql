-- name: GetItemPurchase :one
SELECT * FROM item_purchase
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: CreateItemPurchase :exec
INSERT INTO item_purchase (
    id, group_id, item_id, vendor, purchased_on, purchase_price_minor, order_reference, notes,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(vendor), sqlc.arg(purchased_on),
    sqlc.arg(purchase_price_minor), sqlc.arg(order_reference), sqlc.arg(notes), sqlc.arg(now),
    sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateItemPurchase :execrows
UPDATE item_purchase
SET vendor                = sqlc.arg(vendor),
    purchased_on          = sqlc.arg(purchased_on),
    purchase_price_minor  = sqlc.arg(purchase_price_minor),
    order_reference       = sqlc.arg(order_reference),
    notes                 = sqlc.arg(notes),
    updated_at            = sqlc.arg(now),
    version               = version + 1,
    change_seq            = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteItemPurchase :execrows
UPDATE item_purchase
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemPurchaseChangesSince :many
SELECT * FROM item_purchase
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemPurchaseChangesAtSeq :many
SELECT * FROM item_purchase
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

