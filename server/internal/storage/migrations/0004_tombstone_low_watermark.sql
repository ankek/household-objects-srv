-- +goose Up

ALTER TABLE groups ADD COLUMN tombstone_low_watermark INTEGER NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE groups DROP COLUMN tombstone_low_watermark;
