-- +goose Up
CREATE TABLE browser_extensions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    device_name TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    connected_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE extension_authorization_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(code_hash) = 32),
    code_challenge TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name TEXT,
    status TEXT NOT NULL CHECK (status IN ('draft', 'awaiting_extension', 'collecting', 'awaiting_sku_selection', 'failed')),
    failure_code TEXT,
    failure_detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX projects_owner_created_idx ON projects (owner_id, created_at DESC);

CREATE TABLE project_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    source_url TEXT NOT NULL,
    resolved_url TEXT,
    status TEXT NOT NULL CHECK (status IN ('queued', 'collecting', 'succeeded', 'failed')),
    failure_code TEXT,
    failure_detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, ordinal)
);

CREATE TABLE capture_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    extension_id UUID REFERENCES browser_extensions(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE TABLE capture_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    capture_session_id UUID NOT NULL REFERENCES capture_sessions(id) ON DELETE CASCADE,
    project_source_id UUID NOT NULL REFERENCES project_sources(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('queued', 'dispatched', 'succeeded', 'failed')),
    dispatched_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failure_code TEXT,
    failure_detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(capture_session_id, project_source_id)
);

CREATE INDEX capture_tasks_dispatch_idx ON capture_tasks (status, created_at);

CREATE TABLE product_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_source_id UUID NOT NULL UNIQUE REFERENCES project_sources(id) ON DELETE CASCADE,
    capture_task_id UUID NOT NULL UNIQUE REFERENCES capture_tasks(id) ON DELETE RESTRICT,
    source_url TEXT NOT NULL,
    resolved_url TEXT NOT NULL,
    root_sku TEXT NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL,
    raw_capture JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE snapshot_skus (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id UUID NOT NULL REFERENCES product_snapshots(id) ON DELETE CASCADE,
    sku TEXT NOT NULL,
    title TEXT NOT NULL,
    resolved_url TEXT NOT NULL,
    price TEXT,
    availability TEXT NOT NULL DEFAULT 'available',
    ordinal INTEGER NOT NULL,
    UNIQUE(snapshot_id, sku)
);

CREATE TABLE sku_specifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_sku_id UUID NOT NULL REFERENCES snapshot_skus(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('summary', 'parameters')),
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    ordinal INTEGER NOT NULL
);

CREATE TABLE sku_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_sku_id UUID REFERENCES snapshot_skus(id) ON DELETE CASCADE,
    image_type TEXT NOT NULL CHECK (image_type IN ('main', 'variant_main', 'detail', 'unavailable_thumbnail')),
    original_url TEXT NOT NULL,
    normalized_url TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    unavailable BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE unavailable_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id UUID NOT NULL REFERENCES product_snapshots(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    thumbnail_url TEXT,
    high_resolution_image_url TEXT,
    ordinal INTEGER NOT NULL
);

CREATE TABLE project_sku_selections (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    snapshot_sku_id UUID NOT NULL REFERENCES snapshot_skus(id) ON DELETE CASCADE,
    selected BOOLEAN NOT NULL,
    PRIMARY KEY (project_id, snapshot_sku_id)
);

-- +goose Down
DROP TABLE project_sku_selections;
DROP TABLE unavailable_variants;
DROP TABLE sku_images;
DROP TABLE sku_specifications;
DROP TABLE snapshot_skus;
DROP TABLE product_snapshots;
DROP TABLE capture_tasks;
DROP TABLE capture_sessions;
DROP TABLE project_sources;
DROP TABLE projects;
DROP TABLE extension_authorization_codes;
DROP TABLE browser_extensions;
