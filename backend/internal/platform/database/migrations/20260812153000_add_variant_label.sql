-- +goose Up
ALTER TABLE snapshot_skus ADD COLUMN variant_label TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE snapshot_skus DROP COLUMN variant_label;
