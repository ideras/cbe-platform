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
const diagnostics = row => row.locator('xpath=following-sibling::tr[1]');
const expand = row => row.getByRole('button', { name: /^Details for / }).click();

test('a workspace exam\'s roster has a workspace column, with the TODO progress said to be self-reported', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0), workspaceStudent(1, { state: 'frozen', connected: false, todo_done: 3, cpu_percent: null, memory_mb: null, submissions: 2, submitted_final: true, agent_restarts: 2, last_activity: null })] });
  await expect(page.getByRole('columnheader', { name: /Workspace/ })).toContainText('TODO is self-reported');
  const ada = line(page, 'Ada Lovelace');
  await expect(ada).toContainText('Running');
  await expand(ada);
  await expect(diagnostics(ada)).toContainText('Student: connected');
  await expect(diagnostics(ada)).toContainText('12 s ago');
  await expect(diagnostics(ada)).toContainText('12.5% CPU · 180 MB');
  await expect(ada).toContainText('2 of 3 done');
  await expect(ada.getByText('(self-reported)')).toBeVisible();
  await expect(ada).toContainText('1 submission');
  await expect(diagnostics(ada)).toContainText('Last submission:');
  const alan = line(page, 'Alan Turing');
  await expect(alan).toContainText('Frozen');
  await expand(alan);
  await expect(diagnostics(ada)).toBeHidden();
  await expect(diagnostics(alan)).toContainText('not connected');
  await expect(diagnostics(alan)).toContainText('no activity yet');
  await expect(diagnostics(alan)).toContainText('not measured');
  await expect(alan).toContainText('3 of 3 done');
  await expect(alan).toContainText('2 submissions');
  await expect(alan).toContainText('final made');
  await expect(diagnostics(alan)).toContainText('Workspace restarted 2 times');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1 || !!document.querySelector('.table-scroll'))).toBe(true);
});

test('the TODO progress updates live as the roster is read again', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0)] });
  const ada = line(page, 'Ada Lovelace');
  await expect(ada).toContainText('2 of 3 done');
  await expand(ada);
  await page.evaluate(student => window.ProctorHarness.workspaceRoster({ students: [student] }), workspaceStudent(0, { todo_done: 3, last_activity: '2035-01-01T22:23:20Z' }));
  await expect(ada).toContainText('3 of 3 done');
  await expect(diagnostics(ada)).toContainText('3 s ago');
  await expect(ada.getByText('(self-reported)')).toBeVisible();
});

test('a student without a workspace yet, and an exam that is not a workspace exam, are told plainly or not at all', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0)] });
  await expect(line(page, 'Alan Turing')).toContainText('No workspace yet');
  await page.evaluate(() => window.ProctorHarness.workspaceRoster(null));
  await expect(page.getByRole('columnheader', { name: /Workspace/ })).toHaveCount(0);
  await expect(page.locator('td.workspace')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /^Grading/ })).toHaveCount(0);
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
  await expect(panel.getByRole('button', { name: /^TODO ticks/ })).toContainText('Self-reported, unchecked');
  await panel.getByRole('button', { name: 'Session details' }).click();
  const details = panel.getByRole('region', { name: 'Session details' });
  await expect(details).toContainText('restarted 1 time');
  await expect(details).toContainText('t1, t3 · self-reported');
  await expect(details).toContainText('2 · latest is the final one');
});

test('a workspace that failed says why, as text and not as markup (Phase 8)', async ({ page }) => {
  const extra = data();
  extra.session.state = 'failed';
  extra.session.failure = 'creating the container: the host has no room for another workspace <b>x</b>';
  await review(page, extra);
  const panel = page.getByRole('region', { name: 'Session review' });
  await panel.getByRole('button', { name: 'Session details' }).click();
  const details = panel.getByRole('region', { name: 'Session details' });
  await expect(details).toContainText('failed');
  await expect(details.getByText(/creating the container/)).toContainText('the host has no room for another workspace <b>x</b>');
  await expect(details.locator('b').filter({ hasText: /^x$/ })).toHaveCount(0);
});

test('a workspace exam\'s review is in tabs, the history first, and the tabs go by keyboard', async ({ page }) => {
  await review(page);
  const tabs = page.getByRole('tablist', { name: 'Review sections' });
  await expect(tabs.getByRole('tab')).toHaveText([/History/, /Files\s*7/, /Terminals\s*3/, /Grading/]);
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
  await expect(tab(page, 'Grading')).toBeFocused();
  await expect(page.getByText('This student has not been graded.')).toBeVisible();
  await page.keyboard.press('ArrowLeft');
  await expect(tab(page, 'Terminals')).toBeFocused();
  await expect(page.getByRole('grid', { name: /Transcripts/ })).toBeVisible();
  await page.keyboard.press('ArrowRight');
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
  await expect(tree.getByRole('treeitem', { name: /^src/ })).toHaveAttribute('aria-expanded', 'true');
  await tree.getByRole('treeitem', { name: /^main\.c/ }).click();
  await expect(page.getByRole('region', { name: 'Contents of src/main.c' })).toContainText('the submitted one');
  await page.getByLabel('Show the files of').selectOption('snapshot:5');
  await expect(page.getByRole('region', { name: 'Contents of src/main.c' })).toContainText('not bold');
  expect(errors).toEqual([]);
});

