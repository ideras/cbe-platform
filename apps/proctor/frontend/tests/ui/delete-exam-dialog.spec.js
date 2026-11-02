import { test, expect } from '@playwright/test';
import { connect as connectTo, calls, examCard, examsPanel, tab } from './helpers.js';

// The delete exam dialog, from delete-exam-dialog.html. Two cases: an exam nobody took (Draft quiz, one tick) and an exam students
// took (Practice exam, 14 submissions: download, then type the code). Databases midterm is published and cannot be deleted.
const PRACTICE = '33333333-3333-4333-8333-333333333333';
const DRAFT = '77777777-7777-4777-8777-777777777777';
const PRACTICE_CODE = PRACTICE.slice(0, 8);
const DRAFT_CODE = DRAFT.slice(0, 8);

async function connect(page, prepare) {
  await connectTo(page, { prepare, openClass: 'Mathematics 10A' });
  return examsPanel(page);
}
const dialogOf = page => page.getByRole('dialog', { name: 'Delete exam' });
const deleteButton = dialog => dialog.getByRole('button', { name: /^(Delete exam|Delete exam and [\d,]+ submissions?|Deleting…)$/ });
const keepButton = dialog => dialog.getByRole('button', { name: 'Keep exam', exact: true });
const closeResult = dialog => dialog.locator('.foot').getByRole('button', { name: 'Close', exact: true });
const downloadedTick = dialog => dialog.getByRole('checkbox', { name: 'I have downloaded the submissions for this exam.' });
const codeField = (dialog, code = PRACTICE_CODE) => dialog.getByLabel(`Type the exam code ${code} to unlock`);

async function openDraft(page, exams) {
  await examCard(exams, 'Draft quiz').getByRole('button', { name: 'Delete exam Draft quiz…' }).click();
  return dialogOf(page);
}
async function openPractice(page, exams) {
  await examCard(exams, 'Practice exam').getByRole('button', { name: 'Delete exam Practice exam…' }).click();
  return dialogOf(page);
}
// The two acts that unlock an exam students took. The tick is clicked by its label text: the box is drawn, the real input is invisible.
async function unlockPractice(dialog) {
  await dialog.getByText('I have downloaded the submissions for this exam.').click();
  await codeField(dialog).fill(PRACTICE_CODE);
}

test.describe('an exam nobody took', () => {
  test('says nothing is lost, keeps the safe choice in front and asks for one tick', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openDraft(page, exams);
    await expect(dialog.getByRole('heading', { name: 'Delete exam' })).toBeVisible();
    await expect(dialog).toContainText('Cannot be undone');
    await expect(dialog.locator('.exam .t')).toHaveText('Draft quiz');
    await expect(dialog.locator('.exam .c')).toHaveText('Mathematics 10A');
    await expect(dialog.locator('.exam .id')).toHaveText(DRAFT);
    await expect(dialog.locator('.exam .id b')).toHaveText(DRAFT_CODE);
    await expect(dialog.locator('.exam .tag')).toHaveText('Never taken');
    const rows = dialog.getByRole('listitem');
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0)).toContainText('Nothing is lost'); await expect(rows.nth(0)).toContainText('Nobody has taken it, so there are no answers or screenshots.');
    await expect(rows.nth(1)).toContainText('PIN stops working'); await expect(rows.nth(1)).toContainText('The exam is removed and its PIN no longer works.');
    await expect(rows.nth(2)).toContainText('To give it again'); await expect(rows.nth(2)).toContainText('Import the exam file again.');
    // No download step and no code for an exam nobody took.
    await expect(dialog.getByRole('checkbox')).toHaveCount(1);
    await expect(dialog.getByRole('textbox')).toHaveCount(0);
    await expect(dialog).not.toContainText('Not touched');
    await expect(keepButton(dialog)).toBeFocused();
    await expect(deleteButton(dialog)).toHaveText('Delete exam'); await expect(deleteButton(dialog)).toBeDisabled();
    await expect(dialog.getByRole('checkbox', { name: 'I want to delete “Draft quiz”.' })).not.toBeChecked();
    await dialog.getByText('I want to delete “Draft quiz”.').click();
    await expect(deleteButton(dialog)).toBeEnabled();
    // Untick: locked again.
    await dialog.getByText('I want to delete “Draft quiz”.').click();
    await expect(deleteButton(dialog)).toBeDisabled();
    expect((await calls(page)).deleteExam).toEqual([]);
  });

  test('is deleted after the tick, and the dialog stays with what happened', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openDraft(page, exams);
    await dialog.getByText('I want to delete “Draft quiz”.').click();
    await deleteButton(dialog).click();
    const result = dialog.getByRole('status');
    await expect(result).toContainText('Exam deleted'); await expect(result).toContainText('Draft quiz'); await expect(result).toContainText('Removed from Mathematics 10A');
    // Nobody took it: no reminder about copies, no alert, and the gate and its buttons are gone for Close.
    await expect(dialog).not.toContainText('only copy'); await expect(dialog.getByRole('alert')).toHaveCount(0);
    await expect(deleteButton(dialog)).toHaveCount(0); await expect(keepButton(dialog)).toHaveCount(0);
    await expect(examCard(exams, 'Draft quiz')).toHaveCount(0);
    await expect(tab(page, 'Exams (2)')).toBeVisible();
    await expect(dialog).toBeVisible();
    await closeResult(dialog).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('status')).toContainText('Exam “Draft quiz” deleted.');
    expect((await calls(page)).deleteExam).toEqual([{ examId: DRAFT, submissions: 0, confirmed: true }]);
  });
});

