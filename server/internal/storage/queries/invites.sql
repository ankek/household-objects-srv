-- name: CreateInvite :exec
INSERT INTO invites (
    id, group_id, token_hash, created_by_user_id, expires_at, redeemed_at, redeemed_by_user_id,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(token_hash), sqlc.arg(created_by_user_id), sqlc.arg(expires_at), NULL, NULL,
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: ListInvites :many
SELECT id, created_by_user_id, expires_at, redeemed_at, redeemed_by_user_id, created_at, updated_at
FROM invites
WHERE group_id = sqlc.arg(group_id)
  AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetInviteForRevoke :one
SELECT expires_at, redeemed_at FROM invites
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: RevokeInvite :execrows
UPDATE invites
SET expires_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND redeemed_at IS NULL
  AND expires_at > sqlc.arg(now)
  AND deleted_at IS NULL;

-- name: GetInviteForRedeem :one
SELECT group_id, expires_at, redeemed_at FROM invites
WHERE token_hash = sqlc.arg(token_hash)
  AND deleted_at IS NULL;

-- name: MarkInviteRedeemed :execrows
UPDATE invites
SET redeemed_at          = sqlc.arg(now),
    redeemed_by_user_id  = sqlc.arg(redeemed_by_user_id),
    updated_at           = sqlc.arg(now),
    version              = version + 1,
    change_seq           = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND token_hash = sqlc.arg(token_hash)
  AND deleted_at IS NULL
  AND redeemed_at IS NULL
  AND expires_at > sqlc.arg(now);

