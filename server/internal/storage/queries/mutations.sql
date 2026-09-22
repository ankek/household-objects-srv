-- name: GetMutationByMutationID :one
SELECT * FROM mutations
WHERE group_id = sqlc.arg(group_id)
  AND mutation_id = sqlc.arg(mutation_id);

-- name: CreateMutation :exec
INSERT INTO mutations (id, group_id, mutation_id, entity_type, entity_id, applied_at, outcome, created_at, updated_at)
VALUES (
    sqlc.arg(id),
    sqlc.arg(group_id),
    sqlc.arg(mutation_id),
    sqlc.arg(entity_type),
    sqlc.arg(entity_id),
    sqlc.arg(now),
    sqlc.arg(outcome),
    sqlc.arg(now),
    sqlc.arg(now)
);
