import { test, expect } from '@playwright/test';

async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}
const heartbeat = message => page => page.evaluate(text => window.__studentHarness.monitoring({ active: true, policy: { heartbeat_interval_ms: 15000, focus_threshold_ms: 5000 }, policy_source: 'server', local_focus: 'focused', heartbeat_error: text }), message);
const notifications = page => page.getByRole('region', { name: 'Notifications' });
const fail = heartbeat('Heartbeat reporting unavailable');

test('an error appears in the viewport of a scrolled page without moving the page or the focus', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto('/');
  await page.evaluate(() => window.__studentHarness.exam({ navigation: 'free', content: { exercises: [{ id: '1', title: 'Long', body: '<p>Filler paragraph of an exercise that is long to read.</p>'.repeat(150) + '<div class="slot" data-slot="q1"></div>', slots: [{ id: 'q1', kind: 'text', lines: 3 }] }] } }));
  await login(page);
  const editor = page.getByRole('textbox', { name: 'Answer for q1' });
  await editor.scrollIntoViewIfNeeded();
  await editor.click();
  const before = await page.evaluate(() => ({ y: window.scrollY, focus: document.activeElement?.className }));
  expect(before.y).toBeGreaterThan(1000);
  await fail(page);
  const toast = notifications(page).getByText('Heartbeat reporting unavailable');
  await expect(toast).toBeInViewport();
  const after = await page.evaluate(() => ({ y: window.scrollY, focus: document.activeElement?.className }));
  expect(after).toEqual(before);
  expect(await page.evaluate(() => document.activeElement?.closest('.toasts'))).toBeNull();
});

test('the close button works by mouse and by keyboard, and closing changes nothing else', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(value => window.__studentHarness.capture(value), { state: 'failed', failures: 1 });
  const editor = page.getByRole('textbox', { name: 'Answer for q1' });
  const blocked = notifications(page).getByRole('alert').filter({ hasText: 'Screenshot retest required' });
  await expect(blocked).toBeVisible();
  await expect(editor).not.toHaveAttribute('contenteditable', 'true');
  await blocked.getByRole('button', { name: 'Dismiss error' }).click();
  await expect(blocked).toHaveCount(0);
  // The recovery stays on the page, editing stays paused, and nothing claims the work was saved.
  await expect(editor).not.toHaveAttribute('contenteditable', 'true');
  await expect(page.getByRole('button', { name: 'Test screenshot capture' })).toBeEnabled();
  await page.waitForTimeout(1200); // several polls: a dismissed condition does not come back
  await expect(blocked).toHaveCount(0);
  await fail(page);
  const warning = notifications(page).getByRole('status').filter({ hasText: 'Heartbeat reporting unavailable' });
  const close = warning.getByRole('button', { name: 'Dismiss warning' });
  await close.focus();
  await page.keyboard.press('Enter');
  await expect(warning).toHaveCount(0);
});

test('repeated snapshots make one toast; recovery followed by a new failure makes a new one', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await fail(page);
  const warning = notifications(page).getByText('Heartbeat reporting unavailable');
  await expect(warning).toHaveCount(1);
  await page.waitForTimeout(1600);
  await expect(warning).toHaveCount(1);
  await notifications(page).getByRole('button', { name: 'Dismiss warning' }).click();
  await page.waitForTimeout(1200);
  await expect(warning).toHaveCount(0);
  await heartbeat('')(page);
  await page.waitForTimeout(700);
  await fail(page);
  await expect(warning).toHaveCount(1);
  await heartbeat('')(page);
  await expect(warning).toHaveCount(0); // a condition that ended takes its toast with it
});

test('a warning lasts 15 seconds, waits while the pointer is on it or the window is inactive, and then goes', async ({ page }) => {
  await page.clock.install();
  await page.goto('/');
  await login(page);
  await fail(page);
  const warning = notifications(page).getByText('Heartbeat reporting unavailable');
  await expect(warning).toBeVisible();
  await page.clock.runFor(14000);
  await expect(warning).toBeVisible();
  await warning.hover();
  await page.clock.runFor(60000);
  await expect(warning).toBeVisible(); // the pointer is on it
  await page.mouse.move(5, 5);
  await page.evaluate(() => window.dispatchEvent(new Event('blur')));
  await page.clock.runFor(60000);
  await expect(warning).toBeVisible(); // the window is not the active one
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await page.clock.runFor(16000);
  await expect(warning).toHaveCount(0);
});

test('a blocking failure stays until it is dealt with', async ({ page }) => {
  await page.clock.install();
  await page.goto('/');
  await login(page);
  await page.evaluate(value => window.__studentHarness.capture(value), { state: 'failed', failures: 1 });
  const blocked = notifications(page).getByText('Screenshot retest required', { exact: false });
  await expect(blocked).toBeVisible();
  await page.clock.runFor(120000);
  await expect(blocked).toBeVisible();
});

