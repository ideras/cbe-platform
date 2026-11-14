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

A second paragraph, for an exam where students work in the platform's workspace (an editor and a terminal, D4).
It is longer than the calculator's because more is recorded, and every item in it is something the platform
really keeps (see `docs/workspace/student-ide.md` and `docs/workspace/reports/phase-5.md`). **The owner reviews
this wording before any exam uses it.** If the platform records more, or less, than this says, the wording is
wrong and must change with it, not the other way round.

---

**Your workspace is recorded.** In this exam your files live on the exam server, not on your computer, and
your instructor can read what you did there. The platform keeps:

- **Your files.** Copies of everything in your workspace are taken about every minute, when you press
  Submit, and at the end of the exam, and they are kept with the exam. Your submission is the files the exam
  asks for (your instructor chose which); the rest, such as programs you compiled, are kept but are not
  part of the submission.
- **What you changed.** Each time you save, create, rename or delete a file, the platform records which file
  and how large it became, with the time. It does not record each key you press.
- **Your terminals.** Everything a terminal shows, with timing, is kept as a recording your instructor can
  replay. It is what appears on the screen: the output of your programs, and also what you type, because the
  terminal shows it as you type it. What you type where nothing is shown, such as a password, is not shown
  and so is not recorded.
- **Your runs.** When you press Run or Check, the platform records that you did and how it ended.
- **Your TODO list.** The ticks you make are recorded. They are what you say you have done; nobody checks
  them for you, and they are not a grade.
- **Pastes that were blocked.** You can copy and paste inside the workspace, between your files and your
  terminals. Pasting, or dropping, text or files that you did not copy there is blocked, and the platform
  records that you tried, where (editor, terminal or elsewhere in the window) and how large the text was. It
  does not record the text.
- **Your connection.** When your workspace opened, when you were disconnected, and when you reconnected.

If you press **Finish exam** or your time runs out, what is in your workspace at that moment is submitted. You
do not need to press Submit for your work to count. Use the workspace the way you would show your work on
paper.

---

The same short version for the top of the exam instructions: *"In this exam your files and your terminal
output are recorded and your instructor can read and replay them. Pastes from outside the workspace are
blocked, and the attempt is recorded."* Like the calculator's notice, none of this can be turned off by a
configuration; an exam without a workspace has none of it.
