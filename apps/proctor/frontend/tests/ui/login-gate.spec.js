import { test, expect } from '@playwright/test';

// An exam account is open on one computer at a time. When someone tries it on another, the server refuses and the proctor sees
// the attempt on the student's row, where a release lets the next login in.
async function connect(page) {
  await page.goto('about:blank');
  await page.goto('/#/monitoring');
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}
const rows = page => page.locator('tbody tr[data-session-id]');
const first = page => rows(page).first();
const attention = page => page.locator('[data-stat="attention"] .num');
const harness = (page, call, ...args) => page.evaluate(([call, args]) => window.ProctorHarness[call](...args), [call, args]);

test('a student nobody tried to log in as shows nothing and offers no release', async ({ page }) => {
  await connect(page);
  await expect(first(page)).not.toContainText('Login refused');
  await expect(first(page).getByRole('button', { name: /Allow new login/ })).toHaveCount(0);
});

test('a refused login shows on the row, needs attention, and offers the release', async ({ page }) => {
  await connect(page);
  const before = Number(await attention(page).innerText());
  await harness(page, 'loginRefused', 5, 2, '10.0.9.9');
  const row = rows(page).filter({ hasText: 'Student 005' });
  await expect(row).toContainText('Login refused ×2');
  await expect(row).toContainText('from 10.0.9.9');
  await expect(attention(page)).toHaveText(String(before + 1));
  // The row still shows the computer that has the account, so the proctor can compare the two addresses.
  await expect(row).toContainText('desk-5 · 10.0.0.6');
  await expect(row.getByRole('button', { name: 'Allow new login 0006…', exact: true })).toBeVisible();
});

test('the proctor releases the account with a reason, and the row says the next login will be let in', async ({ page }) => {
  await connect(page);
  await harness(page, 'loginRefused', 5, 1, '10.0.9.9');
  const row = rows(page).filter({ hasText: 'Student 005' });
  await expect(row).toContainText('Login refused ×1'); // the screen has picked the refusal up, so the count below includes it
  const before = Number(await attention(page).innerText());
  await row.getByRole('button', { name: 'Allow new login 0006…', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Allow a new login' });
  await expect(dialog).toBeVisible();
  // It names the student, the computer that has the account now and the one that was refused, and what will happen.
  await expect(dialog).toContainText('Student 005 (0006)');
  await expect(dialog).toContainText('desk-5 · 10.0.0.6');
  await expect(dialog).toContainText('1 login, the last at');
  await expect(dialog).toContainText('from 10.0.9.9');
  await expect(dialog).toContainText('will lose it');
  const allow = dialog.getByRole('button', { name: 'Allow new login', exact: true });
  await expect(allow).toBeDisabled();
  await dialog.getByLabel('Reason').fill('   ');
  await expect(allow).toBeDisabled();
  await dialog.getByLabel('Reason').fill("the student's laptop went flat");
  await expect(allow).toBeEnabled();
  await allow.click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status').filter({ hasText: 'The next login of 0006 will be accepted.' })).toBeVisible();
  const sent = await page.evaluate(() => window.ProctorHarness.calls.release);
  expect(sent).toEqual([{ sessionID: '00000005-0000-4000-8000-000000000000', reason: "the student's laptop went flat", confirmed: true }]);
  // Answered: the row waits for the student's login, needs no more attention, and the button is gone.
  await expect(row).toContainText('Next login allowed');
  await expect(row).toContainText('waiting for the student');
  await expect(row.getByRole('button', { name: /Allow new login/ })).toHaveCount(0);
  await expect(attention(page)).toHaveText(String(before - 1));
  // The student logs in: the report ends.
  await harness(page, 'loggedIn', 5);
  await expect(row).not.toContainText('Next login allowed');
  await expect(row).not.toContainText('Login refused');
});

test('a release the server refuses keeps the dialog open and says why', async ({ page }) => {
  await connect(page);
  await harness(page, 'loginRefused', 0);
  await harness(page, 'releaseFails', 'The session already ended.');
  await first(page).getByRole('button', { name: 'Allow new login 0001…', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Allow a new login' });
  await dialog.getByLabel('Reason').fill('the first computer is being swapped');
  await dialog.getByRole('button', { name: 'Allow new login', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('The session already ended.');
  await expect(dialog).toBeVisible();
  // Cancelling leaves the row as it was: still refused, still offering the release.
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(first(page)).toContainText('Login refused ×1');
  await expect(first(page).getByRole('button', { name: 'Allow new login 0001…', exact: true })).toBeVisible();
});

test('a session that is over offers no release even if someone tried it', async ({ page }) => {
  await connect(page);
  await harness(page, 'finished', 3);
  await harness(page, 'loginRefused', 3);
  const row = rows(page).filter({ hasText: 'Student 003' });
  await expect(row).toContainText('Login refused');
  await expect(row.getByRole('button', { name: /Allow new login/ })).toHaveCount(0);
});
