-- +goose Up
ALTER TABLE projects DROP CONSTRAINT projects_status_check;
ALTER TABLE projects ADD CONSTRAINT projects_status_check CHECK (status IN ('draft', 'awaiting_extension', 'collecting', 'awaiting_sku_selection', 'awaiting_jd_login', 'awaiting_jd_verification', 'failed'));

ALTER TABLE project_sources
    ADD COLUMN interaction_kind TEXT,
    ADD COLUMN interaction_url TEXT;

ALTER TABLE project_sources
    ADD CONSTRAINT project_sources_interaction_kind_check CHECK (interaction_kind IS NULL OR interaction_kind IN ('login', 'verification')),
    ADD CONSTRAINT project_sources_interaction_url_check CHECK (interaction_url IS NULL OR char_length(interaction_url) <= 4096);

-- +goose Down
ALTER TABLE project_sources
    DROP CONSTRAINT project_sources_interaction_url_check,
    DROP CONSTRAINT project_sources_interaction_kind_check,
    DROP COLUMN interaction_url,
    DROP COLUMN interaction_kind;

ALTER TABLE projects DROP CONSTRAINT projects_status_check;
ALTER TABLE projects ADD CONSTRAINT projects_status_check CHECK (status IN ('draft', 'awaiting_extension', 'collecting', 'awaiting_sku_selection', 'failed'));
