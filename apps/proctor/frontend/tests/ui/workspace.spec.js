import { test, expect } from '@playwright/test';

// The proctor's side of workspace exams: the roster's workspace column, a student's workspace in the review window (files, terminals,
// downloads), the replay of a transcript, and the history's workspace events.
const SESSION = '22222222-2222-4222-8222-222222222222'; // the first session of the fake server's roster
const observed = '2035-01-01T22:23:23Z';
const row = (index, name, account) => ({
  session_id: `${String(index + 1).padStart(8, '0')}-0000-4000-8000-000000000000`, exam_id: '11111111-1111-4111-8111-111111111111', student_name: name, account_number: account,
  roster_active: true, status: 'active', persisted_status: 'active', seconds_remaining: 1122, finalization_pending: false, final_version: null, locked: null, network_warning: false, focus_warning: false,
  started_at: '2035-01-01T22:00:00Z', deadline_at: '2035-01-01T22:42:05Z', grace_until: '2035-01-01T22:42:35Z', last_known_unfocused_duration_ms: null,
  presence: { monitoring: true, connectivity: 'healthy', hostname: `lab-pc-${index}`, remote_addr: `10.0.0.${index + 20}`, heartbeat_age_ms: 3100, missed_intervals: 0, volume_percent: 85, volume_muted: false, ever_overdue: false, focus: { state: 'focused', unfocused_duration_ms: 0 } },
});
const students = () => [row(0, 'Ada Lovelace', '001234'), row(1, 'Alan Turing', '002468')];
const workspaceStudent = (index, extra = {}) => ({ session_id: `${String(index + 1).padStart(8, '0')}-0000-4000-8000-000000000000`, account_number: index ? '002468' : '001234', state: 'ready', connected: true, agent_restarts: 0,
  last_activity: '2035-01-01T22:23:11Z', cpu_percent: 12.5, memory_mb: 180, todo_done: 2, todo_total: 3, last_snapshot_at: '2035-01-01T22:23:00Z', submissions: 1, last_submission_at: '2035-01-01T22:20:00Z', submitted_final: false, ...extra });

async function monitor(page, roster) {
  await page.goto('/#/monitoring');
  await page.getByRole('table', { name: /All sessions/ }).waitFor();
  await page.evaluate(({ sessions, roster, observed }) => { window.ProctorHarness.workspace(roster); window.ProctorHarness.monitoringRoster(sessions, [], observed); }, { sessions: students(), roster, observed });
}
const line = (page, name) => page.locator(`tbody tr[data-session-id]`).filter({ hasText: name });

test('a workspace exam\'s roster has a workspace column, with the TODO progress said to be self-reported', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0), workspaceStudent(1, { state: 'frozen', connected: false, todo_done: 3, cpu_percent: null, memory_mb: null, submissions: 2, submitted_final: true, agent_restarts: 2, last_activity: null })] });
  await expect(page.getByRole('columnheader', { name: /Workspace/ })).toContainText('TODO is self-reported');
  const ada = line(page, 'Ada Lovelace');
  await expect(ada).toContainText('Running');
  await expect(ada).toContainText('connected');
  await expect(ada).toContainText('12 s ago');
  await expect(ada).toContainText('12.5% CPU · 180 MB');
  await expect(ada).toContainText('2 of 3 done');
  await expect(ada.getByText('(self-reported)')).toBeVisible();
  await expect(ada).toContainText('1 submission · last');
  const alan = line(page, 'Alan Turing');
  await expect(alan).toContainText('Frozen');
  await expect(alan).toContainText('not connected');
  await expect(alan).toContainText('no activity yet');
  await expect(alan).toContainText('not measured');
  await expect(alan).toContainText('3 of 3 done');
  await expect(alan).toContainText('2 submissions');
  await expect(alan).toContainText('final made');
  await expect(alan).toContainText('Workspace restarted 2 times');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1 || !!document.querySelector('.table-scroll'))).toBe(true);
});

test('the TODO progress updates live as the roster is read again', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0)] });
  const ada = line(page, 'Ada Lovelace');
  await expect(ada).toContainText('2 of 3 done');
  await page.evaluate(student => window.ProctorHarness.workspaceRoster({ students: [student] }), workspaceStudent(0, { todo_done: 3, last_activity: '2035-01-01T22:23:20Z' }));
  await expect(ada).toContainText('3 of 3 done');
  await expect(ada).toContainText('3 s ago');
  await expect(ada.getByText('(self-reported)')).toBeVisible();
});

