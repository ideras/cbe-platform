CREATE TABLE students (
    account_number TEXT PRIMARY KEY NOT NULL CHECK (length(trim(account_number)) > 0),
    canvas_id TEXT NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    sis_user_id TEXT NOT NULL,
    roster_active INTEGER NOT NULL DEFAULT 1 CHECK (roster_active IN (0, 1))
);

CREATE TABLE exams (
    exam_id TEXT PRIMARY KEY NOT NULL,
    title TEXT NOT NULL,
    pin TEXT NOT NULL,
    duration_seconds INTEGER NOT NULL CHECK (duration_seconds > 0),
    grace_seconds INTEGER NOT NULL DEFAULT 30 CHECK (grace_seconds > 0),
    questions_json BLOB NOT NULL CHECK (typeof(questions_json) = 'blob' AND json_valid(questions_json, 8)),
    created_at TEXT NOT NULL,
    opens_at TEXT,
    closes_at TEXT
);

CREATE TABLE sessions (
    session_id TEXT PRIMARY KEY NOT NULL,
    account_number TEXT NOT NULL REFERENCES students(account_number),
    exam_id TEXT NOT NULL REFERENCES exams(exam_id),
    token TEXT NOT NULL UNIQUE,
    started_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    grace_until TEXT NOT NULL,
    last_seen TEXT,
    submitted_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'grace', 'submitted', 'force_submitted')),
    final_version_id INTEGER,
    UNIQUE (account_number, exam_id),
    FOREIGN KEY (session_id, final_version_id) REFERENCES submission_versions(session_id, id),
    CHECK (
        (status IN ('active', 'grace') AND submitted_at IS NULL AND final_version_id IS NULL)
        OR (status IN ('submitted', 'force_submitted') AND submitted_at IS NOT NULL AND final_version_id IS NOT NULL)
    )
);

CREATE TABLE submission_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    version_seq INTEGER NOT NULL CHECK (version_seq > 0),
    answers_json BLOB NOT NULL CHECK (typeof(answers_json) = 'blob' AND json_valid(answers_json, 8)),
    saved_at TEXT NOT NULL,
    UNIQUE (session_id, version_seq),
    UNIQUE (session_id, id)
);

CREATE TABLE event_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    event_type TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    detail TEXT CHECK (detail IS NULL OR json_valid(detail)),
    UNIQUE (session_id, id)
);
CREATE INDEX event_log_session_occurred ON event_log(session_id, occurred_at);

CREATE TABLE screenshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    event_log_id INTEGER NOT NULL UNIQUE,
    file_path TEXT NOT NULL UNIQUE,
    captured_at TEXT NOT NULL,
    FOREIGN KEY (session_id, event_log_id) REFERENCES event_log(session_id, id)
);

CREATE TRIGGER submission_versions_no_update
BEFORE UPDATE ON submission_versions BEGIN
    SELECT RAISE(ABORT, 'answer versions are append-only');
END;
CREATE TRIGGER submission_versions_no_delete
BEFORE DELETE ON submission_versions BEGIN
    SELECT RAISE(ABORT, 'answer versions are append-only');
END;
