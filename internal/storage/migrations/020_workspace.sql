-- Workspace exams (docs/workspace/): an exam is either a question exam, as every exam so far, or a workspace exam, where each student
-- works on a project in a container. The question tables are untouched; a workspace exam is a row of exams too (with no exercises), so
-- classes, PINs, publishing, sessions, timing and deleting work for it as they do for any exam.
ALTER TABLE exams ADD COLUMN kind TEXT NOT NULL DEFAULT 'question' CHECK (kind IN ('question', 'workspace'));

-- What the exam server holds of an imported package: the compiled document (without the bundles, which are files named by hash) and
-- the hashes of the two bundles. The grading bundle never leaves the server.
CREATE TABLE workspace_exams (
    exam_id TEXT PRIMARY KEY NOT NULL REFERENCES exams(exam_id) ON DELETE CASCADE,
    document_json TEXT NOT NULL CHECK (json_valid(document_json)),
    image TEXT NOT NULL,
    starter_hash TEXT NOT NULL,
    grading_hash TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- One row for each student session of a workspace exam: where its container is in its life, and what the student has ticked.
-- state: new, provisioning, ready, frozen (paused at the deadline), removed (container and folder gone; snapshots stay), failed.
CREATE TABLE workspace_sessions (
    session_id TEXT PRIMARY KEY NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    exam_id TEXT NOT NULL REFERENCES exams(exam_id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'new' CHECK (state IN ('new', 'provisioning', 'ready', 'frozen', 'removed', 'failed')),
    created_at TEXT NOT NULL,
    ready_at TEXT,
    frozen_at TEXT,
    agent_restarts INTEGER NOT NULL DEFAULT 0,
    last_snapshot_at TEXT,
    failure TEXT NOT NULL DEFAULT '',
    todo_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(todo_json))
);
CREATE INDEX workspace_sessions_state ON workspace_sessions(state);

-- A snapshot is a manifest (a file named by its hash, listing every path with the hash of its content) and the file blobs it names,
-- which are shared between snapshots. reason: periodic, submit, agent_loss, deadline.
CREATE TABLE workspace_snapshots (
    snapshot_id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    reason TEXT NOT NULL CHECK (reason IN ('periodic', 'submit', 'agent_loss', 'deadline')),
    taken_at TEXT NOT NULL,
    manifest_hash TEXT NOT NULL,
    files INTEGER NOT NULL,
    bytes INTEGER NOT NULL,
    skipped INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX workspace_snapshots_session ON workspace_snapshots(session_id, snapshot_id);
