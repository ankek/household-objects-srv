-- name: GetItemWarranty :one
SELECT * FROM item_warranty
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: CreateItemWarranty :exec
INSERT INTO item_warranty (
    id, group_id, item_id, holder, provider, starts_on, expires_on, is_lifetime, notes,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(holder), sqlc.arg(provider),
    sqlc.arg(starts_on), sqlc.arg(expires_on), sqlc.arg(is_lifetime), sqlc.arg(notes),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateItemWarranty :execrows
UPDATE item_warranty
SET holder      = sqlc.arg(holder),
    provider    = sqlc.arg(provider),
    starts_on   = sqlc.arg(starts_on),
    expires_on  = sqlc.arg(expires_on),
    is_lifetime = sqlc.arg(is_lifetime),
    notes       = sqlc.arg(notes),
    updated_at  = sqlc.arg(now),
    version     = version + 1,
    change_seq  = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteItemWarranty :execrows
UPDATE item_warranty
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemWarrantyChangesSince :many
SELECT * FROM item_warranty
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemWarrantyChangesAtSeq :many
SELECT * FROM item_warranty
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

