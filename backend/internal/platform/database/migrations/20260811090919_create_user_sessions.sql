-- +goose Up
CREATE TABLE user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE
        CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX user_sessions_active_by_user_idx
    ON user_sessions (user_id)
    WHERE revoked_at IS NULL;

CREATE INDEX user_sessions_active_by_expiry_idx
    ON user_sessions (expires_at)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE user_sessions;