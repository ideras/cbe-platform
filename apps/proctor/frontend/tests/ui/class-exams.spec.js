import { test, expect } from '@playwright/test';
import { connect as connectTo, calls, admitting, admittingLine, card, examCard, examsPanel, tab, tickPublish } from './helpers.js';

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

// The publish dialog: its button, the tick that unlocks it (clicked by its label, since the box itself is drawn), and, for it and
// the purge dialog, the Close button of a dialog that shows its result.
const publishButton = dialog => dialog.getByRole('button', { name: /^(Publish & admit students|Publishing…)$/ });
const closeResult = dialog => dialog.locator('.foot').getByRole('button', { name: 'Close', exact: true });

test('the exams of the class are listed with which one students are admitted to', async ({ page }) => {
  const exams = await connect(page);
  await expect(tab(page, 'Exams (3)')).toHaveAttribute('aria-selected', 'true');
  await expect(exams.getByRole('article')).toHaveCount(3);
  const published = examCard(exams, 'Databases midterm');
  await expect(published).toContainText('Published'); await expect(published).toContainText('Monitoring');
  await expect(published).toContainText('12');
  await expect(examCard(exams, 'Practice exam')).toContainText('Forward only'); await expect(published).toContainText('01:30:00 + 00:00:30 grace'); await expect(published).toContainText('1 working / 90 total');
  const practice = examCard(exams, 'Practice exam');
  await expect(practice).not.toContainText('Published'); await expect(practice).toContainText('Logins are closed now');
  // The class is the page's own heading; the cards do not repeat it, and the bar above says what is admitted.
  await expect(exams).not.toContainText('Class: Mathematics 10A');
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
});

test('only the exams of this class are listed', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.historyExam());
  await expect(exams.getByRole('article')).toHaveCount(3);
  await expect(exams).not.toContainText('History final');
  await page.getByRole('button', { name: 'Classes', exact: true }).first().click(); // the back button
  await card(page, 'History 11B').click();
  await expect(tab(page, 'Exams (1)')).toBeVisible();
  const history = examsPanel(page);
  await expect(history.getByRole('article')).toHaveCount(1);
  await expect(history.getByRole('article')).toContainText('History final');
  await expect(history).not.toContainText('Databases midterm');
});

test('a class with no exams invites the first import', async ({ page }) => {
  const exams = await connect(page, undefined, 'History 11B');
  await expect(tab(page, 'Exams (0)')).toBeVisible();
  await expect(exams).toContainText('This class has no exams yet. Import one from its Markdown file.');
  await expect(exams.getByRole('button', { name: 'Import exam…' })).toBeEnabled();
  await expect(exams.getByRole('article')).toHaveCount(0);
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
  await expect(preview.locator('.duration')).toHaveText('01:00:00');
  await expect(preview.getByRole('definition').filter({ hasText: 'grace' }).locator('small')).toHaveText('plus 00:00:30 grace, counted from each student’s first login');
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
  const row = examCard(exams, 'Midterm B');
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
  await expect(examCard(exams, 'Midterm B')).toBeVisible();
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
  await expect(dialog).toContainText('28 students of Mathematics 10A');
  await expect(dialog).toContainText('Students of other classes');
  await expect(dialog).toContainText('1 student still working in “Databases midterm” (Mathematics 10A)');
  await expect(dialog).toContainText('cannot log back in');
  await expect(dialog).toContainText('Its login window is closed by the server’s clock');
  await expect(dialog).toContainText('Publishing this exam replaces it');
  const override = dialog.locator('label.override'), plain = dialog.locator('label.confirm:not(.override)');
  await expect(override).toContainText('Override: publish anyway, although 1 student is still working in “Databases midterm”.');
  await expect(plain).toHaveText('I want to publish “Practice exam”.');
  const confirm = publishButton(dialog);
  await expect(confirm).toBeDisabled();
  // Neither tick is enough alone: the override is a decision of its own, apart from wanting to publish.
  await plain.click(); await expect(confirm).toBeDisabled();
  await plain.click(); await override.click(); await expect(confirm).toBeDisabled();
  await plain.click(); await expect(confirm).toBeEnabled();
  await confirm.click();
  await expect(exams.getByRole('status')).toContainText('“Practice exam” is published to Mathematics 10A');
  expect((await calls(page)).publish).toEqual([{ examId: PRACTICE, expectedExam: EXAM, allowActive: true, confirmed: true }]);
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Practice exam', 1));
  await expect(examCard(exams, 'Practice exam')).toContainText('Monitoring');
  await expect(examCard(exams, 'Practice exam')).toContainText('Published');
  // The dialog stays open on its result until the proctor closes it.
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  await closeResult(dialog).click();
  await expect(dialog).toHaveCount(0);
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
  await expect(dialog.locator('label.override')).toHaveCount(0);
  await tickPublish(dialog); await publishButton(dialog).click();
  await expect(exams.getByRole('status')).toContainText('is published');
  expect((await calls(page)).publish[0]).toMatchObject({ allowActive: false });
});

test('nothing published: the dialog has nothing to replace and sends no earlier exam', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.nothingPublished());
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await expect(dialog).not.toContainText('stops admitting students'); await expect(dialog).not.toContainText('still working');
  await tickPublish(dialog); await publishButton(dialog).click();
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Practice exam', 1));
  expect((await calls(page)).publish).toEqual([{ examId: PRACTICE, expectedExam: '', allowActive: false, confirmed: true }]);
});

test('a refusal from the server stays in the dialog and changes nothing', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.publishFails('The server refused the change: the current exam changed; refresh and retry.'));
  await exams.getByRole('button', { name: 'Publish Practice exam…' }).click();
  const dialog = page.getByRole('dialog');
  await tickPublish(dialog); await publishButton(dialog).click();
  await expect(dialog.getByRole('alert')).toContainText('the current exam changed');
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
  // Nothing was published, so the dialog is still the form, with the tick as the proctor left it, ready to try again.
  await expect(dialog.getByRole('status')).toHaveCount(0);
  await expect(publishButton(dialog)).toBeEnabled();
});

