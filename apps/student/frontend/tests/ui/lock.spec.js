import { test, expect } from '@playwright/test';

async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

// The lock times the exam was published with, as the monitoring view carries them.
async function lockTimes(page, away = 300000, noContact = 420000) {
  await page.evaluate(([a, n]) => window.__studentHarness.monitoring({ active: true, policy: { lock_away_after_ms: a, lock_no_contact_after_ms: n } }), [away, noContact]);
}

test('the lock screen replaces the questions and says why', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('You were away from the exam window for 5 minutes.')).toBeVisible();
  await expect(page.getByText('Call your instructor. The exam time keeps running.')).toBeVisible();
  // No questions and no answer fields are shown.
  await expect(page.getByRole('textbox')).toHaveCount(0);
  await expect(page.getByText('Ejercicio 1', { exact: false })).toHaveCount(0);
});

test('the lock screen names the reason of a lost connection', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page);
  await page.evaluate(() => window.__studentHarness.lock('no_contact'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('The app could not reach the server for 7 minutes.')).toBeVisible();
  await expect(page.getByText('You were away', { exact: false })).toHaveCount(0);
  // The student can reach the network settings during this lock too, and is told that the screens are captured meanwhile.
  await expect(page.getByText('not full screen now', { exact: false })).toBeVisible();
  await expect(page.getByText('Screenshots of all your screens are taken while the exam is locked', { exact: false })).toBeVisible();
});

test('a short lock is told in seconds, and one of a minute and a half keeps the half', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page, 30000, 90000);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByText('You were away from the exam window for 30 seconds.')).toBeVisible();
  await page.evaluate(() => window.__studentHarness.unlock(0));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.lock('no_contact'));
  await expect(page.getByText('The app could not reach the server for 1 minute 30 seconds.')).toBeVisible();
  await page.evaluate(() => window.__studentHarness.unlock(0));
  await lockTimes(page, 1000, 45000);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByText('You were away from the exam window for 1 second.')).toBeVisible();
});

test('the rules before the login tell the real lock times, down to the second', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.__studentHarness.rules({ alarm_millis: 90000, lock_away_millis: 30000, lock_no_contact_millis: 90000 }));
  await page.getByLabel('Server address').fill('http://192.0.2.11:8080');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('for 30 seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('for 1 minute 30 seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('more than 1 minute 30 seconds', { exact: false })).toBeVisible();
});

test('the time given back is told exactly: seconds, whole minutes, or both', async ({ page }) => {
  // Each case is a session of its own: the note of an unlock is told once, not again for the same message.
  for (const [seconds, text] of [[45, '45 seconds were added'], [60, '1 minute was added'], [90, '1 minute 30 seconds were added']]) {
    await page.goto('/');
    await login(page);
    await page.evaluate(() => window.__studentHarness.lock('away'));
    await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
    await page.evaluate(value => window.__studentHarness.unlock(value), seconds);
    await expect(page.getByRole('status').filter({ hasText: `Unlocked by your instructor — ${text}.` })).toBeVisible();
  }
});

test('the lock for an absence does not say the window is small', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('not full screen now', { exact: false })).toHaveCount(0);
});

test('the exam comes back after the unlock, saying so and how much time was given back', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await page.evaluate(() => window.__studentHarness.unlock(420));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Unlocked by your instructor — 7 minutes were added.' })).toBeVisible();
  await expect(page.getByRole('textbox').first()).toBeVisible();
});

test('an unlock without time back says only that the exam was unlocked', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.lock('no_contact'));
  await page.evaluate(() => window.__studentHarness.unlock(0));
  await expect(page.getByRole('status').filter({ hasText: 'Unlocked by your instructor' })).toBeVisible();
  await expect(page.getByText('were added', { exact: false })).toHaveCount(0);
});

test('the rules show the real times the server sent', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('more than 5 seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('your exam locks', { exact: false })).toBeVisible();
  await expect(rules.getByText('for 5 minutes', { exact: false })).toBeVisible();
});

test('the rules fall back to plain words when the server does not answer', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('for 5 minutes', { exact: false })).toBeVisible();
  await page.evaluate(() => window.__studentHarness.rulesUnavailable(true));
  await page.getByLabel('Server address').fill('http://192.0.2.10:8080');
  await expect(rules.getByText('a few seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('for several minutes', { exact: false })).toBeVisible();
});

