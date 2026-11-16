import { test, expect } from '@playwright/test';
import { connect as connectTo, calls, examsPanel, examCard } from './helpers.js';

// A workspace exam's own image (Phase 7): importing a workspace package, the exam list's image chip, the dialog that builds the image and
// shows its log, and a publication that waits for the image.
const PRACTICE = '33333333-3333-4333-8333-333333333333';
const DRAFT = '77777777-7777-4777-8777-777777777777';
const DIGEST = 'sha256:baac8cde6b9011521f80a878fc8fc475f9cf173da0ee274c2c0d316a9715cbb9';

const goodPackage = (extra = {}) => ({
  folder: 'lab-image', valid: true, title: 'Laboratory – a library the course image does not have', duration_seconds: 3600, image: 'cbe/cpp20', terminal: true, run: 'make run', check: 'make check',
  cpu: 1, memory_mb: 1024, disk_mb: 100, todos: [{ id: 't1', text: 'make run prints hello, <your name> using libgreet', points: 10 }], points: 10, starter_files: 2, grading_files: 1, has_grading: true,
  own_image: true, image_files: 3, image_warnings: [], ...extra,
});
const ok = (id, extra = {}) => ({ build_id: id, state: 'ok', hash: '0123456789abcdef', base_name: 'cbe/cpp20', base_digest: DIGEST, warnings: [], duration_ms: 3600, log: `Base image: cbe/cpp20 is localhost/cbe/cpp20:latest\n            digest ${DIGEST} (pinned by this build to the image the server has)\n\nVerified: made from cbe/cpp20, user 1000:1000, the agent is the course image's.\n`, ...extra });

async function open(page, prepare) {
  await connectTo(page, { prepare, openClass: 'Mathematics 10A' });
  return examsPanel(page);
}
// Makes "Practice exam" a workspace exam whose image is in a state.
const workspaceExam = (state, ready, builds = []) => ({ state, ready, builds });
const setUp = (page, args) => page.evaluate(({ id, args }) => window.ProctorHarness.workspaceExam(id, args), { id: PRACTICE, args });

test('a workspace package is previewed, given a PIN and imported, and says its own image has to be built', async ({ page }) => {
  const exams = await open(page);
  await page.evaluate(pkg => window.ProctorHarness.workspacePackage(pkg), goodPackage());
  await exams.getByRole('button', { name: 'Import workspace exam…' }).click();
  const preview = page.getByTestId('workspace-import');
  await expect(preview).toContainText('lab-image');
  await expect(preview).toContainText('Laboratory – a library the course image does not have');
  await expect(preview).toContainText('01:00:00');
  await expect(preview).toContainText('make run prints hello, <your name> using libgreet');
  await expect(preview).toContainText('10 points');
  await expect(preview).toContainText('self-reported');
  await expect(preview).toContainText('2 starter files');
  await expect(preview).toContainText('kept on the server and never sent to students');
  await expect(preview).toContainText('Its own image');
  await expect(preview).toContainText('3 files in image/');
  await expect(preview.getByRole('button', { name: 'Import workspace exam' })).toBeDisabled();
  await expect(preview).toContainText('Enter a PIN to continue');
  await preview.getByLabel('Exam PIN').fill('pin-1234');
  await preview.getByRole('button', { name: 'Import workspace exam' }).click();
  await expect(exams.getByRole('status')).toContainText('Imported “Laboratory – a library the course image does not have” into Mathematics 10A');
  await expect(exams.getByRole('status')).toContainText('It has its own image: build it');
  await expect(preview).toBeHidden();
  const card = examCard(exams, 'Laboratory – a library the course image does not have');
  await expect(card).toContainText('Workspace');
  await expect(card).toContainText('Image not built');
  await expect(card).toContainText('TODO items');
  await expect(card.getByRole('button', { name: /^Publish / })).toBeDisabled();
  await expect(card.getByRole('button', { name: /^Preview / })).toBeDisabled();
  const sent = (await calls(page)).workspaceImport;
  expect(sent.imported).toEqual([{ classId: '44444444-4444-4444-8444-444444444444', pin: 'pin-1234' }]);
});

