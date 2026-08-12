-- +goose Up
-- A JD SKU can appear in more than one product series.  Keep every observed
-- series/variant occurrence so that the user can make the final selection.
ALTER TABLE snapshot_skus DROP CONSTRAINT IF EXISTS snapshot_skus_snapshot_id_sku_key;
ALTER TABLE snapshot_skus ADD COLUMN series_label TEXT NOT NULL DEFAULT '';
ALTER TABLE snapshot_skus ADD COLUMN series_ordinal INTEGER NOT NULL DEFAULT 0;

ALTER TABLE unavailable_variants ADD COLUMN series_label TEXT NOT NULL DEFAULT '';
ALTER TABLE unavailable_variants ADD COLUMN series_ordinal INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE unavailable_variants DROP COLUMN series_ordinal;
ALTER TABLE unavailable_variants DROP COLUMN series_label;

ALTER TABLE snapshot_skus DROP COLUMN series_ordinal;
ALTER TABLE snapshot_skus DROP COLUMN series_label;
ALTER TABLE snapshot_skus ADD CONSTRAINT snapshot_skus_snapshot_id_sku_key UNIQUE(snapshot_id, sku);
