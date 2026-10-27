-- A session keeps when a proctor last unlocked it: the absence and no-contact clocks start again from there.
ALTER TABLE sessions ADD COLUMN unlocked_at TEXT;

-- A finished session is never locked: its lock ended with the exam.
UPDATE sessions SET locked_at = NULL, lock_reason = NULL WHERE status NOT IN ('active', 'grace');

-- Every session follows its own copy of the settings, and the published exam keeps the ones it was published with. Rows
-- written before the settings existed get the stored settings, or the built-in defaults when no proctor saved any.
UPDATE sessions SET policy_json = COALESCE((SELECT policy_json FROM settings WHERE singleton = 1),
    '{"heartbeat_interval_ms":15000,"overdue_threshold_ms":30000,"focus_threshold_ms":5000,"alert_interval_ms":2000,"capture_interval_ms":5000,"lock_away_after_ms":300000,"lock_no_contact_after_ms":300000,"default_grace_ms":30000,"incident_captures":60,"session_captures":200}')
WHERE policy_json IS NULL;
UPDATE exams SET policy_json = COALESCE((SELECT policy_json FROM settings WHERE singleton = 1),
    '{"heartbeat_interval_ms":15000,"overdue_threshold_ms":30000,"focus_threshold_ms":5000,"alert_interval_ms":2000,"capture_interval_ms":5000,"lock_away_after_ms":300000,"lock_no_contact_after_ms":300000,"default_grace_ms":30000,"incident_captures":60,"session_captures":200}')
WHERE policy_json IS NULL AND exam_id IN (SELECT exam_id FROM current_exam);
