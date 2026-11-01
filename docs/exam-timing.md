# Managing exam time

The exam file's duration starts at each student's first login. Its login window (`opens_at` / `closes_at`) only controls admission; closing admission does not end running sessions.

## Exam end time

Publish optionally accepts **Exam ends at**, or use **Change end time…** in the live roster. Enter HH:MM in the proctor computer's local time. The dialog previews the next occurrence (today or tomorrow) and warns about a server clock difference greater than one minute and shortened durations for late arrivals. The server requires at least one minute of notice and an end within 24 hours.

At that time every unfinished session, including locked sessions, is submitted using its last server-saved answers. There is **no grace after the exam end**. Later logins are refused. The student's own deadline and grace remain stored underneath this cap: moving it later or clearing it restores the unused entitlement, but never reopens submitted sessions. Publishing a different exam does not transfer this cap.

## Extra grace

Use **Extend grace…** on a student's row, or **Extend grace for everyone…** in the roster header. Enter 1–60 minutes and a reason; the latter is recorded in history. The dialog previews affected and skipped sessions, and the server returns the actual counts. Finished sessions are never reopened. An exam-wide extension also applies to students who log in later.

Only the grace end moves, not the normal deadline. The exam end time still caps the effective grace: move that end time as well if you need the entire extension to be usable. Unlock's optional give-back follows the same cap.

The student toolbar stays visible and counts down between server reports. If disconnected, it freezes and says **Timing stale**: only the server determines submission. Grace, low-time warnings and instructor time changes appear as toasts. During a network gap the server can only submit the last answers it successfully saved.
