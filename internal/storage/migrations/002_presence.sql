ALTER TABLE sessions ADD COLUMN hostname TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN remote_addr TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN network_gap_since TEXT CHECK (network_gap_since IS NULL OR last_seen IS NOT NULL);

CREATE INDEX event_log_session_type_id ON event_log(session_id, event_type, id);
