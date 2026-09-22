-- name: GetItemCustomField :one
SELECT * FROM item_custom_fields
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListItemCustomFields :many
SELECT * FROM item_custom_fields
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL
ORDER BY created_at ASC, id ASC;

-- name: CreateItemCustomField :exec
INSERT INTO item_custom_fields (
    id, group_id, item_id, field_def_id, name, field_type,
    text_value, number_value, bool_value, date_value,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(item_id), sqlc.arg(field_def_id), sqlc.arg(name), sqlc.arg(field_type),
    sqlc.arg(text_value), sqlc.arg(number_value), sqlc.arg(bool_value), sqlc.arg(date_value),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateItemCustomField :execrows
UPDATE item_custom_fields
SET field_def_id = sqlc.arg(field_def_id),
    name          = sqlc.arg(name),
    field_type    = sqlc.arg(field_type),
    text_value    = sqlc.arg(text_value),
    number_value  = sqlc.arg(number_value),
    bool_value    = sqlc.arg(bool_value),
    date_value    = sqlc.arg(date_value),
    updated_at    = sqlc.arg(now),
    version       = version + 1,
    change_seq    = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteItemCustomField :execrows
UPDATE item_custom_fields
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: CascadeDeleteItemCustomFields :execrows
UPDATE item_custom_fields
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND item_id = sqlc.arg(item_id)
  AND deleted_at IS NULL;

-- name: ListItemCustomFieldChangesSince :many
SELECT * FROM item_custom_fields
WHERE group_id = sqlc.arg(group_id)
  AND change_seq > sqlc.arg(since)
ORDER BY change_seq ASC
LIMIT sqlc.arg(row_limit);

-- name: ListItemCustomFieldChangesAtSeq :many
SELECT * FROM item_custom_fields
WHERE group_id = sqlc.arg(group_id)
  AND change_seq = sqlc.arg(seq);