test.describe('an exam students took', () => {
  test('says what is deleted forever, how many submissions and what is not touched', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await expect(dialog.locator('.exam .t')).toHaveText('Practice exam');
    await expect(dialog.locator('.exam .id b')).toHaveText(PRACTICE_CODE);
    await expect(dialog.locator('.exam .tag')).toHaveText('14 submissions');
    const rows = dialog.getByRole('listitem');
    await expect(rows).toHaveCount(4);
    await expect(rows.nth(0)).toContainText('Deleted forever'); await expect(rows.nth(0)).toContainText('The exam, every student\'s answers, events and screenshots.');
    await expect(rows.nth(0).locator('.n')).toHaveText('14submissions');
    await expect(rows.nth(1)).toContainText('PIN stops working'); await expect(rows.nth(1)).toContainText('Nobody can log in to this exam again.');
    await expect(rows.nth(2)).toContainText('Not touched — your copies'); await expect(rows.nth(2)).toContainText('Downloads, exports and backups made earlier.');
    await expect(rows.nth(3)).toContainText('To give it again');
    // The server does not count screenshots before a deletion, so the dialog claims no number for them.
    await expect(dialog).not.toContainText(/\d[\d,]* screenshots/);
    await expect(dialog).not.toContainText('Nothing is lost');
    await expect(keepButton(dialog)).toBeFocused();
    await expect(deleteButton(dialog)).toHaveText('Delete exam and 14 submissions'); await expect(deleteButton(dialog)).toBeDisabled();
  });

  test('unlocks only after the download tick and the typed code, in that order', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    const tick = downloadedTick(dialog);
    const code = codeField(dialog);
    await expect(dialog.getByText('Before you continue')).toBeVisible();
    // Step 2 is locked while step 1 is not done.
    await expect(tick).not.toBeChecked();
    await expect(code).toBeDisabled(); await expect(code).toHaveAttribute('maxlength', '8'); await expect(code).toHaveAttribute('placeholder', PRACTICE_CODE);
    await expect(dialog.locator('.step').nth(1)).toHaveClass(/locked/);
    await expect(deleteButton(dialog)).toBeDisabled();

    await dialog.getByText('I have downloaded the submissions for this exam.').click();
    await expect(tick).toBeChecked();
    await expect(dialog.locator('.step').nth(0)).toHaveClass(/done/); await expect(dialog.locator('.step').nth(1)).not.toHaveClass(/locked/);
    await expect(code).toBeEnabled(); await expect(code).toBeFocused();
    // The tick alone does not unlock, and neither does a code that is almost right.
    await expect(deleteButton(dialog)).toBeDisabled();
    await code.fill(PRACTICE_CODE.slice(0, 7) + (PRACTICE_CODE.endsWith('3') ? '4' : '3'));
    await expect(deleteButton(dialog)).toBeDisabled(); await expect(dialog.locator('.step').nth(1)).not.toHaveClass(/done/);
    // The code is typed by hand: a paste event is cancelled before it can put anything in the field.
    await code.fill('');
    expect(await code.evaluate(field => {
      const paste = new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: new DataTransfer() });
      field.dispatchEvent(paste);
      return paste.defaultPrevented;
    })).toBe(true);
    // Right, in any case.
    await code.fill(PRACTICE_CODE.toUpperCase());
    await expect(deleteButton(dialog)).toBeEnabled(); await expect(dialog.locator('.step').nth(1)).toHaveClass(/done/); await expect(code).toHaveClass(/match/);
    // Taking the tick back takes the code back too, so the two acts always happen in order.
    await dialog.getByText('I have downloaded the submissions for this exam.').click();
    await expect(tick).not.toBeChecked(); await expect(code).toHaveValue(''); await expect(code).toBeDisabled(); await expect(deleteButton(dialog)).toBeDisabled();
    expect((await calls(page)).deleteExam).toEqual([]);
  });

  test('deletes the exam with its submissions, and the dialog says what went and what remains', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await unlockPractice(dialog);
    await deleteButton(dialog).click();
    const result = dialog.getByRole('status');
    await expect(result).toContainText('Exam deleted'); await expect(result).toContainText('Practice exam');
    await expect(result).toContainText('14 submissions and 214 screenshots removed');
    await expect(dialog).toContainText('Your downloaded submissions, exports and backups still exist; they are the only copy now.');
    await expect(dialog.getByRole('alert')).toHaveCount(0);
    await expect(deleteButton(dialog)).toHaveCount(0); await expect(keepButton(dialog)).toHaveCount(0);
    // The list behind it has already moved on.
    await expect(examCard(exams, 'Practice exam')).toHaveCount(0);
    await expect(tab(page, 'Exams (2)')).toBeVisible();
    await expect(dialog).toBeVisible();
    await closeResult(dialog).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('status')).toContainText('Exam “Practice exam” deleted with its 14 submissions and 214 screenshots. Copies you downloaded, exports and backups are not touched.');
    expect((await calls(page)).deleteExam).toEqual([{ examId: PRACTICE, submissions: 14, confirmed: true }]);
    // The other exams are untouched.
    await expect(examCard(exams, 'Databases midterm')).toBeVisible(); await expect(examCard(exams, 'Draft quiz')).toBeVisible();
  });

  test('says a screenshot file the server could not remove yet, and that the exam is gone all the same', async ({ page }) => {
    const exams = await connect(page, () => { window.ProctorHarness.deleteScreenshots(3); window.ProctorHarness.deleteFilesPending(2); });
    const dialog = await openPractice(page, exams);
    await unlockPractice(dialog);
    await deleteButton(dialog).click();
    await expect(dialog.getByRole('status')).toContainText('14 submissions and 3 screenshots removed');
    await expect(dialog.getByRole('alert')).toContainText('2 screenshot files could not be removed from the server\'s disk yet');
    await expect(dialog.getByRole('alert')).toContainText('The exam and its records are gone');
    await closeResult(dialog).click();
    await expect(page.getByRole('status')).toContainText('2 screenshot files could not be removed from the server yet; it keeps trying.');
    await expect(examCard(exams, 'Practice exam')).toHaveCount(0);
  });

  test('a single submission and a single screenshot are not pluralized', async ({ page }) => {
    const exams = await connect(page, () => { window.ProctorHarness.examSessions('33333333-3333-4333-8333-333333333333', 1); window.ProctorHarness.deleteScreenshots(1); });
    const dialog = await openPractice(page, exams);
    await expect(dialog.locator('.exam .tag')).toHaveText('1 submission');
    await expect(dialog.getByRole('listitem').nth(0).locator('.n')).toHaveText('1submission');
    await expect(deleteButton(dialog)).toHaveText('Delete exam and 1 submission');
    await unlockPractice(dialog);
    await deleteButton(dialog).click();
    await expect(dialog.getByRole('status')).toContainText('1 submission and 1 screenshot removed');
  });

  test('offers the download from inside the dialog and tells what it wrote, without ticking for the proctor', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await dialog.getByRole('button', { name: 'Not yet? Download submissions…' }).click();
    // The download dialog opens on top, for this exam, and the delete dialog waits underneath.
    const download = page.getByRole('dialog', { name: 'Download submissions' });
    await expect(download).toContainText('Practice exam');
    expect((await calls(page)).prepareDownload).toEqual([PRACTICE]);
    await download.getByRole('button', { name: 'Choose folder…' }).click();
    await download.getByRole('button', { name: /^Download \d+ files?$/ }).click();
    await expect(download.getByRole('status')).toContainText('28 submissions downloaded');
    await download.getByRole('button', { name: 'Done' }).click();
    await expect(download).toHaveCount(0);
    // Back in the delete dialog: still locked, with the result of the download shown under the link.
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('Downloaded 28 submissions to /home/proctor/Downloads/');
    await expect(downloadedTick(dialog)).not.toBeChecked(); await expect(deleteButton(dialog)).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Not yet? Download submissions…' })).toBeFocused();
    expect((await calls(page)).deleteExam).toEqual([]);
    await unlockPractice(dialog);
    await expect(deleteButton(dialog)).toBeEnabled();
  });

  test('cancelling the download leaves the delete dialog as it was and sends nothing', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await dialog.getByText('I have downloaded the submissions for this exam.').click();
    await dialog.getByRole('button', { name: 'Not yet? Download submissions…' }).click();
    const download = page.getByRole('dialog', { name: 'Download submissions' });
    await download.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(download).toHaveCount(0);
    await expect(dialog).toBeVisible(); await expect(downloadedTick(dialog)).toBeChecked();
    await expect(dialog).not.toContainText('Downloaded ');
    const done = await calls(page);
    expect(done.download).toEqual([]); expect(done.deleteExam).toEqual([]);
  });
});