test('a student without a workspace yet, and an exam that is not a workspace exam, are told plainly or not at all', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0)] });
  await expect(line(page, 'Alan Turing')).toContainText('No workspace yet');
  await page.evaluate(() => window.ProctorHarness.workspaceRoster(null));
  await expect(page.getByRole('columnheader', { name: /Workspace/ })).toHaveCount(0);
  await expect(page.locator('td.workspace')).toHaveCount(0);
  await expect(page.getByRole('table', { name: /All sessions/ }).getByRole('columnheader')).toHaveCount(5);
});

const cast = lines => lines.map(item => (typeof item === 'string' ? item : JSON.stringify(item))).join('\n') + '\n';
function data() {
  const text = path => ({ path, size: 40 });
  return {
    session: { session_id: SESSION, state: 'ready', agent_restarts: 1, done: { t1: true, t3: true },
      snapshots: [{ id: 3, reason: 'periodic', taken_at: '2035-01-01T22:10:00Z', files: 5, bytes: 100 }, { id: 5, reason: 'final', taken_at: '2035-01-01T22:20:00Z', files: 7, bytes: 300 }],
      submissions: [{ id: 7, snapshot: 3, final: false, submitted_at: '2035-01-01T22:11:00Z', files: 4, bytes: 90 }, { id: 8, snapshot: 5, final: true, submitted_at: '2035-01-01T22:20:00Z', files: 6, bytes: 280 }],
      terminals: [{ id: 11, kind: 'run', started_at: '2035-01-01T22:12:00Z', ended_at: '2035-01-01T22:12:01Z', exit_code: 0, bytes: 60, truncated: false },
        { id: 12, kind: 'shell', started_at: '2035-01-01T22:13:00Z', ended_at: null, exit_code: null, bytes: 10, truncated: false },
        { id: 13, kind: 'check', started_at: '2035-01-01T22:14:00Z', ended_at: '2035-01-01T22:14:03Z', exit_code: 2, bytes: 5000, truncated: true }] },
    trees: {
      'snapshot:5': [{ path: 'Makefile', type: 'file', size: 20 }, { path: 'src', type: 'dir' }, { path: 'src/main.c', type: 'file', size: 40 }, { path: 'src/blob.bin', type: 'file', size: 4 }, { path: 'src/big.txt', type: 'file', size: 2000000 },
        { path: 'src/evil <img src=x onerror="window.__xss=true">.c', type: 'file', size: 9 }, { path: 'build', type: 'dir' }, { path: 'build/app', type: 'file', size: 900 }, { path: 'link', type: 'link', link: 'src/main.c' }],
      'snapshot:3': [{ path: 'Makefile', type: 'file', size: 20 }],
      'submission:8': [{ path: 'Makefile', type: 'file', size: 20 }, { path: 'src', type: 'dir' }, { path: 'src/main.c', type: 'file', size: 40 }] },
    files: {
      'snapshot:5:src/main.c': { ...text('src/main.c'), content: '#include <stdio.h>\nint main(void) {\n  puts("<b>not bold</b>");\n  return 0;\n}\n' },
      'snapshot:5:src/blob.bin': { path: 'src/blob.bin', size: 4, binary: true, content: '' },
      'snapshot:5:src/big.txt': { path: 'src/big.txt', size: 2000000, too_large: true, content: '' },
      'snapshot:5:Makefile': { path: 'Makefile', size: 20, content: 'all:\n\techo hi\n' },
      'submission:8:src/main.c': { ...text('src/main.c'), content: '/* the submitted one */\n' },
      'snapshot:5:src/evil <img src=x onerror="window.__xss=true">.c': { path: 'src/evil <img src=x onerror="window.__xss=true">.c', size: 9, content: '<script>window.__xss=true</script>' } },
    casts: {
      11: cast([{ version: 2, width: 80, height: 24, timestamp: 1, title: 'Run' }, [0.2, 'o', '\x1b[32mcompiling\x1b[0m\r\n'], [0.6, 'o', 'result: 3 2 1\r\n'], [30, 'o', 'a late line\r\n']]),
      12: cast([{ version: 2, width: 100, height: 30, timestamp: 2 }, [0.1, 'o', 'student@workspace:/work$ ls\r\n']]),
      13: 'this is not a transcript' },
  };
}
// The review window of the first student, as a window the application opens: what the server knows of the workspace is there from the start.
async function review(page, extra) {
  await page.addInitScript(value => { window.__proctorFake = { ...window.__proctorFake, workspace: { roster: { students: [] }, data: value } }; }, extra ?? data());
  await page.goto(`/#/review/${SESSION}`);
}

