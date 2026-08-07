CREATE TABLE companies (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at INTEGER NOT NULL
);

ALTER TABLE projects ADD COLUMN company_id INTEGER REFERENCES companies(id);
