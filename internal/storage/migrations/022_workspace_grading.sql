-- Phase 6 of workspace exams: grading. A grading is one run of the exam's hidden tests (grading/grade, in a fresh container) against one
-- submission; a student may be graded again and again, and every run is kept. What the run printed is kept (cut at the runner's limit) so
-- the proctor can read why a run failed. A grade is advisory until the proctor confirms it: confirmed_at is set on one grading of a
-- session at a time.
CREATE TABLE workspace_gradings (
    grading_id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    submission_id INTEGER NOT NULL REFERENCES workspace_submissions(submission_id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'done', 'failed')),
    -- failure is a short code (malformed, timeout, crash, oom, runner, interrupted) and failure_text says it in words; both are empty unless
    -- the run failed.
    failure TEXT NOT NULL DEFAULT '',
    failure_text TEXT NOT NULL DEFAULT '',
    queued_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    exit_code INTEGER,
    timed_out INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    stdout TEXT NOT NULL DEFAULT '',
    stderr TEXT NOT NULL DEFAULT '',
    stdout_truncated INTEGER NOT NULL DEFAULT 0,
    stderr_truncated INTEGER NOT NULL DEFAULT 0,
    -- skipped is a JSON array of the names of the submission's entries the container was not given.
    skipped TEXT NOT NULL DEFAULT '[]',
    points REAL,
    max_points REAL,
    confirmed_at TEXT
);
CREATE INDEX workspace_gradings_session ON workspace_gradings(session_id, grading_id);

-- What a done run said of each TODO. known is 0 for a key of the output that is not a TODO of the exam: it is shown, and not counted.
CREATE TABLE workspace_grading_results (
    grading_id INTEGER NOT NULL REFERENCES workspace_gradings(grading_id) ON DELETE CASCADE,
    todo_id TEXT NOT NULL,
    known INTEGER NOT NULL,
    passed INTEGER NOT NULL,
    points REAL NOT NULL,
    log TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (grading_id, todo_id)
);
