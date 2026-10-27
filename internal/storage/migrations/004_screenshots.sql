ALTER TABLE screenshots ADD COLUMN width INTEGER NOT NULL DEFAULT 0 CHECK (width >= 0);
ALTER TABLE screenshots ADD COLUMN height INTEGER NOT NULL DEFAULT 0 CHECK (height >= 0);
ALTER TABLE screenshots ADD COLUMN byte_size INTEGER NOT NULL DEFAULT 0 CHECK (byte_size >= 0);
ALTER TABLE screenshots ADD COLUMN sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE screenshots ADD COLUMN client_captured_at TEXT;
ALTER TABLE screenshots ADD COLUMN state TEXT NOT NULL DEFAULT 'ready' CHECK (state IN ('ready','deleting'));
CREATE TABLE screenshot_retention (
    exam_id TEXT PRIMARY KEY NOT NULL REFERENCES exams(exam_id),
    purged_at TEXT NOT NULL
);
CREATE TABLE screenshot_store (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    store_id TEXT NOT NULL
);
INSERT INTO screenshot_store(id,store_id) VALUES(1,lower(hex(randomblob(16))));
CREATE INDEX screenshots_session_state ON screenshots(session_id,state);
