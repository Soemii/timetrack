CREATE TABLE tasks (
    id         INTEGER PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    jira_key   TEXT,                        -- NULL = lokale Aufgabe
    title      TEXT NOT NULL,
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_tasks_jira ON tasks (project_id, jira_key) WHERE jira_key IS NOT NULL;

ALTER TABLE projects ADD COLUMN jira_project_key TEXT;
ALTER TABLE entries ADD COLUMN task_id INTEGER REFERENCES tasks(id);
