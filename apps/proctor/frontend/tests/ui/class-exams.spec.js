import { test, expect } from '@playwright/test';
import { connect as connectTo, calls, admitting, card, examsPanel, tab } from './helpers.js';

const EXAM = '11111111-1111-4111-8111-111111111111';
const PRACTICE = '33333333-3333-4333-8333-333333333333';
const DRAFT = '77777777-7777-4777-8777-777777777777';
const HISTORY_FINAL = '88888888-8888-4888-8888-888888888888';
const MATH = '44444444-4444-4444-8444-444444444444';
const HISTORY = '55555555-5555-4555-8555-555555555555';

// Opens Mathematics 10A (or another class) on its Exams tab and returns that panel.
async function connect(page, prepare, openClass = 'Mathematics 10A') {
  await connectTo(page, { prepare, openClass });
  return examsPanel(page);
}
const admittingLine = (klass, exam, working) => `Admitting: ${exam} — ${klass} · ${working} working`;

test('the exams of the class are listed with which one students are admitted to', async ({ page }) => {
  const exams = await connect(page);
  await expect(tab(page, 'Exams (3)')).toHaveAttribute('aria-selected', 'true');
  await expect(exams.locator('tbody tr')).toHaveCount(3);
  const published = exams.locator('tbody tr', { hasText: 'Databases midterm' });
  await expect(published).toContainText('Published'); await expect(published).toContainText('Monitoring');
  await expect(published).toContainText('12');
  await expect(exams.locator('tbody tr', { hasText: 'Practice exam' })).toContainText('Forward only'); await expect(published).toContainText('90 min + 30 s grace'); await expect(published).toContainText('1 working · 90 total');
  const practice = exams.locator('tbody tr', { hasText: 'Practice exam' });
  await expect(practice).not.toContainText('Published'); await expect(practice).toContainText('Logins are closed now');
  // The class is the page's own heading; the table does not repeat it, and the bar above says what is admitted.
  await expect(exams).not.toContainText('Class: Mathematics 10A');
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
});

test('only the exams of this class are listed', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.historyExam());
  await expect(exams.locator('tbody tr')).toHaveCount(3);
  await expect(exams).not.toContainText('History final');
  await page.getByRole('button', { name: 'Classes', exact: true }).first().click(); // the back button
  await card(page, 'History 11B').click();
  await expect(tab(page, 'Exams (1)')).toBeVisible();
  const history = examsPanel(page);
  await expect(history.locator('tbody tr')).toHaveCount(1);
  await expect(history.locator('tbody tr')).toContainText('History final');
  await expect(history).not.toContainText('Databases midterm');
});

test('a class with no exams invites the first import', async ({ page }) => {
  const exams = await connect(page, undefined, 'History 11B');
  await expect(tab(page, 'Exams (0)')).toBeVisible();
  await expect(exams).toContainText('This class has no exams yet. Import one from its Markdown file.');
  await expect(exams.getByRole('button', { name: 'Import exam…' })).toBeEnabled();
  await expect(exams.locator('table')).toHaveCount(0);
});

test('with nothing published, every exam can be published and none can be closed', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.nothingPublished());
  await expect(exams.getByRole('button', { name: 'Publish Databases midterm…' })).toBeVisible();
  await expect(exams.getByRole('button', { name: /Close admission/ })).toHaveCount(0);
  await expect(admitting(page)).toContainText('Admission closed');
});

