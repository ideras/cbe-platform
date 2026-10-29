import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

// Two buttons are on one row when their vertical spans overlap.
async function sameRow(first, second) {
  const [a, b] = [await first.boundingBox(), await second.boundingBox()];
  expect(a.y < b.y + b.height && b.y < a.y + a.height, 'the buttons are on different rows').toBe(true);
}

async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
}

test('local editor assets, safe exam text, rapid exercise switches, and latest visible submit', async ({ page }) => {
  const external = []; const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/*', route => { if (!route.request().url().startsWith('http://127.0.0.1:4178/')) { external.push(route.request().url()); return route.abort(); } return route.continue(); });
  await page.goto('/'); await login(page);
  await expect(page.getByText('Fixture exam')).toBeVisible();
  const exercise = page.getByRole('region', { name: 'Exercise Ejercicio 1 — Código' });
  await expect(exercise).toContainText('Translate <img src=x>:');
  await expect(exercise.locator('pre code')).toHaveText('x = a < b;');
  await expect(page.locator('main img:not([alt])')).toHaveCount(0);
  await expect(page.locator('main a, main script')).toHaveCount(0);
  await expect(page.getByText('leave the exam').first()).toBeVisible();
  expect(await page.evaluate(() => window.__unsafe)).toBeUndefined();
  const first = page.getByRole('textbox', { name: 'Answer for q1' });
  await expect(first).toContainText('restored work');
  await first.fill('first answer\nline two');
  await page.getByRole('button', { name: /^Exercise 2:/ }).click();
  const blank = page.getByRole('textbox', { name: 'Answer for q2' });
  await expect(page.getByRole('cell').filter({ has: blank })).toBeVisible();
  await blank.fill('18');
  await page.getByRole('textbox', { name: 'Answer for q3' }).fill('second answer');
  await expect(page.getByRole('button', { name: /^Exercise 2:/ })).toHaveAccessibleName(/2 of 2 answered/);
  await page.getByRole('button', { name: 'Previous exercise' }).click();
  await expect(first).toContainText('first answer');
  await first.fill('latest visible — no debounce wait');
  await page.getByRole('button', { name: 'Submit current answers — final' }).click();
  await page.getByRole('button', { name: 'Submit my exam' }).click();
  await expect(page.getByRole('heading', { name: 'Submission closed' })).toBeVisible();
  expect(await page.evaluate(() => window.__studentHarness.submitted)).toEqual({ q1: 'latest visible — no debounce wait', q2: '18', q3: 'second answer', q4: '' });
  await expect(first).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Exercises' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Close exam window' }).click();
  expect(await page.evaluate(() => window.__studentHarness.closeCalls)).toBe(1);
  expect(external).toEqual([]); expect(errors).toEqual([]);
});

test('failed/stale feedback retains edits and reconnect does not replace local text', async ({ page }) => {
  await page.goto('/'); await login(page);
  await page.evaluate(() => window.__studentHarness.offline(true));
  const editor = page.getByRole('textbox', { name: 'Answer for q1' });
  await editor.fill('retained through outage');
  await expect(page.getByLabel('Save status')).toContainText('failed');
  await expect(page.getByText('Timing stale / disconnected', { exact: false })).toBeVisible();
  await expect(editor).toHaveAttribute('contenteditable', 'true');
  await page.evaluate(() => window.__studentHarness.offline(false));
  await expect(page.getByLabel('Save status')).toContainText('Saved');
  await expect(editor).toContainText('retained through outage');
});

test('grace keeps editing and force submission locks the loaded view without fetching exam', async ({ page }) => {
  await page.goto('/'); await login(page);
  await page.evaluate(() => window.__studentHarness.status({ status: 'grace', seconds_remaining: 0 }));
  await expect(page.getByText('Grace period — continue editing', { exact: false })).toBeVisible();
  const editor = page.getByRole('textbox', { name: 'Answer for q1' }); await editor.fill('local grace work');
  await page.evaluate(() => window.__studentHarness.status({ status: 'force_submitted', seconds_remaining: 0 }));
  await expect(page.getByRole('heading', { name: 'Submission closed' })).toBeVisible();
  await expect(editor).toHaveCount(0);
  await expect(page.getByText('local grace work', { exact: true })).toHaveCount(0);
  await expect(page.getByText('Server state: force_submitted.')).toBeVisible();
});

test('lost submission response disables repeat action and retry preserves captured text', async ({ page }) => {
  await page.goto('/'); await login(page); await page.evaluate(() => window.__studentHarness.lostSubmit(true));
  const editor = page.getByRole('textbox', { name: 'Answer for q1' }); await editor.fill('immutable pending submit');
  const submit = page.getByRole('button', { name: 'Submit current answers — final' }); await submit.click();
  await page.getByRole('button', { name: 'Submit my exam' }).click();
  await expect(submit).toBeDisabled(); await expect(editor).not.toHaveAttribute('contenteditable', 'true');
  await expect(page.getByRole('button', { name: 'Close exam window' })).toHaveCount(0);
  await expect(page.getByText('Submission awaiting server confirmation.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Retry connection / pending work' }).click();
  await expect(page.getByRole('heading', { name: 'Submission closed' })).toBeVisible();
  expect((await page.evaluate(() => window.__studentHarness.submitted)).q1).toBe('immutable pending submit');
});

test('Ocean Blue stays light with native-looking controls and editor even with dark OS preference', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' }); await page.goto('/');
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(247, 246, 242)');
  await expect(page.getByLabel('Account number')).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await login(page); await expect(page.locator('.cm-editor')).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(page.locator('.cm-gutters')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
});

test('logging in is the only screenshot step: no separate test, one real check per attempt', async ({ page }) => {
  await page.goto('/'); expect(await page.evaluate(() => window.__studentHarness.captureCalls || 0)).toBe(0);
  await expect(page.getByText('Opt-in native feasibility diagnostics', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Capture desktop once (opt-in test)' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Test screenshot capture' })).toHaveCount(0);
  await page.getByLabel('I have read these rules').check();
  await page.waitForTimeout(600); // nothing is captured because the rules were accepted, or just because time passed
  expect(await page.evaluate(() => window.__studentHarness.captureCalls || 0)).toBe(0);
  await page.getByLabel('Account number').fill('00123'); await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await expect(page.getByRole('button', { name: 'Login / resume' })).toBeEnabled();
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  expect(await page.evaluate(() => window.__studentHarness.captureCalls)).toBe(1);
  expect(await page.evaluate(() => window.__studentHarness.loginRequests)).toBe(1);
});

test('terminal login is distinct and initial load failure offers credential-free retry', async ({ page }) => {
  await page.goto('/'); await page.evaluate(() => window.__studentHarness.loginFailure(409)); await login(page);
  await expect(page.getByRole('alert')).toContainText('already been submitted'); await expect(page.getByLabel('Exam PIN')).toHaveValue('');
  await page.evaluate(() => { window.__studentHarness.loginFailure(0); window.__studentHarness.loadFailure(true); }); await login(page);
  await expect(page.getByRole('heading', { name: 'Session started — exam not fully loaded' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.loadFailure(false)); await page.getByRole('button', { name: 'Retry loading exam' }).click();
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toContainText('restored work');
});

test('forward-only exams confirm before leaving an exercise and never go back', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => { window.__studentHarness.navigation('forward_only'); window.__studentHarness.holdAdvance(true); });
  await login(page);
  await expect(page.getByText('Exercise 1 of 3.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Exercise 2:/ })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Previous exercise' })).toHaveCount(0);
  await page.getByRole('textbox', { name: 'Answer for q1' }).fill('done with one');
  await page.getByRole('button', { name: 'Continue to exercise 2 →' }).click();
  await page.getByRole('button', { name: 'Stay on exercise 1' }).click();
  expect(await page.evaluate(() => window.__studentHarness.exercise)).toBe(0);
  await page.getByRole('button', { name: 'Continue to exercise 2 →' }).click();
  const leave = page.getByRole('dialog', { name: 'Leave exercise 1 for good?' });
  await expect(leave).toContainText('You will not be able to see or change it again.');
  await expect(leave.getByRole('button', { name: 'Stay on exercise 1' })).toBeFocused();
  await sameRow(page.getByRole('button', { name: 'Continue to exercise 2', exact: true }), page.getByRole('button', { name: 'Stay on exercise 1' }));
  await page.getByRole('button', { name: 'Continue to exercise 2', exact: true }).click();
  await expect(page.getByText('Moving to the next exercise', { exact: false })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).not.toHaveAttribute('contenteditable', 'true');
  await expect(page.getByRole('button', { name: 'Submit current answers — final' })).toBeDisabled();
  await page.evaluate(() => window.__studentHarness.releaseAdvance());
  await expect(page.getByRole('region', { name: 'Exercise Ejercicio 2 — Tabla' })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Exercise 1:/ })).toHaveAccessibleName(/Closed/);
  await expect(page.getByRole('button', { name: /^Exercise 1:/ })).toBeDisabled();
  expect(await page.evaluate(() => window.__studentHarness.answers.q1)).toBe('done with one');
  await page.getByRole('textbox', { name: 'Answer for q2' }).fill('18');
  await page.evaluate(() => window.__studentHarness.holdAdvance(false));
  await page.getByRole('button', { name: 'Continue to exercise 3 →' }).click();
  await page.getByRole('button', { name: 'Continue to exercise 3', exact: true }).click();
  await expect(page.getByText('This is the last exercise. Submit when you are done.')).toBeVisible();
  await expect(page.getByRole('button', { name: /Continue to exercise/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Submit current answers — final' }).click();
  await page.getByRole('button', { name: 'Submit my exam' }).click();
  await expect(page.getByRole('heading', { name: 'Submission closed' })).toBeVisible();
});

test('the compiled sample exam shows every answer field where its text puts it', async ({ page }, info) => {
  const sample = JSON.parse(readFileSync(new URL('../../../../../testdata/exam.json', import.meta.url), 'utf8'));
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.exam(value), sample);
  await login(page);
  await expect(page.getByText('Exercise 1 of 5.', { exact: false })).toBeVisible();
  const table = page.getByRole('table').filter({ hasText: 'Categoría' });
  for (const slot of ['1a.alu', '1a.mult', '1a.branch', '1a.jump', '1a.total']) await expect(table.getByRole('textbox', { name: `Answer for ${slot}` })).toBeVisible();
  await expect(page.getByRole('listitem').filter({ hasText: 'Nuevo CPI' }).getByRole('textbox')).toHaveCount(2);
  await expect(page.getByRole('textbox', { name: 'Answer for 1a.trace' })).toHaveAttribute('rows', '10');
  await expect(page.getByRole('button', { name: /^Exercise 1:/ })).toHaveAccessibleName(/0 of 11 answered/);
  await page.getByRole('textbox', { name: 'Answer for 1a.alu' }).fill('18');
  await expect(page.getByRole('button', { name: /^Exercise 1:/ })).toHaveAccessibleName(/1 of 11 answered/);
  await page.screenshot({ path: info.outputPath('exercise-1.png'), fullPage: true });
  await page.evaluate(() => window.__studentHarness.navigation('free'));
});

test('submitting asks first, says how much is answered, and keeping working sends nothing', async ({ page }) => {
  await page.goto('/'); await login(page);
  await page.getByRole('textbox', { name: 'Answer for q1' }).fill('only this one');
  await page.getByRole('button', { name: 'Submit current answers — final' }).click();
  const dialog = page.getByRole('dialog', { name: 'Submit your exam now?' });
  await expect(dialog.getByRole('button', { name: 'Keep working' })).toBeFocused();
  await expect(dialog).toContainText('Submit your exam now?');
  await expect(dialog).toContainText('You have answered 1 of 4 fields.');
  await sameRow(dialog.getByRole('button', { name: 'Submit my exam' }), dialog.getByRole('button', { name: 'Keep working' }));
  await dialog.getByRole('button', { name: 'Keep working' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  // Escape is the same as keeping working.
  await page.getByRole('button', { name: 'Submit current answers — final' }).click();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.__studentHarness.submitted)).toBeNull();
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveAttribute('contenteditable', 'true');
  // The routine monitoring and capture details are not on the exam page; only problems are.
  await expect(page.getByRole('region', { name: 'Monitoring status' })).toHaveCount(0);
  await expect(page.getByText('Monitoring limitations')).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.monitoring({ policy: { heartbeat_interval_ms: 15000, focus_threshold_ms: 5000 }, policy_source: 'server', local_focus: 'focused', heartbeat_error: 'Heartbeat delayed; the proctor may see you as disconnected.' }));
  await expect(page.getByRole('region', { name: 'Notifications' }).getByRole('status').filter({ hasText: 'Heartbeat delayed' })).toBeVisible();
});

test('the login screen keeps the rules short and drops the server note', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByRole('heading', { name: 'Exam rules' })).toBeVisible();
  await expect(rules.locator('.rule-list li')).toHaveCount(5);
  // The rules show the real times the server sent.
  await expect(rules.getByText('more than 5 seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('your exam locks', { exact: false })).toBeVisible();
  await expect(page.getByText('isolated-LAN', { exact: false })).toHaveCount(0);
  await expect(page.getByText('never resets the deadline', { exact: false })).toHaveCount(0);
});


test('Tab indents code, Shift+Tab unindents, and submission preserves the edit', async ({ page }) => {
  await page.goto('/'); await login(page);
  const editor = page.getByRole('textbox', { name: 'Answer for q1' });
  await editor.fill('int main() {}');
  await editor.press('Home');
  await editor.press('Tab');
  await expect(editor).toBeFocused();
  await expect(editor).toHaveText('  int main() {}');
  await editor.press('Shift+Tab');
  await expect(editor).toHaveText('int main() {}');
  await editor.press('Tab');
  await page.getByRole('button', { name: 'Submit current answers — final' }).click();
  await page.getByRole('button', { name: 'Submit my exam' }).click();
  await expect(page.getByRole('heading', { name: 'Submission closed' })).toBeVisible();
  expect((await page.evaluate(() => window.__studentHarness.submitted)).q1).toBe('  int main() {}');
  await expect(editor).toHaveCount(0);
});

test('exam and grace clocks use server observations, including while locked and stale', async ({ page }) => {
  await page.goto('/'); await login(page);
  await expect(page.getByRole('region', { name: 'Server timing' })).toContainText('00:01:00');
  await page.evaluate(() => window.__studentHarness.status({ status: 'grace', seconds_remaining: 0, server_time: '2035-01-02T03:05:05Z' }));
  await expect(page.getByRole('region', { name: 'Server timing' })).toContainText('Grace time remaining (last server report): 00:00:30');
  await page.evaluate(() => window.__studentHarness.status({ status: 'grace', seconds_remaining: 0, server_time: '2035-01-02T03:05:16Z' }));
  await expect(page.getByRole('region', { name: 'Server timing' })).toContainText('00:00:19');
  await page.evaluate(() => window.__studentHarness.lock('away'));
  const lock = page.getByRole('region', { name: 'Exam locked' });
  await expect(lock).toContainText('Grace time remaining (last server report): 00:00:19');
  await page.evaluate(() => window.__studentHarness.offline(true));
  await expect(lock).toContainText('Timing stale / disconnected');
  await expect(lock).toContainText('00:00:19');
});
