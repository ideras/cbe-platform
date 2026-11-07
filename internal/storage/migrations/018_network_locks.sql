-- A student whose connection to the server is lost may lock the exam themselves to reconnect to the exam network; only a
-- proctor unlocks it. The session counts those locks, so the limit holds even when the application is restarted.
ALTER TABLE sessions ADD COLUMN network_locks INTEGER NOT NULL DEFAULT 0;