test.describe('dismissing the dialog', () => {
  for (const [name, dismiss] of [
    ['the Keep exam button', dialog => keepButton(dialog).click()],
    ['the close mark', dialog => dialog.getByRole('button', { name: 'Close', exact: true }).first().click()],
    ['Escape', (dialog, page) => page.keyboard.press('Escape')],
  ]) {
    for (const [which, open, unlock] of [
      ['an exam nobody took', openDraft, async dialog => dialog.getByText('I want to delete “Draft quiz”.').click()],
      ['an exam students took', openPractice, unlockPractice],
    ]) {
      test(`${name} keeps ${which}, even fully unlocked, and the next opening starts locked`, async ({ page }) => {
        const exams = await connect(page);
        const dialog = await open(page, exams);
        await unlock(dialog);
        await expect(deleteButton(dialog)).toBeEnabled();
        await dismiss(dialog, page);
        await expect(dialog).toHaveCount(0);
        await expect(exams.getByRole('article')).toHaveCount(3);
        expect((await calls(page)).deleteExam).toEqual([]);
        await expect(exams).not.toContainText('deleted');
        // Opening it again starts from the beginning: nothing stays ticked or typed.
        const again = await open(page, exams);
        await expect(deleteButton(again)).toBeDisabled();
        await expect(again.getByRole('checkbox').first()).not.toBeChecked();
        if (open === openPractice) await expect(codeField(again)).toHaveValue('');
      });
    }
  }

  test('a click outside nudges the dialog instead of closing it', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await unlockPractice(dialog);
    await page.mouse.click(4, 4);
    await expect(dialog).toBeVisible(); await expect(dialog).toHaveClass(/nudge/);
    await expect(deleteButton(dialog)).toBeEnabled();
    expect((await calls(page)).deleteExam).toEqual([]);
  });

  test('returns focus to the delete button it was opened from', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await keepButton(dialog).click();
    await expect(examCard(exams, 'Practice exam').getByRole('button', { name: 'Delete exam Practice exam…' })).toBeFocused();
  });
});