test('publishing an exam of another class replaces the admission, naming the exam and class that stop', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.historyExam(), 'History 11B');
  await exams.getByRole('button', { name: 'Publish History final…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await expect(dialog).toContainText('24 students of History 11B');
  await expect(dialog).toContainText('1 student still working in “Databases midterm” (Mathematics 10A)');
  await tickPublish(dialog); await publishButton(dialog).click();
  await expect(exams.getByRole('status')).toContainText('“History final” is published to History 11B');
  await expect(admitting(page)).toContainText(admittingLine('History 11B', 'History final', 1));
  expect((await calls(page)).publish).toEqual([{ examId: HISTORY_FINAL, expectedExam: EXAM, allowActive: true, confirmed: true }]);
  // The exam of the first class is no longer published there.
  await closeResult(dialog).click();
  await page.getByRole('button', { name: 'Classes', exact: true }).first().click();
  await card(page, 'Mathematics 10A').click();
  await expect(examCard(examsPanel(page), 'Databases midterm')).not.toContainText('Published');
});

test('closing admission is confirmed and leaves running students alone', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Close admission to Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Nobody new will be able to log in'); await expect(dialog).toContainText('1 student still working');
  const override = dialog.getByRole('checkbox', { name: /Override/ }), plain = dialog.getByRole('checkbox', { name: 'I want to close admission.' });
  await expect(override.locator('..')).toContainText('Override: close admission anyway, although 1 student is still working in “Databases midterm”.');
  const close = dialog.getByRole('button', { name: 'Close admission', exact: true });
  await expect(close).toBeDisabled();
  // Neither tick is enough alone: the override is a decision of its own, apart from wanting to close.
  await plain.check(); await expect(close).toBeDisabled();
  await plain.uncheck(); await override.check(); await expect(close).toBeDisabled();
  await plain.check(); await expect(close).toBeEnabled();
  await close.click();
  await expect(page.getByRole('status')).toContainText('Admission is closed');
  await expect(admitting(page)).toContainText('Admission closed');
  expect((await calls(page)).unpublish).toEqual([{ expectedExam: EXAM, allowActive: true, confirmed: true }]);
});

test('closing admission when nobody is working needs only a plain confirmation', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.working(0));
  await exams.getByRole('button', { name: 'Close admission to Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('checkbox')).toHaveCount(1); await expect(dialog).not.toContainText('Override');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Close admission', exact: true }).click();
  await expect(admitting(page)).toContainText('Admission closed');
  expect((await calls(page)).unpublish).toEqual([{ expectedExam: EXAM, allowActive: false, confirmed: true }]);
});

test('the working count in the close-admission dialog follows the server while it is open, and the override is asked for again', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.working(0));
  await exams.getByRole('button', { name: 'Close admission to Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  const close = dialog.getByRole('button', { name: 'Close admission', exact: true });
  await dialog.getByRole('checkbox').check(); await expect(close).toBeEnabled();
  // A student logs in after the dialog opened: the override appears and the button waits for it.
  await page.evaluate(() => window.ProctorHarness.working(2));
  const override = dialog.getByRole('checkbox', { name: /Override/ });
  await expect(override).toBeVisible(); await expect(dialog).toContainText('2 students still working'); await expect(close).toBeDisabled();
  await override.check(); await expect(close).toBeEnabled();
  // The number changes after it was ticked: it is a different decision, so the tick is asked for again.
  await page.evaluate(() => window.ProctorHarness.working(3));
  await expect(dialog).toContainText('3 students are still working in “Databases midterm”');
  await expect(override).not.toBeChecked(); await expect(close).toBeDisabled();
  // They all finish: nothing is left to override.
  await page.evaluate(() => window.ProctorHarness.working(0));
  await expect(override).toHaveCount(0); await expect(close).toBeEnabled();
  await close.click();
  expect((await calls(page)).unpublish).toEqual([{ expectedExam: EXAM, allowActive: false, confirmed: true }]);
});

test('a student who logs in just before closing admission is refused, then the override is offered', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.working(0));
  await exams.getByRole('button', { name: 'Close admission to Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  const close = dialog.getByRole('button', { name: 'Close admission', exact: true });
  await dialog.getByRole('checkbox').check();
  await page.evaluate(() => window.ProctorHarness.studentsLogInLate(2));
  await close.click();
  await expect(dialog.getByRole('alert')).toContainText('students are still working');
  await expect(admitting(page)).not.toContainText('Admission closed');
  const override = dialog.getByRole('checkbox', { name: /Override/ });
  await expect(override).toBeVisible(); await expect(dialog).toContainText('2 students still working'); await expect(close).toBeDisabled();
  await override.check(); await close.click();
  await expect(admitting(page)).toContainText('Admission closed');
  expect((await calls(page)).unpublish).toEqual([
    { expectedExam: EXAM, allowActive: false, confirmed: true },
    { expectedExam: EXAM, allowActive: true, confirmed: true },
  ]);
});

test('monitoring another exam never publishes it', async ({ page }) => {
  const exams = await connect(page);
  await exams.getByRole('button', { name: 'Monitor Practice exam' }).click();
  await expect.poll(() => calls(page).then(c => c.windows)).toEqual([{ name: 'monitoring', url: '/#/monitoring' }]);
  await expect(admitting(page)).toContainText(admittingLine('Mathematics 10A', 'Databases midterm', 1));
  const c = await calls(page);
  expect(c.select).toEqual([PRACTICE]); expect(c.publish).toEqual([]);
});

test('an exam nobody has taken is deleted after one tick, and the page says so', async ({ page }) => {
  const exams = await connect(page);
  const draft = examCard(exams, 'Draft quiz');
  // A published exam cannot be deleted, so it has no button; any other exam has one, taken or not (see delete-exam-dialog.spec.js).
  await expect(examCard(exams, 'Databases midterm').getByRole('button', { name: /Delete exam/ })).toHaveCount(0);
  await expect(examCard(exams, 'Practice exam').getByRole('button', { name: 'Delete exam Practice exam…' })).toBeEnabled();
  await draft.getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete exam' });
  await expect(dialog).toContainText('Cannot be undone'); await expect(dialog).toContainText('Never taken'); await expect(dialog).toContainText('Nothing is lost');
  const confirm = dialog.getByRole('button', { name: 'Delete exam', exact: true });
  await expect(confirm).toBeDisabled();
  await dialog.getByRole('button', { name: 'Keep exam', exact: true }).click();
  expect((await calls(page)).deleteExam).toEqual([]);
  await draft.getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  await dialog.locator('label.confirm').click();
  await confirm.click();
  await expect(dialog.getByRole('status')).toContainText('Exam deleted');
  await expect(dialog).toContainText('Removed from Mathematics 10A');
  await closeResult(dialog).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('Exam “Draft quiz” deleted.');
  await expect(examCard(exams, 'Draft quiz')).toHaveCount(0);
  await expect(tab(page, 'Exams (2)')).toBeVisible();
  expect((await calls(page)).deleteExam).toEqual([{ examId: DRAFT, submissions: 0, confirmed: true }]);
});