test('login errors name the problem, stay until dismissed or the next attempt, and never show the PIN', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.__studentHarness.loginFailure(401));
  await login(page).catch(() => {});
  const toast = notifications(page).getByRole('alert');
  await expect(toast).toContainText('Credentials were rejected.');
  await expect(toast).not.toContainText('synthetic PIN');
  await expect(toast).not.toContainText('Error:');
  await page.waitForTimeout(500);
  await expect(toast).toHaveCount(1);
});

test('toasts stay usable with the calculator open: visible, clickable and reachable from the keyboard', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.__studentHarness.calculatorTools({ calculator: { enabled: true, angle_mode: 'user', modes: ['scientific'], disabled_functions: [] } }));
  await login(page);
  await page.getByRole('button', { name: 'Calculator', exact: true }).click();
  const calculator = page.getByRole('dialog', { name: 'Calculator' });
  await expect(calculator).toBeVisible();
  await fail(page);
  const warning = notifications(page).getByRole('status').filter({ hasText: 'Heartbeat reporting unavailable' });
  await expect(warning).toBeInViewport();
  const stack = await warning.boundingBox();
  // Whatever is on top at the toast is the toast itself, not the calculator's backdrop.
  expect(await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest('.toasts') !== null, { x: stack.x + 20, y: stack.y + 10 })).toBe(true);
  const typed = calculator.getByRole('textbox').first();
  await typed.focus();
  await page.keyboard.press('Alt+Shift+N');
  const close = warning.getByRole('button', { name: 'Dismiss warning' });
  await expect(close).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(typed).toBeFocused(); // back where the student was, with the calculator still open
  await expect(calculator).toBeVisible();
  await page.keyboard.press('Alt+Shift+N');
  await page.keyboard.press('Enter');
  await expect(warning).toHaveCount(0);
  await expect(typed).toBeFocused();
  await heartbeat('')(page); // a new failure comes only after the condition has ended
  await page.waitForTimeout(700);
  await fail(page);
  await warning.getByRole('button', { name: 'Dismiss warning' }).click(); // by mouse, calculator still open
  await expect(calculator).toBeVisible();
});

test('narrow windows and long messages keep the icon, text and close button inside the window', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 600 });
  await page.goto('/');
  await login(page);
  const long = 'Heartbeat reporting unavailable: ' + 'a-very-long-unbroken-diagnostic-word-'.repeat(8) + ' and then many ordinary words '.repeat(12);
  await heartbeat(long)(page);
  const toast = notifications(page).getByRole('status');
  await expect(toast).toBeVisible();
  const box = await toast.boundingBox();
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(320);
  const close = await toast.getByRole('button', { name: 'Dismiss warning' }).boundingBox();
  expect(close.x + close.width).toBeLessThanOrEqual(box.x + box.width);
  expect(close.width).toBeGreaterThanOrEqual(24);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.addStyleTag({ content: 'html { font-size: 28px; }' }); // larger text
  const bigger = await toast.boundingBox();
  expect(bigger.x + bigger.width).toBeLessThanOrEqual(320);
  await expect(toast.getByRole('button', { name: 'Dismiss warning' })).toBeInViewport();
});

test('volume corrections are announced once each, worded by what the app did, and never blame the login adjustment', async ({ page }) => {
  await page.goto('/');
  await login(page);
  const notice = value => page.evaluate(value => window.__studentHarness.soundNotice(value), value);
  const stack = notifications(page);
  await notice({ sequence: 1, kind: 'restored', initial: true, minimum_percent: 70, volume_percent: 20 });
  await expect(stack).toContainText('The volume was set to 70%');
  await expect(stack).not.toContainText("Please don't lower");
  await page.waitForTimeout(1200); // the same notice on every snapshot is one toast
  await expect(stack.getByRole('status')).toHaveCount(1);
  await notice({ sequence: 2, kind: 'restored', initial: false, minimum_percent: 70, volume_percent: 10 });
  await expect(stack).toContainText("Please don't lower or mute the volume during the exam. The required volume has been restored to 70% so you can hear the alerts.");
  await stack.getByRole('button', { name: 'Dismiss warning' }).click();
  await page.waitForTimeout(1200);
  await expect(stack).not.toContainText("Please don't lower"); // closing it is not an event: it does not come back
  await notice({ sequence: 3, kind: 'failed', initial: false, minimum_percent: 70 });
  const failed = stack.getByRole('alert').filter({ hasText: 'The app could not restore the required volume. Keep your sound on and notify your instructor.' });
  await expect(failed).toBeVisible();
  await expect(stack).not.toContainText('has been restored');
});