const tab = (page, name) => page.getByRole('tab', { name: new RegExp(`^${name}`) });
const openTab = async (page, name) => { await tab(page, name).click(); await expect(tab(page, name)).toHaveAttribute('aria-selected', 'true'); };

test('the review window shows a student\'s workspace: its state, what they ticked as their own word, and the submissions', async ({ page }) => {
  await review(page);
  const panel = page.getByRole('region', { name: 'Session review' });
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('restarted 1 time');
  await expect(panel).toContainText('TODO (self-reported)');
  await expect(panel).toContainText('t1, t3 ticked by the student');
  await expect(panel).toContainText('Nothing checked these ticks');
  await expect(panel).toContainText('2 · latest is the final one');
});

test('a workspace exam\'s review is in tabs, the history first, and the tabs go by keyboard', async ({ page }) => {
  await review(page);
  const tabs = page.getByRole('tablist', { name: 'Parts of the session review' });
  await expect(tabs.getByRole('tab')).toHaveText([/History/, /Files/, /Terminals\s*3/]);
  await expect(tab(page, 'History')).toHaveAttribute('aria-selected', 'true');
  // Only the chosen tab's part is shown: the history's table, not the files or the terminals.
  await expect(page.getByRole('table').first()).toBeVisible();
  await expect(page.getByRole('tree', { name: /Files of the student's workspace/ })).toBeHidden();
  await expect(page.getByRole('grid', { name: /Transcripts/ })).toBeHidden();
  await tab(page, 'History').focus();
  await page.keyboard.press('ArrowRight');
  await expect(tab(page, 'Files')).toBeFocused();
  await expect(tab(page, 'Files')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tree', { name: /Files of the student's workspace/ })).toBeVisible();
  await expect(page.getByRole('table')).toHaveCount(0);
  await page.keyboard.press('End');
  await expect(tab(page, 'Terminals')).toBeFocused();
  await expect(page.getByRole('grid', { name: /Transcripts/ })).toBeVisible();
  await page.keyboard.press('ArrowRight');
  await expect(tab(page, 'History')).toBeFocused();
  // Only the chosen tab is in the tab order.
  await expect(tab(page, 'Files')).toHaveAttribute('tabindex', '-1');
  // What was chosen in a tab is still there when it is chosen again.
  await openTab(page, 'Files');
  await page.getByRole('tree', { name: /Files of the student's workspace/ }).getByRole('treeitem', { name: /^Makefile/ }).click();
  await expect(page.getByRole('region', { name: 'Contents of Makefile' })).toBeVisible();
  await openTab(page, 'History');
  await openTab(page, 'Files');
  await expect(page.getByRole('region', { name: 'Contents of Makefile' })).toBeVisible();
});

test('a session that is not a workspace\'s shows nothing of one: no tabs, only the history', async ({ page }) => {
  await page.goto(`/#/review/${SESSION}`);
  await expect(page.getByRole('region', { name: 'Session review' })).toBeVisible();
  await page.waitForTimeout(300);
  await expect(page.getByRole('tablist')).toHaveCount(0);
  await expect(page.getByText('Container')).toHaveCount(0);
  await expect(page.getByRole('table').first()).toBeVisible();
});

test('the files are read from the latest snapshot or from a submission, as text', async ({ page }) => {
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await review(page);
  await openTab(page, 'Files');
  const tree = page.getByRole('tree', { name: /Files of the student's workspace/ });
  // The newest snapshot first: everything, build products too.
  await expect(page.getByLabel('Show the files of')).toHaveValue('snapshot:5');
  await expect(tree.getByRole('treeitem', { name: /^build/ })).toBeVisible();
  await expect(tree.getByRole('treeitem', { name: /^link/ })).toContainText('→ src/main.c');
  await expect(tree.getByRole('treeitem', { name: /^main\.c/ })).toHaveCount(0);
  await tree.getByRole('treeitem', { name: /^src/ }).click();
  await expect(tree.getByRole('treeitem', { name: /^src/ })).toHaveAttribute('aria-expanded', 'true');
  await tree.getByRole('treeitem', { name: /^main\.c/ }).click();
  const viewer = page.getByRole('region', { name: 'Contents of src/main.c' });
  await expect(viewer).toContainText('puts("<b>not bold</b>");');
  expect(await viewer.locator('b').count()).toBe(0);
  await expect(viewer.locator('.numbers')).toContainText('5');
  await tree.getByRole('treeitem', { name: /^blob\.bin/ }).click();
  await expect(page.getByText('This file is not text, so it is not shown')).toBeVisible();
  await tree.getByRole('treeitem', { name: /^big\.txt/ }).click();
  await expect(page.getByText('larger than 1 MiB')).toBeVisible();
  // A file name and a file's text are never markup.
  await tree.getByRole('treeitem', { name: /^evil/ }).click();
  await expect(page.getByRole('region', { name: /^Contents of src\/evil/ })).toContainText('<script>window.__xss=true</script>');
  expect(await page.evaluate(() => window.__xss)).toBeUndefined();
  expect(await page.locator('.tree img, .viewer img, .viewer script').count()).toBe(0);
  // A submission is only what the exam submits.
  await page.getByLabel('Show the files of').selectOption('submission:8');
  await expect(tree.getByRole('treeitem', { name: /^build/ })).toHaveCount(0);
  await expect(tree.getByRole('treeitem', { name: /^Makefile/ })).toBeVisible();
  await tree.getByRole('treeitem', { name: /^src/ }).click();
  await tree.getByRole('treeitem', { name: /^main\.c/ }).click();
  await expect(page.getByRole('region', { name: 'Contents of src/main.c' })).toContainText('the submitted one');
  expect(errors).toEqual([]);
});

test('a tree that cannot be read says so, and the files keep their place when it is read again', async ({ page }) => {
  await review(page);
  await openTab(page, 'Files');
  await page.evaluate(() => window.ProctorHarness.workspaceReadFails('Server request failed.'));
  await page.getByLabel('Show the files of').selectOption('snapshot:3');
  await expect(page.getByRole('alert').filter({ hasText: 'The files could not be read' })).toBeVisible();
});

test('the zip and the report are saved where the proctor chooses', async ({ page }) => {
  await review(page);
  await page.getByRole('button', { name: /Download submitted files/ }).click();
  await expect(page.getByText('Saved to /home/proctor/Downloads/file-files.zip')).toBeVisible();
  await page.getByRole('button', { name: /Download report/ }).click();
  await expect(page.getByText('Saved to /home/proctor/Downloads/file-report.md')).toBeVisible();
  const calls = await page.evaluate(() => window.ProctorHarness.calls.workspace.filter(call => call.call.startsWith('Save')));
  // The zip is of the latest submission.
  expect(calls).toEqual([{ call: 'SaveWorkspaceZip', sessionId: SESSION, submissionId: 8 }, { call: 'SaveWorkspaceReport', sessionId: SESSION }]);
  await page.evaluate(() => window.ProctorHarness.saveCancelled(true));
  await page.getByRole('button', { name: /Download report/ }).click();
  await expect(page.getByText(/Saved to/)).toHaveCount(0);
  // Nothing to download before something is submitted.
  const none = data(); none.session.submissions = [];
  await page.evaluate(value => window.ProctorHarness.workspace({ students: [] }, value), none);
  await expect(page.getByRole('button', { name: /Download submitted files/ })).toBeDisabled({ timeout: 8000 });
  await expect(page.getByRole('button', { name: /Download report/ })).toBeDisabled();
});

test('a terminal\'s transcript is replayed, paused, sped up, sought and shown whole', async ({ page }) => {
  await review(page);
  await openTab(page, 'Terminals');
  const grid = page.getByRole('grid', { name: /Transcripts of the student's terminals/ });
  await expect(grid).toContainText('ended with code 0');
  await expect(grid).toContainText('still open');
  await expect(grid).toContainText('cut at the limit');
  // A row is chosen to replay it: there is no Replay button on the rows, and nothing is replayed until one is chosen.
  await expect(page.getByRole('button', { name: /Replay/ })).toHaveCount(0);
  await expect(page.getByRole('region', { name: /^Replay of/ })).toHaveCount(0);
  await grid.getByRole('row', { name: /^Run/ }).click();
  const player = page.getByRole('region', { name: /^Replay of Run at/ });
  await expect(player).toContainText('3 pieces of output');
  await expect(player).toContainText('00:30 long');
  const screen = player.locator('.xterm-rows');
  await expect(screen).not.toContainText('compiling');
  // Played at ten times the speed, with the long pause shortened: it is over in about a second.
  await player.getByLabel('Replay speed').selectOption('10');
  await player.getByRole('button', { name: 'Play', exact: true }).click();
  await expect(screen).toContainText('compiling');
  await expect(screen).toContainText('result: 3 2 1');
  await expect(screen).toContainText('a late line', { timeout: 5000 });
  await expect(player.getByRole('button', { name: 'Play again' })).toBeVisible();
  // Seeking shows the terminal as it was at that point.
  await player.getByLabel('Position in the transcript').fill('1');
  await expect(screen).toContainText('compiling');
  await expect(screen).not.toContainText('result: 3 2 1');
  await expect(player.getByLabel('Position in the transcript')).toHaveAttribute('aria-valuetext', '00:00 of 00:30');
  await player.getByRole('button', { name: 'Show all' }).click();
  await expect(screen).toContainText('a late line');
  // The text, for a screen reader and for a copy, without the control sequences.
  await player.getByText('The transcript as plain text').click();
  const plain = player.getByLabel(/as plain text/);
  await expect(plain).toContainText('compiling\nresult: 3 2 1\na late line');
  expect(await plain.textContent()).not.toContain('\x1b');
  // Pause stops it.
  await player.getByLabel('Replay speed').selectOption('1');
  await player.getByLabel('Position in the transcript').fill('0');
  await player.getByRole('button', { name: 'Play', exact: true }).click();
  await expect(player.getByRole('button', { name: 'Pause' })).toBeVisible();
  await player.getByRole('button', { name: 'Pause' }).click();
  await expect(player.getByRole('button', { name: 'Play', exact: true })).toBeVisible();
  await player.getByRole('button', { name: 'Close replay' }).click();
  await expect(player).toHaveCount(0);
});

test('a transcript that is not one says so instead of showing something', async ({ page }) => {
  await review(page);
  await openTab(page, 'Terminals');
  await page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Check/ }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'transcript' })).toBeVisible();
  await expect(page.locator('.xterm')).toHaveCount(0);
});

test('the terminals are a grid with chosen rows: the chosen one is marked, replayed under the grid, and closing the replay clears it', async ({ page }) => {
  await review(page);
  await openTab(page, 'Terminals');
  const grid = page.getByRole('grid', { name: /Transcripts of the student's terminals/ });
  const rows = grid.getByRole('row');
  await expect(rows).toHaveCount(4); // the header and the three terminals
  const run = grid.getByRole('row', { name: /^Run/ }), shell = grid.getByRole('row', { name: /^Terminal \d/ }), check = grid.getByRole('row', { name: /^Check/ });
  for (const row of [run, shell, check]) await expect(row).toHaveAttribute('aria-selected', 'false');
  // Choosing a row marks it and shows its replay; choosing another replaces it.
  await run.click();
  await expect(run).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: /^Replay of Run at/ })).toBeVisible();
  await shell.click();
  await expect(shell).toHaveAttribute('aria-selected', 'true');
  await expect(run).toHaveAttribute('aria-selected', 'false');
  await expect(page.getByRole('region', { name: /^Replay of Terminal at/ })).toBeVisible();
  await expect(page.getByRole('region', { name: /^Replay of Run at/ })).toHaveCount(0);
  await expect(page.locator('.xterm')).toHaveCount(1);
  // Closing the replay clears the choice and puts the focus back on the row.
  await page.getByRole('button', { name: 'Close replay' }).click();
  await expect(shell).toHaveAttribute('aria-selected', 'false');
  await expect(shell).toBeFocused();
  await expect(page.getByRole('region', { name: /^Replay of/ })).toHaveCount(0);
});

test('the grid goes by keyboard: the arrows move between rows, Enter chooses, and only one row is in the tab order', async ({ page }) => {
  await review(page);
  await openTab(page, 'Terminals');
  const grid = page.getByRole('grid', { name: /Transcripts/ });
  const rows = grid.locator('tbody tr');
  await expect(rows.nth(0)).toHaveAttribute('tabindex', '0');
  await expect(rows.nth(1)).toHaveAttribute('tabindex', '-1');
  await rows.nth(0).focus();
  await page.keyboard.press('ArrowDown');
  await expect(rows.nth(1)).toBeFocused();
  await page.keyboard.press('End');
  await expect(rows.nth(2)).toBeFocused();
  await page.keyboard.press('ArrowDown'); // the last row is the last
  await expect(rows.nth(2)).toBeFocused();
  await page.keyboard.press('Home');
  await expect(rows.nth(0)).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(rows.nth(0)).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: /^Replay of Run at/ })).toBeVisible();
  // The chosen row is the one in the tab order now.
  await expect(rows.nth(0)).toHaveAttribute('tabindex', '0');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press(' ');
  await expect(rows.nth(1)).toHaveAttribute('aria-selected', 'true');
  await expect(rows.nth(1)).toHaveAttribute('tabindex', '0');
  await expect(rows.nth(0)).toHaveAttribute('tabindex', '-1');
});

