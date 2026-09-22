-- +goose Up

ALTER TABLE groups ADD COLUMN warranty_visible INTEGER NOT NULL DEFAULT 0 CHECK (warranty_visible IN (0, 1));
ALTER TABLE groups ADD COLUMN sale_visible     INTEGER NOT NULL DEFAULT 0 CHECK (sale_visible IN (0, 1));
ALTER TABLE groups ADD COLUMN purchase_visible INTEGER NOT NULL DEFAULT 0 CHECK (purchase_visible IN (0, 1));

-- +goose Down

ALTER TABLE groups DROP COLUMN purchase_visible;
ALTER TABLE groups DROP COLUMN sale_visible;
ALTER TABLE groups DROP COLUMN warranty_visible;
