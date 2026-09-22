-- name: ListFieldVersionsByEntity :many
SELECT * FROM field_versions
WHERE group_id = sqlc.arg(group_id)
  AND entity_type = sqlc.arg(entity_type)
  AND entity_id = sqlc.arg(entity_id);

-- name: UpsertFieldVersion :exec
INSERT INTO field_versions (group_id, entity_type, entity_id, field_name, version, updated_at)
VALUES (
    sqlc.arg(group_id), sqlc.arg(entity_type), sqlc.arg(entity_id), sqlc.arg(field_name),
    sqlc.arg(version), sqlc.arg(now)
)
ON CONFLICT (group_id, entity_type, entity_id, field_name)
DO UPDATE SET version = excluded.version, updated_at = excluded.updated_at;