test('choosing a row scrolls the window to the replay when it is below the fold, and does not when it is already in view', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 760 });
  await review(page);
  await openTab(page, 'Terminals');
  const grid = page.getByRole('grid', { name: /Transcripts/ });
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
  await grid.getByRole('row', { name: /^Run/ }).click();
  const player = page.getByRole('region', { name: /^Replay of Run at/ });
  await expect(player).toBeVisible();
  // The whole replay is in the window (it fits in it), which it was not before the window was scrolled to it.
  await expect(player).toBeInViewport({ ratio: 1 });
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
  // A window tall enough to hold everything does not move.
  await page.getByRole('button', { name: 'Close replay' }).click();
  await page.setViewportSize({ width: 1200, height: 2400 });
  await page.evaluate(() => window.scrollTo(0, 0));
  await grid.getByRole('row', { name: /^Run/ }).click();
  await expect(player).toBeInViewport({ ratio: 1 });
  await page.waitForTimeout(300);
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
});

test('the replay the proctor chose is kept while the workspace is read again', async ({ page }) => {
  await review(page);
  await openTab(page, 'Terminals');
  await page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Run/ }).click();
  await expect(page.getByRole('region', { name: /^Replay of Run at/ })).toBeVisible();
  // A new terminal appears in the next reading (every five seconds); the chosen row and its replay stay.
  const more = data(); more.session.terminals.push({ id: 14, kind: 'run', started_at: '2035-01-01T22:15:00Z', ended_at: '2035-01-01T22:15:01Z', exit_code: 0, bytes: 20, truncated: false });
  more.casts[14] = cast([{ version: 2, width: 80, height: 24, timestamp: 3 }, [0.1, 'o', 'x\r\n']]);
  await page.evaluate(value => window.ProctorHarness.workspace({ students: [] }, value), more);
  await expect(page.getByRole('grid', { name: /Transcripts/ }).locator('tbody tr')).toHaveCount(4, { timeout: 8000 });
  await expect(page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Run/ }).first()).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: /^Replay of Run at/ })).toBeVisible();
  await expect(page.getByRole('tab', { name: /^Terminals/ })).toContainText('4');
});

