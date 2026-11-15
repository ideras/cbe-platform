-- Phase 7 of workspace exams: an exam's own image. A package may hold image/, the build context of a Containerfile that starts from the
-- exam's course image; its bundle is kept like the starter and the grading bundle, by its hash. The proctor builds the image before
-- publishing, and each build is a row: queued, running, then ok or failed, with the log, so the proctor can read why a build failed and
-- which base it was made on. The exam's image is the latest ok build of its current bundle.
ALTER TABLE workspace_exams ADD COLUMN image_hash TEXT NOT NULL DEFAULT '';

CREATE TABLE workspace_image_builds (
    build_id INTEGER PRIMARY KEY AUTOINCREMENT,
    exam_id TEXT NOT NULL REFERENCES exams(exam_id) ON DELETE CASCADE,
    -- bundle_hash is the image bundle this build was made from, so a build is never taken for the image of other files.
    bundle_hash TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'ok', 'failed')),
    rebuild INTEGER NOT NULL DEFAULT 0,
    -- failure is a short code (containerfile, base, build, timeout, verify, runner, interrupted) and message says it in words; both are
    -- empty unless the build failed.
    failure TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    -- cached says nothing was built: the runner had the image already.
    cached INTEGER NOT NULL DEFAULT 0,
    -- hash is the runner's name of the build (16 hexadecimal digits), image_id the runtime's ID of the image and base_digest the digest of
    -- the course image it was built on.
    hash TEXT NOT NULL DEFAULT '',
    image_id TEXT NOT NULL DEFAULT '',
    base_name TEXT NOT NULL DEFAULT '',
    base_digest TEXT NOT NULL DEFAULT '',
    warnings TEXT NOT NULL DEFAULT '[]',
    log TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    queued_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT
);
CREATE INDEX workspace_image_builds_exam ON workspace_image_builds(exam_id, build_id);
