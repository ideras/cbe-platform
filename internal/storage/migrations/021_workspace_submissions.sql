-- Phase 5 of workspace exams: submissions, terminal transcripts, and a snapshot reason for a session that ended before its time.

-- A snapshot now also says why when the student ended the exam (or a proctor did): 'final', beside 'deadline' for time that ran out.
-- SQLite cannot change a CHECK, so the table is made again with the rows it had.
CREATE TABLE workspace_snapshots_new (
    snapshot_id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    reason TEXT NOT NULL CHECK (reason IN ('periodic', 'submit', 'agent_loss', 'deadline', 'final')),
    taken_at TEXT NOT NULL,
    manifest_hash TEXT NOT NULL,
    files INTEGER NOT NULL,
    bytes INTEGER NOT NULL,
    skipped INTEGER NOT NULL DEFAULT 0
);
INSERT INTO workspace_snapshots_new(snapshot_id, session_id, reason, taken_at, manifest_hash, files, bytes, skipped)
    SELECT snapshot_id, session_id, reason, taken_at, manifest_hash, files, bytes, skipped FROM workspace_snapshots;
DROP TABLE workspace_snapshots;
ALTER TABLE workspace_snapshots_new RENAME TO workspace_snapshots;
CREATE INDEX workspace_snapshots_session ON workspace_snapshots(session_id, snapshot_id);

-- A submission is the part of a snapshot the exam submits (its `submit` and `ignore` lists applied): a manifest of its own, which names
-- the snapshot's blobs. kind 'submit' is the student pressing Submit; 'final' is the one made when the exam ends, by the student's
-- choice, by a proctor, or by time. The latest submission of a session is the one that counts.
CREATE TABLE workspace_submissions (
    submission_id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    snapshot_id INTEGER NOT NULL REFERENCES workspace_snapshots(snapshot_id),
    kind TEXT NOT NULL CHECK (kind IN ('submit', 'final')),
    submitted_at TEXT NOT NULL,
    manifest_hash TEXT NOT NULL,
    files INTEGER NOT NULL,
    bytes INTEGER NOT NULL
);
CREATE INDEX workspace_submissions_session ON workspace_submissions(session_id, submission_id);

-- One row for each terminal session of a student (what the relay saw open, and what the recorder wrote to its transcript, an
-- asciinema v2 file under the workspace folder).
CREATE TABLE workspace_terminals (
    terminal_id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
    term INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('shell', 'run', 'check', 'program')),
    started_at TEXT NOT NULL,
    ended_at TEXT,
    exit_code INTEGER,
    bytes INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0,
    cast_file TEXT NOT NULL
);
CREATE INDEX workspace_terminals_session ON workspace_terminals(session_id, terminal_id);