test('an exam file is previewed as students see it, imported into the class with the PIN the proctor types, but not published', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'midterm-b.md', valid: true, title: 'Midterm B', exercise_count: 2, slot_count: 2, points: 10, navigation: 'forward_only', duration_seconds: 3600, grace_seconds: 30, min_volume_percent: 70, opens_at: null, closes_at: null }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  const preview = exams.locator('.import-stack');
  await expect(preview.getByRole('region', { name: 'Exam file', exact: true })).toBeVisible();
  await expect(preview.getByTitle('midterm-b.md')).toBeVisible();
  await expect(preview).toContainText('Midterm B'); await expect(preview).toContainText('2 exercises, 2 answer fields, 10 points');
  await expect(preview.locator('.duration')).toHaveText('1 h');
  await expect(preview.getByRole('definition').filter({ hasText: 'grace' }).locator('small')).toHaveText('plus 30 s grace, counted from each student’s first login');
  await expect(preview).toContainText('kept at 70% or louder'); await expect(preview).toContainText('any time');
  await expect(preview).toContainText('Importing stores the exam in Mathematics 10A');
  await preview.getByRole('button', { name: 'Preview the exam', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Preview — Midterm B', exact: true });
  const student = dialog.locator('.exam-preview');
  await expect(student.getByRole('cell', { name: '1a.alu' })).toBeVisible();
  await expect(student).toContainText('2.code · code (mips)'); await expect(student).toContainText('main:');
  await expect(student.locator('img')).toHaveCount(0);
  expect(await page.evaluate(() => window.__unsafe)).toBeUndefined();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  // The file asked for forward-only navigation; the proctor can change it before importing.
  const forward = preview.getByRole('checkbox', { name: /Students cannot go back to an exercise once they move on/ });
  await expect(forward).toBeChecked();
  const importButton = preview.getByRole('button', { name: 'Import exam', exact: true });
  await expect(importButton).toBeDisabled();
  const pin = preview.getByLabel('Exam PIN');
  await pin.fill('announce-me-42');
  await expect(pin).toHaveAttribute('type', 'password');
  await preview.getByRole('button', { name: 'Show PIN' }).click(); await expect(pin).toHaveAttribute('type', 'text');
  await preview.getByRole('button', { name: 'Hide PIN' }).click(); await expect(pin).toHaveAttribute('type', 'password');
  await forward.uncheck();
  // The focus threshold is not a per-exam setting any more: it lives in the proctor's Settings.
  await expect(preview.getByLabel(/Focus alert after/)).toHaveCount(0);
  await importButton.click();
  await expect(page.getByRole('status')).toContainText('Imported “Midterm B” into Mathematics 10A. It is stored but not published');
  const row = exams.locator('tbody tr', { hasText: 'Midterm B' });
  await expect(row).toBeVisible(); await expect(row).not.toContainText('Published'); await expect(row).not.toContainText('Forward only');
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
  await expect(preview).toHaveCount(0);
  await expect(tab(page, 'Exams (4)')).toBeVisible();
  const done = await calls(page);
  expect(done.chosenFor).toEqual([MATH]);
  expect(done.imported).toEqual([{ classId: MATH, pin: 'announce-me-42', forwardOnly: false }]);
});

test('an exam is imported into the class that is open, whichever it is', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'midterm-b.md', valid: true, title: 'Midterm B', exercise_count: 4, slot_count: 2, navigation: 'free', duration_seconds: 3600, grace_seconds: 30, min_volume_percent: 70, opens_at: null, closes_at: null }), 'History 11B');
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await exams.getByLabel('Exam PIN').fill('p');
  await exams.getByRole('button', { name: 'Import exam', exact: true }).click();
  await expect(exams.locator('tbody tr', { hasText: 'Midterm B' })).toBeVisible();
  await expect(page.getByRole('status')).toContainText('into History 11B');
  expect((await calls(page)).imported[0]).toMatchObject({ classId: HISTORY });
});

test('problems in an exam file are listed and nothing can be imported', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'broken.md', valid: false, problems: ['title is required and must be at most 512 bytes of text', 'line 12: answer field "1a.x" is already used; give each field its own name'] }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(exams.getByRole('alert')).toContainText('This file cannot be imported');
  await expect(exams.getByRole('listitem').filter({ hasText: 'title is required' })).toBeVisible();
  await expect(exams.getByRole('listitem').filter({ hasText: 'is already used; give each field its own name' })).toBeVisible();
  await expect(exams.getByRole('button', { name: 'Import exam', exact: true })).toHaveCount(0);
  await exams.getByRole('button', { name: 'Close', exact: true }).click();
  expect(await calls(page)).toMatchObject({ importExam: 0, discardImport: 1 });
});