test('the line number gutter stays shaded and visible when a long file is scrolled', async ({ page }) => {
  const fixture = data();
  fixture.files['snapshot:5:src/main.c'].content = Array.from({ length: 80 }, (_, index) => `int line${index + 1} = ${index}; // ${'long text '.repeat(30)}`).join('\n');
  await review(page, fixture);
  await openTab(page, 'Files');
  await page.getByRole('treeitem', { name: /^src$/ }).click();
  await page.getByRole('treeitem', { name: /^main\.c/ }).click();
  const preview = page.getByRole('region', { name: 'Contents of src/main.c' });
  const gutter = preview.locator('.numbers');
  await expect(gutter).toContainText('80');
  // Check actual pixels: on WebKit, the numbers overflowed their painted background.
  for (const scroll of [0, 180, 10000]) {
    const { top, left } = await preview.evaluate((node, y) => {
      node.scrollTop = y;
      node.scrollLeft = 100;
      return { top: node.scrollTop, left: node.scrollLeft };
    }, scroll);
    expect(left).toBe(100);
    if (scroll) expect(top).toBeGreaterThanOrEqual(180);
    const image = await preview.screenshot({ scale: 'css' });
    const shaded = await gutter.evaluate(async (node, png) => {
      const image = document.createElement('img');
      image.src = `data:image/png;base64,${png}`;
      await image.decode();
      const canvas = document.createElement('canvas');
      canvas.width = image.width;
      canvas.height = image.height;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(image, 0, 0);
      const pixel = (x, y) => String(ctx.getImageData(x, y, 1, 1).data);
      const actual = [15, Math.floor(image.height / 2), image.height - 24].map(y => pixel(4, y));
      ctx.fillStyle = getComputedStyle(node).backgroundColor;
      ctx.fillRect(0, 0, 1, 1);
      return actual.every(color => color === pixel(0, 0));
    }, image.toString('base64'));
    expect(shaded, `gutter must stay shaded at scrollTop=${top}`).toBe(true);
  }
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
  await openTab(page, 'Files');
  await page.getByRole('button', { name: /Download submitted files/ }).click();
  await expect(page.getByText('Saved to /home/proctor/Downloads/file-files.zip')).toBeVisible();
  await openTab(page, 'Grading');
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
  await openTab(page, 'History');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await openTab(page, 'Files');
  await expect(page.getByRole('button', { name: /Download submitted files/ })).toBeDisabled();
  await openTab(page, 'Grading');
  await expect(page.getByRole('button', { name: /Download report/ })).toBeDisabled();
});

test('a terminal\'s transcript is replayed, paused, sped up, sought and shown whole', async ({ page }) => {
  await review(page);
  await openTab(page, 'Terminals');
  const grid = page.getByRole('grid', { name: /Transcripts of the student's terminals/ });
  await expect(grid).toContainText('ended with code 0');
  await expect(grid).toContainText('still open');
  await expect(grid.getByTitle('Output was cut at the limit')).toBeVisible();
  // A row is chosen to replay it: there is no Replay button on the rows, and nothing is replayed until one is chosen.
  await expect(page.getByRole('button', { name: /Replay/ })).toHaveCount(0);
  await expect(page.getByRole('region', { name: /^Replay of/ })).toHaveCount(0);
  await grid.getByRole('row', { name: /^Run/ }).click();
  const player = page.getByRole('region', { name: /^Replay of Run at/ });
  await expect(player).toContainText('3 pieces of output');
  await expect(player).toContainText('00:30 long');
  const screen = player.locator('.xterm-rows');
  await expect(screen).not.toContainText('compiling');
  // Played at four times the speed, with the long pause shortened.
  const speeds = player.getByRole('group', { name: 'Replay speed' });
  await expect(speeds.getByRole('button', { name: '½×' })).toBeVisible();
  await speeds.getByRole('button', { name: '4×' }).click();
  await player.getByRole('button', { name: 'Play', exact: true }).click();
  await expect(screen).toContainText('compiling');
  await expect(screen).toContainText('result: 3 2 1');
  await expect(screen).toContainText('a late line', { timeout: 5000 });
  await expect(player.getByRole('button', { name: 'Play again' })).toBeVisible();
  // Seeking shows the terminal as it was at that point.
  await player.getByLabel('Position in the transcript').fill('0.3');
  await expect(screen).toContainText('compiling');
  await expect(screen).not.toContainText('result: 3 2 1');
  await expect(player.getByLabel('Position in the transcript')).toHaveAttribute('aria-valuetext', '00:00 of 00:30');
  await player.getByRole('button', { name: 'Show all' }).click();
  await expect(screen).toContainText('a late line');
  // The text, for a screen reader and for a copy, without the control sequences.
  await player.getByRole('button', { name: 'Plain text', exact: true }).click();
  await expect(player.getByRole('group', { name: 'Replay controls' })).toBeHidden();
  const plain = player.getByLabel(/as plain text/);
  await expect(plain).toContainText('compiling\nresult: 3 2 1\na late line');
  expect(await plain.textContent()).not.toContain('\x1b');
  // Pause stops it.
  await player.getByRole('button', { name: 'Replay', exact: true }).click();
  await speeds.getByRole('button', { name: '1×' }).click();
  await player.getByLabel('Position in the transcript').fill('0');
  await player.getByRole('button', { name: 'Play', exact: true }).click();
  await expect(player.getByRole('button', { name: 'Pause' })).toBeVisible();
  await player.getByRole('button', { name: 'Pause' }).click();
  await expect(player.getByRole('button', { name: 'Play', exact: true })).toBeVisible();
  await player.getByRole('button', { name: 'Close replay' }).click();
  await expect(player).toHaveCount(0);
});

test('normal replay shows closely timed key echoes and uses the right edge for scrollback', async ({ page }) => {
  const fixture = data();
  fixture.casts[12] = cast([
    { version: 2, width: 42, height: 8 },
    [0, 'o', '$ '], [0.15, 'o', 'm'], [0.151, 'o', 'a'], [0.152, 'o', 'k'], [0.153, 'o', 'e'],
    [0.154, 'o', `\r\n${Array.from({ length: 30 }, (_, index) => `Result ${index + 1}`).join('\r\n')}`],
  ]);
  await review(page, fixture);
  await openTab(page, 'Terminals');
  await page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Terminal \d/ }).click();
  const player = page.getByRole('region', { name: /^Replay of Terminal/ });
  const screen = player.locator('.xterm-rows');
  // Observe distinct terminal renders, not separate writes that might be painted in the same frame.
  await screen.evaluate(node => {
    node.seen = [];
    const observer = new MutationObserver(() => node.seen.push(node.textContent.trim()));
    observer.observe(node, { childList: true, subtree: true, characterData: true });
  });
  await player.getByRole('button', { name: 'Play', exact: true }).click();
  await expect(screen).toContainText('Result 30');
  const renders = await screen.evaluate(node => node.seen);
  expect(renders).toEqual(expect.arrayContaining(['$ m', '$ ma', '$ mak', '$ make']));
  const scrollbar = player.locator('.xterm-scrollable-element > .scrollbar.vertical');
  await expect(scrollbar).toHaveClass(/\bvisible\b/);
  const track = await scrollbar.boundingBox();
  const pane = await player.boundingBox();
  expect(pane.x + pane.width - track.x - track.width).toBeLessThan(20);
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
  await page.setViewportSize({ width: 1200, height: 630 });
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
  // A new terminal appears after refresh; the chosen row and its replay stay.
  const more = data(); more.session.terminals.push({ id: 14, kind: 'run', started_at: '2035-01-01T22:15:00Z', ended_at: '2035-01-01T22:15:01Z', exit_code: 0, bytes: 20, truncated: false });
  more.casts[14] = cast([{ version: 2, width: 80, height: 24, timestamp: 3 }, [0.1, 'o', 'x\r\n']]);
  await page.evaluate(value => window.ProctorHarness.workspace({ students: [] }, value), more);
  await openTab(page, 'History');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await openTab(page, 'Terminals');
  await expect(page.getByRole('grid', { name: /Transcripts/ }).locator('tbody tr')).toHaveCount(4);
  await expect(page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Run/ }).first()).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: /^Replay of Run at/ })).toBeVisible();
  await expect(page.getByRole('tab', { name: /^Terminals/ })).toContainText('4');
});

