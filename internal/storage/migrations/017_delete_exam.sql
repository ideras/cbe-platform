-- An exam can be deleted together with the sessions students had in it. That deletion is one transaction and it is the only
-- way answer versions may ever be deleted: the transaction first lists the exam in exam_being_deleted, which the guard below
-- looks for, and unlists it before it commits. Any other attempt to delete a version still aborts, as before.
CREATE TABLE exam_being_deleted (
    exam_id TEXT PRIMARY KEY NOT NULL
);
DROP TRIGGER submission_versions_no_delete;
CREATE TRIGGER submission_versions_no_delete
BEFORE DELETE ON submission_versions
WHEN NOT EXISTS (SELECT 1 FROM sessions s JOIN exam_being_deleted d ON d.exam_id = s.exam_id WHERE s.session_id = OLD.session_id)
BEGIN
    SELECT RAISE(ABORT, 'answer versions are append-only');
END;

-- Deleting an exam deletes its sessions, answers, events and screenshot records, but the screenshot files those records named
-- cannot be removed inside the transaction. Each path waits here until its file is gone, so a crash or a file that cannot be
-- removed leaves nothing behind for good: the server removes what is listed here when it starts and every minute after.
CREATE TABLE deleted_screenshot_files (
    file_path TEXT PRIMARY KEY NOT NULL
);