test('closing the file dialog or cancelling a preview imports nothing', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(page.getByRole('heading', { name: /^Exam file/ })).toHaveCount(0);
  await page.evaluate(() => window.ProctorHarness.examFile({ file: 'a.md', valid: true, title: 'A', exercise_count: 1, slot_count: 2, navigation: 'free', duration_seconds: 60, grace_seconds: 30, min_volume_percent: 0 }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(exams).toContainText('the student’s volume is left alone');
  await exams.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(await calls(page)).toMatchObject({ chooseExam: 2, importExam: 0, discardImport: 1 });
});

test('leaving the class with a preview open forgets the file, and the preview is not kept for the next class', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'midterm-b.md', valid: true, title: 'Midterm B', exercise_count: 4, slot_count: 2, navigation: 'free', duration_seconds: 3600, grace_seconds: 30, min_volume_percent: 70, opens_at: null, closes_at: null }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(page.getByRole('region', { name: 'Exam file', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Classes', exact: true }).first().click(); // the back button
  await expect.poll(async () => (await calls(page)).discardImport).toBe(1);
  await card(page, 'Mathematics 10A').click();
  await expect(page.getByRole('heading', { name: /^Exam file/ })).toHaveCount(0);
});

test('a failed import is reported and the preview is discarded', async ({ page }) => {
  const exams = await connect(page, () => { window.ProctorHarness.importFails(true); window.ProctorHarness.examFile({ file: 'a.md', valid: true, title: 'A', exercise_count: 1, slot_count: 2, navigation: 'free', duration_seconds: 60, grace_seconds: 30, min_volume_percent: 0 }); });
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await exams.getByLabel('Exam PIN').fill('p');
  await exams.getByRole('button', { name: 'Import exam', exact: true }).click();
  await expect(exams.getByRole('alert')).toContainText('The server rejected the document');
  await expect(page.getByRole('heading', { name: /^Exam file/ })).toHaveCount(0);
});

test('publishing needs no class: it names the students of this class, and over working students needs an explicit confirmation', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await expect(dialog.getByLabel('Class')).toHaveCount(0);
  await expect(dialog).toContainText('The 28 students of Mathematics 10A will log in to Practice exam');
  await expect(dialog).toContainText('Students of other classes cannot.');
  await expect(dialog).toContainText('1 student still working in “Databases midterm” (Mathematics 10A)');
  await expect(dialog).toContainText('cannot log back in');
  await expect(dialog).toContainText('Its login window is closed by the server’s clock');
  const confirm = dialog.getByRole('button', { name: 'Publish exam', exact: true });
  await expect(confirm).toBeDisabled();
  await dialog.getByRole('checkbox').check();
  await confirm.click();
  await expect(page.getByRole('status')).toContainText('“Practice exam” is published to Mathematics 10A');
  expect((await calls(page)).publish).toEqual([{ examId: PRACTICE, expectedExam: EXAM, allowActive: true, confirmed: true }]);
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Practice exam', 1));
  await expect(exams.locator('tbody tr', { hasText: 'Practice exam' })).toContainText('Monitoring');
  await expect(exams.locator('tbody tr', { hasText: 'Practice exam' })).toContainText('Published');
});

test('cancelling the publish dialog sends nothing', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await calls(page)).publish).toEqual([]);
});

test('publishing when nobody is working needs only a plain confirmation', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.working(0));
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).not.toContainText('still working'); await expect(dialog).toContainText('nobody is working in it');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Publish exam', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('is published');
  expect((await calls(page)).publish[0]).toMatchObject({ allowActive: false });
});

test('nothing published: the dialog has nothing to replace and sends no earlier exam', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.nothingPublished());
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await expect(dialog).not.toContainText('stops admitting students'); await expect(dialog).not.toContainText('still working');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Publish exam', exact: true }).click();
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Practice exam', 1));
  expect((await calls(page)).publish).toEqual([{ examId: PRACTICE, expectedExam: '', allowActive: false, confirmed: true }]);
});

test('a refusal from the server stays in the dialog and changes nothing', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.publishFails('The server refused the change: the current exam changed; refresh and retry.'));
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Publish exam', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('the current exam changed');
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
});

test('publishing an exam of another class replaces the admission, naming the exam and class that stop', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.historyExam(), 'History 11B');
  await exams.getByRole('button', { name: 'Publish History final…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await expect(dialog).toContainText('The 24 students of History 11B will log in to History final');
  await expect(dialog).toContainText('1 student still working in “Databases midterm” (Mathematics 10A)');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Publish exam', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('“History final” is published to History 11B');
  await expect(admitting(page)).toContainText(admittingLine('History 11B', 'History final', 1));
  expect((await calls(page)).publish).toEqual([{ examId: HISTORY_FINAL, expectedExam: EXAM, allowActive: true, confirmed: true }]);
  // The exam of the first class is no longer published there.
  await page.getByRole('button', { name: 'Classes', exact: true }).first().click();
  await card(page, 'Mathematics 10A').click();
  await expect(examsPanel(page).locator('tbody tr', { hasText: 'Databases midterm' })).not.toContainText('Published');
});

