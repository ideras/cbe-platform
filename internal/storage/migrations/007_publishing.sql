-- The exam new students are admitted to, chosen at runtime by the proctor. No row means admission is closed.
CREATE TABLE current_exam (
    singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
    exam_id TEXT NOT NULL REFERENCES exams(exam_id),
    published_at TEXT NOT NULL
);
