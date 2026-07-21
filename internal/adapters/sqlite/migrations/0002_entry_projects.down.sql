ALTER TABLE entries ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE RESTRICT;

UPDATE entries SET project_id = (
    SELECT MIN(project_id) FROM entry_projects WHERE entry_id = entries.id
);

DROP TABLE entry_projects;
