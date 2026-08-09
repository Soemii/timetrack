-- name: ListTasksForProject :many
SELECT * FROM tasks
WHERE project_id = ? AND (archived = 0 OR @include_archived)
ORDER BY jira_key IS NULL, jira_key, title;

-- name: ListTasks :many
SELECT * FROM tasks ORDER BY id;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = ?;

-- name: CreateTask :one
INSERT INTO tasks (project_id, jira_key, title, created_at)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: UpsertJiraTask :one
INSERT INTO tasks (project_id, jira_key, title, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (project_id, jira_key) WHERE jira_key IS NOT NULL
DO UPDATE SET title = excluded.title, archived = 0
RETURNING id;

-- name: RenameTask :exec
UPDATE tasks SET title = ? WHERE id = ?;

-- name: SetTaskArchived :exec
UPDATE tasks SET archived = ? WHERE id = ?;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = ?;

-- name: CountEntriesForTask :one
SELECT COUNT(*) FROM entries WHERE task_id = ?;