test('a package with no image folder and with warnings is told so, and a package with problems cannot be imported', async ({ page }) => {
  const exams = await open(page);
  await page.evaluate(pkg => window.ProctorHarness.workspacePackage(pkg), goodPackage({ own_image: false, image_files: 0, has_grading: false, grading_files: 0 }));
  await exams.getByRole('button', { name: 'Import workspace exam…' }).click();
  let preview = page.getByTestId('workspace-import');
  await expect(preview).toContainText('The course image cbe/cpp20, as it is.');
  await expect(preview).toContainText('None: this exam cannot be graded by the server.');
  await preview.getByRole('button', { name: 'Cancel' }).click();
  await expect(preview).toBeHidden();
  expect((await calls(page)).workspaceImport.discarded).toBe(1);

  await page.evaluate(pkg => window.ProctorHarness.workspacePackage(pkg), goodPackage({ image_warnings: ['image/Containerfile line 3: apt-get installs libfoo-dev without a version (write name=version)'] }));
  await exams.getByRole('button', { name: 'Import workspace exam…' }).click();
  preview = page.getByTestId('workspace-import');
  await expect(preview).toContainText('The Containerfile has warnings');
  await expect(preview).toContainText('apt-get installs libfoo-dev without a version');
  await preview.getByRole('button', { name: 'Cancel' }).click();

  await page.evaluate(pkg => window.ProctorHarness.workspacePackage(pkg), { folder: 'bad', valid: false, problems: ['image/Containerfile line 1: FROM ubuntu:24.04: the only image a workspace exam’s Containerfile may start from is the exam’s own course image, cbe/cpp20'], todos: [], image_warnings: [] });
  await exams.getByRole('button', { name: 'Import workspace exam…' }).click();
  preview = page.getByTestId('workspace-import');
  await expect(preview.getByRole('alert')).toContainText('This package cannot be imported');
  await expect(preview).toContainText('FROM ubuntu:24.04');
  await expect(preview.getByLabel('Exam PIN')).toHaveCount(0);
  await preview.getByRole('button', { name: 'Close' }).click();
  await expect(preview).toBeHidden();
});

test('what the server refuses on import is shown and the package is used up', async ({ page }) => {
  const exams = await open(page);
  await page.evaluate(pkg => { window.ProctorHarness.workspacePackage(pkg); window.ProctorHarness.importWorkspaceFails('invalid workspace exam: this server does not have the course image cbe/cpp20'); }, goodPackage());
  await exams.getByRole('button', { name: 'Import workspace exam…' }).click();
  const preview = page.getByTestId('workspace-import');
  await preview.getByLabel('Exam PIN').fill('pin-1234');
  await preview.getByRole('button', { name: 'Import workspace exam' }).click();
  await expect(exams.getByRole('alert')).toContainText('this server does not have the course image cbe/cpp20');
  await expect(preview).toBeHidden();
});

