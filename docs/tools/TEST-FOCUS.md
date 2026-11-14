# Manual test: the calculator never causes a focus loss

The calculator is an in-window overlay. Focus loss is detected from the **native window's** focus events, which
no page element can cause — so opening, using or closing the calculator must produce no `focus_lost` event, no
alarm, and no screenshots. The automated UI tests verify the calculator calls nothing native but the `Calc*`
methods; only a real application can verify the native side. This script is that verification.

**Run it on every platform the students use** (Linux with X11, Linux with Wayland, Windows, macOS). Mark the
result of each platform in the phase report. Every step needs a real student application connected to a real
server, watched in the proctor's review window.

## Setup

1. Build the student application and the server (`make server student`).
2. Import an exam whose file says `calculator: mode=[scientific]`, publish it, and admit your own student
   account.
3. Start the server and the student application; log in with the PIN and pass the screenshot test.
4. Open the proctor's review window for your session (Monitor → your student). Note the current last row.

## Steps

1. Open the calculator with its button.
2. Evaluate ten expressions, including `sin(30)`, `log(8, 2)`, `2pi`, one that fails (`1/0`), and `ans+1`.
3. Toggle the angle mode if the exam says `angle=user` (the DEG/RAD button); otherwise skip.
4. Close the calculator with its ✕ button.
5. Open it again, evaluate one expression, and close it with the Escape key.
6. Watch the exam window the whole time: it must stay fullscreen, stay always on top, and **no alarm must
   sound**.
7. In the proctor's review window, filter **Focus**: no new "Left the exam window" (`focus_lost`) row may
   appear since the setup's last row. No new screenshot rows either.
8. Still in the review window, note the calculator rows exist (filter **Calculator**, from Phase 4 on) — the
   ten expressions plus the failed one are listed with their results. This confirms the calculator works, not
   only that it is silent.

## Programmer panel and its tabs (Phase 6A)

Repeat with an exam whose file says `calculator: mode=[scientific, programmer]`, still watching the exam window
and the review window as above.

1. Open the calculator and switch to the **Programmer** mode tab.
2. Type `10.625` and press Enter on the **Keypad** tab: the integer rows show dashes, the value row 10.625.
3. Switch to the **IEEE 754** tab with the mouse; the input stays `10.625` and the tab encodes it. Choose
   binary16, then switch **Encode | Decode** to Decode and back, and press **Inspect**.
4. Switch to the **Radix Point** tab with the keyboard: focus the tab row, use the left and right arrow keys, and
   select a tab with Enter. The tab converts the input.
5. Type a digit while a tab button has the focus: it goes to the shared input.
6. Close the calculator with Escape.
7. In the review window, filter **Focus**: no new `focus_lost` row since the start, and no screenshots. Filter
   **Calculator**: the evaluations, the inspections (with `encode` or `decode` and the panel settings) and one
   "Calculator tab changed" row per tab switch are listed.

Switching tabs, formats or the Encode | Decode control must cause no focus loss, like any other calculator use.

## The workspace IDE (workspace exams, Phase 4)

The same rule holds for the IDE of a workspace exam: it is a page in the exam window, so using it must cause no `focus_lost` event, no
alarm and no screenshots. Terminal and editor focus are the new things: the terminal's keyboard is a hidden text field of the page, and
the editor is a text field too, but neither is a native window.

Setup: build the student application, the server, the runner and the course image (docs/workspace/deployment.md), import and publish
the example package (`testdata/workspace/lab-listas`, compile it with `examc`), log in with the PIN and pass the screenshot test, and
watch the proctor's review window for your session as above. Note the last row.

1. Open `src/list.cpp` from the file tree and type in it for half a minute. Switch between two files with the mouse and with the
   arrow keys on the tab row.
2. Press **Run**, then **Check**. Watch their output tabs fill.
3. Open a terminal (**+**), click in it, type `ls`, press Enter, then press **Ctrl+C** and **Ctrl+D**. Move between the editor, the
   terminal and the file tree with the mouse and with **F6**.
4. Create a file and a folder from the file tree, rename one, delete one (confirm the dialog), then press **Ctrl+S**.
5. Tick a TODO item. Press **Submit**.
6. Try to paste text copied from outside the application (another window's text) into the editor and into the terminal: it must be
   refused with a notice, and the exam window must keep its focus.
7. Open the dialogs (new file, delete) and close them with Escape.
8. Throughout: the window stays fullscreen and always on top, and **no alarm sounds**. In the review window, filter **Focus**: no new
   `focus_lost` row since the setup's last row; no new screenshot rows either.

Then pull the network cable (or turn the Wi-Fi off) for 30 seconds while typing in the editor, and put it back: the banner says
"Reconnecting…", what was typed is still in the editor, and it is saved when the connection returns. (If the application cannot reach
the server for the policy's time, the exam locks, as for any exam; that is the monitoring working, not a failure of the IDE.)

## What counts as a failure

- Any `focus_lost` event, any alarm beep, any new screenshot between step 1 and step 6.
- The exam window leaving fullscreen or always-on-top.
- A new window or native dialog opened by the calculator (the calculator lives in the exam window only).

## Notes per platform

| Platform | Focus behavior | Result |
|---|---|---|
| Linux X11 | `WindowLostFocus`/`WindowFocus` hooks of the native window | not run yet; the IDE steps not run on any platform yet |
| Linux Wayland | same hooks; compositor focus | **passed** (owner, 2026-10-25, KDE Plasma; through Phase 6A, with the programmer steps) |
| Windows | same hooks | not run yet; the owner runs it later |
| macOS | same hooks | not run yet; the owner runs it later |

Automated coverage (Playwright, `tests/ui/calculator.spec.js`): opening, evaluating and closing call no native
method other than the `Calc*` ones (Snapshot entries are the app's own poll timer), and `window.open` is never
called. `tests/ui/programmer.spec.js` checks the same for switching the programmer tabs and the Encode | Decode
control (Phase 6A). `tests/ui/workspace.spec.js` checks that using the IDE (editing, terminals, Run, Check, the TODO list, Submit) calls no native
method other than `WorkspaceRequest`, `WorkspaceInput` and the poll's `Snapshot`. Focus detection itself is native and cannot be exercised in the
browser harness.