test('closing admission is confirmed and leaves running students alone', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Close admission to Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Nobody new will be able to log in'); await expect(dialog).toContainText('1 student still working');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Close admission', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Admission is closed');
  await expect(admitting(page)).toContainText('Admission closed');
  expect((await calls(page)).unpublish).toEqual([{ expectedExam: EXAM, allowActive: true, confirmed: true }]);
});

test('monitoring another exam never publishes it', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Monitor Practice exam' }).click();
  await expect.poll(() => calls(page).then(c => c.windows)).toEqual([{ name: 'monitoring', url: '/#/monitoring' }]);
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
  const c = await calls(page);
  expect(c.select).toEqual([PRACTICE]); expect(c.publish).toEqual([]);
});

test('an exam nobody has taken and that is not published can be deleted, after a confirmation', async ({ page }) => {
  const exams = await connect(page);
  const draft = exams.locator('tbody tr', { hasText: 'Draft quiz' });
  // Only exams nobody took, and that are not published, offer it.
  await expect(exams.locator('tbody tr', { hasText: 'Databases midterm' }).getByRole('button', { name: /Delete exam/ })).toHaveCount(0);
  await expect(exams.locator('tbody tr', { hasText: 'Practice exam' }).getByRole('button', { name: /Delete exam/ })).toHaveCount(0);
  await draft.getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete exam' });
  await expect(dialog).toContainText('Delete Draft quiz from Mathematics 10A?');
  await expect(dialog).toContainText('cannot be undone'); await expect(dialog).toContainText('no answers or screenshots are lost');
  const confirm = dialog.getByRole('button', { name: 'Delete exam', exact: true });
  await expect(confirm).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await calls(page)).deleteExam).toEqual([]);
  await draft.getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  await dialog.getByRole('checkbox').check();
  await confirm.click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('Exam “Draft quiz” deleted.');
  await expect(exams.locator('tbody tr', { hasText: 'Draft quiz' })).toHaveCount(0);
  await expect(tab(page, 'Exams (2)')).toBeVisible();
  expect((await calls(page)).deleteExam).toEqual([{ examId: DRAFT, confirmed: true }]);
});

test('a deletion the server refuses is explained in the dialog and the exam stays', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.deleteExamFails('The exam is published or students have taken it, so it cannot be deleted.'));
  await exams.getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete exam' });
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Delete exam', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('published or students have taken it');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(exams.locator('tbody tr', { hasText: 'Draft quiz' })).toBeVisible();
});

test('the exams panel stays inside the window at a narrow width', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 700 });
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'midterm-b.md', valid: true, title: 'Midterm B', exercise_count: 4, slot_count: 2, navigation: 'free', duration_seconds: 3600, grace_seconds: 30, min_volume_percent: 70 }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(exams).toContainText('Midterm B');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const bounds = await page.getByRole('dialog').boundingBox();
  expect(bounds.x).toBeGreaterThanOrEqual(0); expect(bounds.x + bounds.width).toBeLessThanOrEqual(600);
});