test('a deletion the server refuses is explained in the dialog and the exam stays', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.deleteExamFails('The exam is published or students are still working in it, so it cannot be deleted. Close admission and let them finish first.'));
  await exams.getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete exam' });
  await dialog.locator('label.confirm').click(); await dialog.getByRole('button', { name: 'Delete exam', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('students are still working in it');
  await dialog.getByRole('button', { name: 'Keep exam', exact: true }).click();
  await expect(examCard(exams, 'Draft quiz')).toBeVisible();
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
  const row = examCard(exams, long);
  const published = examCard(exams, 'Databases midterm');
  const practice = examCard(exams, 'Practice exam');
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

// The code the proctor types to unlock a purge: the first eight characters of the exam's ID.
const PRACTICE_CODE = PRACTICE.slice(0, 8);

async function openPurge(page, exams) {
  await examCard(exams, 'Practice exam').getByRole('button', { name: 'Purge screenshots of Practice exam…' }).click();
  return page.getByRole('dialog', { name: 'Purge screenshots' });
}
// Ticks the grading confirmation and types the code, which is what unlocks the purge button.
async function unlockPurge(dialog) {
  await dialog.getByText('Grading is finalized and I confirm screenshot deletion for this exam.').click();
  await dialog.getByLabel(`Type the exam code ${PRACTICE_CODE} to unlock`).fill(PRACTICE_CODE);
}
const purgeButton = dialog => dialog.getByRole('button', { name: /^(Purge screenshots|Purging…)$/ });

test('the purge dialog says what goes, what stays and what it does not reach, and keeps the safe choice in front', async ({ page }) => {
  const exams = await connect(page);
  const practice = examCard(exams, 'Practice exam');
  await expect(examCard(exams, 'Draft quiz').getByRole('button', { name: /Purge screenshots/ })).toHaveCount(0);
  await expect(examCard(exams, 'Databases midterm').getByRole('button', { name: /Purge screenshots/ })).toBeDisabled();
  const dialog = await openPurge(page, exams);
  await expect(dialog.getByRole('heading', { name: 'Purge screenshots' })).toBeVisible();
  await expect(dialog).toContainText('Irreversible');
  await expect(dialog.locator('.exam')).toContainText('Practice exam');
  await expect(dialog.locator('.exam .id')).toHaveText(PRACTICE);
  await expect(dialog.locator('.exam .id b')).toHaveText(PRACTICE_CODE);
  const rows = dialog.getByRole('listitem');
  await expect(rows).toHaveCount(4);
  await expect(rows.nth(0)).toContainText('Deleted forever'); await expect(rows.nth(0)).toContainText('All stored screenshots for this exam.');
  await expect(rows.nth(1)).toContainText('Disabled permanently'); await expect(rows.nth(1)).toContainText('No further images can be captured for this exam.');
  await expect(rows.nth(2)).toContainText('Kept'); await expect(rows.nth(2)).toContainText('Answers and events (focus losses, heartbeats, locks).');
  await expect(rows.nth(3)).toContainText('Not touched — delete separately'); await expect(rows.nth(3)).toContainText('Downloads, exports and backups made earlier.');
  // The server does not say how many screenshots an exam has before a purge, so the dialog claims no number.
  await expect(dialog).not.toContainText(/\d[\d,]* (images|screenshots)/);
  // The safe button has the first focus; the dangerous one waits for the confirmation.
  await expect(dialog.getByRole('button', { name: 'Keep screenshots' })).toBeFocused();
  await expect(purgeButton(dialog)).toBeDisabled();
  await dialog.getByRole('button', { name: 'Keep screenshots' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(practice.getByRole('button', { name: 'Purge screenshots of Practice exam…' })).toBeFocused();
  expect((await calls(page)).purge).toEqual([]);
});

test('the purge button unlocks only after ticking that grading is final and typing the exam code', async ({ page }) => {
  const exams = await connect(page);
  const dialog = await openPurge(page, exams);
  const tick = dialog.getByRole('checkbox');
  const code = dialog.getByLabel(`Type the exam code ${PRACTICE_CODE} to unlock`);
  await expect(code).toBeDisabled(); await expect(code).toHaveAttribute('maxlength', '8'); await expect(code).toHaveAttribute('placeholder', PRACTICE_CODE);
  await expect(purgeButton(dialog)).toBeDisabled();
  await dialog.getByText('Grading is finalized and I confirm screenshot deletion for this exam.').click();
  await expect(tick).toBeChecked(); await expect(code).toBeEnabled(); await expect(code).toBeFocused();
  // Ticking alone is not enough, and neither is a code that is almost right.
  await expect(purgeButton(dialog)).toBeDisabled();
  await code.fill('3333333'); await expect(purgeButton(dialog)).toBeDisabled();
  await code.fill('33333334'); await expect(purgeButton(dialog)).toBeDisabled(); await expect(code).not.toHaveClass(/match/);
  await code.fill(PRACTICE_CODE); await expect(purgeButton(dialog)).toBeEnabled(); await expect(code).toHaveClass(/match/);
  // The code has to be typed: pasting it is refused.
  expect(await code.evaluate(field => { const event = new Event('paste', { bubbles: true, cancelable: true }); field.dispatchEvent(event); return event.defaultPrevented; })).toBe(true);
  // Taking the tick back takes the code back with it.
  await dialog.getByText('Grading is finalized and I confirm screenshot deletion for this exam.').click();
  await expect(tick).not.toBeChecked(); await expect(code).toBeDisabled(); await expect(code).toHaveValue('');
  await expect(purgeButton(dialog)).toBeDisabled();
  expect((await calls(page)).purge).toEqual([]);
});

test('Enter in the code field purges only once it is unlocked', async ({ page }) => {
  const exams = await connect(page);
  const dialog = await openPurge(page, exams);
  await dialog.getByText('Grading is finalized and I confirm screenshot deletion for this exam.').click();
  const code = dialog.getByLabel(`Type the exam code ${PRACTICE_CODE} to unlock`);
  await code.fill('3333'); await code.press('Enter');
  expect((await calls(page)).purge).toEqual([]);
  await code.fill(PRACTICE_CODE); await code.press('Enter');
  await expect(dialog.getByRole('status')).toContainText('Screenshots purged');
  expect((await calls(page)).purge).toEqual([{ exam: PRACTICE, confirmed: true }]);
});

test('while the server purges the dialog shows it is working and cannot be closed, then reports what was deleted', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.holdPurge());
  const dialog = await openPurge(page, exams);
  await unlockPurge(dialog);
  await purgeButton(dialog).click();
  await expect(dialog.getByRole('progressbar', { name: 'Purging screenshots' })).toBeVisible();
  await expect(dialog).toContainText('Deleting screenshots…');
  await expect(dialog.getByRole('button', { name: 'Purging…' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Keep screenshots' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('checkbox')).toBeDisabled();
  await expect(dialog.getByLabel(`Type the exam code ${PRACTICE_CODE} to unlock`)).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.releasePurge());
  const result = dialog.getByRole('status');
  await expect(result).toContainText('Screenshots purged');
  await expect(result).toContainText('1 image deleted · capture disabled for this exam');
  await expect(dialog.getByRole('progressbar')).toHaveCount(0);
  await expect(dialog).toContainText('Remember: any downloads, exports or backups that contain these screenshots still exist and must be deleted by hand.');
  await expect(dialog.getByRole('alert')).toHaveCount(0);
  await expect(dialog.getByText('Before you continue')).toHaveCount(0);
  // Only Close is left, and the status line of the page keeps the outcome once the dialog is gone.
  await expect(dialog.getByRole('button', { name: 'Purge screenshots' })).toHaveCount(0);
  await closeResult(dialog).click();
  await expect(dialog).toHaveCount(0);
  await expect(exams.getByRole('status')).toContainText('Screenshots of “Practice exam”: 1 deleted. Complete; this exam accepts no more images.');
  expect((await calls(page)).purge).toEqual([{ exam: PRACTICE, confirmed: true }]);
});

test('a purge that could not delete every image says which to retry, in the dialog and on the page', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.partialPurge());
  const dialog = await openPurge(page, exams);
  await unlockPurge(dialog);
  await purgeButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Screenshots partly purged');
  await expect(dialog.getByRole('status')).toContainText('1 image deleted; 1 image could not be deleted · capture disabled for this exam');
  await expect(dialog.getByRole('alert')).toContainText('PARTIAL: retry for image IDs 7.');
  await expect(dialog).toContainText('must be deleted by hand');
  await closeResult(dialog).click();
  await expect(exams.getByRole('status')).toContainText('PARTIAL: retry for image IDs 7');
});

test('the purge result counts with separators and says so when there was nothing stored', async ({ page }) => {
  let exams = await connect(page, () => window.ProctorHarness.purgeDeleted(1284));
  let dialog = await openPurge(page, exams);
  await unlockPurge(dialog); await purgeButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('1,284 images deleted · capture disabled for this exam');
  exams = await connect(page, () => window.ProctorHarness.purgeDeleted(0));
  dialog = await openPurge(page, exams);
  await unlockPurge(dialog); await purgeButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('No stored screenshots to delete · capture disabled for this exam');
});

test('a purge the server refuses keeps the dialog and the confirmation and says why', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.purgeFails(true));
  const dialog = await openPurge(page, exams);
  await unlockPurge(dialog);
  await purgeButton(dialog).click();
  await expect(dialog.getByRole('alert')).toContainText('The server could not purge the screenshots.');
  await expect(dialog.getByRole('status')).toHaveCount(0);
  await expect(dialog.getByRole('progressbar')).toHaveCount(0);
  // Nothing was deleted as far as the dialog knows, and the proctor's two acts still stand, so the purge can be tried again.
  await expect(dialog.getByRole('checkbox')).toBeChecked();
  await expect(purgeButton(dialog)).toBeEnabled();
  await expect(dialog.getByRole('button', { name: 'Keep screenshots' })).toBeEnabled();
  await page.evaluate(() => window.ProctorHarness.purgeFails(false));
  await purgeButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Screenshots purged');
  await expect(dialog.getByRole('alert')).toHaveCount(0);
  expect((await calls(page)).purge).toHaveLength(2);
});

