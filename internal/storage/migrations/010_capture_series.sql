-- An exam may set its own sustained-unfocus threshold (0 uses the server's -focus-threshold); each session keeps the one it
-- started with.
ALTER TABLE exams ADD COLUMN focus_threshold_seconds INTEGER NOT NULL DEFAULT 0 CHECK (focus_threshold_seconds BETWEEN 0 AND 600);
ALTER TABLE sessions ADD COLUMN focus_threshold_ms INTEGER NOT NULL DEFAULT 0 CHECK (focus_threshold_ms >= 0);

-- A sustained focus loss is captured repeatedly, so a focus-loss event holds a series of screenshots numbered by
-- capture_index, instead of at most one. Stored images are canonical JPEG from now on; earlier ones stay PNG.
CREATE TABLE screenshots_series (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    event_log_id INTEGER NOT NULL,
    capture_index INTEGER NOT NULL DEFAULT 0 CHECK (capture_index >= 0),
    file_path TEXT NOT NULL UNIQUE,
    content_type TEXT NOT NULL DEFAULT 'image/png' CHECK (content_type IN ('image/png', 'image/jpeg')),
    captured_at TEXT NOT NULL,
    width INTEGER NOT NULL DEFAULT 0 CHECK (width >= 0),
    height INTEGER NOT NULL DEFAULT 0 CHECK (height >= 0),
    byte_size INTEGER NOT NULL DEFAULT 0 CHECK (byte_size >= 0),
    sha256 TEXT NOT NULL DEFAULT '',
    client_captured_at TEXT,
    state TEXT NOT NULL DEFAULT 'ready' CHECK (state IN ('ready','deleting')),
    UNIQUE (event_log_id, capture_index),
    FOREIGN KEY (session_id, event_log_id) REFERENCES event_log(session_id, id)
);
INSERT INTO screenshots_series(id, session_id, event_log_id, capture_index, file_path, content_type, captured_at, width, height, byte_size, sha256, client_captured_at, state)
SELECT id, session_id, event_log_id, 0, file_path, 'image/png', captured_at, width, height, byte_size, sha256, client_captured_at, state FROM screenshots;
DROP TABLE screenshots;
ALTER TABLE screenshots_series RENAME TO screenshots;
CREATE INDEX screenshots_session_state ON screenshots(session_id, state);
CREATE INDEX screenshots_event ON screenshots(event_log_id, capture_index);
