-- The proctor's monitoring settings live on the server, so every proctor computer sees the same values. One row holds them;
-- no row means the built-in defaults.
CREATE TABLE settings (
    singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
    policy_json TEXT NOT NULL CHECK (json_valid(policy_json)),
    updated_at TEXT NOT NULL
);

-- An exam keeps the settings frozen at its latest publish; NULL until it is published for the first time.
ALTER TABLE exams ADD COLUMN policy_json TEXT;

-- A session copies the exam's frozen settings at login: everything about a session follows its own copy.
ALTER TABLE sessions ADD COLUMN policy_json TEXT;

-- A locked session keeps when it was locked and why: "away" (the student was out of the exam window) or "no_contact"
-- (nothing reached the server from the application).
ALTER TABLE sessions ADD COLUMN locked_at TEXT;
ALTER TABLE sessions ADD COLUMN lock_reason TEXT;

-- The per-exam focus threshold is gone: the proctor's Settings is the only place these values live now.
ALTER TABLE exams DROP COLUMN focus_threshold_seconds;
ALTER TABLE sessions DROP COLUMN focus_threshold_ms;
