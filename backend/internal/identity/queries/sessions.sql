-- name: CreateUserSession :one
INSERT INTO user_sessions (
    user_id,
    token_hash,
    expires_at
)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(token_hash),
    sqlc.arg(expires_at)
)
RETURNING id, user_id, expires_at, created_at, last_seen_at;

-- name: GetActiveSessionByTokenHash :one
SELECT id, user_id, expires_at
FROM user_sessions
WHERE token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL
  AND expires_at > now()
LIMIT 1;

-- name: RevokeUserSessionByTokenHash :execrows
UPDATE user_sessions
SET revoked_at = now()
WHERE token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :execrows
UPDATE user_sessions
SET revoked_at = now()
WHERE user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL;
