-- name: GetLocation :one
SELECT * FROM locations
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(location_id)
  AND deleted_at IS NULL;

-- name: ListLocations :many
SELECT * FROM locations
WHERE group_id = sqlc.arg(group_id)
  AND deleted_at IS NULL
ORDER BY name ASC, id ASC;

-- name: CreateLocation :exec
INSERT INTO locations (
    id, group_id, name, parent_id,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(name), sqlc.arg(parent_id),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateLocation :execrows
UPDATE locations
SET name        = sqlc.arg(name),
    parent_id   = sqlc.arg(parent_id),
    updated_at  = sqlc.arg(now),
    version     = version + 1,
    change_seq  = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(location_id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteLocation :execrows
UPDATE locations
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(location_id)
  AND deleted_at IS NULL;

-- name: ListLocationParentEdges :many
SELECT id, parent_id, deleted_at FROM locations
WHERE group_id = sqlc.arg(group_id);

-- name: CountLiveChildLocations :one
SELECT COUNT(*) FROM locations
WHERE group_id = sqlc.arg(group_id)
  AND parent_id = sqlc.arg(parent_id)
  AND deleted_at IS NULL;

-- name: CountLiveItemsInLocation :one
SELECT COUNT(*) FROM items
WHERE group_id = sqlc.arg(group_id)
  AND location_id = sqlc.arg(location_id)
  AND deleted_at IS NULL;

-- name: ReparentLiveChildren :execrows
UPDATE locations
SET parent_id  = sqlc.narg(new_parent_id),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND parent_id = sqlc.arg(old_parent_id)
  AND deleted_at IS NULL;

-- name: MoveLiveItemsToLocation :execrows
UPDATE items
SET location_id = sqlc.narg(new_location_id),
    updated_at  = sqlc.arg(now),
    version     = version + 1,
    change_seq  = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND location_id = sqlc.arg(old_location_id)
  AND deleted_at IS NULL;

-- name: CountLiveItemsPerLocation :many
SELECT location_id, COUNT(*) AS item_count FROM items
WHERE group_id = sqlc.arg(group_id)
  AND deleted_at IS NULL
  AND location_id IS NOT NULL
GROUP BY location_id;

-- name: ListLocationChangesSince :many
SELECT * FROM locations
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListLocationChangesAtSeq :many
SELECT * FROM locations
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