test('the image chips of the exam list say what each exam has, and only workspace exams have one', async ({ page }) => {
  const exams = await open(page, () => window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', { state: 'ready', ready: true, builds: [{ build_id: 1, state: 'ok', hash: '0123456789abcdef' }] }));
  const practice = examCard(exams, 'Practice exam');
  await expect(practice).toContainText('Workspace');
  await expect(practice).toContainText('Image ready');
  await expect(practice).toContainText('TODO items');
  await expect(practice.getByRole('button', { name: /^Publish / })).toBeEnabled();
  // A question exam has none of it.
  const databases = examCard(exams, 'Databases midterm');
  await expect(databases).not.toContainText('Workspace');
  await expect(databases).not.toContainText('Image');
  await expect(databases).toContainText('Exercises');
  await expect(databases.getByRole('button', { name: /^Image of / })).toHaveCount(0);
  // The states, one by one, and what each does to publishing.
  for (const [state, ready, chip, blocked] of [['not_built', false, 'Image not built', true], ['queued', false, 'Build waiting', true], ['building', false, 'Building the image…', true], ['failed', false, 'Build failed', true],
    ['failed', true, 'Last build failed', false], ['missing', false, 'Image missing', true], ['ready', true, 'Image ready', false]]) {
    await setUp(page, workspaceExam(state, ready, [{ build_id: 1, state: state === 'ready' ? 'ok' : state === 'building' ? 'running' : state === 'queued' ? 'queued' : 'failed', hash: ready ? '0123456789abcdef' : '' }]));
    await expect(practice).toContainText(chip);
    const publish = practice.getByRole('button', { name: /^Publish / });
    if (blocked) { await expect(publish).toBeDisabled(); await expect(publish).toHaveAttribute('title', /build|image/i); } else await expect(publish).toBeEnabled();
  }
});

test('the download of a workspace exam is its grades, and a question exam still downloads students\' answers (Phase 8)', async ({ page }) => {
  const exams = await open(page, () => window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', { state: 'ready', ready: true, builds: [{ build_id: 1, state: 'ok', hash: '0123456789abcdef' }] }));
  const practice = examCard(exams, 'Practice exam');
  await expect(practice.getByRole('button', { name: 'Download submissions of Practice exam…' })).toHaveCount(0);
  await practice.getByRole('button', { name: 'Download grades of Practice exam…' }).click();
  await expect(exams.getByRole('status')).toContainText('Grades of “Practice exam” saved to ');
  await expect(exams.getByRole('status')).toContainText('advisory_points');
  await expect(page.getByRole('dialog', { name: /Download/ })).toHaveCount(0);
  expect((await calls(page)).workspace.filter(call => call.call === 'SaveWorkspaceGrades')).toEqual([{ call: 'SaveWorkspaceGrades', examId: PRACTICE }]);
  // A question exam opens the dialog of its answers, as it always did.
  await examCard(exams, 'Databases midterm').getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
});

test('the image dialog builds the image, follows the build and shows its log with the base image’s digest', async ({ page }) => {
  const exams = await open(page, () => window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', { state: 'not_built', ready: false }));
  const practice = examCard(exams, 'Practice exam');
  await practice.getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  await expect(dialog).toContainText('Image not built');
  await expect(dialog).toContainText('Students’ containers never have network');
  await expect(dialog).toContainText('Nothing has been built yet.');
  await expect(dialog.getByRole('button', { name: 'Build the image' })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Build the image' }).click();
  await expect(dialog).toContainText('Build waiting');
  await expect(dialog.getByRole('button', { name: 'Build the image' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Building…' })).toBeDisabled();
  await expect(dialog).toContainText('The build is waiting for its turn');
  expect((await calls(page)).workspaceImport.builds).toEqual([{ id: PRACTICE, rebuild: false }]);
  // The build starts, goes on, and ends: the dialog follows it without a click.
  await page.evaluate(() => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { state: 'running', started_at: new Date().toISOString() }));
  await expect(dialog).toContainText('Building the image…', { timeout: 6000 });
  await expect(dialog).toContainText('has not printed anything yet');
  await page.evaluate(log => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { state: 'ok', hash: '0123456789abcdef', duration_ms: 3600, finished_at: new Date().toISOString(), log }),
    `Base image: cbe/cpp20 is localhost/cbe/cpp20:latest\n            digest ${DIGEST} (pinned by this build to the image the server has)\n\nVerified: made from cbe/cpp20, user 1000:1000, the agent is the course image's.\n`);
  await expect(dialog).toContainText('Image ready', { timeout: 6000 });
  await expect(dialog).toContainText('sha256:baac8cde6b90…');
  await expect(dialog.getByText('0123456789abcdef', { exact: true })).toBeVisible();
  await expect(dialog.getByText('0123456789abcdef', { exact: true })).toHaveAttribute('title', 'localhost/cbe-exam/33333333-3333-4333-8333-333333333333:0123456789abcdef');
  await expect(dialog.getByRole('heading', { name: 'Build log · build #1' })).toBeVisible();
  await expect(dialog).toContainText('4 s');
  await expect(dialog.getByLabel('Build output')).toContainText(`digest ${DIGEST}`);
  await expect(dialog.getByLabel('Build output')).toContainText('Verified: made from cbe/cpp20');
  // The exam list behind the dialog follows too.
  await dialog.getByTestId('image-dialog').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(practice).toContainText('Image ready');
  await expect(practice.getByRole('button', { name: /^Publish / })).toBeEnabled();
});

test('a build that fails says why, keeps its log, and can be tried again; warnings are shown', async ({ page }) => {
  const failed = { build_id: 2, state: 'failed', failure: 'verify', message: 'the build changed the user of the image to "root", and it must stay "1000:1000": end the Containerfile with USER 1000:1000',
    warnings: ['image/Containerfile line 3: apt-get installs libfoo-dev without a version'], duration_ms: 1500, log: 'Base image: cbe/cpp20\nthe build changed the user of the image to "root"\n' };
  const exams = await open(page);
  await page.evaluate(args => window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', args), workspaceExam('failed', false, [failed]));
  const practice = examCard(exams, 'Practice exam');
  await expect(practice).toContainText('Build failed');
  await practice.getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  await expect(dialog.getByRole('alert')).toContainText('The image does not keep the course image’s contract.');
  await expect(dialog.getByRole('alert')).toContainText('end the Containerfile with USER 1000:1000');
  await expect(dialog.getByRole('alert')).not.toContainText('An earlier build');
  await expect(dialog).toContainText('Warnings');
  await expect(dialog).toContainText('apt-get installs libfoo-dev without a version');
  await expect(dialog.getByLabel('Build output')).toContainText('the build changed the user of the image');
  await expect(dialog.getByRole('button', { name: 'Try the build again' })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Try the build again' }).click();
  await expect(dialog).toContainText('Build waiting');
  expect((await calls(page)).workspaceImport.builds).toEqual([{ id: PRACTICE, rebuild: false }]);
});

test('an image that was built before a failed rebuild is still the exam’s image, and an earlier log can be read', async ({ page }) => {
  const builds = [{ build_id: 3, state: 'failed', failure: 'build', message: 'building at STEP "RUN make": exit status 2', log: 'cc: error: no such file\n', duration_ms: 900 },
    { build_id: 2, state: 'ok', hash: '0123456789abcdef', cached: true, log: 'nothing was built\n', duration_ms: 10 }, { build_id: 1, state: 'ok', hash: '0123456789abcdef', log: 'the first build\nVerified.\n', duration_ms: 3600 }];
  const exams = await open(page);
  await page.evaluate(args => window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', args), workspaceExam('failed', true, builds));
  const practice = examCard(exams, 'Practice exam');
  await expect(practice).toContainText('Last build failed');
  await practice.getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  await expect(dialog.getByRole('alert')).toContainText('A step of the build failed.');
  await expect(dialog.getByRole('alert')).toContainText('An earlier build of the same files is still the exam’s image.');
  await expect(dialog.getByLabel('Build output')).toContainText('cc: error: no such file');
  await expect(dialog.getByRole('button', { name: 'Build again from scratch' })).toBeEnabled();
  const list = dialog.getByLabel('Log of');
  await expect(list.locator('option')).toHaveText(['#3 · failed', '#2 · ready (already built)', '#1 · ready']);
  await list.selectOption({ label: '#1 · ready' });
  await expect(dialog.getByLabel('Build output')).toContainText('the first build');
  await expect(dialog.getByRole('heading', { name: /build #1/ })).toBeVisible();
  expect((await calls(page)).workspaceImport.buildReads).toEqual([{ id: PRACTICE, buildId: 1 }]);
  await dialog.getByRole('button', { name: 'Build again from scratch' }).click();
  await expect(dialog).toContainText('Build waiting');
  expect((await calls(page)).workspaceImport.builds).toEqual([{ id: PRACTICE, rebuild: true }]);
});

test('a refusal of the server to build is shown, and a missing image is said', async ({ page }) => {
  const exams = await open(page);
  await page.evaluate(args => { window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', args); window.ProctorHarness.buildFails('The server refused: this exam has no image folder: it runs in its course image, so there is nothing to build'); }, workspaceExam('missing', false, [{ build_id: 1, state: 'ok', hash: '0123456789abcdef' }]));
  await examCard(exams, 'Practice exam').getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  await expect(dialog.getByRole('alert').first()).toContainText('The runner does not have this exam’s image any more.');
  await dialog.getByRole('button', { name: /Build again|Try the build again/ }).first().click();
  await expect(dialog.getByRole('alert').last()).toContainText('this exam has no image folder');
});

test('the dialog says so when the image cannot be read, and closes with the keyboard', async ({ page }) => {
  const exams = await open(page);
  await page.evaluate(args => { window.ProctorHarness.workspaceExam('33333333-3333-4333-8333-333333333333', args); window.ProctorHarness.imageReadFails('Server unreachable or request timed out.'); }, workspaceExam('not_built', false));
  await examCard(exams, 'Practice exam').getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  await expect(dialog.getByRole('alert')).toContainText('Server unreachable');
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

// The log of a build that is going (decision P7-10): it grows in the dialog as the build prints, from where the last look ended, with where the
// build is and when it last printed; the follow box keeps its end in view, and scrolling up to read stops it.
test('the dialog shows the log of a build as it grows, asking only for what is new, with its progress', async ({ page }) => {
  const exams = await open(page);
  const P = '33333333-3333-4333-8333-333333333333';
  const lines = (from, to) => Array.from({ length: to - from + 1 }, (_, i) => `output line ${from + i}\n`).join('');
  const start = 'Base image: cbe/cpp20 is localhost/cbe/cpp20:latest\nSTEP 1/9: FROM localhost/cbe/cpp20@sha256:baac\nSTEP 2/9: RUN make\n';
  await page.evaluate(({ id, args }) => window.ProctorHarness.workspaceExam(id, args), { id: P, args: workspaceExam('building', false, [{ build_id: 1, state: 'running', started_at: new Date().toISOString(), step: 2, steps: 9, step_text: 'RUN make', log: start, last_output_at: new Date().toISOString() }]) });
  await examCard(exams, 'Practice exam').getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  const output = dialog.getByLabel('Build output');
  await expect(output).toContainText('STEP 2/9: RUN make');
  await expect(dialog.getByTestId('build-progress')).toContainText('Step 2 of 9');
  await expect(dialog.getByTestId('build-progress')).toContainText('RUN make');
  await expect(dialog.getByRole('progressbar', { name: 'Build progress' })).toHaveAttribute('aria-valuenow', '11');
  await expect(dialog.getByTestId('build-progress')).toContainText('Last output just now');
  // More output arrives, and only it is asked for: the offset of the last look.
  const first = start.length;
  await page.evaluate(({ log, step }) => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { log, step, step_text: 'RUN apt-get install -y make' }), { log: start + lines(1, 3), step: 3 });
  await expect(output).toContainText('output line 3', { timeout: 6000 });
  await expect(dialog.getByTestId('build-progress')).toContainText('Step 3 of 9');
  await expect(dialog.getByTestId('build-progress')).toContainText('apt-get install');
  const reads = (await calls(page)).workspaceImport.reads.filter(item => item.withLog);
  expect(reads[0].logFrom).toBe(0);
  expect(reads.some(item => item.logFrom === first)).toBe(true);
  // What was shown once is not shown twice.
  expect(await output.innerText()).toBe(start + lines(1, 3));
  // A long log keeps its end in view while the box follows it.
  await page.evaluate(({ log }) => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { log }), { log: start + lines(1, 200) });
  await expect(output).toContainText('output line 200', { timeout: 6000 });
  await expect.poll(() => output.evaluate(box => box.scrollHeight - box.scrollTop - box.clientHeight)).toBeLessThan(24);
  // Scrolling up to read stops following: new output does not move the box. The follow box says so, and turns it on again.
  await output.evaluate(box => { box.scrollTop = 0; });
  await expect(dialog.getByLabel('Follow the end of the log')).not.toBeChecked();
  await page.evaluate(({ log }) => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { log }), { log: start + lines(1, 220) });
  await expect(output).toContainText('output line 220', { timeout: 6000 });
  expect(await output.evaluate(box => box.scrollTop)).toBeLessThan(24);
  await dialog.getByLabel('Follow the end of the log').check();
  await expect.poll(() => output.evaluate(box => box.scrollHeight - box.scrollTop - box.clientHeight)).toBeLessThan(24);
  // The build ends: the log is the whole of the runner's, the progress is gone, and there is nothing to follow.
  await page.evaluate(({ log }) => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { state: 'ok', hash: '0123456789abcdef', duration_ms: 9000, finished_at: new Date().toISOString(), log }), { log: 'Base image: cbe/cpp20\n[the whole log of the runner]\nVerified.\n' });
  await expect(dialog).toContainText('Image ready', { timeout: 6000 });
  await expect(output).toHaveText('Base image: cbe/cpp20\n[the whole log of the runner]\nVerified.\n');
  await expect(dialog.getByTestId('build-progress')).toHaveCount(0);
  await expect(dialog.getByLabel('Follow the end of the log')).toHaveCount(0);
});

test('a build that has been quiet for a while says so, and a build that goes on from the start of a new one starts its log again', async ({ page }) => {
  const exams = await open(page);
  const P = '33333333-3333-4333-8333-333333333333';
  await page.evaluate(({ id, args }) => window.ProctorHarness.workspaceExam(id, args), { id: P, args: workspaceExam('building', false, [{ build_id: 1, state: 'running', started_at: new Date(Date.now() - 90000).toISOString(), step: 4, steps: 4, step_text: 'RUN apt-get update', log: 'STEP 4/4: RUN apt-get update\n', last_output_at: new Date(Date.now() - 65000).toISOString() }]) });
  await examCard(exams, 'Practice exam').getByRole('button', { name: /^Image of / }).click();
  const dialog = page.getByRole('dialog', { name: 'Image of Practice exam' });
  await expect(dialog.getByTestId('build-progress')).toContainText(/Last output 1:0\d ago/);
  // The build fails and the proctor starts another: its log is its own, from the start.
  await page.evaluate(() => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { state: 'failed', failure: 'build', message: 'exit status 100', log: 'STEP 4/4: RUN apt-get update\nE: could not resolve\n' }));
  await expect(dialog).toContainText('Build failed', { timeout: 6000 });
  await dialog.getByRole('button', { name: /Try the build again/ }).click();
  await expect(dialog).toContainText('Build waiting');
  await page.evaluate(() => window.ProctorHarness.imageBuild('33333333-3333-4333-8333-333333333333', { state: 'running', started_at: new Date().toISOString(), step: 1, steps: 4, step_text: 'FROM x', log: 'STEP 1/4: FROM x\n' }));
  await expect(dialog.getByLabel('Build output')).toHaveText('STEP 1/4: FROM x\n', { timeout: 6000 });
  await expect(dialog.getByLabel('Build output')).not.toContainText('could not resolve');
});
