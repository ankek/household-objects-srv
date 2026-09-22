-- name: GetItemLabel :one
SELECT * FROM item_labels
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND label_id = sqlc.arg(label_id)
  AND deleted_at IS NULL;

-- name: ListItemLabels :many
SELECT l.* FROM item_labels il
JOIN labels l
  ON l.id = il.label_id
WHERE il.group_id = sqlc.arg(group_id)
  AND l.group_id = sqlc.arg(group_id)
  AND il.item_id = sqlc.arg(item_id)
  AND il.deleted_at IS NULL
  AND l.deleted_at IS NULL
ORDER BY l.name ASC, l.id ASC;

-- name: ListItemIDsByLabel :many
SELECT il.item_id FROM item_labels il
JOIN items i
  ON i.id = il.item_id
WHERE il.group_id = sqlc.arg(group_id)
  AND i.group_id = sqlc.arg(group_id)
  AND il.label_id = sqlc.arg(label_id)
  AND il.deleted_at IS NULL
  AND i.deleted_at IS NULL
ORDER BY il.item_id ASC;

-- name: CreateItemLabel :exec
INSERT INTO item_labels (
    id, group_id, item_id, label_id,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(label_id),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: ReviveItemLabel :execrows
UPDATE item_labels
SET deleted_at = NULL,
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND label_id = sqlc.arg(label_id)
  AND deleted_at IS NOT NULL;

-- name: DeleteItemLabel :execrows
UPDATE item_labels
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND label_id = sqlc.arg(label_id)
  AND deleted_at IS NULL;

-- name: CascadeDeleteItemLabels :execrows
UPDATE item_labels
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemLabelChangesSince :many
SELECT * FROM item_labels
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemLabelChangesAtSeq :many
SELECT * FROM item_labels
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