test('row actions are icon-only buttons whose name and tooltip still say what they do to which exam', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'long.md', valid: true, title: 'Manual synthetic exam — no grading with a very long title indeed', exercise_count: 3, slot_count: 2, navigation: 'free', duration_seconds: 600, grace_seconds: 30, min_volume_percent: 80, opens_at: null, closes_at: null }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await page.getByLabel('Exam PIN').fill('p');
  await page.getByLabel('Exam PIN').press('Enter');
  const long = 'Manual synthetic exam — no grading with a very long title indeed';
  const row = exams.locator('tbody tr', { hasText: 'Manual synthetic exam' });
  const published = exams.locator('tbody tr', { hasText: 'Databases midterm' });
  const practice = exams.locator('tbody tr', { hasText: 'Practice exam' });
  for (const [button, title] of [
    [row.getByRole('button', { name: `Monitor ${long}`, exact: true }), 'Nobody has taken this exam yet; publish it first'],
    [row.getByRole('button', { name: `Publish ${long}…`, exact: true }), `Publish ${long}…`],
    [row.getByRole('button', { name: `Delete exam ${long}…`, exact: true }), `Delete exam ${long}…`],
    [published.getByRole('button', { name: 'Close admission to Databases midterm…', exact: true }), 'Close admission to Databases midterm…'],
    [practice.getByRole('button', { name: 'Purge screenshots of Practice exam…', exact: true }), 'Delete the screenshots of Practice exam after grading'],
  ]) {
    await expect(button).toHaveText('');
    await expect(button).toHaveAttribute('title', title);
    const box = await button.boundingBox();
    expect(box.height).toBeLessThanOrEqual(40); expect(box.width).toBeLessThanOrEqual(40);
    const icon = button.locator('svg');
    await expect(icon).toBeVisible();
    await expect(icon).toHaveAttribute('viewBox', '0 0 24 24');
    await expect(icon).toHaveAttribute('aria-hidden', 'true');
    expect(await icon.locator('path, rect, circle').count()).toBeGreaterThan(0);
  }
  const iconSize = async button => (await button.locator('svg').boundingBox()).height;
  expect(await iconSize(row.getByRole('button', { name: `Monitor ${long}` }))).toBeGreaterThan(await iconSize(row.getByRole('button', { name: `Publish ${long}…` })));
  // Destructive actions keep a coral icon; the neutral ones stay ink-2 (ghost buttons, no outline).
  const iconColor = button => button.evaluate(element => getComputedStyle(element).color);
  const danger = await iconColor(practice.getByRole('button', { name: 'Purge screenshots of Practice exam…' }));
  expect(await iconColor(row.getByRole('button', { name: `Delete exam ${long}…` }))).toBe(danger);
  expect(await iconColor(row.getByRole('button', { name: `Publish ${long}…` }))).not.toBe(danger);
});

test('when the exams cannot be loaded the panel says so instead of loading forever, and the tab still counts them', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.catalogDown(true));
  await expect(exams.getByRole('alert')).toContainText('Cannot load the exams from the server. Invalid exam catalog; previous observations retained.');
  await expect(exams).not.toContainText('Loading exams…');
  await expect(tab(page, 'Exams (3)')).toBeVisible(); // from the class list
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  expect((await calls(page)).chooseExam).toBe(1);
});

test('screenshots are purged from the exam list after grading, with explicit confirmation and partial failures shown', async ({ page }) => {
  const exams = await connect(page);
  const practice = exams.locator('tbody tr', { hasText: 'Practice exam' });
  await expect(exams.locator('tbody tr', { hasText: 'Draft quiz' }).getByRole('button', { name: /Purge screenshots/ })).toHaveCount(0);
  await expect(exams.locator('tbody tr', { hasText: 'Databases midterm' }).getByRole('button', { name: /Purge screenshots/ })).toBeDisabled();
  await practice.getByRole('button', { name: 'Purge screenshots of Practice exam…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Practice exam'); await expect(dialog).toContainText('Downloads, exports and backups require separate deletion');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await calls(page)).purge).toEqual([]);
  await page.evaluate(() => window.ProctorHarness.partialPurge());
  await practice.getByRole('button', { name: 'Purge screenshots of Practice exam…' }).click();
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Confirm irreversible action' }).click();
  await expect(page.getByRole('status')).toContainText('PARTIAL: retry for image IDs 7');
  expect((await calls(page)).purge).toEqual([{ exam: PRACTICE, confirmed: true }]);
});

test('download submissions is offered only for exams somebody has taken, with an editable default name', async ({ page }) => {
  const exams = await connect(page);
  const published = exams.locator('tbody tr', { hasText: 'Databases midterm' });
  const practice = exams.locator('tbody tr', { hasText: 'Practice exam' });
  const draft = exams.locator('tbody tr', { hasText: 'Draft quiz' });
  await expect(draft.getByRole('button', { name: 'Download submissions of Draft quiz…' })).toHaveCount(0);
  await expect(published.getByRole('button', { name: 'Download submissions of Databases midterm…' })).toBeVisible();
  await expect(practice.getByRole('button', { name: 'Download submissions of Practice exam…' })).toBeVisible();
  await published.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Download submissions — Databases midterm' });
  await expect(dialog).toContainText('One Markdown file per student, with their answers in the exam.');
  await expect(dialog).toContainText('28'); await expect(dialog).toContainText('finished'); await expect(dialog).toContainText('will be downloaded');
  await expect(dialog).toContainText('2'); await expect(dialog).toContainText('still working'); await expect(dialog).toContainText('skipped; download again when they finish');
  await expect(dialog).toContainText('3'); await expect(dialog).toContainText('never logged in'); await expect(dialog).toContainText('listed in _index.md only');
  const name = dialog.getByLabel('Output folder name');
  await expect(name).toHaveValue('Mathematics_10A-Databases_midterm');
  const parent = dialog.getByLabel('Selected folder');
  await expect(parent).toHaveValue(''); await expect(parent).toHaveAttribute('readonly', '');
  await expect(dialog.getByRole('button', { name: 'Start download' })).toBeDisabled();
  await expect(dialog).not.toContainText('not on the server');
  expect((await calls(page)).prepareDownload).toEqual([EXAM]);
});

