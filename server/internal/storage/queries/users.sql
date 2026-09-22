-- name: CreateUser :exec
INSERT INTO users (id, group_id, username, password_hash, role, created_at, updated_at, version, deleted_at, change_seq)
VALUES (sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(username), sqlc.arg(password_hash), sqlc.arg(role), sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq));

-- name: GetUserForLogin :one
SELECT id, group_id, username, password_hash, role FROM users
WHERE username = sqlc.arg(username) AND deleted_at IS NULL;

-- name: UpdateUserPasswordHash :execrows
UPDATE users
SET password_hash = sqlc.arg(password_hash),
    updated_at     = sqlc.arg(now),
    version        = version + 1,
    change_seq     = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id) AND id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: ListGroupMembers :many
SELECT id, username, role, created_at FROM users
WHERE group_id = sqlc.arg(group_id) AND deleted_at IS NULL
ORDER BY created_at ASC, id ASC;

