CREATE TABLE entry_projects (
    entry_id   INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    PRIMARY KEY (entry_id, project_id)
);

INSERT INTO entry_projects (entry_id, project_id)
    SELECT id, project_id FROM entries WHERE project_id IS NOT NULL;

ALTER TABLE entries DROP COLUMN project_id;
