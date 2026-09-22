-- name: GetItem :one
SELECT * FROM items
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: GetItemByShortCode :one
SELECT * FROM items
WHERE group_id = sqlc.arg(group_id)
  AND short_code = sqlc.arg(short_code)
  AND deleted_at IS NULL;

-- name: GetItemsByIDs :many
SELECT * FROM items
WHERE group_id = sqlc.arg(group_id)
  AND id IN (sqlc.slice(item_ids))
  AND deleted_at IS NULL;

-- name: ListItems :many
SELECT * FROM items
WHERE group_id = sqlc.arg(group_id)
  AND deleted_at IS NULL
ORDER BY updated_at DESC, id ASC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CreateItem :exec
INSERT INTO items (
    id, group_id, name, description, location_id, quantity, short_code,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(location_id), sqlc.arg(quantity), sqlc.arg(short_code),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateItem :execrows
UPDATE items
SET name        = sqlc.arg(name),
    description = sqlc.arg(description),
    location_id = sqlc.arg(location_id),
    quantity    = sqlc.arg(quantity),
    updated_at  = sqlc.arg(now),
    version     = version + 1,
    change_seq  = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(item_id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteItem :execrows
UPDATE items
SET deleted_at = sqlc.arg(deleted_at),
    updated_at  = sqlc.arg(now),
    version     = version + 1,
    change_seq  = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemChangesSince :many
SELECT * FROM items
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemChangesAtSeq :many
SELECT * FROM items
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