test('a click outside the purge dialog nudges it instead of closing it, and Escape still keeps the screenshots', async ({ page }) => {
  const exams = await connect(page);
  const dialog = await openPurge(page, exams);
  await page.mouse.click(4, 4);
  await expect(dialog).toBeVisible(); await expect(dialog).toHaveClass(/nudge/);
  // A click inside the dialog is not a nudge.
  await dialog.evaluate(element => element.classList.remove('nudge'));
  await dialog.getByRole('heading', { name: 'Purge screenshots' }).click();
  await expect(dialog).not.toHaveClass(/nudge/);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  expect((await calls(page)).purge).toEqual([]);
});

test('the purge dialog stays inside a phone-sized window with its buttons stacked', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 760 });
  const exams = await connect(page);
  const dialog = await openPurge(page, exams);
  await unlockPurge(dialog);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const bounds = await dialog.boundingBox();
  expect(bounds.x).toBeGreaterThanOrEqual(0); expect(bounds.x + bounds.width).toBeLessThanOrEqual(360);
  expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  const keep = await dialog.getByRole('button', { name: 'Keep screenshots' }).boundingBox();
  const purge = await purgeButton(dialog).boundingBox();
  expect(purge.y).toBeLessThan(keep.y); // the dangerous button is above the safe one, as in the mockup
  expect(Math.abs(purge.width - keep.width)).toBeLessThan(1); expect(Math.abs(purge.x - keep.x)).toBeLessThan(1);
});

