package main

const schemaDDL = `
CREATE TABLE groups (
    id                 TEXT PRIMARY KEY,
    name               TEXT    NOT NULL,
    currency_code      TEXT    NOT NULL DEFAULT 'EUR',
    registration_enabled INTEGER NOT NULL DEFAULT 0,
    change_seq_counter INTEGER NOT NULL DEFAULT 0,
    updated_at         INTEGER NOT NULL,
    version            INTEGER NOT NULL DEFAULT 1,
    deleted_at         INTEGER,
    change_seq         INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE locations (
    id         TEXT PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups(id),
    name       TEXT    NOT NULL,
    parent_id  TEXT    REFERENCES locations(id),
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX idx_locations_group ON locations(group_id, deleted_at);
CREATE INDEX idx_locations_parent ON locations(group_id, parent_id);

CREATE TABLE labels (
    id         TEXT PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups(id),
    name       TEXT    NOT NULL,
    color      TEXT    NOT NULL DEFAULT '#888888',
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX idx_labels_group ON labels(group_id, deleted_at);

CREATE TABLE items (
    id          TEXT PRIMARY KEY,
    group_id    TEXT    NOT NULL REFERENCES groups(id),
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    location_id TEXT    REFERENCES locations(id),
    quantity    INTEGER NOT NULL DEFAULT 0,
    short_code  TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    deleted_at  INTEGER,
    change_seq  INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE UNIQUE INDEX idx_items_short_code ON items(group_id, short_code);
CREATE INDEX idx_items_group_live ON items(group_id, deleted_at, updated_at DESC);
CREATE INDEX idx_items_group_seq ON items(group_id, change_seq);
CREATE INDEX idx_items_group_location ON items(group_id, location_id);
CREATE INDEX idx_items_group_name ON items(group_id, name);

CREATE TABLE item_labels (
    id         TEXT PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups(id),
    item_id    TEXT    NOT NULL REFERENCES items(id),
    label_id   TEXT    NOT NULL REFERENCES labels(id),
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE UNIQUE INDEX idx_item_labels_pair ON item_labels(item_id, label_id);
CREATE INDEX idx_item_labels_item ON item_labels(group_id, item_id);
CREATE INDEX idx_item_labels_label ON item_labels(group_id, label_id);

CREATE TABLE item_identifications (
    id         TEXT PRIMARY KEY,
    group_id   TEXT    NOT NULL REFERENCES groups(id),
    item_id    TEXT    NOT NULL REFERENCES items(id),
    kind       TEXT    NOT NULL,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    change_seq INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX idx_item_identifications_item ON item_identifications(group_id, item_id);
CREATE INDEX idx_item_identifications_item_only ON item_identifications(item_id);
CREATE INDEX idx_item_identifications_value ON item_identifications(group_id, value);

CREATE TABLE item_purchase (
    id                   TEXT PRIMARY KEY,
    group_id             TEXT    NOT NULL REFERENCES groups(id),
    item_id              TEXT    NOT NULL REFERENCES items(id),
    vendor               TEXT    NOT NULL DEFAULT '',
    purchased_on         TEXT,
    purchase_price_minor INTEGER NOT NULL DEFAULT 0,
    order_reference      TEXT    NOT NULL DEFAULT '',
    notes                TEXT    NOT NULL DEFAULT '',
    updated_at           INTEGER NOT NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    deleted_at           INTEGER,
    change_seq           INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE UNIQUE INDEX idx_item_purchase_item ON item_purchase(item_id);

CREATE TABLE stock_adjustments (
    id                 TEXT PRIMARY KEY,
    group_id           TEXT    NOT NULL REFERENCES groups(id),
    item_id            TEXT    NOT NULL REFERENCES items(id),
    delta              INTEGER NOT NULL,
    reason             TEXT    NOT NULL DEFAULT '',
    note               TEXT    NOT NULL DEFAULT '',
    resulting_quantity INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    version            INTEGER NOT NULL DEFAULT 1,
    deleted_at         INTEGER,
    change_seq         INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX idx_stock_adjustments_item ON stock_adjustments(group_id, item_id, updated_at DESC);

-- FTS5 index over the searchable fields (data-model.md: items.name,
-- items.description, item_identifications.value, detail-block notes).
-- Deliberately a plain (content-owning) FTS5 table rather than an
-- external-content one: the real index draws from three source tables, which
-- external-content FTS5 cannot express without a materialised view.
CREATE VIRTUAL TABLE items_fts USING fts5(
    item_id UNINDEXED,
    name,
    description,
    identifiers,
    notes,
    tokenize = 'unicode61 remove_diacritics 2'
);
`

const ftsTriggerDDL = `
CREATE TRIGGER items_fts_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(item_id, name, description, identifiers, notes)
    SELECT new.id, new.name, new.description, '', ''
    WHERE new.deleted_at IS NULL;
END;

CREATE TRIGGER items_fts_ad AFTER DELETE ON items BEGIN
    DELETE FROM items_fts WHERE item_id = old.id;
END;

-- Soft delete (deleted_at set) must drop the row out of the search index, which
-- is why the update trigger rebuilds rather than patches.
CREATE TRIGGER items_fts_au AFTER UPDATE ON items BEGIN
    DELETE FROM items_fts WHERE item_id = old.id;
    INSERT INTO items_fts(item_id, name, description, identifiers, notes)
    SELECT new.id,
           new.name,
           new.description,
           COALESCE((SELECT group_concat(value, ' ')
                       FROM item_identifications
                      WHERE item_id = new.id AND deleted_at IS NULL), ''),
           COALESCE((SELECT notes FROM item_purchase
                      WHERE item_id = new.id AND deleted_at IS NULL), '')
    WHERE new.deleted_at IS NULL;
END;

CREATE TRIGGER item_identifications_fts_ai AFTER INSERT ON item_identifications BEGIN
    UPDATE items_fts
       SET identifiers = COALESCE((SELECT group_concat(value, ' ')
                                     FROM item_identifications
                                    WHERE item_id = new.item_id AND deleted_at IS NULL), '')
     WHERE item_id = new.item_id;
END;

CREATE TRIGGER item_identifications_fts_au AFTER UPDATE ON item_identifications BEGIN
    UPDATE items_fts
       SET identifiers = COALESCE((SELECT group_concat(value, ' ')
                                     FROM item_identifications
                                    WHERE item_id = new.item_id AND deleted_at IS NULL), '')
     WHERE item_id = new.item_id;
END;

CREATE TRIGGER item_identifications_fts_ad AFTER DELETE ON item_identifications BEGIN
    UPDATE items_fts
       SET identifiers = COALESCE((SELECT group_concat(value, ' ')
                                     FROM item_identifications
                                    WHERE item_id = old.item_id AND deleted_at IS NULL), '')
     WHERE item_id = old.item_id;
END;

CREATE TRIGGER item_purchase_fts_ai AFTER INSERT ON item_purchase BEGIN
    UPDATE items_fts SET notes = new.notes WHERE item_id = new.item_id;
END;

CREATE TRIGGER item_purchase_fts_au AFTER UPDATE ON item_purchase BEGIN
    UPDATE items_fts
       SET notes = CASE WHEN new.deleted_at IS NULL THEN new.notes ELSE '' END
     WHERE item_id = new.item_id;
END;
`
