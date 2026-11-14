# Student notice: the calculator is recorded

A short paragraph an instructor can paste into the exam instructions (D4). The wording is fixed — because
logging is always on when the calculator is enabled, there is nothing to configure or promise. **The owner
reviews this wording before any exam uses it.**

---

**The calculator keeps a record.** If your exam has a calculator, every expression you type into it and every
result or error it shows — including the ones you delete or redo — is recorded with a timestamp and is part of
your exam record. Your instructor can read this record after the exam. Use the calculator the way you would
show your work on paper.

---

The calculator's own modal says the same in one sentence: *"Every calculation you make is recorded and is part
of your exam."* Neither sentence can be turned off by a configuration; the only way an exam has no record is to
have no calculator.

---

# Student notice: a workspace exam is recorded

A second notice, for an exam where students work in the platform's workspace (an editor and a terminal, D4). The wording is the owner's
(2026-11-14), kept short on purpose: students read it before an exam, and a long notice is a notice nobody reads. Every item in it is
something the platform really keeps (see `docs/workspace/student-ide.md` and `docs/workspace/reports/phase-5.md`). If the platform records
more, or less, than this says, the wording is wrong and must change with it, not the other way round. The button it calls **Save** is the
one that took a snapshot and was called Submit until the owner renamed it (P5-16); the exam's own **Finish exam** is a different button.

---

**Your exam workspace is recorded.** Your files are stored on the exam server, and your instructor can review activity there. The platform keeps:

- **Files:** Snapshots are saved about every minute, when you submit, and when the exam ends. Only the required files are submitted; other files are retained but not graded.
- **File changes:** Saving, creating, renaming, or deleting a file records its name, size, and time—not individual keystrokes.
- **Terminal recordings:** Everything visible in a terminal, including commands and program output, is recorded and can be replayed. Hidden input, such as passwords, is not recorded.
- **Runs and checks:** The platform records when you use Run or Check and their result.
- **TODO list:** Your checkmarks are recorded, but are not verified or graded.
- **Blocked pastes:** You may paste content copied within the workspace. Attempts to paste or drop external content are blocked and recorded by location and size—not by content.
- **Connection events:** Opening, disconnection, and reconnection times are recorded.

When you select **Finish exam** or time expires, the current workspace is submitted automatically. You do not need to press **Save** for your work to count. Treat the workspace as you would your written exam paper.

---

The same short version for the top of the exam instructions: *"In this exam your files and your terminal
output are recorded and your instructor can read and replay them. Pastes from outside the workspace are
blocked, and the attempt is recorded."* Like the calculator's notice, none of this can be turned off by a
configuration; an exam without a workspace has none of it.
