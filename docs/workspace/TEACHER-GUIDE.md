# Teacher guide: workspace exams

For the person who writes a lab, runs it on exam day and reads what the students did. It says what to do and in what order; the reference for
each part is linked where it comes up, and nothing here has to be read twice.

A **workspace exam** gives every student a project, an editor and a terminal inside a container on the exam server. They work on a TODO list;
the platform records what they did; you can run your hidden tests on what they submitted and read the result. A **question exam** (a Markdown
file with answer fields) is a different kind of exam and works as it always did; the two are never mixed.

## What you do, in order

| When | What | Where |
|---|---|---|
| Weeks before | Write the package (`workspace.md`, `project/`, `grading/`, maybe `image/`) and check it with `build/examc` | [Writing a package](#writing-a-package) |
| Weeks before | Make sure the course image has what the lab needs, or add it to the exam | [Course images](#course-images-and-an-exams-own-image) |
| A day before | Import the exam, **build its image** if it has one, try the grading on your own solution | [The day before](#the-day-before) |
| The exam | Publish, watch the **Workspace** column, answer students | [During the exam](#during-the-exam) |
| After | Grade the class, read what each student did, confirm the grades, download | [After the exam](#after-the-exam) |

## Writing a package

A package is a folder:

```text
lab-listas/
  workspace.md      the title, the time, the settings, the instructions and the TODO list
  project/          the starter files every student gets in /work
  grading/          your hidden tests (optional): an executable `grade`; students never receive it
  image/            the exam's own image (optional): a Containerfile
```

The full specification, with every rule the compiler enforces, is [`docs/workspace-format.md`](../workspace-format.md); a complete example is
[`testdata/workspace/lab-listas/`](../../testdata/workspace/lab-listas). The parts that matter most:

- **`workspace.md`** starts with a front matter block (`title`, `duration` and `image` are required), then the instructions, then the TODO items:

  ```markdown
  ---
  title: Laboratorio 2 – Listas enlazadas
  duration: 2h
  grace: 2m
  image: cbe/cpp20
  run: make run
  check: make check
  submit: [src/**, include/**, tests/**, Makefile]
  points: 40
  ---

  Build with `make`. Run the sample tests with `make check`.

  [ ] Implement `push_front` in `src/list.cpp` {#t1 points=10}
  [ ] Implement `insert_sorted` {#t2 points=15}
  ```

- **TODO items** are the lines that start with `[ ]`. Each is one line. `{#t1 points=10}` gives it an ID (your `grade` program reports under that
  ID) and its points. **A tick is the student's own word:** nothing checks it. The platform says so wherever it shows ticks.
- **`run` and `check`** are the commands behind the **Run** and **Check** buttons (students also have a terminal, unless you write `terminal: off`).
  Put in `check` what you want the student to see: it is the sample of your tests, not the hidden ones.
- **`submit` and `ignore`** say which files are the submission. The default is everything in `/work` except build products. Name what the
  grading needs (`src/**`, `tests/**`, the `Makefile`) so a student cannot lose a mark by leaving a file out of it.
- **`resources`** (`cpu=1, memory=1g, disk=100m` by default). Ask for what the project needs and no more: **the server keeps the memory limits
  of all the students' containers under a budget** (a class of 40 at 1 GiB fits a machine of 64 GiB; a class of 40 at 2 GiB does not). A student
  whose container cannot be made because the budget is used is told the workspace failed and the proctor sees why.
- **No secrets in `project/`.** Everything in it goes to every student. The solution, the rubric and the hidden tests go in `grading/` only.
- **Check it before you hand it in:** `build/examc lab-listas/` lists every problem at once. A package that compiles can be imported.

### The `grade` contract

`grading/grade` is a script or a program. For a student it runs in a **fresh container with no network**, with their submission in `/work` and your
`grading/` folder, read-only, at `/grading`. It must:

1. print **one JSON object and nothing else on standard output**, keyed by TODO ID: `{"t1": {"passed": true, "points": 10, "log": "4/4 tests passed"}, "t2": {"passed": false, "points": 6, "log": "3/5"}}`
   (`passed` is required; `points` and `log` are optional; partial credit is a `points` between 0 and what the TODO is worth);
2. send everything else (compiler output, diagnostics) to **standard error**: the proctor reads both;
3. **exit with 0** once it has printed. A student's program that crashes is a result to report (`"passed": false, "log": "crashed"`), not a reason to
   fail yourself;
4. end within `grade_timeout` (5 minutes unless the exam says otherwise).

A minimal `grade` in shell:

```sh
#!/bin/sh
cd /work || exit 1
if make >&2 && ./build/app --check >&2; then
  echo '{"t1": {"passed": true, "log": "builds and passes the checks"}}'
else
  echo '{"t1": {"passed": false, "log": "does not build or fails the checks"}}'
fi
```

The example [`testdata/workspace/lab-listas/grading/grade`](../../testdata/workspace/lab-listas/grading) builds the student's code with a hidden driver
and gives partial credit per operation. Two things to know about it: **the student's code runs in the same container as `grade`, so it can read
`/grading`** (grading measures; it is not a lock), and **`/tmp` cannot run programs** (build into `/work`). More in [grading.md](grading.md).

**Try your `grade` on your own solution and on a bad submission before the exam**, the way the platform will run it: import the exam in a test
class, submit as a student, grade ([The day before](#the-day-before)).

## Course images and an exam's own image

A workspace runs in a **course image**: one image for the course, with the compiler and tools (`cbe/cpp20`: g++ for C++20, make, cmake, gdb, flex, bison).
The person who runs the server builds and approves course images; an exam names one in `image:`.

- **The tool you need is not in the image, and others will want it too:** add a course image. Copy `images/cpp20/` to `images/<course>/`, change the
  packages and the labels, keep the user, the agent and the end of the Containerfile as they are, build it, and run `images/test.sh`. The steps and
  the contract every image keeps are in [`images/README.md`](../../images/README.md). The server's owner approves it (the runner's list).
- **Only this exam needs a library or a file:** give the package an `image/` folder with a `Containerfile` that starts `FROM cbe/cpp20`, install
  what you need, and end as `USER 1000:1000`. The proctor builds it before publishing (the **Image** button on the exam), and the platform checks
  that it is still a course image (same user, agent and entrypoint). The exam **cannot be published until its image is built.** Only this exam's
  students and its grading containers get it. Rules, an example and the checks: [images.md](images.md).
- **The network is off for students and graders always**, whatever the image was built with. A student cannot install anything during the exam;
  if they might need a library, put it in the image.
- Pin versions in an image you write (`apt-get install name=version`, `pip install name==version`): the build warns about a package with no version, because a
  rebuild next year would not be the same image. **The warnings are on by default.** An exam that does not want them (it installs from a source it trusts, or it is a
  quick lab) says `image_warnings: off` in the front matter of `workspace.md`: `build/examc`, the import preview and the build log then do not mention them. A Containerfile that
  breaks a rule is still refused; only the warnings go.

## The day before

In the proctor application, open the class and use **Import workspace exam…**:

1. Choose the package's `workspace.md`. The application compiles the folder and **shows what it understood** (title, time, TODO items, the starter
   files, the hidden tests, the limits, whether it has its own image, with its warnings) before anything is stored. A package with problems lists them all and cannot be imported.
2. Choose the **PIN**, as for any exam. Import. The exam is imported, **not** published.
3. If the exam has its own image, press **Image**, then **Build the image**. You can watch the log as it grows: the step the build is at, the command, and when it
   last printed. A first build that installs packages takes minutes; the same files built again are instant (cached).
4. **Try it as a student**, in a test class with your own account: log in, work, **Save**, finish. Then, as proctor, open the student's **Review**
   window: the Files tab shows what was submitted, **Grade this student** runs `grade`, and the Grading tab shows each TODO. If your own solution does not score
   what you expect, fix `grade` and import again (it is a new exam); do not discover it with forty students waiting.
5. Check the machine: **how many students, how much memory each** (`resources`), against what the server's owner has (the runner's `host_memory_mb`).
   The load test (`docs/workspace/load-test.md`) is what the platform was measured with: 40 students, each building every few seconds, on a machine of
   12 threads and 64 GiB.
6. Tell students what the platform records: what they type and save, their terminals, their TODO ticks and every paste it blocked (a paste into the editor or the
   terminal is refused and noted). Their own notice is `docs/tools/STUDENT-NOTICE.md`.

## During the exam

- **Publish** the exam when you are ready (it is disabled, with the reason, until the image is built). Students log in with their account and the PIN; a
  workspace is made for each of them as they log in (in the load test, 40 logins over 30 seconds each had theirs in under a second), and the IDE opens when theirs is up.
- The monitoring table gets a **Workspace** column. Per student: the container's state (Starting, Running, Paused, Frozen, Failed, Removed), whether they are
  connected, when they last did anything, the container's CPU and memory, the TODO ticks ("2 of 3 done (self-reported)"), how many times they pressed
  **Save**, and how many times the workspace restarted. The reference for each line is [proctor-view.md](proctor-view.md).
- **A student says "my workspace does not open":** look at their badge. *Starting* for more than a minute, or *Failed*, is yours to read: open their **Review** window and the
  **Container** row says why (the exam's image not built, the host's memory budget used, the runtime unavailable). They try again by logging in again; a
  workspace that failed is made again. Their files are never lost by this: they live on the server's disk, and a restarted workspace has them.
- **A student is disconnected** (Wi-Fi): their container keeps running, and their work stays. When they come back, the workspace is as they left it. The
  platform snapshots a student's files every 60 seconds (or the `snapshot` of the exam), so even a lost machine loses at most that.
- **Focus loss, screenshots and the lock** work as in a question exam. A locked student's workspace is taken away from the window until you unlock them.
- **Extra time** for one student works as in a question exam; their container is frozen when *their* time and grace are over.
- At the deadline plus the grace, every workspace is **frozen** after a final snapshot of its files: that snapshot is the final submission.

## After the exam

1. **Grade.** In the monitoring window press **Grade submissions…**: it says how many will be graded and queues one run for each (two at a time).
   A run takes about as long as your `grade` does. The **Grading** line counts the class: graded, waiting, failed, submitted and not graded.
   A **failed** run says why (it printed something that is not the JSON, it took too long, it ran out of memory, the runner could not run it) and shows what it printed;
   fix `grade` or ask again for that student, and the others are not affected.
2. **Read the review window of any student.** Four tabs:
   - **History**: what they did, in words: files saved with the size change, a terminal opened, a TODO ticked, a paste blocked, the final submission.
   - **Files**: what they had (the newest snapshot), or what they submitted (each submission), as a read-only tree. A file that is binary or over 1 MiB is named and left for the zip.
   - **Terminals**: each terminal they had, and its **replay** as it printed, with the pauses shortened, pausable and seekable.
   - **Grading**: their runs, each TODO as *Passed* / *Not passed* with points and your `log`, and what `grade` printed.
3. **Confirm the grade.** A grade is **advisory** until you press **Confirm this grade** on a run; it is then the student's grade, and it takes the confirmation from any other run of theirs. Take it back with **Take the confirmation back**. A TODO tick is never part of the grade.
4. **Download the grades** as a spreadsheet file: in the monitoring window, **Download grades…** beside *Grade submissions…* (or the download button of the exam in the
   exam list). You choose where to save it. It is a CSV (open it with Excel, LibreOffice or Google Sheets, as UTF-8) with **a line for every student of the class**, also one who never logged in:

   | Column | What it has |
   |---|---|
   | `account_number`, `name`, `email` | the student |
   | `status` | `confirmed`, `advisory`, `not graded`, `no submission`, `not logged in`, `waiting to be graded`, `being graded`, or `grading failed: <why>` |
   | `points` | **the confirmed grade**, and nothing else |
   | `advisory_points` | the points of the latest run of a student whose grade you have **not** confirmed |
   | `max_points` | what the TODO items are worth together |
   | one column for each TODO, such as `t1 (10)` | the points that TODO got in the same run as `points` or `advisory_points` |
   | `todos_ticked_by_student` | how many TODO items the student says they did (nothing checks it) |
   | `submitted`, `final_submission`, `last_submission_at`, `graded_at` | when they pressed Save and when their grade was made (UTC) |

   The two columns of points exist so that a grade you have not confirmed cannot reach the gradebook by mistake; take `points` for the final grades. Excel drops the leading zeros of an
   account number such as `001234` unless the column is imported as text.
5. **Download a student's work** from their review window: the submitted files as a zip, and a Markdown report of the submission.
6. The students' containers are removed after the exam's retention time (48 hours by default); their snapshots and submissions are kept with the exam.

## What the platform does not do

- It does not check a student's ticks, and does not grade by itself: a grade is your hidden tests plus your confirmation.
- It does not stop a student's own program from reading `/grading` while it is being graded (it is a measure, not a lock), nor from fork bombs or filling the
  disk: those are contained (256 processes, a size and file-count limit on `/work`, memory) and cannot reach another student.
- It does not give students the network, a package manager or a debugger interface; the image has `gdb` in the terminal.
- It does not keep any workspace file on the student's own computer.

## When something is wrong

| You see | It means | What to do |
|---|---|---|
| Import says the package has problems | The compiler found a rule broken; it lists all of them | Fix them in the package; `build/examc` shows the same list |
| **Publish** is off, "image not built" | The exam has an `image/` and no good build | **Image → Build the image**; read the log if it fails |
| Build failed: "the image is not the course image" | The Containerfile changed the user, the entrypoint or the agent | Make it end with `USER 1000:1000` and keep the course image's `ENTRYPOINT`; [images.md](images.md) |
| A student's badge is *Failed* | Their workspace could not be made | Open their Review window: the Container row has the reason; most often the memory budget or the image; ask them to log in again |
| A graded run says *malformed* | `grade` printed something besides the JSON on standard output | Send diagnostics to standard error |
| A graded run says *timeout* | `grade` did not end in `grade_timeout` | Make it faster, or set `grade_timeout:` in `workspace.md` |
| A run says *crash* with 137 | Memory: the container's limit was reached | Raise `resources: memory=` for the exam (within the server's maximum) |
| "Workspace restarted" counts go up for a student | Their container's agent was lost and started again; files are kept | Nothing; it is noted in their history |

## Limits to plan with

| What | Value |
|---|---|
| A package | 20 MiB and 2,000 files for `project/` and for `grading/` |
| TODO items | 1 to 100 |
| Students' containers at once | the host's memory budget (`host_memory_mb`) divided by the exam's memory limit |
| Terminals per student | 4 |
| One file in the editor | 1 MiB (text) |
| Autosave | 18 ms at the 95th percentile and 99 ms at the 99th, with 40 students all compiling, measured on the exam server's own machine, and 11 ms and 24 ms from another laptop over the Wi-Fi router (the plan's target is 300 ms); [load-test.md](load-test.md) |

## Where to read more

[workspace-format.md](../workspace-format.md) (every rule of a package) · [grading.md](grading.md) · [images.md](images.md) ·
[proctor-view.md](proctor-view.md) · [student-ide.md](student-ide.md) · [deployment.md](deployment.md) (for the person who installs the server) · [`images/README.md`](../../images/README.md)