test('cancelling the download dialog sends nothing', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const done = await calls(page);
  expect(done.download).toEqual([]); expect(done.chooseDownloadFolder).toEqual([]);
});

test('choosing a folder only fills the path; Start download shows progress and keeps the dialog open', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.holdDownload());
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  // The folder is selected, but nothing has been written and the dialog is still there.
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Selected folder')).toHaveValue('/home/proctor/Downloads');
  await expect(dialog.getByLabel('Selected folder')).toHaveAttribute('readonly', '');
  await expect(dialog.getByRole('button', { name: 'Start download' })).toBeEnabled();
  expect((await calls(page)).download).toEqual([]);

  await dialog.getByLabel('Output folder name').fill('My Notes');
  await expect(dialog).toContainText('Files go into /home/proctor/Downloads/My Notes.');
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('button', { name: 'Downloading…' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Choose folder…' })).toBeDisabled();
  await expect(dialog.getByLabel('Output folder name')).toBeDisabled();
  await expect(dialog.getByLabel('Selected folder')).toBeDisabled();
  const bar = dialog.getByRole('progressbar', { name: 'Downloading submissions' });
  await expect(bar).toBeVisible();
  // Meaningful progress: the dialog shows the file count the server reports while the download runs.
  await page.evaluate(() => window.ProctorHarness.downloadProgress({ running: true, done: 2, total: 5, current: 'Student 003' }));
  await expect(bar).toHaveAttribute('aria-valuenow', '2');
  await expect(dialog).toContainText('2 of 5 files — Student 003');

  await page.evaluate(() => window.ProctorHarness.releaseDownload());
  await expect(dialog.getByRole('status')).toContainText('Downloaded 28 submissions to /home/proctor/Downloads/My Notes.');
  await expect(dialog).toContainText('2 students still working and were skipped.');
  await expect(dialog.getByRole('button', { name: 'Start download' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toHaveCount(0);
  await expect(dialog.locator('.actions').getByRole('button', { name: 'Close' })).toBeVisible();
  const open = dialog.getByRole('button', { name: 'Open folder' });
  await expect(open).toBeVisible();
  await open.click();
  expect((await calls(page)).openDownloadFolder).toBe(1);
  // The dialog stays open until Close is clicked.
  await expect(dialog).toBeVisible();
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect((await calls(page)).download).toEqual([{ examId: EXAM, folderName: 'My Notes', replace: false, confirmed: true }]);
});

test('a failure to open the folder is shown in the dialog, which stays open', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.openFolderFails('The download folder is no longer there.'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('status')).toContainText('Downloaded 28 submissions');
  await dialog.getByRole('button', { name: 'Open folder' }).click();
  await expect(dialog.getByRole('alert')).toContainText('The download folder is no longer there.');
  await expect(dialog).toBeVisible();
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
});

test('an existing download folder is replaced only after the question, and notes in it are lost', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadNeedsReplace());
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog).toContainText('Replace the 28 files from the earlier download?');
  await expect(dialog).toContainText('Notes you wrote in them are lost. Files you added yourself are left alone.');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);

  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Choose folder…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Start download' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Replace files' }).click();
  await expect(page.getByRole('dialog').getByRole('status')).toContainText('Downloaded 28 submissions');
  const done = await calls(page);
  expect(done.download).toEqual([{ examId: EXAM, folderName: 'Mathematics_10A-Databases_midterm', replace: false, confirmed: true }, { examId: EXAM, folderName: 'Mathematics_10A-Databases_midterm', replace: false, confirmed: true }, { examId: EXAM, folderName: 'Mathematics_10A-Databases_midterm', replace: true, confirmed: true }]);
  await page.getByRole('dialog').locator('.actions').getByRole('button', { name: 'Close' }).click();
});