test('in a short window the purge dialog’s header and buttons stay in view while the rest scrolls', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 420 });
  const exams = await connect(page);
  const dialog = await openPurge(page, exams);
  const header = dialog.getByRole('heading', { name: 'Purge screenshots' });
  expect(await dialog.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  for (const top of [0, 10000]) {
    await dialog.evaluate((element, value) => { element.scrollTop = value; }, top);
    await expect(header).toBeInViewport(); await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeInViewport();
    await expect(dialog.getByRole('button', { name: 'Keep screenshots' })).toBeInViewport(); await expect(purgeButton(dialog)).toBeInViewport();
  }
  // At the end of the scroll the gate is reachable above the footer.
  await expect(dialog.getByLabel(`Type the exam code ${PRACTICE_CODE} to unlock`)).toBeInViewport();
});

test('download submissions is offered only for exams somebody has taken, with an editable default name', async ({ page }) => {
  const exams = await connect(page);
  const published = examCard(exams, 'Databases midterm');
  const practice = examCard(exams, 'Practice exam');
  const draft = examCard(exams, 'Draft quiz');
  await expect(draft.getByRole('button', { name: 'Download submissions of Draft quiz…' })).toHaveCount(0);
  await expect(published.getByRole('button', { name: 'Download submissions of Databases midterm…' })).toBeVisible();
  await expect(practice.getByRole('button', { name: 'Download submissions of Practice exam…' })).toBeVisible();
  await published.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Download submissions' });
  await expect(dialog).toContainText('Databases midterm');
  await expect(dialog).toContainText('Format · one Markdown file per student');
  await expect(dialog).toContainText('28'); await expect(dialog).toContainText('finished'); await expect(dialog).toContainText('one file each');
  await expect(dialog).toContainText('2'); await expect(dialog).toContainText('still working'); await expect(dialog).toContainText('skipped'); await expect(dialog).toContainText('Download again when they finish.');
  await expect(dialog).toContainText('3'); await expect(dialog).toContainText('never logged in'); await expect(dialog).toContainText('listed in _index.md only');
  const name = dialog.getByLabel('Output folder name');
  await expect(name).toHaveValue('Mathematics_10A-Databases_midterm');
  await expect(dialog.locator('.path')).toContainText('No folder chosen');
  await expect(dialog.getByText('Files go into')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Download 28 files' })).toBeDisabled();
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

test('choosing a folder only fills the path; the download button shows progress and keeps the dialog open', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.holdDownload());
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  // The folder is selected, but nothing has been written and the dialog is still there.
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.path')).toContainText('/home/proctor/Downloads');
  await expect(dialog.getByRole('button', { name: 'Download 28 files' })).toBeEnabled();
  expect((await calls(page)).download).toEqual([]);

  await dialog.getByLabel('Output folder name').fill('My Notes');
  await expect(dialog).toContainText('Files go into /home/proctor/Downloads/My Notes');
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('button', { name: 'Downloading…' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Choose folder…' })).toBeDisabled();
  await expect(dialog.getByLabel('Output folder name')).toBeDisabled();
  const bar = dialog.getByRole('progressbar', { name: 'Downloading submissions' });
  await expect(bar).toBeVisible();
  // Meaningful progress: the dialog shows the file count the server reports while the download runs.
  await page.evaluate(() => window.ProctorHarness.downloadProgress({ running: true, done: 2, total: 5, current: 'Student 003' }));
  await expect(bar).toHaveAttribute('aria-valuenow', '2');
  await expect(dialog).toContainText('Writing files… Student 003'); await expect(dialog).toContainText('2 / 5');

  await page.evaluate(() => window.ProctorHarness.releaseDownload());
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  await expect(dialog.locator('.summary')).toContainText('/home/proctor/Downloads/My Notes');
  await expect(dialog.locator('.summary')).toContainText('2 still working');
  await expect(dialog.getByRole('button', { name: 'Download 28 files' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Done' })).toBeVisible();
  const open = dialog.getByRole('button', { name: 'Open folder' });
  await expect(open).toBeVisible();
  await open.click();
  expect((await calls(page)).openDownloadFolder).toBe(1);
  // The dialog stays open until Close is clicked.
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect((await calls(page)).download).toEqual([{ examId: EXAM, folderName: 'My Notes', mode: 'complete', replace: false, confirmed: true }]);
});

test('a failure to open the folder is shown in the dialog, which stays open', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.openFolderFails('The download folder is no longer there.'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  await dialog.getByRole('button', { name: 'Open folder' }).click();
  await expect(dialog.getByRole('alert')).toContainText('The download folder is no longer there.');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Done' }).click();
});

test('an existing download folder is replaced only after the question, and notes in it are lost', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadNeedsReplace());
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog).toContainText('Replace the 28 files from the earlier download?');
  await expect(dialog).toContainText('Notes you wrote in them are lost. Files you added yourself are left alone.');
  await expect(dialog).toContainText('existing folder · files replaced');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);

  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Choose folder…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Download 28 files' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Replace files' }).click();
  await expect(page.getByRole('dialog').getByRole('status')).toContainText('28 submissions downloaded');
  const done = await calls(page);
  expect(done.download).toEqual([{ examId: EXAM, folderName: 'Mathematics_10A-Databases_midterm', mode: 'complete', replace: false, confirmed: true }, { examId: EXAM, folderName: 'Mathematics_10A-Databases_midterm', mode: 'complete', replace: false, confirmed: true }, { examId: EXAM, folderName: 'Mathematics_10A-Databases_midterm', mode: 'complete', replace: true, confirmed: true }]);
  await page.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
});

