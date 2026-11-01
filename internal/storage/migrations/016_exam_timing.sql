ALTER TABLE exams ADD COLUMN ends_at TEXT NULL;
ALTER TABLE sessions ADD COLUMN ends_at TEXT NULL;
ALTER TABLE exams ADD COLUMN extra_grace_seconds INTEGER NOT NULL DEFAULT 0 CHECK(extra_grace_seconds >= 0);