test('the screenshot retest block never says the exam is locked', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.capture({ state: 'failed' }));
  await expect(page.getByRole('alert').filter({ hasText: 'Screenshot retest required' })).toBeVisible();
  await expect(page.getByText('Editing is locked', { exact: false })).toHaveCount(0);
});

// The numbers the native side sends while the app cannot reach the server.
async function connectionLost(page, extra = {}) {
  await page.evaluate(value => window.__studentHarness.monitoring({ active: true, contact_lost: true, no_contact_ms: 40000, no_contact_lock_in_ms: 260000,
    network_lock_available: true, network_locks_left: 2, policy: { lock_away_after_ms: 300000, lock_no_contact_after_ms: 300000 }, ...value }), extra);
}

test('a window that is in front but not the active one tells the student to click it', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await expect(page.getByText('This window is not the active one', { exact: false })).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, focus: 'unfocused', contact_lost: false, network_locks_left: 2 }));
  await expect(page.getByText('Click anywhere in it to continue', { exact: false })).toBeVisible();
  // Clicking it makes it the active one: the note goes.
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, focus: 'focused', contact_lost: false, network_locks_left: 2 }));
  await expect(page.getByText('This window is not the active one', { exact: false })).toHaveCount(0);
});

test('a lost connection is announced with how long is left before the exam locks', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await expect(page.getByRole('heading', { name: 'The app cannot reach the exam server' })).toHaveCount(0);
  await connectionLost(page);
  const warning = page.getByRole('region', { name: 'Connection' });
  await expect(warning.getByRole('heading', { name: 'The app cannot reach the exam server' })).toBeVisible();
  await expect(warning.getByText('4:20')).toBeVisible();
  await expect(warning.getByText('Only your instructor can unlock it', { exact: false })).toBeVisible();
  // The answers stay editable: the warning does not lock anything.
  await expect(page.getByRole('textbox').first()).toBeVisible();
  // The connection comes back: the warning goes away.
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, contact_lost: false, network_locks_left: 2 }));
  await expect(page.getByRole('heading', { name: 'The app cannot reach the exam server' })).toHaveCount(0);
});

test('with the no-contact lock off the warning has no countdown', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await connectionLost(page, { no_contact_lock_in_ms: undefined });
  const warning = page.getByRole('region', { name: 'Connection' });
  await expect(warning.getByRole('heading', { name: 'The app cannot reach the exam server' })).toBeVisible();
  await expect(warning.getByText('your exam locks in', { exact: false })).toHaveCount(0);
});

test('the text beside the button asks whether the computer changed network, in plain words', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await connectionLost(page);
  const warning = page.getByRole('region', { name: 'Connection' });
  await expect(warning.getByText('Has your computer connected to another network?', { exact: false })).toBeVisible();
  await expect(warning.getByText('Windows or your computer', { exact: false })).toHaveCount(0);
});

test('the student locks the exam to change network, after confirming', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await connectionLost(page);
  await page.getByRole('button', { name: 'I need to change network' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByText('Only your instructor can unlock it, and the exam time keeps running', { exact: false })).toBeVisible();
  await expect(dialog.getByText('You can do this 2 more times.')).toBeVisible();
  await expect(dialog.getByText('screenshots of all your screens are taken', { exact: false })).toBeVisible();
  await expect(dialog.getByText('goes back to full screen when your instructor unlocks the exam', { exact: false })).toBeVisible();
  // Nothing is locked until the student confirms; the safe choice keeps the exam.
  await dialog.getByRole('button', { name: 'Keep working' }).click();
  await expect(dialog).toHaveCount(0);
  expect(await page.evaluate(() => window.__studentHarness.networkLockRequests)).toBe(0);
  await page.getByRole('button', { name: 'I need to change network' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Lock my exam' }).click();
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('You locked the exam to change network.')).toBeVisible();
  await expect(page.getByText('tell your instructor', { exact: false })).toBeVisible();
  await expect(page.getByText('not full screen now', { exact: false })).toBeVisible();
  expect(await page.evaluate(() => window.__studentHarness.networkLockRequests)).toBe(1);
  // The student has nothing to press to unlock it.
  await expect(page.getByRole('button', { name: /unlock/i })).toHaveCount(0);
  await expect(page.getByRole('textbox')).toHaveCount(0);
});

test('the option disappears when the student has no requests left', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await connectionLost(page, { network_lock_available: false, network_locks_left: 0 });
  await expect(page.getByRole('heading', { name: 'The app cannot reach the exam server' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'I need to change network' })).toHaveCount(0);
});

