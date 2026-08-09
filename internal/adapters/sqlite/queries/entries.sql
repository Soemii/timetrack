-- name: GetOpenEntry :one
SELECT * FROM entries WHERE end_ts IS NULL LIMIT 1;

-- name: CreateEntry :one
INSERT INTO entries (kind, start_ts, end_ts, note, task_id)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: CloseEntry :exec
UPDATE entries SET end_ts = ? WHERE id = ?;

-- name: GetEntry :one
SELECT * FROM entries WHERE id = ?;

-- name: UpdateEntry :exec
UPDATE entries SET kind = ?, start_ts = ?, end_ts = ?, note = ?, task_id = ?
WHERE id = ?;

-- name: DeleteEntry :exec
DELETE FROM entries WHERE id = ?;

-- name: ListEntriesTouching :many
SELECT * FROM entries
WHERE start_ts < @until AND COALESCE(end_ts, @now) > @from_ts
ORDER BY start_ts;

-- name: AddEntryProject :exec
INSERT INTO entry_projects (entry_id, project_id) VALUES (?, ?);

-- name: DeleteEntryProjects :exec
DELETE FROM entry_projects WHERE entry_id = ?;

-- name: ListEntryProjectsFor :many
SELECT entry_id, project_id FROM entry_projects
WHERE entry_id IN (sqlc.slice('ids'))
ORDER BY entry_id, project_id;