test('a partial download names the students to retry, and still offers the folder', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadFailure('Student 099'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('status')).toContainText('Downloaded 27 submissions');
  await expect(dialog.getByRole('alert')).toContainText('PARTIAL');
  await expect(dialog.getByRole('alert')).toContainText('Student 099');
  await expect(dialog.getByRole('button', { name: 'Open folder' })).toBeVisible();
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
});

test('an error stops progress, keeps the dialog open, and lets the proctor try again', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadFails('Server unreachable. The download did not start.'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Server unreachable. The download did not start.');
  await expect(dialog.getByRole('progressbar')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Choose folder…' })).toBeEnabled();
  await expect(dialog.getByLabel('Output folder name')).toBeEnabled();
  await expect(dialog.getByRole('button', { name: 'Start download' })).toBeEnabled();
  await expect(dialog).toBeVisible();

  await page.evaluate(() => window.ProctorHarness.downloadFails(''));
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('status')).toContainText('Downloaded 28 submissions');
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
});

test('the folder chooser can fail or be cancelled without starting anything', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.chooseFolderFails('The folder dialog failed.'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await expect(dialog.getByRole('alert')).toContainText('The folder dialog failed.');
  await expect(dialog.getByRole('button', { name: 'Start download' })).toBeDisabled();
  expect((await calls(page)).download).toEqual([]);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
});

test('an exam without a stored source still downloads without a missing-source warning', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadPreview({ source_available: false, finishing: 2 }));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).not.toContainText('not on the server');
  await expect(dialog).not.toContainText('answers without the questions');
  await expect(dialog).toContainText('2'); await expect(dialog).toContainText('still finishing'); await expect(dialog).toContainText('the server is finalizing their answers');
});

test('the result wraps a long output path inside the dialog', async ({ page }) => {
  await page.setViewportSize({ width: 760, height: 700 });
  const exams = await connect(page, () => window.ProctorHarness.downloadFolder('/home/ideras/classes/2026/Q4_2026/CCC403-Organizacion_De_Computadoras/Evaluations'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('status')).toContainText('Downloaded 28 submissions to');
  // The long path wraps instead of widening the dialog past the window.
  const box = await dialog.boundingBox();
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(760);
  const path = dialog.locator('.result code');
  expect(await path.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
});

test('the panel keeps the result and Open folder after the download dialog is closed', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadFailure('Student 099'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('status')).toContainText('Downloaded 27 submissions');
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const line = exams.getByRole('status');
  await expect(line).toContainText('PARTIAL: downloaded 27 of 28 submissions to /home/proctor/Downloads/Mathematics_10A-Databases_midterm. Retry for Student 099.');
  await line.getByRole('button', { name: 'Open folder' }).click();
  expect((await calls(page)).openDownloadFolder).toBe(1);
  // The next action in the panel replaces the status line, and the button goes with it.
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(exams.getByRole('button', { name: 'Open folder' })).toHaveCount(0);
});

test('a download that wrote no student file offers no Open folder', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadPreview({ finished: 0, working: 2 }));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Start download' }).click();
  await expect(dialog.getByRole('status')).toContainText('Downloaded 0 submissions');
  await expect(dialog.getByRole('button', { name: 'Open folder' })).toHaveCount(0);
  await dialog.locator('.actions').getByRole('button', { name: 'Close' }).click();
  await expect(exams.getByRole('status')).toContainText('Downloaded 0 submissions');
  await expect(exams.getByRole('button', { name: 'Open folder' })).toHaveCount(0);
});

