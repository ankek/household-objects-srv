-- name: CreateSession :exec
INSERT INTO sessions (
    id, group_id, user_id, token_hash, expires_at, revoked_at,
    user_agent, created_from_ip, created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at), NULL,
    sqlc.arg(user_agent), sqlc.arg(created_from_ip), sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: GetSessionForAuth :one
SELECT s.group_id   AS group_id,
       s.user_id    AS user_id,
       s.expires_at AS expires_at,
       s.revoked_at AS revoked_at,
       u.role       AS role
FROM sessions s
JOIN users u ON u.group_id = s.group_id AND u.id = s.user_id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: ListSessionsForUser :many
SELECT id, user_agent, created_from_ip, created_at, updated_at, expires_at, revoked_at
FROM sessions
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetSessionForRevoke :one
SELECT revoked_at FROM sessions
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND revoked_at IS NULL
  AND deleted_at IS NULL;

-- name: RevokeAllSessionsForUser :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND deleted_at IS NULL;

-- name: RevokeSessionByTokenHash :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND user_id = sqlc.arg(user_id)
  AND token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL
  AND deleted_at IS NULL;

