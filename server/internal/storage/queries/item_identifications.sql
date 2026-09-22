-- name: GetItemIdentification :one
SELECT * FROM item_identifications
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListItemIdentifications :many
SELECT * FROM item_identifications
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: CreateItemIdentification :exec
INSERT INTO item_identifications (
    id, group_id, item_id, kind, value,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(kind), sqlc.arg(value),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateItemIdentification :execrows
UPDATE item_identifications
SET kind       = sqlc.arg(kind),
    value      = sqlc.arg(value),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteItemIdentification :execrows
UPDATE item_identifications
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: CascadeDeleteItemIdentifications :execrows
UPDATE item_identifications
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemIdentificationChangesSince :many
SELECT * FROM item_identifications
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemIdentificationChangesAtSeq :many
SELECT * FROM item_identifications
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

