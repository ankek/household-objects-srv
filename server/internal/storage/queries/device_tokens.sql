-- name: CreateDeviceToken :exec
INSERT INTO device_tokens (
    id, group_id, user_id, token_hash, device_label, revoked_at,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(device_label), NULL,
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: GetDeviceTokenForAuth :one
SELECT d.group_id   AS group_id,
       d.user_id    AS user_id,
       d.revoked_at AS revoked_at,
       u.role       AS role
FROM device_tokens d
JOIN users u ON u.group_id = d.group_id AND u.id = d.user_id
WHERE d.token_hash = sqlc.arg(token_hash)
  AND d.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: ListDeviceTokensForUser :many
SELECT id, device_label, created_at, updated_at, revoked_at
FROM device_tokens
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetDeviceTokenForRevoke :one
SELECT revoked_at FROM device_tokens
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: RevokeDeviceToken :execrows
UPDATE device_tokens
SET revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND deleted_at IS NULL;

-- name: RevokeAllDeviceTokensForUser :execrows
UPDATE device_tokens
SET revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND deleted_at IS NULL;

