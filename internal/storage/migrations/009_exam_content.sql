-- Exams are compiled from Markdown into exercises, items and answer slots (shared/examdoc); the flat question list is gone.
-- Only development databases ever held exams in the old shape; they are emptied rather than converted.
ALTER TABLE exams RENAME COLUMN questions_json TO content_json;
UPDATE exams SET content_json = jsonb('{"exercises":[]}') WHERE json_type(content_json) <> 'object';
ALTER TABLE exams ADD COLUMN navigation TEXT NOT NULL DEFAULT 'free' CHECK (navigation IN ('free', 'forward_only'));

-- The exercise a student of a forward-only exam is on. It only ever grows; answers to earlier exercises are frozen.
ALTER TABLE sessions ADD COLUMN current_exercise INTEGER NOT NULL DEFAULT 0 CHECK (current_exercise >= 0);