test.describe('while it works and when it fails', () => {
  test('nothing can be closed while the server deletes, and the result then replaces the form', async ({ page }) => {
    const exams = await connect(page, () => window.ProctorHarness.holdDelete());
    const dialog = await openPractice(page, exams);
    await unlockPractice(dialog);
    await deleteButton(dialog).click();
    await expect(deleteButton(dialog)).toHaveText('Deleting…'); await expect(deleteButton(dialog)).toBeDisabled();
    await expect(dialog.getByRole('progressbar', { name: 'Deleting the exam' })).toBeVisible();
    await expect(dialog).toContainText('Deleting the exam and its submissions…');
    await expect(keepButton(dialog)).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
    await expect(downloadedTick(dialog)).toBeDisabled(); await expect(codeField(dialog)).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Not yet? Download submissions…' })).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeVisible();
    // Only one request was sent, and it is not repeated by pressing Enter in the meantime.
    await page.keyboard.press('Enter');
    expect((await calls(page)).deleteExam).toHaveLength(1);
    await page.evaluate(() => window.ProctorHarness.releaseDelete());
    await expect(dialog.getByRole('status')).toContainText('Exam deleted');
    expect((await calls(page)).deleteExam).toHaveLength(1);
  });

  test('a refusal is explained in the dialog, the gate stays as it was and the exam stays', async ({ page }) => {
    const exams = await connect(page, () => window.ProctorHarness.deleteExamFails('The exam is published or students are still working in it, so it cannot be deleted. Close admission and let them finish first.'));
    const dialog = await openPractice(page, exams);
    await unlockPractice(dialog);
    await deleteButton(dialog).click();
    await expect(dialog.getByRole('alert')).toContainText('The exam is published or students are still working in it');
    await expect(dialog.getByRole('status')).toHaveCount(0);
    await expect(deleteButton(dialog)).toBeEnabled(); await expect(downloadedTick(dialog)).toBeChecked(); await expect(codeField(dialog)).toHaveValue(PRACTICE_CODE);
    await expect(examCard(exams, 'Practice exam')).toBeVisible();
    // Trying again once the server allows it works from the same dialog.
    await page.evaluate(() => window.ProctorHarness.deleteExamFails(''));
    await deleteButton(dialog).click();
    await expect(dialog.getByRole('status')).toContainText('Exam deleted');
    await expect(dialog.getByRole('alert')).toHaveCount(0);
  });

  test('submissions that change while the dialog is open take the confirmation back', async ({ page }) => {
    const exams = await connect(page);
    const dialog = await openPractice(page, exams);
    await unlockPractice(dialog);
    await expect(deleteButton(dialog)).toBeEnabled();
    // Somebody else's session shows up in the exam list: what was ticked and typed was for 14 submissions, not 15.
    await page.evaluate(() => window.ProctorHarness.examSessions('33333333-3333-4333-8333-333333333333', 15));
    await expect(dialog.locator('.exam .tag')).toHaveText('15 submissions');
    await expect(deleteButton(dialog)).toHaveText('Delete exam and 15 submissions');
    await expect(deleteButton(dialog)).toBeDisabled(); await expect(downloadedTick(dialog)).not.toBeChecked(); await expect(codeField(dialog)).toHaveValue('');
    expect((await calls(page)).deleteExam).toEqual([]);
    await unlockPractice(dialog);
    await deleteButton(dialog).click();
    await expect(dialog.getByRole('status')).toContainText('15 submissions and 214 screenshots removed');
    expect((await calls(page)).deleteExam).toEqual([{ examId: PRACTICE, submissions: 15, confirmed: true }]);
  });
});

