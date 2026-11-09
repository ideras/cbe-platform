-- The release of the student application a session last sent a heartbeat from, as it names itself in its User-Agent
-- ("cbe-portal-student/v0.16.0"). Empty until a heartbeat names one: an application older than the field never does.
ALTER TABLE sessions ADD COLUMN app_version TEXT NOT NULL DEFAULT '';
