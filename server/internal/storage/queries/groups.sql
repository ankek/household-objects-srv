-- name: GetGroup :one
SELECT * FROM groups
WHERE id = sqlc.arg(group_id)
  AND deleted_at IS NULL;

-- name: CountGroups :one
SELECT COUNT(*) FROM groups;

-- name: ListGroupIDs :many
SELECT id FROM groups;

-- name: CreateGroupIfNone :execrows
INSERT INTO groups (id, name, registration_enabled, change_seq_counter, created_at, updated_at, version, deleted_at, change_seq)
SELECT sqlc.arg(id), sqlc.arg(name), 0, 0, sqlc.arg(now), sqlc.arg(now), 1, NULL, 0
WHERE NOT EXISTS (SELECT 1 FROM groups);

-- name: CreateGroup :exec
INSERT INTO groups (id, name, registration_enabled, change_seq_counter, created_at, updated_at, version, deleted_at, change_seq)
VALUES (sqlc.arg(id), sqlc.arg(name), 0, 0, sqlc.arg(now), sqlc.arg(now), 1, NULL, 0);

-- name: GetGroupVisibility :one
SELECT id, warranty_visible, sale_visible, purchase_visible, version
FROM groups
WHERE id = sqlc.arg(group_id)
  AND deleted_at IS NULL;

-- name: UpdateGroupVisibility :execrows
UPDATE groups
SET warranty_visible = sqlc.arg(warranty_visible),
    sale_visible      = sqlc.arg(sale_visible),
    purchase_visible  = sqlc.arg(purchase_visible),
    updated_at        = sqlc.arg(now),
    version           = version + 1,
    change_seq        = sqlc.arg(change_seq)
WHERE id = sqlc.arg(group_id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: GetGroupTombstoneLowWatermark :one
SELECT tombstone_low_watermark FROM groups
WHERE id = sqlc.arg(group_id)
  AND deleted_at IS NULL;

-- name: SetGroupTombstoneLowWatermark :execrows
UPDATE groups
SET tombstone_low_watermark = sqlc.arg(tombstone_low_watermark)
WHERE id = sqlc.arg(group_id)
  AND deleted_at IS NULL;

