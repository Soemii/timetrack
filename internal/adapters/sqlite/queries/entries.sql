-- name: GetOpenEntry :one
SELECT * FROM entries WHERE end_ts IS NULL LIMIT 1;

-- name: CreateEntry :one
INSERT INTO entries (kind, project_id, start_ts, end_ts, note)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: CloseEntry :exec
UPDATE entries SET end_ts = ? WHERE id = ?;

-- name: GetEntry :one
SELECT * FROM entries WHERE id = ?;

-- name: UpdateEntry :exec
UPDATE entries SET kind = ?, project_id = ?, start_ts = ?, end_ts = ?, note = ?
WHERE id = ?;

-- name: DeleteEntry :exec
DELETE FROM entries WHERE id = ?;

-- name: ListEntriesTouching :many
SELECT * FROM entries
WHERE start_ts < @until AND COALESCE(end_ts, @now) > @from_ts
ORDER BY start_ts;
