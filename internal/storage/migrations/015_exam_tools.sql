-- An exam keeps the tools its file declared (the calculator, for now), compiled and validated; NULL means the
-- file named no tools. The defaults are applied when the config is sent to the student, not here.
ALTER TABLE exams ADD COLUMN tools_json TEXT;
