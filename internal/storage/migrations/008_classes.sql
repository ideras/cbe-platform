-- Classes are named rosters kept on the server. A student can belong to several classes; an exam is published to one class,
-- and only that class's students can log in to it. students keeps one row per person so sessions and history are untouched.
CREATE TABLE classes (
    class_id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX classes_name ON classes(name COLLATE NOCASE);

CREATE TABLE class_students (
    class_id TEXT NOT NULL REFERENCES classes(class_id),
    account_number TEXT NOT NULL REFERENCES students(account_number),
    PRIMARY KEY (class_id, account_number)
) WITHOUT ROWID;
CREATE INDEX class_students_account ON class_students(account_number);

-- Every class an exam has been published to, with when it was last published there; the students of these classes who
-- have no session are "not logged in yet".
CREATE TABLE exam_classes (
    exam_id TEXT NOT NULL REFERENCES exams(exam_id),
    class_id TEXT NOT NULL REFERENCES classes(class_id),
    published_at TEXT NOT NULL,
    PRIMARY KEY (exam_id, class_id)
) WITHOUT ROWID;

-- The roster that used to be global becomes the first class.
INSERT INTO classes(class_id, name, created_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
             substr('89ab', 1 + abs(random()) % 4, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))),
       'Imported roster', strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'
WHERE EXISTS (SELECT 1 FROM students WHERE roster_active = 1);

INSERT INTO class_students(class_id, account_number)
SELECT (SELECT class_id FROM classes), account_number FROM students WHERE roster_active = 1;

-- The published exam now names its class. With no roster nobody could log in, so such a publication is dropped.
CREATE TABLE current_exam_next (
    singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
    exam_id TEXT NOT NULL REFERENCES exams(exam_id),
    class_id TEXT NOT NULL REFERENCES classes(class_id),
    published_at TEXT NOT NULL
);
INSERT INTO current_exam_next(singleton, exam_id, class_id, published_at)
SELECT 1, exam_id, (SELECT class_id FROM classes), published_at FROM current_exam WHERE EXISTS (SELECT 1 FROM classes);
DROP TABLE current_exam;
ALTER TABLE current_exam_next RENAME TO current_exam;

INSERT INTO exam_classes(exam_id, class_id, published_at)
SELECT e.exam_id, (SELECT class_id FROM classes), e.created_at FROM exams e
WHERE EXISTS (SELECT 1 FROM classes)
  AND (e.exam_id IN (SELECT exam_id FROM sessions) OR e.exam_id IN (SELECT exam_id FROM current_exam));

ALTER TABLE students DROP COLUMN roster_active;
