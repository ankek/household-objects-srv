-- name: GetAttachment :one
SELECT * FROM attachments
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListAttachmentsForGroup :many
SELECT * FROM attachments
WHERE group_id = sqlc.arg(group_id);

-- name: ListAttachmentsForItem :many
SELECT * FROM attachments
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL
ORDER BY created_at ASC, id ASC;

-- name: CreateAttachment :exec
INSERT INTO attachments (
    id, group_id, item_id, category, original_filename, content_type, size_bytes,
    storage_path, sha256, created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(category),
    sqlc.arg(original_filename), sqlc.arg(content_type), sqlc.arg(size_bytes),
    sqlc.arg(storage_path), sqlc.arg(sha256), sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: SetAttachmentThumbnailPath :execrows
UPDATE attachments
SET thumbnail_path = sqlc.arg(thumbnail_path),
    updated_at     = sqlc.arg(now),
    version        = version + 1,
    change_seq     = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ClearAttachmentThumbnailPath :execrows
UPDATE attachments
SET thumbnail_path = NULL,
    updated_at      = sqlc.arg(now),
    version         = version + 1,
    change_seq      = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL
  AND thumbnail_path IS NOT NULL;

-- name: DeleteAttachment :execrows
UPDATE attachments
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListAttachmentChangesSince :many
SELECT * FROM attachments
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListAttachmentChangesAtSeq :many
SELECT * FROM attachments
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

