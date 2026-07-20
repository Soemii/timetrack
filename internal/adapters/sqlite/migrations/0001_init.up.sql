CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE TABLE entries (
    id         INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('work','break')),
    project_id INTEGER REFERENCES projects(id) ON DELETE RESTRICT,
    start_ts   INTEGER NOT NULL,
    end_ts     INTEGER,
    note       TEXT,
    CHECK (end_ts IS NULL OR end_ts > start_ts)
);

CREATE UNIQUE INDEX idx_one_open ON entries ((1)) WHERE end_ts IS NULL;
CREATE INDEX idx_entries_start ON entries (start_ts);

CREATE TABLE absences (
    id       INTEGER PRIMARY KEY,
    date     TEXT NOT NULL UNIQUE,
    type     TEXT NOT NULL CHECK (type IN ('urlaub','krank','feiertag')),
    fraction REAL NOT NULL DEFAULT 1.0 CHECK (fraction IN (0.5, 1.0)),
    note     TEXT
);

CREATE TABLE config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
