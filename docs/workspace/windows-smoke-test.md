# Smoke test of a workspace exam on a clean Windows machine

Phase 8, task 6: "Windows build and smoke test on a clean machine". The student application is a Windows program for most of the class, and the IDE is
new code in it (a WebSocket from the Go client, xterm.js and CodeMirror in the WebView2 page, Ctrl keys), so it is tried on a Windows machine that has
nothing of the project on it: no Go, no Node, no repository.

**Who runs it:** the owner, on the Windows VM (the same one that ran the focus-loss script), because the machine and the router are theirs. **How long:** about 25 minutes.
What was already checked without a Windows machine is at the end.

## What you need

| Thing | Where |
|---|---|
| `cbe-portal-student.exe` (this tree, cross-built from Linux, unsigned) | `build/phase8/cbe-portal-student.exe` (built from this tree with `GOOS=windows`, version `v0.16.0-91-g727c6b9-dirty`; your own `build/cbe-portal-student.exe` was not touched); the CI job `windows-2025` builds the same program (`.github/workflows`) |
| A throwaway workspace exam server the VM can reach | on the Linux host: `deploy/workspace/test-server.sh --manual 0.0.0.0:8090` (it prints the PIN, the accounts `0001` to `0003` and the address; Ctrl+C removes everything it made) |
| For the question-exam check, an ordinary exam server | the one you already use, or `make demo-server` |

On the VM: a clean Windows 10 or 11, **WebView2 runtime** present (Windows 11 has it; Windows 10 may need the Evergreen runtime), the exe copied by hand (a shared folder or a
USB stick), and the Windows Firewall as it comes. Start the application with `cbe-portal-student.exe -server http://<the Linux host's address>:8090`.

## The checklist

Mark each with what you saw. **Stop and tell me** at the first thing that is not as written.

**A. It starts and logs in**

1. The window opens with the login page (no error dialog; **a console window may open beside it**: the cross-build from Linux is a console program, and nothing in the build asks Windows for the GUI subsystem; tell me if it is in the way). *Expected:* the usual rules page, then the login.
2. Log in as `0001` with the PIN. *Expected:* the exam's instructions, the TODO list, the file tree with `Makefile`, `include`, `src`, `tests`, and the status in the toolbar goes from
   *Starting the workspace* to *Connected*, within about 10 seconds.

**B. Working**

3. Open `src/list.cpp`, type a few characters. *Expected:* a dot on the tab, then *Saved hh:mm:ss* in the toolbar a moment later. Ctrl+S saves at once.
4. Press **Check**. *Expected:* the terminal panel opens, `make check` runs, the output has colours, and it ends with a failing check (the starter is incomplete).
5. Press **Terminal**, type `ls`, `nano notes.txt` (Ctrl+X to leave), `gdb --version`. *Expected:* it behaves like a terminal: arrows, Backspace, Tab, Ctrl+C all go to the program,
   not to the application. Resize the window: the terminal follows.
6. Ctrl and `+` / `-` / `0` in the editor. *Expected:* the **code** gets larger and smaller and the cursor stays on the line you click (the A+ bug of Phase 5 must not come back).
7. Try to **paste** into the editor and into the terminal (Ctrl+V and a right click). *Expected:* nothing is pasted; a short notice says so. In the proctor's history the attempt appears as *Blocked a paste*.
8. Tick the TODO items. Press **Save** (the application's Save, not Ctrl+S), then finish the exam (**Finish exam…**, confirm). *Expected:* a *saved* confirmation, then the closing page.

**C. The network goes away**

9. In the editor type a sentence. **Turn the VM's network adapter off**, wait **30 seconds** (typing meanwhile), turn it on. *Expected:* a *Reconnecting…* banner while it is off, no error dialog,
   the banner goes within about 10 seconds of the adapter's coming back, and the text typed meanwhile is in the file (the toolbar says *Saved*). Open the terminal: the one you had is still there.

**D. The rules of the question exam still hold with this build**

10. Log in as `0002`. Press **Win+L** and come back. *Expected, as in the focus-loss script:* the alarm sounds, a screenshot is taken, and the proctor sees an incident (`docs/tools/TEST-FOCUS.md`).
11. Close the application window during the exam and open it again. *Expected:* the exam is still locked or resumes as it always did (the workspace is as it was).
12. With **the question-exam server**: log in to a question exam with this same exe. *Expected:* exactly as before this phase (the exercises, the calculator, the focus loss).

**E. Nothing is written to the student's disk (rule 11)**

13. After B (before closing the VM), in PowerShell:

    ```powershell
    Get-ChildItem -Recurse -ErrorAction SilentlyContinue $env:LOCALAPPDATA, $env:APPDATA, $env:TEMP, $env:USERPROFILE |
      Select-String -SimpleMatch -Pattern 'insert_sorted', 'made_in_the_terminal', 'push_front' -List | Select-Object -First 5 Path
    ```

    *Expected:* nothing from the student application's own folders (the folder of the exe, and any folder the application made under `%LOCALAPPDATA%` or `%APPDATA%`). Matches in the project's own source files, in browser caches of other programs or in
    the PowerShell history of this command are not the application's. Also look at the folder the exe is in: no `list.cpp`, no `workspace` folder.

## What to send back

The step numbers that did not behave as written, with a screenshot of each; the names of any folder the application made under `%LOCALAPPDATA%` or `%APPDATA%`; the server's log (`docs/workspace/…/manual-*/server.log`).

## What is already checked without a Windows machine

- The student application **builds for Windows** (`GOOS=windows`, amd64) and `go vet` passes for it; CI builds and tests it on `windows-2025` and `windows-11-arm`.
- The Go client's workspace code (the WebSocket, the reconnection, the input batching) is tested on Linux against a fake server (`apps/student/internal/client/workspace_test.go`) and against
  the real one (`deploy/workspace/test-server.sh`, phase D), with the race detector, and a test makes sure the application's source opens no Unix socket and links no container runtime.
- The page (IDE, terminal, the editor's text size and cursor) is tested in Chromium and WebKit (Playwright). **WebView2 is Chromium**, so the Chromium results are the ones that bear on it.
- What has never run on Windows: the `Win+L`/focus code with the IDE open (the owner ran it on 2026-11-14, before the Phase 5 changes), the terminal's keys in WebView2, and the paste interception in
  WebView2. These are the reasons for steps 4 to 7 and 10.
