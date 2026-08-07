-- name: GetProjectByName :one
SELECT * FROM projects WHERE name = ? COLLATE NOCASE;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: CreateProject :one
INSERT INTO projects (name, created_at) VALUES (?, ?) RETURNING id;

-- name: ListProjects :many
SELECT * FROM projects WHERE archived = 0 OR @include_archived ORDER BY name;

-- name: RenameProject :exec
UPDATE projects SET name = ? WHERE id = ?;

-- name: SetProjectArchived :exec
UPDATE projects SET archived = ? WHERE id = ?;

-- name: SetProjectMeta :exec
UPDATE projects SET color = ?, note = ? WHERE id = ?;
