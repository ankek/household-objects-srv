-- +goose Up

CREATE TABLE groups (
    id                   TEXT    NOT NULL PRIMARY KEY,
    name                 TEXT    NOT NULL,
    currency_code        TEXT    NOT NULL DEFAULT 'EUR',
    registration_enabled INTEGER NOT NULL DEFAULT 0 CHECK (registration_enabled IN (0, 1)),
    change_seq_counter   INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    deleted_at           INTEGER,
    change_seq           INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE users (
    id            TEXT    NOT NULL PRIMARY KEY,
    group_id      TEXT    NOT NULL REFERENCES groups (id),
    username      TEXT    NOT NULL COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'member',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1,
    deleted_at    INTEGER,
    change_seq    INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE UNIQUE INDEX ux_users_username        ON users (username);
CREATE UNIQUE INDEX ux_users_group_id_id     ON users (group_id, id);
CREATE INDEX        ix_users_group_change_seq ON users (group_id, change_seq);
CREATE INDEX        ix_users_group_live       ON users (group_id, username) WHERE deleted_at IS NULL;

CREATE TABLE sessions (
    id              TEXT    NOT NULL PRIMARY KEY,
    group_id        TEXT    NOT NULL REFERENCES groups (id),
    user_id         TEXT    NOT NULL,
    token_hash      TEXT    NOT NULL,
    expires_at      INTEGER NOT NULL,
    revoked_at      INTEGER,
    user_agent      TEXT    NOT NULL DEFAULT '',
    created_from_ip TEXT    NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    deleted_at      INTEGER,
    change_seq      INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, user_id) REFERENCES users (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_sessions_token_hash       ON sessions (token_hash);
CREATE INDEX        ix_sessions_group_user       ON sessions (group_id, user_id, deleted_at);
CREATE INDEX        ix_sessions_group_change_seq ON sessions (group_id, change_seq);

CREATE TABLE device_tokens (
    id           TEXT    NOT NULL PRIMARY KEY,
    group_id     TEXT    NOT NULL REFERENCES groups (id),
    user_id      TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL,
    device_label TEXT    NOT NULL DEFAULT '',
    revoked_at   INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    deleted_at   INTEGER,
    change_seq   INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, user_id) REFERENCES users (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_device_tokens_token_hash       ON device_tokens (token_hash);
CREATE INDEX        ix_device_tokens_group_user       ON device_tokens (group_id, user_id, deleted_at);
CREATE INDEX        ix_device_tokens_group_change_seq ON device_tokens (group_id, change_seq);

CREATE TABLE invites (
    id                   TEXT    NOT NULL PRIMARY KEY,
    group_id             TEXT    NOT NULL REFERENCES groups (id),
    token_hash           TEXT    NOT NULL,
    created_by_user_id   TEXT    NOT NULL,
    expires_at           INTEGER NOT NULL,
    redeemed_at          INTEGER,
    redeemed_by_user_id  TEXT,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    deleted_at           INTEGER,
    change_seq           INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, created_by_user_id)  REFERENCES users (group_id, id),
    FOREIGN KEY (group_id, redeemed_by_user_id) REFERENCES users (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_invites_token_hash       ON invites (token_hash);
CREATE INDEX        ix_invites_group_open       ON invites (group_id, expires_at)
    WHERE deleted_at IS NULL AND redeemed_at IS NULL;
CREATE INDEX        ix_invites_group_change_seq ON invites (group_id, change_seq);

CREATE TABLE locations (
    id         TEXT    NOT NULL PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups (id),
    name       TEXT    NOT NULL,
    parent_id  TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, parent_id) REFERENCES locations (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_locations_group_id_id     ON locations (group_id, id);
CREATE INDEX        ix_locations_group_parent    ON locations (group_id, parent_id) WHERE deleted_at IS NULL;
CREATE INDEX        ix_locations_group_name      ON locations (group_id, name)      WHERE deleted_at IS NULL;
CREATE INDEX        ix_locations_group_change_seq ON locations (group_id, change_seq);

CREATE TABLE labels (
    id         TEXT    NOT NULL PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups (id),
    name       TEXT    NOT NULL,
    color      TEXT    NOT NULL DEFAULT '#888888',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE UNIQUE INDEX ux_labels_group_id_id      ON labels (group_id, id);
CREATE INDEX        ix_labels_group_name       ON labels (group_id, name) WHERE deleted_at IS NULL;
CREATE INDEX        ix_labels_group_change_seq ON labels (group_id, change_seq);

CREATE TABLE custom_field_defs (
    id            TEXT    NOT NULL PRIMARY KEY,
    group_id      TEXT    NOT NULL REFERENCES groups (id),
    name          TEXT    NOT NULL,
    field_type    TEXT    NOT NULL CHECK (field_type IN ('text', 'number', 'boolean', 'date')),
    display_order INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1,
    deleted_at    INTEGER,
    change_seq    INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE UNIQUE INDEX ux_custom_field_defs_group_id_id      ON custom_field_defs (group_id, id);
CREATE INDEX        ix_custom_field_defs_group_order      ON custom_field_defs (group_id, display_order) WHERE deleted_at IS NULL;
CREATE INDEX        ix_custom_field_defs_group_change_seq ON custom_field_defs (group_id, change_seq);

CREATE TABLE items (
    id          TEXT    NOT NULL PRIMARY KEY,
    group_id    TEXT    NOT NULL REFERENCES groups (id),
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    location_id TEXT,
    quantity    INTEGER NOT NULL DEFAULT 0,
    short_code  TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    deleted_at  INTEGER,
    change_seq  INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, location_id) REFERENCES locations (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_items_group_id_id      ON items (group_id, id);
CREATE UNIQUE INDEX ux_items_group_short_code ON items (group_id, short_code) WHERE deleted_at IS NULL;
CREATE INDEX        ix_items_group_change_seq ON items (group_id, change_seq);
CREATE INDEX        ix_items_group_updated    ON items (group_id, deleted_at, updated_at DESC);
CREATE INDEX        ix_items_group_name       ON items (group_id, name)        WHERE deleted_at IS NULL;
CREATE INDEX        ix_items_group_location   ON items (group_id, location_id) WHERE deleted_at IS NULL;

CREATE TABLE item_labels (
    id         TEXT    NOT NULL PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups (id),
    item_id    TEXT    NOT NULL,
    label_id   TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id)  REFERENCES items  (group_id, id),
    FOREIGN KEY (group_id, label_id) REFERENCES labels (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_item_labels_group_item_label ON item_labels (group_id, item_id, label_id)
    WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_labels_group_label      ON item_labels (group_id, label_id, item_id) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_labels_group_item       ON item_labels (group_id, item_id);
CREATE INDEX        ix_item_labels_group_change_seq ON item_labels (group_id, change_seq);

CREATE TABLE item_identifications (
    id         TEXT    NOT NULL PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups (id),
    item_id    TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('serial', 'model', 'asset_tag', 'barcode', 'other')),
    value      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id)
) STRICT;

CREATE INDEX ix_item_identifications_group_item       ON item_identifications (group_id, item_id);
CREATE INDEX ix_item_identifications_group_value      ON item_identifications (group_id, value) WHERE deleted_at IS NULL;
CREATE INDEX ix_item_identifications_group_change_seq ON item_identifications (group_id, change_seq);

CREATE TABLE item_custom_fields (
    id           TEXT    NOT NULL PRIMARY KEY,
    group_id     TEXT    NOT NULL REFERENCES groups (id),
    item_id      TEXT    NOT NULL,
    field_def_id TEXT,
    name         TEXT    NOT NULL,
    field_type   TEXT    NOT NULL CHECK (field_type IN ('text', 'number', 'boolean', 'date')),
    text_value   TEXT,
    number_value REAL,
    bool_value   INTEGER CHECK (bool_value IN (0, 1)),
    date_value   TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    deleted_at   INTEGER,
    change_seq   INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id)      REFERENCES items             (group_id, id),
    FOREIGN KEY (group_id, field_def_id) REFERENCES custom_field_defs (group_id, id)
) STRICT;

CREATE INDEX ix_item_custom_fields_group_item       ON item_custom_fields (group_id, item_id);
CREATE INDEX ix_item_custom_fields_group_name       ON item_custom_fields (group_id, name, text_value) WHERE deleted_at IS NULL;
CREATE INDEX ix_item_custom_fields_group_change_seq ON item_custom_fields (group_id, change_seq);

CREATE TABLE item_warranty (
    id          TEXT    NOT NULL PRIMARY KEY,
    group_id    TEXT    NOT NULL REFERENCES groups (id),
    item_id     TEXT    NOT NULL,
    holder      TEXT    NOT NULL DEFAULT '',
    provider    TEXT    NOT NULL DEFAULT '',
    starts_on   TEXT,
    expires_on  TEXT,
    is_lifetime INTEGER NOT NULL DEFAULT 0 CHECK (is_lifetime IN (0, 1)),
    notes       TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    deleted_at  INTEGER,
    change_seq  INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_item_warranty_group_item       ON item_warranty (group_id, item_id) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_warranty_group_item_all   ON item_warranty (group_id, item_id);
CREATE INDEX        ix_item_warranty_group_expires    ON item_warranty (group_id, expires_on) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_warranty_group_change_seq ON item_warranty (group_id, change_seq);

CREATE TABLE item_purchase (
    id                   TEXT    NOT NULL PRIMARY KEY,
    group_id             TEXT    NOT NULL REFERENCES groups (id),
    item_id              TEXT    NOT NULL,
    vendor               TEXT    NOT NULL DEFAULT '',
    purchased_on         TEXT,
    purchase_price_minor INTEGER NOT NULL DEFAULT 0,
    order_reference      TEXT    NOT NULL DEFAULT '',
    notes                TEXT    NOT NULL DEFAULT '',
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    deleted_at           INTEGER,
    change_seq           INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_item_purchase_group_item       ON item_purchase (group_id, item_id) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_purchase_group_item_all   ON item_purchase (group_id, item_id);
CREATE INDEX        ix_item_purchase_group_date       ON item_purchase (group_id, purchased_on) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_purchase_group_change_seq ON item_purchase (group_id, change_seq);

CREATE TABLE item_sale (
    id               TEXT    NOT NULL PRIMARY KEY,
    group_id         TEXT    NOT NULL REFERENCES groups (id),
    item_id          TEXT    NOT NULL,
    buyer_name       TEXT    NOT NULL DEFAULT '',
    sold_on          TEXT,
    sale_price_minor INTEGER NOT NULL DEFAULT 0,
    notes            TEXT    NOT NULL DEFAULT '',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1,
    deleted_at       INTEGER,
    change_seq       INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_item_sale_group_item       ON item_sale (group_id, item_id) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_sale_group_item_all   ON item_sale (group_id, item_id);
CREATE INDEX        ix_item_sale_group_date       ON item_sale (group_id, sold_on) WHERE deleted_at IS NULL;
CREATE INDEX        ix_item_sale_group_change_seq ON item_sale (group_id, change_seq);

CREATE TABLE stock_adjustments (
    id                 TEXT    NOT NULL PRIMARY KEY,
    group_id           TEXT    NOT NULL REFERENCES groups (id),
    item_id            TEXT    NOT NULL,
    delta              INTEGER NOT NULL,
    reason             TEXT    NOT NULL DEFAULT '',
    note               TEXT    NOT NULL DEFAULT '',
    resulting_quantity INTEGER NOT NULL,
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    version            INTEGER NOT NULL DEFAULT 1,
    deleted_at         INTEGER,
    change_seq         INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id)
) STRICT;

CREATE INDEX ix_stock_adjustments_group_item       ON stock_adjustments (group_id, item_id, created_at DESC);
CREATE INDEX ix_stock_adjustments_group_change_seq ON stock_adjustments (group_id, change_seq);

CREATE TABLE attachments (
    id                TEXT    NOT NULL PRIMARY KEY,
    group_id          TEXT    NOT NULL REFERENCES groups (id),
    item_id           TEXT    NOT NULL,
    category          TEXT    NOT NULL CHECK (category IN ('image', 'manual', 'warranty', 'receipt', 'general')),
    original_filename TEXT    NOT NULL,
    content_type      TEXT    NOT NULL,
    size_bytes        INTEGER NOT NULL,
    storage_path      TEXT    NOT NULL,
    thumbnail_path    TEXT,
    sha256            TEXT    NOT NULL,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    version           INTEGER NOT NULL DEFAULT 1,
    deleted_at        INTEGER,
    change_seq        INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id, item_id) REFERENCES items (group_id, id)
) STRICT;

CREATE UNIQUE INDEX ux_attachments_storage_path      ON attachments (storage_path);
CREATE INDEX        ix_attachments_group_item        ON attachments (group_id, item_id, deleted_at);
CREATE INDEX        ix_attachments_group_sha256      ON attachments (group_id, sha256) WHERE deleted_at IS NULL;
CREATE INDEX        ix_attachments_group_change_seq  ON attachments (group_id, change_seq);

CREATE TABLE mutations (
    id          TEXT    NOT NULL PRIMARY KEY,
    group_id    TEXT    NOT NULL REFERENCES groups (id),
    mutation_id TEXT    NOT NULL,
    entity_type TEXT    NOT NULL,
    entity_id   TEXT    NOT NULL,
    applied_at  INTEGER NOT NULL,
    outcome     TEXT    NOT NULL CHECK (outcome IN ('applied', 'skipped_duplicate', 'conflict')),
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX ux_mutations_group_mutation_id ON mutations (group_id, mutation_id);
CREATE UNIQUE INDEX ux_mutations_group_id_id       ON mutations (group_id, id);
CREATE INDEX        ix_mutations_group_updated     ON mutations (group_id, updated_at);
CREATE INDEX        ix_mutations_group_entity      ON mutations (group_id, entity_type, entity_id);

CREATE TABLE conflicts (
    id                           TEXT    NOT NULL PRIMARY KEY,
    group_id                     TEXT    NOT NULL REFERENCES groups (id),
    entity_type                  TEXT    NOT NULL,
    entity_id                    TEXT    NOT NULL,
    field_name                   TEXT    NOT NULL,
    server_value_snapshot        TEXT,
    losing_client_value_snapshot TEXT,
    detected_at                  INTEGER NOT NULL,
    mutation_id                  TEXT,
    created_at                   INTEGER NOT NULL,
    updated_at                   INTEGER NOT NULL,
    FOREIGN KEY (group_id, mutation_id) REFERENCES mutations (group_id, id)
) STRICT;

CREATE INDEX ix_conflicts_group_detected ON conflicts (group_id, detected_at DESC);
CREATE INDEX ix_conflicts_group_entity   ON conflicts (group_id, entity_type, entity_id);

CREATE VIRTUAL TABLE items_fts USING fts5(
    name,
    description,
    identifier,
    notes,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- +goose StatementBegin
CREATE TRIGGER items_fts_ai AFTER INSERT ON items
WHEN new.deleted_at IS NULL
BEGIN
    INSERT INTO items_fts (rowid, name, description, identifier, notes)
    VALUES (
        new.rowid,
        new.name,
        new.description,
        COALESCE((SELECT group_concat(d.value, ' ') FROM item_identifications d
                   WHERE d.group_id = new.group_id AND d.item_id = new.id AND d.deleted_at IS NULL), ''),
        TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                        WHERE w.group_id = new.group_id AND w.item_id = new.id AND w.deleted_at IS NULL), '') || ' ' ||
             COALESCE((SELECT p.notes FROM item_purchase p
                        WHERE p.group_id = new.group_id AND p.item_id = new.id AND p.deleted_at IS NULL), '') || ' ' ||
             COALESCE((SELECT s.notes FROM item_sale s
                        WHERE s.group_id = new.group_id AND s.item_id = new.id AND s.deleted_at IS NULL), ''))
    );
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER items_fts_ad AFTER DELETE ON items
BEGIN
    DELETE FROM items_fts WHERE rowid = old.rowid;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER items_fts_au AFTER UPDATE OF name, description, deleted_at ON items
WHEN new.name IS NOT old.name
  OR new.description IS NOT old.description
  OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
BEGIN
    DELETE FROM items_fts WHERE rowid = old.rowid;
    INSERT INTO items_fts (rowid, name, description, identifier, notes)
    SELECT new.rowid,
           new.name,
           new.description,
           COALESCE((SELECT group_concat(d.value, ' ') FROM item_identifications d
                      WHERE d.group_id = new.group_id AND d.item_id = new.id AND d.deleted_at IS NULL), ''),
           TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                           WHERE w.group_id = new.group_id AND w.item_id = new.id AND w.deleted_at IS NULL), '') || ' ' ||
                COALESCE((SELECT p.notes FROM item_purchase p
                           WHERE p.group_id = new.group_id AND p.item_id = new.id AND p.deleted_at IS NULL), '') || ' ' ||
                COALESCE((SELECT s.notes FROM item_sale s
                           WHERE s.group_id = new.group_id AND s.item_id = new.id AND s.deleted_at IS NULL), ''))
    WHERE new.deleted_at IS NULL;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_identifications_fts_ai AFTER INSERT ON item_identifications
BEGIN
    UPDATE items_fts
       SET identifier = COALESCE((SELECT group_concat(d.value, ' ') FROM item_identifications d
                                   WHERE d.group_id = new.group_id AND d.item_id = new.item_id
                                     AND d.deleted_at IS NULL), '')
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_identifications_fts_ad AFTER DELETE ON item_identifications
BEGIN
    UPDATE items_fts
       SET identifier = COALESCE((SELECT group_concat(d.value, ' ') FROM item_identifications d
                                   WHERE d.group_id = old.group_id AND d.item_id = old.item_id
                                     AND d.deleted_at IS NULL), '')
     WHERE rowid = (SELECT rowid FROM items WHERE id = old.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_identifications_fts_au AFTER UPDATE OF item_id, value, deleted_at ON item_identifications
WHEN new.value IS NOT old.value
  OR new.item_id IS NOT old.item_id
  OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
BEGIN
    UPDATE items_fts
       SET identifier = COALESCE((SELECT group_concat(d.value, ' ') FROM item_identifications d
                                   WHERE d.group_id = old.group_id AND d.item_id = old.item_id
                                     AND d.deleted_at IS NULL), '')
     WHERE old.item_id IS NOT new.item_id
       AND rowid = (SELECT rowid FROM items WHERE id = old.item_id);
    UPDATE items_fts
       SET identifier = COALESCE((SELECT group_concat(d.value, ' ') FROM item_identifications d
                                   WHERE d.group_id = new.group_id AND d.item_id = new.item_id
                                     AND d.deleted_at IS NULL), '')
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_warranty_fts_ai AFTER INSERT ON item_warranty
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = new.group_id AND w.item_id = new.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = new.group_id AND p.item_id = new.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = new.group_id AND s.item_id = new.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_warranty_fts_ad AFTER DELETE ON item_warranty
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = old.group_id AND w.item_id = old.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = old.group_id AND p.item_id = old.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = old.group_id AND s.item_id = old.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = old.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_warranty_fts_au AFTER UPDATE OF item_id, notes, deleted_at ON item_warranty
WHEN new.notes IS NOT old.notes
  OR new.item_id IS NOT old.item_id
  OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = old.group_id AND w.item_id = old.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = old.group_id AND p.item_id = old.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = old.group_id AND s.item_id = old.item_id AND s.deleted_at IS NULL), ''))
     WHERE old.item_id IS NOT new.item_id
       AND rowid = (SELECT rowid FROM items WHERE id = old.item_id);
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = new.group_id AND w.item_id = new.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = new.group_id AND p.item_id = new.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = new.group_id AND s.item_id = new.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_purchase_fts_ai AFTER INSERT ON item_purchase
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = new.group_id AND w.item_id = new.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = new.group_id AND p.item_id = new.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = new.group_id AND s.item_id = new.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_purchase_fts_ad AFTER DELETE ON item_purchase
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = old.group_id AND w.item_id = old.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = old.group_id AND p.item_id = old.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = old.group_id AND s.item_id = old.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = old.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_purchase_fts_au AFTER UPDATE OF item_id, notes, deleted_at ON item_purchase
WHEN new.notes IS NOT old.notes
  OR new.item_id IS NOT old.item_id
  OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = old.group_id AND w.item_id = old.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = old.group_id AND p.item_id = old.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = old.group_id AND s.item_id = old.item_id AND s.deleted_at IS NULL), ''))
     WHERE old.item_id IS NOT new.item_id
       AND rowid = (SELECT rowid FROM items WHERE id = old.item_id);
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = new.group_id AND w.item_id = new.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = new.group_id AND p.item_id = new.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = new.group_id AND s.item_id = new.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_sale_fts_ai AFTER INSERT ON item_sale
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = new.group_id AND w.item_id = new.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = new.group_id AND p.item_id = new.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = new.group_id AND s.item_id = new.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_sale_fts_ad AFTER DELETE ON item_sale
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = old.group_id AND w.item_id = old.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = old.group_id AND p.item_id = old.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = old.group_id AND s.item_id = old.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = old.item_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER item_sale_fts_au AFTER UPDATE OF item_id, notes, deleted_at ON item_sale
WHEN new.notes IS NOT old.notes
  OR new.item_id IS NOT old.item_id
  OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