test('a partial download names the students to retry, and still offers the folder', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadFailure('Student 099'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('27 of 28 submissions downloaded');
  await expect(dialog.getByRole('alert')).toContainText('PARTIAL');
  await expect(dialog.getByRole('alert')).toContainText('Student 099');
  await expect(dialog.getByRole('button', { name: 'Open folder' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Done' }).click();
});

test('an error stops progress, keeps the dialog open, and lets the proctor try again', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadFails('Server unreachable. The download did not start.'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Server unreachable. The download did not start.');
  await expect(dialog.getByRole('progressbar')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Choose folder…' })).toBeEnabled();
  await expect(dialog.getByLabel('Output folder name')).toBeEnabled();
  await expect(dialog.getByRole('button', { name: 'Download 28 files' })).toBeEnabled();
  await expect(dialog).toBeVisible();

  await page.evaluate(() => window.ProctorHarness.downloadFails(''));
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  await dialog.getByRole('button', { name: 'Done' }).click();
});

test('the folder chooser can fail or be cancelled without starting anything', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.chooseFolderFails('The folder dialog failed.'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await expect(dialog.getByRole('alert')).toContainText('The folder dialog failed.');
  await expect(dialog.getByRole('button', { name: 'Download 28 files' })).toBeDisabled();
  expect((await calls(page)).download).toEqual([]);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
});

test('an exam without a stored source still downloads without a missing-source warning', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadPreview({ source_available: false, finishing: 2 }));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).not.toContainText('not on the server');
  await expect(dialog).not.toContainText('answers without the questions');
  await expect(dialog).toContainText('2'); await expect(dialog).toContainText('still finishing'); await expect(dialog).toContainText('skipped; being finalized');
});

test('the result wraps a long output path inside the dialog', async ({ page }) => {
  await page.setViewportSize({ width: 760, height: 700 });
  const exams = await connect(page, () => window.ProctorHarness.downloadFolder('/home/ideras/classes/2026/Q4_2026/CCC403-Organizacion_De_Computadoras/Evaluations'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  // The long path wraps instead of widening the dialog past the window.
  const box = await dialog.boundingBox();
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(760);
  const path = dialog.locator('.summary .path-text');
  expect(await path.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await dialog.getByRole('button', { name: 'Done' }).click();
});

test('the panel keeps the result and Open folder after the download dialog is closed', async ({ page }) => {
  const exams = await connect(page, () => window.ProctorHarness.downloadFailure('Student 099'));
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('27 of 28 submissions downloaded');
  await dialog.getByRole('button', { name: 'Done' }).click();
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
  await expect(dialog).toContainText('Only _index.md will be written');
  await dialog.getByRole('button', { name: 'Download index only' }).click();
  await expect(dialog.getByRole('status')).toContainText('0 submissions downloaded');
  await expect(dialog.getByRole('button', { name: 'Open folder' })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Done' }).click();
  await expect(exams.getByRole('status')).toContainText('Downloaded 0 submissions');
  await expect(exams.getByRole('button', { name: 'Open folder' })).toHaveCount(0);
});

// The download format: Complete exam (the default) or Answers only. Choosing it only changes what the dialog says.
const COMPLETE_NAME = 'Mathematics_10A-Databases_midterm';
const ANSWERS_NAME = 'Mathematics_10A-Databases_midterm-AnswersOnly';
async function openDownload(page, prepare) {
  const exams = await connect(page, prepare);
  await exams.getByRole('button', { name: 'Download submissions of Databases midterm…' }).click();
  return { exams, dialog: page.getByRole('dialog', { name: 'Download submissions' }) };
}
const formatGroup = dialog => dialog.getByRole('group', { name: 'Format' });

test('the download dialog offers Complete exam, selected, and Answers only, with their help text', async ({ page }) => {
  const { dialog } = await openDownload(page);
  const complete = formatGroup(dialog).getByRole('radio', { name: 'Complete exam' });
  const answers = formatGroup(dialog).getByRole('radio', { name: 'Answers only' });
  await expect(formatGroup(dialog).getByRole('radio')).toHaveCount(2);
  await expect(complete).toBeChecked(); await expect(answers).not.toBeChecked();
  await expect(complete).toHaveAccessibleDescription('The exam text with each student’s answers inserted. Self-contained for grading');
  await expect(answers).toHaveAccessibleDescription('Saved answers and field labels, without the exam questions. Needs the original exam to grade');
  await expect(dialog).toContainText('Format · one Markdown file per student');
  await expect(dialog.getByLabel('Output folder name')).toHaveValue(COMPLETE_NAME);
});

test('choosing Answers only changes the untouched folder name, and sends nothing', async ({ page }) => {
  const { dialog } = await openDownload(page);
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Answers only' })).toBeChecked();
  await expect(dialog.getByLabel('Output folder name')).toHaveValue(ANSWERS_NAME);
  // Back to the complete exam: the suggestion comes back with it.
  await formatGroup(dialog).getByRole('radio', { name: 'Complete exam' }).check();
  await expect(dialog.getByLabel('Output folder name')).toHaveValue(COMPLETE_NAME);
  const done = await calls(page);
  expect(done.download).toEqual([]); expect(done.chooseDownloadFolder).toEqual([]);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const cancelled = await calls(page);
  expect(cancelled.download).toEqual([]); expect(cancelled.chooseDownloadFolder).toEqual([]); expect(cancelled.prepareDownload).toEqual([EXAM]);
});

test('a folder name the proctor typed survives a change of format', async ({ page }) => {
  const { dialog } = await openDownload(page);
  const name = dialog.getByLabel('Output folder name');
  await name.fill('My Notes');
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await expect(name).toHaveValue('My Notes');
  await formatGroup(dialog).getByRole('radio', { name: 'Complete exam' }).check();
  await expect(name).toHaveValue('My Notes');
  // Even a name typed over the suggestion of the other format is the proctor's now.
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await name.fill(ANSWERS_NAME);
  await formatGroup(dialog).getByRole('radio', { name: 'Complete exam' }).check();
  await expect(name).toHaveValue(ANSWERS_NAME);
});

test('a typed folder name can be reset to the default, and a name the dashboard refuses is flagged', async ({ page }) => {
  const { dialog } = await openDownload(page);
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  const name = dialog.getByLabel('Output folder name');
  const start = dialog.getByRole('button', { name: 'Download 28 files' });
  await expect(dialog.getByRole('button', { name: 'Use default name' })).toHaveCount(0);
  await name.fill('Grading copies');
  await expect(dialog.getByRole('button', { name: 'Use default name' })).toBeVisible();
  await expect(start).toBeEnabled();
  await name.fill('Grading/copies');
  await expect(name).toHaveAttribute('aria-invalid', 'true');
  await expect(dialog).toContainText('The output folder name cannot contain / \\ : * ? " < > or |.');
  await expect(start).toBeDisabled();
  await name.fill('');
  await expect(dialog).toContainText('Enter a name for the output folder.');
  await expect(start).toBeDisabled();
  await dialog.getByRole('button', { name: 'Use default name' }).click();
  await expect(name).toHaveValue(COMPLETE_NAME);
  await expect(name).toHaveAttribute('aria-invalid', 'false');
  await expect(dialog.getByRole('button', { name: 'Use default name' })).toHaveCount(0);
  await expect(start).toBeEnabled();
  // Following the format again: the default is the other format's suggestion once Answers only is chosen.
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await expect(name).toHaveValue(ANSWERS_NAME);
  expect((await calls(page)).download).toEqual([]);
});

test('in a short window the header and the buttons stay in view while the steps scroll', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 520 });
  const { dialog } = await openDownload(page);
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  const header = dialog.getByRole('heading', { name: 'Download submissions' });
  const start = dialog.getByRole('button', { name: 'Download 28 files' });
  expect(await dialog.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  for (const top of [0, 10000]) {
    await dialog.evaluate((element, value) => { element.scrollTop = value; }, top);
    await expect(header).toBeInViewport(); await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeInViewport();
    await expect(start).toBeInViewport(); await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeInViewport();
  }
  // At the end of the scroll the last step is reachable above the footer.
  await expect(dialog.getByLabel('Output folder name')).toBeInViewport();
});

test('the tiles count who is included, with a fourth for students who are still finishing', async ({ page }) => {
  const { dialog } = await openDownload(page, () => window.ProctorHarness.downloadPreview({ finished: 1, working: 0, finishing: 2, never_logged_in: 0 }));
  await expect(dialog.locator('.who .w')).toHaveCount(4);
  await expect(dialog.locator('.who .w').first()).toContainText('1');
  await expect(dialog.locator('.who .w').first()).toContainText('finished');
  await expect(dialog.locator('.who .w').nth(1)).toContainText('0'); await expect(dialog.locator('.who .w').nth(1)).toContainText('still working');
  await expect(dialog.locator('.who .w').nth(2)).toContainText('2'); await expect(dialog.locator('.who .w').nth(2)).toContainText('still finishing');
  await expect(dialog).toContainText('Download again when they finish.');
  await expect(dialog.getByRole('button', { name: 'Download 1 file' })).toBeVisible();
  await expect(dialog).toContainText('1 file will be written');
});

test('the result lists who was not included and copies the folder path', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const { dialog } = await openDownload(page);
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  const summary = dialog.locator('.summary');
  await expect(summary).toContainText('28 student files + _index.md');
  await expect(summary).toContainText('2 still working, 3 never logged in (listed in the index)');
  await summary.getByRole('button', { name: 'Copy path' }).click();
  await expect(summary.getByRole('button', { name: 'Copied!' })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`/home/proctor/Downloads/${COMPLETE_NAME}`);
  await dialog.getByRole('button', { name: 'Done' }).click();
});

test('Answers only is sent with its folder, and the result says which format was written', async ({ page }) => {
  const { exams, dialog } = await openDownload(page);
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await expect(dialog).toContainText(`Files go into /home/proctor/Downloads/${ANSWERS_NAME}`);
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  await expect(dialog.getByRole('status')).toContainText('Answers only · plus _index.md');
  await expect(dialog.locator('.summary')).toContainText(`/home/proctor/Downloads/${ANSWERS_NAME}`);
  await expect(dialog).toContainText('Answers-only files don’t include the questions.');
  await expect(dialog.getByRole('button', { name: 'Open folder' })).toBeVisible();
  // The result replaces the options, so the format can no longer be changed.
  await expect(formatGroup(dialog)).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Done' }).click();
  expect((await calls(page)).download).toEqual([{ examId: EXAM, folderName: ANSWERS_NAME, mode: 'answers-only', replace: false, confirmed: true }]);
  // The panel keeps the result, and Open folder, after the dialog is closed.
  const line = exams.getByRole('status');
  await expect(line).toContainText(`Downloaded 28 answers-only submissions to /home/proctor/Downloads/${ANSWERS_NAME}.`);
  await line.getByRole('button', { name: 'Open folder' }).click();
  expect((await calls(page)).openDownloadFolder).toBe(1);
});

test('the complete exam is sent by default and its result names the complete format', async ({ page }) => {
  const { dialog } = await openDownload(page);
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  await expect(dialog.getByRole('status')).toContainText('Complete exam · plus _index.md');
  await expect(dialog.locator('.summary')).toContainText(`/home/proctor/Downloads/${COMPLETE_NAME}`);
  await expect(dialog).not.toContainText('Answers-only files don’t include the questions.');
  await dialog.getByRole('button', { name: 'Done' }).click();
  expect((await calls(page)).download).toEqual([{ examId: EXAM, folderName: COMPLETE_NAME, mode: 'complete', replace: false, confirmed: true }]);
});

test('the format control locks while the download runs', async ({ page }) => {
  const { dialog } = await openDownload(page, () => window.ProctorHarness.holdDownload());
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('button', { name: 'Downloading…' })).toBeDisabled();
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Complete exam' })).toBeDisabled();
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Answers only' })).toBeDisabled();
  await expect(dialog.getByLabel('Output folder name')).toBeDisabled();
  await page.evaluate(() => window.ProctorHarness.releaseDownload());
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
});

test('the format control locks while the replace question is open', async ({ page }) => {
  const { dialog } = await openDownload(page, () => window.ProctorHarness.downloadNeedsReplace());
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog).toContainText('Replace the 28 files from the earlier download?');
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Complete exam' })).toBeDisabled();
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Answers only' })).toBeDisabled();
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Answers only' })).toBeChecked();
  await expect(dialog.getByLabel('Output folder name')).toBeDisabled();
});

