-- An exam is imported into a class and belongs to it for good; publishing no longer picks a class. exams is referenced by
-- sessions, screenshot_retention and current_exam, so it cannot be rebuilt inside this transaction: the column is added
-- instead (SQLite only allows a nullable one) and the triggers below stand in for NOT NULL.
ALTER TABLE exams ADD COLUMN class_id TEXT REFERENCES classes(class_id);

-- The published exam keeps the class it admits; any other exam keeps the class it was published to last.
UPDATE exams SET class_id = COALESCE(
    (SELECT ce.class_id FROM current_exam ce WHERE ce.exam_id = exams.exam_id),
    (SELECT x.class_id FROM exam_classes x WHERE x.exam_id = exams.exam_id ORDER BY x.published_at DESC, x.class_id LIMIT 1));

-- 008_classes.sql recorded a class for every exam with sessions as long as a class existed. If the roster was empty back
-- then, such an exam keeps the people who took it, in a class of its own, so no history is lost.
INSERT INTO classes(class_id, name, created_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
             substr('89ab', 1 + abs(random()) % 4, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
       'Imported exams', strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'
WHERE EXISTS (SELECT 1 FROM exams WHERE class_id IS NULL AND exam_id IN (SELECT exam_id FROM sessions))
  AND NOT EXISTS (SELECT 1 FROM classes WHERE name = 'Imported exams' COLLATE NOCASE);

INSERT OR IGNORE INTO class_students(class_id, account_number)
SELECT (SELECT class_id FROM classes WHERE name = 'Imported exams' COLLATE NOCASE), s.account_number
FROM sessions s JOIN exams e ON e.exam_id = s.exam_id WHERE e.class_id IS NULL;

UPDATE exams SET class_id = (SELECT class_id FROM classes WHERE name = 'Imported exams' COLLATE NOCASE)
WHERE class_id IS NULL AND exam_id IN (SELECT exam_id FROM sessions);

-- What is left was never published and never taken.
DELETE FROM screenshot_retention WHERE exam_id IN (SELECT exam_id FROM exams WHERE class_id IS NULL);
DELETE FROM exams WHERE class_id IS NULL;

CREATE INDEX exams_class ON exams(class_id);

CREATE TRIGGER exams_class_required_on_insert
BEFORE INSERT ON exams WHEN NEW.class_id IS NULL BEGIN
    SELECT RAISE(ABORT, 'an exam must belong to a class');
END;
CREATE TRIGGER exams_class_required_on_update
BEFORE UPDATE OF class_id ON exams WHEN NEW.class_id IS NULL BEGIN
    SELECT RAISE(ABORT, 'an exam must belong to a class');
END;

-- The class of the published exam is the class of the exam, so current_exam no longer stores one.
CREATE TABLE current_exam_next (
    singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
    exam_id TEXT NOT NULL REFERENCES exams(exam_id),
    published_at TEXT NOT NULL
);
INSERT INTO current_exam_next(singleton, exam_id, published_at) SELECT singleton, exam_id, published_at FROM current_exam;
DROP TABLE current_exam;
ALTER TABLE current_exam_next RENAME TO current_exam;

DROP TABLE exam_classes;