test('the rules say that switching to a text console or another user counts as leaving the window', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('Switching to a text console or to another user counts as leaving it.', { exact: false })).toBeVisible();
});

test('the rules say how many times the student may lock the exam to change network', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('you can lock your exam yourself, up to 2 times', { exact: false })).toBeVisible();
  await expect(rules.getByText('the alarm stays quiet while it is locked, but screenshots of all your screens are taken', { exact: false })).toBeVisible();
  await expect(rules.getByText('goes back to full screen when your instructor unlocks the exam', { exact: false })).toBeVisible();
  // The lock for lack of contact leaves the same room, and the rules say so.
  await expect(rules.getByText('If it locked because the app could not reach the server, the window also stops being full screen', { exact: false })).toBeVisible();
});

// A screenshot that fails while the exam is locked (another user had the screen, say) can be tested at once: the retest is on the
// lock screen, not behind the unlock that brings the exam page back.
test('a capture that failed during a lock can be retested on the lock screen at once', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  // While nothing has failed there is nothing to test.
  await expect(page.getByRole('button', { name: 'Test screenshot capture' })).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.capture({ state: 'failed', failures: 1 }));
  const retest = page.getByRole('button', { name: 'Test screenshot capture' });
  await expect(retest).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('Screenshot retest required', { exact: false }).first()).toBeVisible();
  // The test passes: the retest goes away, and the exam stays locked until the instructor unlocks it.
  await retest.click();
  await expect(retest).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await page.evaluate(() => window.__studentHarness.unlock(0));
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveAttribute('contenteditable', 'true');
});

test('the dialog for changing network is centered in the window, says what happens in three parts, and counts the requests in words', async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 760 });
  await page.goto('/');
  await login(page);
  await connectionLost(page, { network_locks_left: 1 });
  await page.getByRole('button', { name: 'I need to change network' }).click();
  const dialog = page.getByRole('dialog', { name: 'Lock your exam to change network?' });
  await expect(dialog.getByText('Your exam locks now')).toBeVisible();
  await expect(dialog.getByText('While it is locked', { exact: true })).toBeVisible();
  await expect(dialog.getByText('When you are connected again')).toBeVisible();
  await expect(dialog.getByText('Tell your instructor when you are connected and back in this window.')).toBeVisible();
  await expect(dialog.getByText('You can do this one more time.')).toBeVisible();
  await expect(dialog.getByRole('button')).toHaveCount(2);
  // The window's own layout cannot take its automatic margins away: it is as far from the top as from the bottom.
  const box = await dialog.boundingBox();
  expect(box.y).toBeGreaterThan(10);
  expect(Math.abs(box.y - (760 - box.y - box.height))).toBeLessThan(2);
});

test('the dialog for changing network keeps its header and its choices in view in a short or a narrow window', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await connectionLost(page);
  // Short: the content scrolls between the header and the footer, which stay where they are.
  await page.setViewportSize({ width: 900, height: 480 });
  await page.getByRole('button', { name: 'I need to change network' }).click();
  const dialog = page.getByRole('dialog', { name: 'Lock your exam to change network?' });
  await expect(dialog.getByRole('heading', { name: 'Lock your exam to change network?' })).toBeInViewport({ ratio: 1 });
  await expect(dialog.getByRole('button', { name: 'Lock my exam' })).toBeInViewport({ ratio: 1 });
  await expect(dialog.getByRole('button', { name: 'Keep working' })).toBeInViewport({ ratio: 1 });
  // Narrow: the choices are one above the other as in the proctor's dialogs, the primary on top, as wide as the dialog, and nothing runs out sideways.
  await page.setViewportSize({ width: 420, height: 760 });
  const [safe, lock] = [await dialog.getByRole('button', { name: 'Keep working' }).boundingBox(), await dialog.getByRole('button', { name: 'Lock my exam' }).boundingBox()];
  expect(lock.y).toBeLessThan(safe.y);
  expect(Math.abs(safe.width - lock.width)).toBeLessThan(2);
  expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
});