test.describe('the button on the exam row', () => {
  test('is offered for every exam that is not published and says what it will delete', async ({ page }) => {
    const exams = await connect(page);
    await expect(examCard(exams, 'Databases midterm').getByRole('button', { name: /Delete exam/ })).toHaveCount(0);
    await expect(examCard(exams, 'Draft quiz').getByRole('button', { name: 'Delete exam Draft quiz…' })).toHaveAttribute('title', 'Delete exam Draft quiz…');
    const practice = examCard(exams, 'Practice exam').getByRole('button', { name: 'Delete exam Practice exam…' });
    await expect(practice).toBeEnabled(); await expect(practice).toHaveAttribute('title', 'Delete exam Practice exam and its 14 submissions…');
  });

  test('waits while students are still working in the exam, and the server would refuse it anyway', async ({ page }) => {
    const exams = await connect(page, () => window.ProctorHarness.examWorking('33333333-3333-4333-8333-333333333333', 2));
    const practice = examCard(exams, 'Practice exam').getByRole('button', { name: 'Delete exam Practice exam…' });
    await expect(practice).toBeDisabled();
    await expect(practice).toHaveAttribute('title', 'Students are still working in this exam; delete it once they have finished');
    // An exam nobody is working in is not held up by this one.
    await expect(examCard(exams, 'Draft quiz').getByRole('button', { name: 'Delete exam Draft quiz…' })).toBeEnabled();
  });

  test('with nothing published, the exam that was published is deletable like any other taken one', async ({ page }) => {
    const exams = await connect(page, () => window.ProctorHarness.nothingPublished());
    await expect(examCard(exams, 'Databases midterm').getByRole('button', { name: 'Delete exam Databases midterm…' })).toBeEnabled();
  });
});

