-- +goose Up

ALTER TABLE items ADD COLUMN external_ref TEXT;

CREATE UNIQUE INDEX ux_items_group_external_ref ON items (group_id, external_ref) WHERE external_ref IS NOT NULL;

CREATE TABLE import_sessions (
    id         TEXT    NOT NULL PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups (id),
    source     TEXT    NOT NULL,
    status     TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX ux_import_sessions_group_id_id ON import_sessions (group_id, id);
CREATE INDEX        ix_import_sessions_group_created ON import_sessions (group_id, created_at DESC);
CREATE INDEX        ix_import_sessions_group_expires ON import_sessions (group_id, expires_at);

-- +goose Down

DROP TABLE IF EXISTS import_sessions;
DROP INDEX IF EXISTS ux_items_group_external_ref;
ALTER TABLE items DROP COLUMN external_ref;
