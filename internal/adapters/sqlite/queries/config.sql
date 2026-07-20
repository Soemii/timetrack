-- name: GetConfig :one
SELECT value FROM config WHERE key = ?;

-- name: SetConfig :exec
INSERT INTO config (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: AllConfig :many
SELECT * FROM config ORDER BY key;