test('the replace question repeats the same format and folder, and names a different format in the folder', async ({ page }) => {
  const { dialog } = await openDownload(page, () => { window.ProctorHarness.downloadNeedsReplace(); window.ProctorHarness.downloadExistingFormat('complete'); });
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await dialog.getByLabel('Output folder name').fill('Grading copies');
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog).toContainText('Replace the 28 files from the earlier download?');
  await expect(dialog).toContainText('The earlier download in this folder is complete-exam, and this one is answers-only. Its files change format.');
  await expect(dialog).toContainText('Notes you wrote in them are lost. Files you added yourself are left alone.');
  await dialog.getByRole('button', { name: 'Replace files' }).click();
  await expect(dialog.getByRole('status')).toContainText('Answers only · plus _index.md');
  await expect(dialog.locator('.summary')).toContainText('/home/proctor/Downloads/Grading copies');
  expect((await calls(page)).download).toEqual([
    { examId: EXAM, folderName: 'Grading copies', mode: 'answers-only', replace: false, confirmed: true },
    { examId: EXAM, folderName: 'Grading copies', mode: 'answers-only', replace: true, confirmed: true },
  ]);
});

test('the replace question says nothing about formats when the folder holds the same one, or none can be told', async ({ page }) => {
  for (const existing of ['answers-only', '']) {
    const { dialog } = await openDownload(page, () => { window.ProctorHarness.downloadNeedsReplace(); });
    await page.evaluate(format => window.ProctorHarness.downloadExistingFormat(format), existing);
    await dialog.getByRole('button', { name: 'Choose folder…' }).click();
    await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
    await dialog.getByRole('button', { name: 'Download 28 files' }).click();
    await expect(dialog).toContainText('Replace the 28 files from the earlier download?');
    await expect(dialog).not.toContainText('change format');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
});

test('a complete download in an answers-only folder is warned about before replacing it', async ({ page }) => {
  const { dialog } = await openDownload(page, () => { window.ProctorHarness.downloadNeedsReplace(); window.ProctorHarness.downloadExistingFormat('answers-only'); });
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByLabel('Output folder name').fill('Shared folder');
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog).toContainText('The earlier download in this folder is answers-only, and this one is complete-exam. Its files change format.');
  await dialog.getByRole('button', { name: 'Replace files' }).click();
  await expect(dialog.getByRole('status')).toContainText('Complete exam · plus _index.md');
});

