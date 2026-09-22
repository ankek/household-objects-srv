-- name: CreateImportSession :exec
INSERT INTO import_sessions (id, group_id, source, status, created_at, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(source), 'staged', sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetImportSession :one
SELECT * FROM import_sessions
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id);

-- name: MarkImportSessionCommitted :execrows
UPDATE import_sessions
SET status = 'committed'
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND status = 'staged';

-- name: DeleteImportSession :exec
DELETE FROM import_sessions
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id);