test('the Terminals tab and the replay\'s controls have their icons, and Close replay is an icon with no text', async ({ page }) => {
  await review(page);
  await expect(tab(page, 'Terminals').locator('svg')).toHaveCount(1);
  await openTab(page, 'Terminals');
  await page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Run/ }).click();
  const player = page.getByRole('region', { name: /^Replay of Run at/ });
  await expect(player).toBeVisible();
  // Each control that has a word has an icon with it (drawn in the page, not an image or a character).
  for (const name of ['Play', 'Show all']) {
    const button = player.getByRole('button', { name, exact: true });
    await expect(button.locator('svg')).toHaveCount(1);
    await expect(button).toHaveText(name);
  }
  await player.getByRole('button', { name: 'Show all' }).click();
  await expect(player.getByRole('button', { name: 'Play again', exact: true }).locator('svg')).toHaveCount(1);
  await player.getByRole('button', { name: 'Play again', exact: true }).click();
  await expect(player.getByRole('button', { name: 'Pause', exact: true }).locator('svg')).toHaveCount(1);
  await player.getByRole('button', { name: 'Pause', exact: true }).click();
  // Close replay: the icon alone, named for a screen reader and by a tooltip.
  const close = player.getByRole('button', { name: 'Close replay' });
  await expect(close.locator('svg')).toHaveCount(1);
  expect((await close.textContent()).trim()).toBe('');
  await expect(close).toHaveAttribute('title', 'Close replay');
  const box = await close.boundingBox();
  expect(Math.round(box.width)).toBe(Math.round(box.height));
});

// ---- Grading (Phase 6) ----
const todos = [{ id: 't1', text: 'Implement `push_front`', points: 10 }, { id: 't2', text: 'Implement `insert_sorted`', points: 15 }, { id: 't3', text: 'Add `tests/mine.cpp`', points: 15 }];
const run = (id, extra = {}) => ({ id, submission: 8, state: 'done', failure: '', failure_text: '', queued_at: '2035-01-01T22:21:00Z', started_at: '2035-01-01T22:21:01Z', finished_at: '2035-01-01T22:21:03Z', duration_ms: 1900, points: 16, max_points: 40,
  confirmed: false, skipped: [], stdout: '{"t1": {"passed": true}}', stderr: 'ok: push_front 1 2 3 -> 3 2 1\n', stdout_truncated: false, stderr_truncated: false,
  results: [{ todo_id: 't1', known: true, passed: true, points: 10, log: '4/4 tests passed' }, { todo_id: 't2', known: true, passed: false, points: 6, log: '2/5 tests passed\nFAILED: insert_sorted 5 1 3' }, { todo_id: 'zz', known: false, passed: true, points: 99, log: 'a key nobody asked for' }], ...extra });
function gradedData(runs) {
  const withGradings = data();
  withGradings.gradings = { todos, runs };
  return withGradings;
}
const gradingTab = page => tab(page, 'Grading');

