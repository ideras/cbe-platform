-- The proctor compiles an exam Markdown file and sends the compiled document; the source is kept too, as opaque text, so
-- downloaded submissions can be the exam itself with the students' answers filled in. It is NULL for exams imported before
-- this change and for API imports without a source; nothing parses it on the server.
ALTER TABLE exams ADD COLUMN source_markdown TEXT;