test('a missing exam source is not mentioned in either format', async ({ page }) => {
  const { dialog } = await openDownload(page, () => window.ProctorHarness.downloadPreview({ source_available: false }));
  for (const label of ['Complete exam', 'Answers only']) {
    await formatGroup(dialog).getByRole('radio', { name: label }).check();
    await expect(dialog).not.toContainText('not on the server');
    await expect(dialog).not.toContainText('stored Markdown');
    await expect(dialog).not.toContainText('no longer matches');
    await expect(dialog).not.toContainText('answers without the questions');
    await expect(dialog.getByRole('alert')).toHaveCount(0);
  }
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  await expect(dialog.locator('.tip').filter({ hasText: /stored Markdown|no longer matches|not on the server/ })).toHaveCount(0);
});

test('an answers-only download shows the fields the instructor must check, and a partial failure names the students', async ({ page }) => {
  const warnings = ['2 students’ files have answer fields with no saved value (Ana, Luis). They are marked missing in the file and listed in _index.md.'];
  const { dialog } = await openDownload(page, () => { window.ProctorHarness.downloadFailure('Student 099'); });
  await page.evaluate(value => window.ProctorHarness.downloadWarnings(value), warnings);
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('27 of 28 submissions downloaded');
  await expect(dialog.getByRole('alert')).toContainText('PARTIAL: 1 could not be written. Retry for Student 099.');
  await expect(dialog.locator('.tip').filter({ hasText: 'no saved value (Ana, Luis)' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Open folder' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'PARTIAL: downloaded 27 of 28 answers-only submissions to /home/proctor/Downloads/' + ANSWERS_NAME })).toContainText('Retry for Student 099.');
});

test('an error keeps the chosen format so the proctor can try again', async ({ page }) => {
  const { dialog } = await openDownload(page, () => window.ProctorHarness.downloadFails('Server unreachable. The download did not start.'));
  await dialog.getByRole('button', { name: 'Choose folder…' }).click();
  await formatGroup(dialog).getByRole('radio', { name: 'Answers only' }).check();
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Server unreachable. The download did not start.');
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Answers only' })).toBeChecked();
  await expect(formatGroup(dialog).getByRole('radio', { name: 'Answers only' })).toBeEnabled();
  await page.evaluate(() => window.ProctorHarness.downloadFails(''));
  await dialog.getByRole('button', { name: 'Download 28 files' }).click();
  await expect(dialog.getByRole('status')).toContainText('28 submissions downloaded');
  expect((await calls(page)).download.map(item => item.mode)).toEqual(['answers-only', 'answers-only']);
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
  const draft = examCard(exams, 'Draft quiz');
  await expect(draft.getByText('Calculator: scientific, programmer · radians · 1 function disabled')).toBeVisible();
  const published = examCard(exams, 'Databases midterm');
  await expect(published.getByText('Calculator:')).toHaveCount(0);
});

test("the exam row preview action opens the exam; its calculator waits for the tools button and floats", async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const exams = await connect(page);
  const row = examCard(exams, 'Draft quiz');
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
  await calculator.getByRole('button', { name: 'Copy answer', exact: true }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('14');
  const before = await calculator.boundingBox();
  await calculator.getByRole('button', { name: 'Move calculator' }).press('ArrowLeft');
  expect((await calculator.boundingBox()).x).toBeCloseTo(before.x - 10, 0);
  // Non-modal: the exam content stays visible and usable behind the floating panel.
  await expect(dialog.getByText('The exam as students see it')).toBeVisible();
  // The tools button reflects the state, and the panel's own close button closes just the calculator.
  await expect(tools).toHaveAttribute('aria-expanded', 'true');
  await calculator.getByRole('button', { name: 'Close the calculator' }).click();
  await expect(dialog.locator('.calc-floating')).toHaveCount(0);
  await expect(dialog).toBeVisible(); // the preview dialog itself stays open
  // An exam without a calculator offers no tools button.
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  const published = examCard(exams, 'Databases midterm');
  await published.getByRole('button', { name: 'Preview Databases midterm' }).click();
  await expect(page.getByRole('dialog', { name: 'Preview — Databases midterm' })).toContainText('This exam has no calculator');
  await expect(page.getByRole('dialog', { name: 'Preview — Databases midterm' }).getByRole('button', { name: 'Open the calculator' })).toHaveCount(0);
});