test('the roster says each student\'s grade in words: waiting, being graded, advisory, confirmed, failed', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 700 });
  const students2 = [row(0, 'Ada Lovelace', '001234'), row(1, 'Alan Turing', '002468'), row(2, 'Grace Hopper', '003579'), row(3, 'Joan Clarke', '004680'), row(4, 'Linus T', '005791'), row(5, 'No One', '006802')];
  await page.goto('/#/monitoring');
  await page.getByRole('table', { name: /All sessions/ }).waitFor();
  const rosterOf = grades => ({ students: grades.map((grade, i) => ({ ...workspaceStudent(i), account_number: students2[i].account_number, submissions: i === 5 ? 0 : 1, grade })) });
  await page.evaluate(({ sessions, roster, observed }) => { window.ProctorHarness.workspace(roster); window.ProctorHarness.monitoringRoster(sessions, [], observed); },
    { sessions: students2, roster: rosterOf([{ grading: 1, state: 'done', points: 28, max_points: 40, confirmed: false, at: observed }, { grading: 2, state: 'done', points: 40, max_points: 40, confirmed: true, at: observed },
      { grading: 3, state: 'queued', at: observed }, { grading: 4, state: 'running', at: observed }, { grading: 5, state: 'failed', failure: 'timeout', at: observed }, null]), observed });
  await openTab(page, 'Grading');
  const grades = page.getByRole('table', { name: 'Class grading' });
  const gradeRow = name => grades.getByRole('row').filter({ hasText: name });
  await expect(grades.locator('tbody tr').first()).toContainText('Joan Clarke');
  await expect(grades.locator('tbody tr').nth(1)).toContainText('Grace Hopper');
  await expect(gradeRow('Ada Lovelace')).toContainText('28 of 40 points');
  await expect(gradeRow('Alan Turing')).toContainText('40 of 40 points');
  await expect(gradeRow('Alan Turing')).toContainText('Confirmed');
  await expect(gradeRow('Grace Hopper')).toContainText('Waiting');
  await expect(gradeRow('Joan Clarke')).toContainText('Being graded');
  await expect(gradeRow('Linus T')).toContainText('Grading failed: it did not end in time');
  await expect(gradeRow('No One')).toContainText('No submission');
  await expect(page.getByRole('group', { name: 'Grading actions' })).toContainText('A grade is advisory until you confirm it');
});

test('the class is graded from the roster after the proctor is told what will happen, and what was left out is said', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 800 });
  await monitor(page, { students: [workspaceStudent(0, { submissions: 2 }), workspaceStudent(1, { submissions: 0 })] });
  await openTab(page, 'Grading');
  const button = page.getByRole('button', { name: 'Grade submissions…' });
  await expect(button).toBeEnabled();
  await page.evaluate(() => window.ProctorHarness.gradeExamResult({ queued: [11], skipped: [{ session_id: '00000002-0000-4000-8000-000000000000', account_number: '002468', reason: 'has not submitted anything' }] }));
  await button.click();
  const dialog = page.getByRole('dialog', { name: 'Grade the class' });
  await expect(dialog.getByRole('button', { name: 'Grade 1 submission' })).toBeFocused();
  await expect(dialog).toContainText('no network');
  await expect(dialog).toContainText('advisory until you confirm it');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(button).toBeFocused();
  expect(await page.evaluate(() => window.ProctorHarness.calls.workspace?.filter(call => call.call === 'GradeExam').length ?? 0)).toBe(0);
  await button.click();
  await page.getByRole('dialog', { name: 'Grade the class' }).getByRole('button', { name: 'Grade 1 submission' }).click();
  const receipt = page.getByRole('region', { name: 'Class grading request' });
  await expect(receipt.getByRole('status')).toHaveText('1 submission was queued for grading.');
  await receipt.getByText('1 student left out — see why').click();
  await expect(receipt.getByText('002468: has not submitted anything')).toBeVisible();
  await page.evaluate(students => window.ProctorHarness.workspaceRoster({ students }), [
    workspaceStudent(0, { grade: { grading: 11, state: 'done', points: 28, max_points: 40 } }), workspaceStudent(1, { submissions: 0 }),
  ]);
  await expect(receipt).toContainText('Class grading finished · 1 graded');
  await expect(receipt).not.toContainText('queued for grading');
  await expect(receipt.getByText('002468: has not submitted anything')).toBeVisible();
  expect(await page.evaluate(() => window.ProctorHarness.calls.workspace.filter(call => call.call === 'GradeExam'))).toEqual([{ call: 'GradeExam', sessionIds: [] }]);
  // A refusal of the server is shown in the dialog, in the server's words.
  await page.evaluate(() => window.ProctorHarness.gradeFails('The server refused the change: this exam has no grading folder, so there is nothing to grade it with.'));
  await button.click();
  await page.getByRole('dialog', { name: 'Grade the class' }).getByRole('button', { name: 'Grade 1 submission' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'no grading folder' })).toBeVisible();
});

test('roster tab keys and independent filters preserve the open student across polls', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0), workspaceStudent(1, { submissions: 0 })] });
  const monitoring = tab(page, 'Monitoring');
  const grading = tab(page, 'Grading');
  await expect(monitoring).toHaveAttribute('aria-selected', 'true');
  const ada = line(page, 'Ada Lovelace');
  await page.getByRole('button', { name: /^In progress 2$/ }).click();
  await ada.getByRole('button', { name: /^Details for / }).press('Space');
  await monitoring.press('ArrowRight');
  await expect(grading).toBeFocused();
  await expect(monitoring).toHaveAttribute('tabindex', '-1');
  const none = page.getByRole('group', { name: 'Filter grades' }).getByRole('button', { name: /^No submission 1$/ });
  await none.click();
  const grades = page.getByRole('table', { name: 'Class grading' });
  await expect(grades.getByRole('row').filter({ hasText: 'Alan Turing' })).toBeVisible();
  await page.evaluate(student => window.ProctorHarness.workspaceRoster({ students: [student] }), workspaceStudent(0, { state: 'frozen' }));
  await expect(grades.locator('tbody tr')).toHaveCount(1);
  await grading.press('Home');
  await expect(monitoring).toBeFocused();
  await expect(ada).toContainText('Frozen');
  await expect(diagnostics(ada)).toBeVisible();
  await expect(page.getByRole('button', { name: /^In progress 2$/ })).toHaveAttribute('aria-pressed', 'true');
  await monitoring.press('End');
  await expect(none).toHaveAttribute('aria-pressed', 'true');
});

