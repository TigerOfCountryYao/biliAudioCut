-- +goose Up
ALTER TABLE browser_extensions
    ADD COLUMN build_id TEXT CHECK (build_id IS NULL OR char_length(build_id) <= 100);

-- +goose Down
ALTER TABLE browser_extensions
    DROP COLUMN build_id;