test('exams can be deleted one after another, taken or not, and the list keeps up', async ({ page }) => {
  const exams = await connect(page);
  for (const [open, unlock] of [[openDraft, async dialog => dialog.getByText('I want to delete “Draft quiz”.').click()], [openPractice, unlockPractice]]) {
    const dialog = await open(page, exams);
    await unlock(dialog);
    await deleteButton(dialog).click();
    await closeResult(dialog).click();
  }
  await expect(tab(page, 'Exams (1)')).toBeVisible();
  await expect(exams.getByRole('article')).toHaveCount(1);
  expect((await calls(page)).deleteExam.map(call => call.examId)).toEqual([DRAFT, PRACTICE]);
});

test('the dialog stays inside a phone-sized window with its buttons stacked', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 760 });
  const exams = await connect(page);
  const dialog = await openPractice(page, exams);
  await unlockPractice(dialog);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const bounds = await dialog.boundingBox();
  expect(bounds.x).toBeGreaterThanOrEqual(0); expect(bounds.x + bounds.width).toBeLessThanOrEqual(360);
  expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  const keep = await keepButton(dialog).boundingBox();
  const remove = await deleteButton(dialog).boundingBox();
  expect(remove.y).toBeLessThan(keep.y); // the dangerous button is above the safe one, as in the mockup
  expect(Math.abs(remove.width - keep.width)).toBeLessThan(1); expect(Math.abs(remove.x - keep.x)).toBeLessThan(1);
});

test('in a short window the header and buttons stay in view while the rest scrolls', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 420 });
  const exams = await connect(page);
  const dialog = await openPractice(page, exams);
  const header = dialog.getByRole('heading', { name: 'Delete exam' });
  expect(await dialog.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  for (const top of [0, 10000]) {
    await dialog.evaluate((element, value) => { element.scrollTop = value; }, top);
    await expect(header).toBeInViewport(); await expect(keepButton(dialog)).toBeInViewport(); await expect(deleteButton(dialog)).toBeInViewport();
  }
  await expect(codeField(dialog)).toBeInViewport();
});
