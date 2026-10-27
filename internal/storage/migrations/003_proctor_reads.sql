CREATE INDEX sessions_exam_id ON sessions(exam_id, session_id);
CREATE INDEX versions_session_timeline ON submission_versions(
    session_id,
    substr(saved_at,1,19)||'.'||substr(rtrim(substr(saved_at,21),'Z')||'000000000',1,9)||'Z',
    id
);
CREATE INDEX events_session_timeline ON event_log(
    session_id,
    substr(occurred_at,1,19)||'.'||substr(rtrim(substr(occurred_at,21),'Z')||'000000000',1,9)||'Z',
    id
);