test('the import preview and the exam row say which calculator the exam gives its students', async ({ page }) => {
  // The import preview shows the calculator the file declares, with the defaults applied.
  const exams = await connect(page, () => window.ProctorHarness.examFile({ file: 'neural-b.md', valid: true, title: 'Neural B', exercise_count: 2, slot_count: 3, navigation: 'free', duration_seconds: 1800, grace_seconds: 30, min_volume_percent: 80, opens_at: null, closes_at: null, calculator: { modes: ['scientific', 'neural'], angle: 'rad', disabled: 1 } }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(exams.getByRole('definition').filter({ hasText: 'scientific, neural · radians · 1 function disabled' })).toBeVisible();
  await exams.locator('.actions').getByRole('button', { name: 'Cancel', exact: true }).click();
  // A file without a calculator explicitly marks the summary row as unavailable.
  await connect(page, () => window.ProctorHarness.examFile({ file: 'plain.md', valid: true, title: 'Plain', exercise_count: 2, slot_count: 3, navigation: 'free', duration_seconds: 1800, grace_seconds: 30, min_volume_percent: 80, opens_at: null, closes_at: null }));
  await exams.getByRole('button', { name: 'Import exam…' }).click();
  await expect(exams.getByText('Calculator', { exact: true }).locator('..').getByRole('definition')).toHaveText('Not available');
  await exams.locator('.actions').getByRole('button', { name: 'Cancel', exact: true }).click();
  // Without scientific mode the angle setting has no effect, so it is not named.
  for (const [modes, text] of [[['programmer'], 'Calculator: programmer'], [['neural'], 'Calculator: neural'], [['programmer', 'neural'], 'Calculator: programmer, neural · 1 function disabled']]) {
    const file = { file: 'modes.md', valid: true, title: 'Modes', exercise_count: 2, slot_count: 3, navigation: 'free', duration_seconds: 1800, grace_seconds: 30, min_volume_percent: 80, opens_at: null, closes_at: null, calculator: { modes, angle: 'user', disabled: modes.length === 2 ? 1 : 0 } };
    await connect(page, `window.ProctorHarness.examFile(${JSON.stringify(file)})`);
    await exams.getByRole('button', { name: 'Import exam…' }).click();
    await expect(exams.getByText('Calculator', { exact: true }).locator('..').getByRole('definition')).toHaveText(text.replace(/^Calculator: /, ''));
    await exams.locator('.actions').getByRole('button', { name: 'Cancel', exact: true }).click();
  }
  // The exam row shows the calculator as a badge on exams that have one.
  const draft = exams.locator('tbody tr', { hasText: 'Draft quiz' });
  await expect(draft.getByText('Calculator: scientific, programmer · radians · 1 function disabled')).toBeVisible();
  const published = exams.locator('tbody tr', { hasText: 'Databases midterm' });
  await expect(published.getByText('Calculator:')).toHaveCount(0);
});

test("the exam row preview action opens the exam; its calculator waits for the tools button and floats", async ({ page }) => {
  const exams = await connect(page);
  const row = exams.locator('tbody tr', { hasText: 'Draft quiz' });
  // The action sits after the monitor action; the Draft quiz declares the calculator.
  await row.getByRole('button', { name: 'Preview Draft quiz' }).click();
  const dialog = page.getByRole('dialog', { name: 'Preview — Draft quiz' });
  await expect(dialog).toBeVisible();
  // No calculator on open: the tools button waits in the top-left corner.
  await expect(dialog.locator('.calc-floating')).toHaveCount(0);
  const tools = dialog.getByRole('button', { name: 'Open the calculator' });
  await expect(tools).toBeVisible();
  await tools.click();
  const calculator = dialog.locator('.calc-floating');
  await expect(calculator).toBeVisible();
  await expect(calculator).toContainText('Nothing here is recorded');
  await expect(calculator.getByRole('tab', { name: 'Programmer' })).toBeVisible(); // both modes from the exam
  await calculator.getByLabel('Expression').fill('2+3*4');
  await calculator.getByRole('button', { name: 'Evaluate' }).click();
  await expect(calculator.locator('.calc-result')).toContainText('= 14');
  // Non-modal: the exam content stays visible and usable behind the floating panel.
  await expect(dialog.getByText('The exam as students see it')).toBeVisible();
  // The tools button reflects the state, and the panel's own close button closes just the calculator.
  await expect(tools).toHaveAttribute('aria-expanded', 'true');
  await calculator.getByRole('button', { name: 'Close the calculator' }).click();
  await expect(dialog.locator('.calc-floating')).toHaveCount(0);
  await expect(dialog).toBeVisible(); // the preview dialog itself stays open
  // An exam without a calculator offers no tools button.
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  const published = exams.locator('tbody tr', { hasText: 'Databases midterm' });
  await published.getByRole('button', { name: 'Preview Databases midterm' }).click();
  await expect(page.getByRole('dialog', { name: 'Preview — Databases midterm' })).toContainText('This exam has no calculator');
  await expect(page.getByRole('dialog', { name: 'Preview — Databases midterm' }).getByRole('button', { name: 'Open the calculator' })).toHaveCount(0);
});
