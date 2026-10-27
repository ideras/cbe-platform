import { test, expect } from '@playwright/test';

// Everything else is satisfied, so Login depends only on what the sound card might (wrongly) require.
async function readyToLogin(page) {
  await page.goto('/');
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(page.getByLabel('Screenshot test result')).toContainText('Screenshot capture succeeded');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  return { login: page.getByRole('button', { name: 'Login / resume' }), card: page.getByRole('region', { name: 'Sound volume' }) };
}

// Records what the alert element is asked to play and with which volume; behavior is switchable from the test.
async function instrumentAudio(page) {
  await page.addInitScript(() => {
    window.__plays = []; window.__mode = 'normal';
    const realPlay = HTMLMediaElement.prototype.play;
    HTMLMediaElement.prototype.play = function () {
      window.__plays.push({ src: this.currentSrc || this.src, volume: this.volume });
      window.__paused = 0; const pause = this.pause.bind(this); this.pause = () => { window.__paused++; pause(); };
      if (window.__mode === 'unsupported') return Promise.reject(Object.assign(new Error('The operation is not supported.'), { name: 'NotSupportedError' }));
      return realPlay.call(this);
    };
  });
}

test('only the screenshot test gates login; sound only shows the current level', async ({ page }) => {
  const { login, card } = await readyToLogin(page);
  await expect(page.getByRole('button', { name: /sound/i })).toHaveCount(0);
  await expect(card).toContainText('Current volume: 35%');
  await expect(card).toContainText('turns the volume up if needed and restores it when the exam ends');
  await expect(login).toBeEnabled();
  await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
});

test('the card follows the volume the computer reports, including muted and unreadable', async ({ page }) => {
  const { card } = await readyToLogin(page);
  const level = card.getByLabel('Current sound volume');
  await page.evaluate(() => window.__studentHarness.sound({ volume_percent: 20, volume_muted: true }));
  await expect(level).toContainText('Current volume: 20% · muted');
  await page.evaluate(() => window.__studentHarness.sound({ volume_muted: true }));
  await expect(level).toContainText('Current volume: muted');
  await page.evaluate(() => window.__studentHarness.sound({}));
  await expect(level).toContainText('Current volume: not available');
  await page.evaluate(() => window.__studentHarness.sound({ volume_percent: 0, volume_muted: false }));
  await expect(level).toContainText('Current volume: 0%');
});

test('the login click unlocks audio from an inline data URL without ever making a sound or touching the volume', async ({ page }) => {
  await instrumentAudio(page);
  const { login } = await readyToLogin(page);
  expect(await page.evaluate(() => window.__plays.length)).toBe(0); // nothing plays before login, not even silently
  await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  const [prime] = await page.evaluate(() => window.__plays);
  expect(prime.src).toMatch(/^data:audio\/(wav|x-wav|wave);base64,/); // wails:// media cannot play in WebKitGTK
  expect(prime.volume).toBe(1); // never 0: PipeWire remembers a zero per media role and WebKit will not raise it again
  expect(await page.evaluate(() => window.__paused)).toBe(1); // paused in the same click, before any audio starts
  await page.evaluate(() => window.__studentHarness.alert(1));
  await expect.poll(() => page.evaluate(() => window.__plays.length)).toBe(2);
  const [, alert] = await page.evaluate(() => window.__plays);
  expect(alert.volume).toBe(1); // the exam's system volume, not the element, sets how loud it is
});

test('an unsupported audio path is reported but does not block login', async ({ page }) => {
  await instrumentAudio(page);
  const { login } = await readyToLogin(page);
  await page.evaluate(() => { window.__mode = 'unsupported'; });
  await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  await expect(page.getByRole('alert').filter({ hasText: 'could not open the alert sound (NotSupportedError)' })).toContainText('Audible alerts may not play; monitoring continues.');
});
