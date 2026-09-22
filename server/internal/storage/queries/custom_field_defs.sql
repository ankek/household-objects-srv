-- name: GetCustomFieldDef :one
-- name: "do not go looking for a T056-style third predicate").
SELECT * FROM custom_field_defs
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: ListCustomFieldDefs :many
SELECT * FROM custom_field_defs
WHERE group_id = sqlc.arg(group_id)
  AND deleted_at IS NULL
ORDER BY display_order ASC, created_at ASC, id ASC;

-- name: CreateCustomFieldDef :exec
INSERT INTO custom_field_defs (
    id, group_id, name, field_type, display_order,
    created_at, updated_at, version, deleted_at, change_seq
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(name), sqlc.arg(field_type), sqlc.arg(display_order),
    sqlc.arg(now), sqlc.arg(now), 1, NULL, sqlc.arg(change_seq)
);

-- name: UpdateCustomFieldDef :execrows
UPDATE custom_field_defs
SET name          = sqlc.arg(name),
    field_type    = sqlc.arg(field_type),
    display_order = sqlc.arg(display_order),
    updated_at    = sqlc.arg(now),
    version       = version + 1,
    change_seq    = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
  AND deleted_at IS NULL;

-- name: DeleteCustomFieldDef :execrows
UPDATE custom_field_defs
SET deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(now),
    version    = version + 1,
    change_seq = sqlc.arg(change_seq)
WHERE group_id = sqlc.arg(group_id)
  AND id = sqlc.arg(id)
  AND deleted_at IS NULL;

