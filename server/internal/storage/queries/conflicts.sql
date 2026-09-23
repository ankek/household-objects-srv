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
