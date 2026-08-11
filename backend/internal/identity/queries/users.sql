-- name: HasUsers :one
SELECT EXISTS (
    SELECT 1
    FROM users
);

-- name: CreateUser :one
INSERT INTO users (
    email,
    display_name,
    password_hash,
    role,
    status
)
VALUES (
    lower(sqlc.arg(email)),
    sqlc.arg(display_name),
    sqlc.arg(password_hash),
    sqlc.arg(role),
    'active'
)
RETURNING *;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = lower(sqlc.arg(email))
LIMIT 1;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = sqlc.arg(id)
LIMIT 1;