BEGIN
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = old.group_id AND w.item_id = old.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = old.group_id AND p.item_id = old.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = old.group_id AND s.item_id = old.item_id AND s.deleted_at IS NULL), ''))
     WHERE old.item_id IS NOT new.item_id
       AND rowid = (SELECT rowid FROM items WHERE id = old.item_id);
    UPDATE items_fts
       SET notes = TRIM(COALESCE((SELECT w.notes FROM item_warranty w
                                   WHERE w.group_id = new.group_id AND w.item_id = new.item_id AND w.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT p.notes FROM item_purchase p
                                   WHERE p.group_id = new.group_id AND p.item_id = new.item_id AND p.deleted_at IS NULL), '') || ' ' ||
                        COALESCE((SELECT s.notes FROM item_sale s
                                   WHERE s.group_id = new.group_id AND s.item_id = new.item_id AND s.deleted_at IS NULL), ''))
     WHERE rowid = (SELECT rowid FROM items WHERE id = new.item_id);
END;
-- +goose StatementEnd

-- +goose Down

DROP TABLE IF EXISTS items_fts;
DROP TABLE IF EXISTS conflicts;
DROP TABLE IF EXISTS mutations;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS stock_adjustments;
DROP TABLE IF EXISTS item_sale;
DROP TABLE IF EXISTS item_purchase;
DROP TABLE IF EXISTS item_warranty;
DROP TABLE IF EXISTS item_custom_fields;
DROP TABLE IF EXISTS item_identifications;
DROP TABLE IF EXISTS item_labels;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS custom_field_defs;
DROP TABLE IF EXISTS labels;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS device_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS groups;
