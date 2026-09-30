-- name: CreateConflict :exec
INSERT INTO conflicts (
    id, group_id, entity_type, entity_id, field_name,
    server_value_snapshot, losing_client_value_snapshot,
    detected_at, mutation_id, created_at, updated_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(group_id), sqlc.arg(entity_type), sqlc.arg(entity_id), sqlc.arg(field_name),
    sqlc.arg(server_value_snapshot), sqlc.arg(losing_client_value_snapshot),
    sqlc.arg(detected_at), sqlc.arg(mutation_id), sqlc.arg(now), sqlc.arg(now)
);

-- name: ListConflictsPage :many
SELECT
    c.id,
    m.mutation_id AS wire_mutation_id,
    c.entity_type,
    c.entity_id,
    c.field_name,
    c.server_value_snapshot,
    c.losing_client_value_snapshot,
    c.detected_at
FROM conflicts c
LEFT JOIN mutations m
    ON m.id = c.mutation_id
   AND m.group_id = c.group_id
   AND m.group_id = sqlc.arg(group_id)
WHERE c.group_id = sqlc.arg(group_id)
  AND (c.detected_at, c.id) < (CAST(sqlc.arg(after_detected_at) AS INTEGER), CAST(sqlc.arg(after_id) AS TEXT))
ORDER BY c.detected_at DESC, c.id DESC
LIMIT sqlc.arg(page_limit);