test('class progress counts matching run IDs only and keeps export, retry and Review usable', async ({ page }) => {
  await monitor(page, { students: [workspaceStudent(0, { grade: { grading: 1, state: 'done', confirmed: true, points: 27, max_points: 40 } }), workspaceStudent(1)] });
  await page.evaluate(value => {
    const { students } = value;
    window.ProctorHarness.workspace({ students }, value.data);
    window.ProctorHarness.gradeExamResult({ queued: [11, 12], skipped: [] });
  }, { students: [workspaceStudent(0, { grade: { grading: 1, state: 'done', confirmed: true, points: 27, max_points: 40 } }), workspaceStudent(1)], data: gradedData([run(12, { state: 'failed' })]) });
  await openTab(page, 'Grading');
  await page.getByRole('button', { name: /^Confirmed 1$/ }).click();
  await page.getByRole('button', { name: 'Grade submissions…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Grade 2 submissions' }).click();
  const receipt = page.getByRole('region', { name: 'Class grading request' });
  const update = async (first, second) => page.evaluate(students => window.ProctorHarness.workspaceRoster({ students }), [workspaceStudent(0, { grade: first }), workspaceStudent(1, { grade: second })]);
  const confirmed = { grading: 1, state: 'done', confirmed: true, points: 27, max_points: 40 };
  await update(confirmed, { grading: 12, state: 'running' });
  await expect(receipt).toContainText('0 of 2 finished · 0 graded · 0 failed · 1 running');
  await expect(receipt).toContainText('1 run not shown in the roster');
  await page.getByRole('button', { name: 'Download grades…' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Grades saved to' })).toContainText('1 grade confirmed');
  await update(confirmed, { grading: 12, state: 'failed', failure: 'timeout' });
  await expect(receipt).toContainText('1 of 2 finished · 0 graded · 1 failed');
  await page.getByRole('button', { name: /^Failed 1$/ }).click();
  const retry = page.getByRole('button', { name: 'Retry grading 002468' });
  await page.evaluate(() => window.ProctorHarness.gradeFails('Student is already being graded.'));
  await retry.click();
  await expect(page.getByRole('alert')).toContainText('already being graded');
  await expect(retry).toBeEnabled();
  await page.evaluate(() => window.ProctorHarness.gradeFails(''));
  await retry.click();
  await expect(retry).toBeDisabled();
  await expect(page.getByRole('status').filter({ hasText: 'Retry #13 queued' })).toBeVisible();
  const sent = await page.evaluate(() => window.ProctorHarness.calls.workspace.filter(call => /^Grade/.test(call.call)));
  expect(sent).toEqual([{ call: 'GradeExam', sessionIds: [] }, ...Array.from({ length: 2 }, () => ({ call: 'GradeStudent', sessionId: '00000002-0000-4000-8000-000000000000', submissionId: 0 }))]);
  await page.getByRole('button', { name: 'Review 002468' }).click();
  expect(await page.evaluate(() => window.ProctorHarness.calls.windows.at(-1).name)).toBe('review-00000002-0000-4000-8000-000000000000');
  // The other class run was masked by the first student's confirmed grade. It becomes visible now.
  await update({ grading: 11, state: 'done', points: 35, max_points: 40 }, { grading: 13, state: 'running' });
  await expect(receipt).toContainText('Class grading finished · 1 graded');
  await expect(receipt).toContainText('2 of 2 finished · 1 graded · 1 failed');
  await expect(receipt).not.toContainText('queued for grading');
  await receipt.getByRole('button', { name: 'Hide', exact: true }).click();
  await expect(receipt).toHaveCount(0);
});

for (const width of [1280, 390]) {
  test(`workspace roster and grade preview fit at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 850 });
    await monitor(page, { students: [workspaceStudent(0, { grade: { grading: 1, state: 'done', points: 26, max_points: 40 } }), workspaceStudent(1, { grade: { grading: 2, state: 'running' } })] });
    await expand(line(page, 'Ada Lovelace'));
    await page.screenshot({ path: info.outputPath('workspace-monitoring.png'), fullPage: true });
    await openTab(page, 'Grading');
    const scroll = page.getByRole('region', { name: /^Grading table/ });
    if (width === 390) {
      await scroll.focus();
      await page.keyboard.press('ArrowRight');
      await expect.poll(() => scroll.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
      await scroll.evaluate(el => el.scrollLeft = 0);
    }
    await page.screenshot({ path: info.outputPath('workspace-grading.png'), fullPage: true });
    const open = page.getByRole('button', { name: 'Grade submissions…' });
    await open.click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    // When all the content fits, the footer should sit just below the form, not at the bottom of the viewport.
    const footerGap = await dialog.evaluate(el => el.querySelector('footer').getBoundingClientRect().top - el.querySelector('form').getBoundingClientRect().bottom);
    expect(Math.abs(footerGap)).toBeLessThanOrEqual(1);
    await dialog.screenshot({ path: info.outputPath('grade-preview.png') });
    if (width === 390) {
      await page.setViewportSize({ width, height: 500 });
      const body = dialog.locator('.body');
      expect(await body.evaluate(el => el.scrollHeight)).toBeGreaterThan(await body.evaluate(el => el.clientHeight));
      await body.evaluate(el => { el.scrollTop = el.scrollHeight; });
      await expect(dialog.getByRole('heading')).toBeInViewport();
      await expect(dialog.getByRole('button', { name: 'Grade 1 submission' })).toBeInViewport();
    }
    await page.keyboard.press('Escape');
    await expect(open).toBeFocused();
  });
}

test('the grades are downloaded as a file the proctor chooses, and the proctor is told how many are confirmed (Phase 8)', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 800 });
  const students2 = [row(0, 'Ada Lovelace', '001234'), row(1, 'Alan Turing', '002468'), row(2, 'Grace Hopper', '003579')];
  await page.goto('/#/monitoring');
  await page.getByRole('table', { name: /All sessions/ }).waitFor();
  const roster = { students: [{ ...workspaceStudent(0), grade: { grading: 1, state: 'done', points: 28, max_points: 40, confirmed: true, at: observed } },
    { ...workspaceStudent(1), account_number: '002468', grade: { grading: 2, state: 'done', points: 31, max_points: 40, confirmed: false, at: observed } },
    { ...workspaceStudent(2), account_number: '003579', submissions: 0 }] };
  await page.evaluate(({ sessions, roster, observed }) => { window.ProctorHarness.workspace(roster); window.ProctorHarness.monitoringRoster(sessions, [], observed); }, { sessions: students2, roster, observed });
  await openTab(page, 'Grading');
  const button = page.getByRole('button', { name: 'Download grades…' });
  await expect(button).toBeEnabled();
  await button.click();
  await expect(page.getByRole('status').filter({ hasText: 'Grades saved to ' })).toContainText('1 grade confirmed, 1 not confirmed yet (in the advisory_points column, not in points)');
  expect((await page.evaluate(() => window.ProctorHarness.calls.workspace)).filter(call => call.call === 'SaveWorkspaceGrades')).toEqual([{ call: 'SaveWorkspaceGrades', examId: expect.any(String) }]);
  // Cancelling the file dialog says nothing, and a failure is shown in words.
  await page.evaluate(() => window.ProctorHarness.saveCancelled(true));
  await button.click();
  await expect(page.getByRole('status').filter({ hasText: 'Grades saved to ' })).toHaveCount(0);
  await page.evaluate(() => { window.ProctorHarness.saveCancelled(false); window.ProctorHarness.gradeFails('The file could not be written there.'); });
  await button.click();
  await expect(page.getByRole('alert').filter({ hasText: 'The file could not be written there.' })).toBeVisible();
});

test('with nobody graded there are no grades to download', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 800 });
  await monitor(page, { students: [workspaceStudent(0, { submissions: 1 }), workspaceStudent(1, { submissions: 1, grade: { grading: 1, state: 'running', at: observed } })] });
  await openTab(page, 'Grading');
  await expect(page.getByRole('button', { name: 'Download grades…' })).toBeDisabled();
  await page.evaluate(() => window.ProctorHarness.workspaceRoster(null));
  await expect(page.getByRole('button', { name: 'Download grades…' })).toHaveCount(0, { timeout: 8000 });
});

test('nobody to grade, no button to press; a question exam has no grading at all', async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 800 });
  await monitor(page, { students: [workspaceStudent(0, { submissions: 0 }), workspaceStudent(1, { submissions: 1, grade: { grading: 1, state: 'running', at: observed } })] });
  await openTab(page, 'Grading');
  await expect(page.getByRole('button', { name: 'Grade submissions…' })).toBeDisabled();
  await page.evaluate(() => window.ProctorHarness.workspaceRoster(null));
  await expect(page.getByRole('tab', { name: /^Grading/ })).toHaveCount(0, { timeout: 8000 });
  await expect(page.getByRole('button', { name: 'Grade submissions…' })).toHaveCount(0);
});

test('the review window has a Grading tab: the runs, the results of each TODO with their logs, and the grade that stands', async ({ page }) => {
  await review(page, gradedData([run(3, { state: 'running', points: null, max_points: null, results: [], stdout: '', stderr: '', finished_at: null, duration_ms: 0 }), run(2, { confirmed: true }), run(1, { points: 40, results: [] })]));
  await expect(gradingTab(page)).toContainText('running');
  const tile = page.getByRole('button', { name: /^Grade Being graded/ });
  await tile.click();
  await expect(gradingTab(page)).toBeFocused();
  const runTabs = page.getByRole('tablist', { name: 'Grading runs' });
  await expect(runTabs.getByRole('tab')).toHaveCount(3);
  await expect(runTabs.getByRole('tab', { name: /^#2/ })).toContainText('Confirmed');
  // The latest run is open first; a different run can also be selected by keyboard.
  await expect(page.getByRole('region', { name: 'Results of run 3' })).toContainText('Running the hidden tests');
  await runTabs.getByRole('tab', { name: /^#3/ }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(runTabs.getByRole('tab', { name: /^#2/ })).toBeFocused();
  const results = page.getByRole('region', { name: 'Results of run 2' });
  await expect(results).toContainText('16 / 40');
  await expect(results).toContainText('Confirmed');
  const firstTodo = results.getByRole('button', { name: /^t1/ });
  await expect(firstTodo).toContainText('10 / 10');
  await firstTodo.click();
  await expect(results.getByText('4/4 tests passed')).toBeVisible();
  await results.getByRole('button', { name: 'Collapse all' }).click();
  await expect(firstTodo).toBeHidden();
  await results.getByRole('button', { name: 'Expand all' }).click();
  await expect(results.getByRole('button', { name: /^t2/ })).toContainText('6 / 15');
  await expect(results.getByText('FAILED: insert_sorted 5 1 3', { exact: false })).toBeVisible();
  await expect(results.getByText('The grade program said nothing of it')).toBeVisible();
  await expect(results.getByRole('button', { name: /^zz/ })).toContainText('Not a TODO of this exam; it is not counted');
  const output = results.getByRole('button', { name: 'What the run printed' });
  await output.click();
  const dialog = page.getByRole('dialog', { name: 'What run #2 printed' });
  await expect(dialog.getByRole('table')).toContainText('4/4 tests passed');
  await dialog.getByRole('button', { name: 'Raw JSON' }).click();
  await expect(dialog.getByLabel('Standard output of the run')).toContainText('{"t1": {"passed": true}}');
  await dialog.getByRole('button', { name: /^Standard error/ }).click();
  await expect(dialog.getByLabel('Standard error of the run')).toContainText('ok: push_front 1 2 3 -> 3 2 1');
  await expect(dialog.locator('script, img')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(output).toBeFocused();
});

test('a grade is confirmed and the confirmation taken back, and the proctor is told the server\'s refusals', async ({ page }) => {
  await review(page, gradedData([run(2), run(1, { points: 40 })]));
  await openTab(page, 'Grading');
  const results = page.getByRole('region', { name: 'Results of run 2' });
  const confirmDialog = page.getByRole('dialog', { name: 'Confirm grade' });
  const confirmButton = results.getByRole('button', { name: 'Confirm 16 / 40' });
  await confirmButton.click();
  await expect(confirmDialog.getByRole('checkbox')).toBeFocused();
  await expect(confirmDialog.getByRole('button', { name: 'Confirm 16 / 40' })).toBeDisabled();
  await page.mouse.click(4, 4);
  await expect(confirmDialog).toBeHidden();
  await expect(confirmButton).toBeFocused();
  expect(await page.evaluate(() => window.ProctorHarness.calls.workspace.filter(call => call.call === 'ConfirmGrading'))).toEqual([]);
  await confirmButton.click();
  await confirmDialog.getByRole('checkbox').check();
  await confirmDialog.getByRole('button', { name: 'Confirm 16 / 40' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Grade confirmed: 16 / 40' })).toBeVisible();
  const runs = page.getByRole('tablist', { name: 'Grading runs' });
  await expect(runs.getByRole('tab', { name: /^#2/ })).toContainText('Confirmed');
  await expect(page.getByRole('region', { name: 'Summary' })).toContainText('Confirmed · run #2');
  // Confirming the other run takes the confirmation from this one.
  await runs.getByRole('tab', { name: /^#1/ }).click();
  const runOne = page.getByRole('region', { name: 'Results of run 1' });
  await runOne.getByRole('button', { name: 'Confirm 40 / 40' }).click();
  await confirmDialog.getByRole('checkbox').check();
  await confirmDialog.getByRole('button', { name: 'Confirm 40 / 40' }).click();
  await expect(runs.getByRole('tab', { name: /^#2/ })).toContainText('Advisory');
  await runOne.getByRole('button', { name: 'Take the confirmation back' }).click();
  const undo = page.getByRole('dialog', { name: 'Take the confirmation back' });
  await undo.getByRole('checkbox').check();
  await undo.getByRole('button', { name: 'Take confirmation back' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'advisory again' })).toBeVisible();
  expect(await page.evaluate(() => window.ProctorHarness.calls.workspace.filter(call => call.call === 'ConfirmGrading'))).toEqual([
    { call: 'ConfirmGrading', id: 2, confirm: true }, { call: 'ConfirmGrading', id: 1, confirm: true }, { call: 'ConfirmGrading', id: 1, confirm: false }]);
  await page.evaluate(() => window.ProctorHarness.gradeFails('The server refused the change: only a run that is done can be confirmed.'));
  await runOne.getByRole('button', { name: 'Confirm 40 / 40' }).click();
  await confirmDialog.getByRole('checkbox').check();
  await confirmDialog.getByRole('button', { name: 'Confirm 40 / 40' }).click();
  await expect(confirmDialog.getByRole('alert')).toContainText('only a run that is done can be confirmed');
});

test('a student is graded from the review window, the new run waits, and its end shows without a click', async ({ page }) => {
  await review(page, gradedData([run(1)]));
  await openTab(page, 'Grading');
  const start = page.getByRole('button', { name: 'Grade this student' });
  await expect(page.getByLabel('Submission to grade').locator('option:checked')).toContainText('the latest');
  await start.click();
  await expect(page.getByRole('status').filter({ hasText: 'The run is queued' })).toBeVisible();
  const runs = page.getByRole('tablist', { name: 'Grading runs' }).getByRole('tab');
  await expect(runs).toHaveCount(2, { timeout: 8000 });
  await expect(runs.first()).toContainText('Queued');
  expect(await page.evaluate(() => window.ProctorHarness.calls.workspace.filter(call => call.call === 'GradeStudent'))).toEqual([{ call: 'GradeStudent', sessionId: SESSION, submissionId: 0 }]);
  // A run that is going cannot be started again; when it ends, the window shows the result by itself.
  await expect(page.getByRole('button', { name: 'A run is going…' })).toBeDisabled();
  await page.evaluate(() => window.ProctorHarness.gradingRun(2, { state: 'done', points: 25, max_points: 40, finished_at: '2035-01-01T22:26:00Z', results: [{ todo_id: 't1', known: true, passed: true, points: 10, log: 'all good' }] }));
  await expect(runs.first()).toContainText('25 / 40', { timeout: 8000 });
  await expect(start).toBeEnabled();
  await expect(page.getByRole('status').filter({ hasText: /Run #2 finished/ })).toBeVisible();
  const result = page.getByRole('region', { name: 'Results of run 2' });
  await result.getByRole('button', { name: /^t1/ }).click();
  await expect(result.getByText('all good')).toBeVisible();
  // A refusal comes back in the server's words.
  await page.evaluate(() => window.ProctorHarness.gradeFails('The server refused the change: this student is already waiting to be graded or being graded.'));
  await start.click();
  await expect(page.getByRole('alert').filter({ hasText: 'already waiting to be graded' })).toBeVisible();
});

test('a run that failed says why, and shows what the grade program printed', async ({ page }) => {
  await review(page, gradedData([run(1, { state: 'failed', failure: 'malformed', failure_text: 'what grade printed is not one JSON object: invalid character \'m\'', points: null, max_points: null, results: [],
    stdout: 'make: entering directory /work\n{"t1": {"passed": true}}', stderr: 'warning: something\n', confirmed: false })]));
  await openTab(page, 'Grading');
  const results = page.getByRole('region', { name: 'Results of run 1' });
  await expect(results.getByRole('alert')).toContainText('Grading failed: its output was not what a grade program must print');
  await expect(results.getByRole('alert')).toContainText('not one JSON object');
  await expect(results.getByRole('button', { name: /^Confirm/ })).toHaveCount(0);
  await results.getByText('What the run printed').click();
  const dialog = page.getByRole('dialog', { name: 'What run #1 printed' });
  await expect(dialog.getByLabel('Standard output of the run')).toContainText('make: entering directory /work');
  await dialog.getByRole('button', { name: 'Close', exact: true }).last().click();
  await expect(page.getByRole('region', { name: 'Summary' })).toContainText('Grading failed: its output was not what a grade program must print');
});

test('a student who was never graded says so', async ({ page }) => {
  await review(page, gradedData([]));
  await openTab(page, 'Grading');
  await expect(page.getByText('This student has not been graded.')).toBeVisible();
  await expect(page.getByRole('region', { name: 'Summary' })).toContainText('Not graded');
});

const event = (id, type, detail) => ({ id: `event:${id}`, kind: 'event', occurred_at: '2035-01-01T00:05:00Z', event: { event_log_id: id, event_type: type, detail, screenshots: [] } });

test('the history names workspace events in words and has a workspace filter', async ({ page }) => {
  const withEvents = data();
  withEvents.events = [event(89, 'workspace.submitted', { final: false, snapshot: 4, files: 5 }), event(90, 'workspace.opened', null), event(91, 'file.saved', { path: 'src/main.c', bytes: 120, delta_bytes: 16 }), event(92, 'paste.blocked', { target: 'editor', bytes: 30 }),
    event(93, 'todo.set', { id: 't1', done: true }), event(94, 'run.started', { which: 'check', exit_code: 2 }), event(95, 'workspace.submitted', { final: true, snapshot: 5, files: 6 })];
  await review(page, withEvents);
  const history = page.getByRole('group', { name: 'Show events' });
  const workspaceFilter = history.getByRole('button', { name: /^Workspace/ });
  await expect(workspaceFilter).toBeVisible();
  await expect(workspaceFilter).toHaveAttribute('aria-pressed', 'true');
  const table = page.getByRole('region', { name: 'Session review' }).getByRole('table').first();
  await expect(table).toContainText('src/main.c · 120 bytes (+16)');
  await expect(table).toContainText('Blocked a paste into the editor (30 bytes)');
  await expect(table).toContainText('t1 marked done (self-reported)');
  await expect(table).toContainText('Check ended with code 2');
  await expect(table).toContainText('Final submission · snapshot 5 · 6 files');
  // What the student's own button is called is what the history calls it, and it is not the end.
  await expect(table).toContainText('Save pressed · snapshot 4 · 5 files');
  await expect(table.getByRole('row').filter({ hasText: 'Paste blocked' })).toHaveCount(1);
  // The filter hides them.
  await workspaceFilter.click();
  await expect(table).not.toContainText('src/main.c · 120 bytes');
  await expect(table).toContainText('Answers');
});

test('a session with no workspace events has no workspace filter', async ({ page }) => {
  await page.goto(`/#/review/${SESSION}`);
  const filters = page.getByRole('group', { name: 'Show events' });
  await expect(filters).toBeVisible();
  await expect(filters.getByRole('button', { name: /^Workspace/ })).toHaveCount(0);
});

test('the auto-refresh switch pauses background requests but not a manual refresh', async ({ page }) => {
  await page.clock.install();
  await review(page, gradedData([run(1, { state: 'running' })]));
  await expect(gradingTab(page)).toContainText('running');
  const refresh = page.getByRole('switch', { name: 'Auto-refresh' });
  const requests = () => page.evaluate(() => {
    const calls = window.ProctorHarness.calls;
    return { history: calls.timeline.length, workspace: calls.workspace.length };
  });
  await expect(refresh).toBeChecked();
  await refresh.click();
  const before = await requests();
  await page.clock.fastForward(6000);
  expect(await requests()).toEqual(before);
  await page.evaluate(() => window.ProctorHarness.gradingRun(1, { state: 'done', points: 16, max_points: 40 }));
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(gradingTab(page)).toContainText('advisory');
  expect((await requests()).history).toBeGreaterThan(before.history);
  await refresh.click();
  const resumed = await requests();
  await page.clock.fastForward(6000);
  await expect.poll(async () => (await requests()).history).toBeGreaterThan(resumed.history);
});

test('the review fits a narrow window and copies real data from each view', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async text => { window.__copied = text; } } });
  });
  const errors = [];
  page.on('pageerror', err => errors.push(err.message));
  await review(page, gradedData([run(1)]));
  const fits = async () => expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  const copied = () => page.evaluate(() => window.__copied);
  await page.getByRole('button', { name: 'Session details' }).click();
  await page.getByRole('button', { name: 'Copy session ID' }).click();
  await expect.poll(copied).toBe(SESSION);
  await fits();
  await openTab(page, 'Files');
  await page.getByRole('treeitem', { name: /^Makefile/ }).click();
  await page.getByRole('button', { name: 'Copy file' }).click();
  await expect.poll(copied).toBe('all:\n\techo hi\n');
  await fits();
  await openTab(page, 'Terminals');
  await page.getByRole('grid', { name: /Transcripts/ }).getByRole('row', { name: /^Run/ }).click();
  await page.getByRole('button', { name: 'Plain text' }).click();
  await page.getByRole('button', { name: 'Copy transcript' }).click();
  await expect.poll(copied).toBe('compiling\nresult: 3 2 1\na late line\n');
  await fits();
  await openTab(page, 'Grading');
  await page.getByRole('button', { name: 'Expand all' }).click();
  await fits();
  await page.getByRole('button', { name: 'What the run printed' }).click();
  const output = page.getByRole('dialog', { name: 'What run #1 printed' });
  await output.getByRole('button', { name: 'Copy output' }).click();
  await expect(output.getByRole('status')).toContainText('Output copied');
  await expect.poll(copied).toBe(run(1).stdout);
  await fits();
  expect(errors).toEqual([]);
});
