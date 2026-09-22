-- +goose Up

CREATE TABLE field_versions (
    group_id    TEXT    NOT NULL REFERENCES groups (id),
    entity_type TEXT    NOT NULL,
    entity_id   TEXT    NOT NULL,
    field_name  TEXT    NOT NULL,
    version     INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (group_id, entity_type, entity_id, field_name)
) STRICT;

-- +goose Down

DROP TABLE IF EXISTS field_versions;
