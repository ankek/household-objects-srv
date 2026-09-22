-- name: GetLabel :one
SELECT * FROM labels
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: GetLabelByName :one
SELECT * FROM labels
WHERE group_id = sqlc.arg(group_id)
  AND name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: ListLabels :many
SELECT * FROM labels
WHERE group_id = sqlc.arg(group_id)
  AND deleted_at IS NULL
ORDER BY name ASC, id ASC;

-- name: CreateLabel :exec
INSERT INTO labels (
    id, group_id, name, color,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(name), sqlc.arg(color),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateLabel :execrows
UPDATE labels
SET name       = sqlc.arg(name),
    color      = sqlc.arg(color),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteLabel :execrows
UPDATE labels
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListLabelChangesSince :many
SELECT * FROM labels
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListLabelChangesAtSeq :many
SELECT * FROM labels
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

