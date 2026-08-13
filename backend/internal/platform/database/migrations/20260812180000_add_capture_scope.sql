-- +goose Up
ALTER TABLE projects ADD COLUMN capture_all_skus BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE projects DROP COLUMN capture_all_skus;