const event = (id, type, detail) => ({ id: `event:${id}`, kind: 'event', occurred_at: '2035-01-01T00:05:00Z', event: { event_log_id: id, event_type: type, detail, screenshots: [] } });

test('the history names a workspace\'s events in words, with a filter that exists only when there are some', async ({ page }) => {
  const withEvents = data();
  withEvents.events = [event(90, 'workspace.opened', null), event(91, 'file.saved', { path: 'src/main.c', bytes: 120, delta_bytes: 16 }), event(92, 'paste.blocked', { target: 'editor', bytes: 30 }),
    event(93, 'todo.set', { id: 't1', done: true }), event(94, 'run.started', { which: 'check', exit_code: 2 }), event(95, 'workspace.submitted', { final: true, snapshot: 5, files: 6 })];
  await review(page, withEvents);
  const history = page.getByRole('group', { name: 'Show' });
  await expect(history.getByLabel(/Workspace/)).toBeVisible();
  await expect(history.getByLabel(/Workspace/)).toBeChecked();
  const table = page.getByRole('region', { name: 'Session review' }).getByRole('table').first();
  await expect(table).toContainText('src/main.c · 120 bytes (+16)');
  await expect(table).toContainText('Blocked a paste into the editor (30 bytes)');
  await expect(table).toContainText('t1 marked done (self-reported)');
  await expect(table).toContainText('Check ended with code 2');
  await expect(table).toContainText('Final submission · snapshot 5 · 6 files');
  await expect(table.getByRole('row').filter({ hasText: 'Paste blocked' })).toHaveCount(1);
  // The filter hides them.
  await history.getByLabel(/Workspace/).uncheck();
  await expect(table).not.toContainText('src/main.c · 120 bytes');
  await expect(table).toContainText('Answers');
});

test('a session with no workspace events has no workspace filter', async ({ page }) => {
  await page.goto(`/#/review/${SESSION}`);
  await expect(page.getByRole('group', { name: 'Show' })).toBeVisible();
  await expect(page.getByRole('group', { name: 'Show' }).getByLabel(/Workspace/)).toHaveCount(0);
});
