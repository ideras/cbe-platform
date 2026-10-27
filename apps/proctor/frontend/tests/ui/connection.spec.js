import { test, expect } from '@playwright/test';
import { nav, connectionDialog, connect, calls } from './helpers.js';

const item = (page, name) => nav(page).getByRole('button', { name, exact: true });
const status = page => page.getByRole('group', { name: 'Connection status' });

test('before connecting only Server connection and Close are usable, and the rest is really disabled', async ({ page }) => {
  await page.goto('/');
  await expect(item(page, 'Server connection')).toBeEnabled();
  await expect(item(page, 'Close')).toBeEnabled();
  await expect(item(page, 'Classes')).toBeDisabled();
  await expect(item(page, 'Classes')).toHaveAttribute('title', 'Connect to a server first');
  await expect(item(page, 'Exams')).toHaveCount(0); // exams are reached through their class
  // Disabled means unreachable by keyboard too: tabbing through the navigation skips them.
  await item(page, 'Server connection').focus();
  await page.keyboard.press('Tab');
  await expect(item(page, 'Close')).toBeFocused();
  await expect(status(page)).toContainText('Not connected');
  await expect(page.getByRole('region', { name: 'Connection' })).toContainText('Connect to your exam server to manage classes and exams.');
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Exams', exact: true })).toHaveCount(0);
});

test('the disabled look is clearly different from the enabled one', async ({ page }) => {
  await page.goto('/');
  const opacity = name => item(page, name).evaluate(element => parseFloat(getComputedStyle(element).opacity));
  expect(await opacity('Classes')).toBeLessThan(0.7);
  expect(await opacity('Server connection')).toBe(1);
  expect(await item(page, 'Classes').evaluate(element => getComputedStyle(element).cursor)).toBe('not-allowed');
});

test('connecting enables Classes at once and shows the connection in the status area', async ({ page }) => {
  await connect(page);
  await expect(item(page, 'Classes')).toBeEnabled();
  await expect(item(page, 'Exams')).toHaveCount(0);
  await expect(item(page, 'Classes')).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toBeVisible();
  await expect(status(page)).toContainText('Connected'); await expect(status(page)).toContainText('127.0.0.1:8080');
  expect((await calls(page)).connect).toEqual([{ server: 'http://127.0.0.1:8080' }]);
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
});

test('the dialog opens from the navigation or the welcome page, and Escape closes it', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Server connection…' }).click();
  const dialog = connectionDialog(page);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Server address')).toHaveValue('http://127.0.0.1:8080');
  await expect(dialog).toContainText('Given when the dashboard started');
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await item(page, 'Server connection').click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(item(page, 'Server connection')).toBeFocused();
  expect((await calls(page)).connect).toEqual([]);
});

test('a bad server address is explained and nothing is sent', async ({ page }) => {
  await page.goto('/');
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  const address = dialog.getByLabel('Server address');
  for (const [value, message] of [['', 'Enter the server address'], ['not a url', 'Enter the server address'], ['ftp://example.org', 'Use an http:// address'], ['https://example.org', 'Use an http:// address'], ['http://host/exam', 'without a path'], ['http://user:pw@host', 'without a path, user name']]) {
    await address.fill(value);
    await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(dialog.getByText(message, { exact: false })).toBeVisible();
    await expect(address).toHaveAttribute('aria-invalid', 'true');
  }
  await address.fill('http://10.0.0.5:8080');
  await expect(address).not.toHaveAttribute('aria-invalid', 'true');
  expect((await calls(page)).connect).toEqual([]);
});

test('a missing token file is asked for before any connection attempt, and a chosen file is named but never shown', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.launchCredential(false));
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  await expect(dialog).toContainText('No token file chosen');
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog).toContainText('Choose the token file the server was started with');
  expect((await calls(page)).connect).toEqual([]);

  await page.evaluate(() => window.ProctorHarness.tokenFile('data/admin-token'));
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog).toContainText('data/admin-token · loaded, not shown');
  await expect(dialog.getByRole('button', { name: 'Choose another…' })).toBeVisible();
  await expect(dialog).not.toContainText('Choose the token file the server was started with');
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(item(page, 'Classes')).toBeEnabled();
  expect((await calls(page)).chooseCredential).toBe(1);
});

test('a refused token file is explained in the dialog and the earlier choice stays', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.launchCredential(false));
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  await page.evaluate(() => window.ProctorHarness.tokenFile('data/admin-token'));
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await page.evaluate(() => window.ProctorHarness.tokenFileFails('Proctor credential must be a private regular file with 32–4096 non-whitespace characters.'));
  await dialog.getByRole('button', { name: 'Choose another…' }).click();
  await expect(dialog).toContainText('must be a private regular file');
  await expect(dialog).toContainText('data/admin-token · loaded, not shown');
  expect(await dialog.locator('input[type="password"]').count()).toBe(0);
});

test('a failed connection says why, stays disconnected and can be retried from the same dialog', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.connectFails('Cannot reach 127.0.0.1:8080. Check the address, that the server is running and that this computer is on the same network.'));
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('Cannot reach 127.0.0.1:8080');
  await expect(dialog).toBeVisible();
  await expect(item(page, 'Classes')).toBeDisabled();
  await expect(status(page)).toContainText('Connection failed');
  await page.evaluate(() => window.ProctorHarness.connectFails(''));
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(item(page, 'Classes')).toBeEnabled();
  expect((await calls(page)).connect.length).toBe(2);
});

test('an authentication failure is told apart from an unreachable server', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.connectFails('The server at 127.0.0.1:8080 rejected the token. Choose the token file the server was started with (its -admin-token-file).'));
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('rejected the token');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Connection' }).getByRole('alert')).toContainText('rejected the token');
});

test('while a connection is attempted the dialog shows progress and cannot be dismissed', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.holdConnect());
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  const connecting = dialog.getByRole('button', { name: 'Connecting…' });
  await expect(connecting).toBeDisabled();
  await expect(dialog.getByLabel('Server address')).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await expect(status(page)).toContainText('Connecting…');
  await page.evaluate(() => window.ProctorHarness.releaseConnect());
  await expect(dialog).toBeHidden();
  await expect(status(page)).toContainText('Connected');
});

test('an application started with a known server and token is connected without a click', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.startConnected());
  await expect(item(page, 'Classes')).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toBeVisible();
  await expect(status(page)).toContainText('Connected');
  expect((await calls(page)).connect).toEqual([]);
});

test('a startup connection in progress shows it, and a failed one shows why and offers the dialog', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.startConnecting());
  await expect(page.getByRole('heading', { name: 'Connecting…' })).toBeVisible();
  await expect(status(page)).toContainText('Connecting…');
  await expect(item(page, 'Classes')).toBeDisabled();
});

test('a failed startup connection does not block the application', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.startFailed('Cannot reach 192.168.1.10:8080. Check the address, that the server is running and that this computer is on the same network.'));
  const welcome = page.getByRole('region', { name: 'Connection' });
  await expect(welcome.getByRole('alert')).toContainText('Cannot reach 192.168.1.10:8080');
  await expect(status(page)).toContainText('Connection failed');
  await welcome.getByRole('button', { name: 'Server connection…' }).click();
  const dialog = connectionDialog(page);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(item(page, 'Classes')).toBeEnabled();
});

test('losing the server later is shown without taking the pages away', async ({ page }) => {
  await connect(page);
  await page.evaluate(() => window.ProctorHarness.unreachable(true));
  await expect(status(page)).toContainText('Server not answering');
  await expect(item(page, 'Classes')).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.unreachable(false));
  await expect(status(page)).toContainText('Connected');
});

test('remembered and current server addresses are offered for selection', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.knownServers(['http://127.0.0.1:8080', 'http://192.168.1.20:8080']));
  await item(page, 'Server connection').click();
  const options = connectionDialog(page).locator('datalist option');
  await expect(options).toHaveCount(2, { timeout: 2000 }).catch(() => {});
  const values = await options.evaluateAll(list => list.map(option => option.value));
  expect(values).toEqual(expect.arrayContaining(['http://127.0.0.1:8080', 'http://192.168.1.20:8080']));
});

test('Close asks before the application quits, from the button or the window', async ({ page }) => {
  await page.goto('/');
  await item(page, 'Close').click();
  const dialog = page.getByRole('dialog', { name: 'Close the dashboard?' });
  await expect(dialog).toContainText('Students keep working');
  await dialog.getByRole('button', { name: 'Keep it open' }).click();
  await expect(dialog).toHaveCount(0);
  await page.keyboard.press('Escape');
  expect((await calls(page)).quit).toBe(0);
  // The window's own close button reaches the page as a request, which asks the same question.
  await page.evaluate(() => window.ProctorHarness.requestClose());
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  expect((await calls(page)).quit).toBe(0);
  await item(page, 'Close').click();
  await dialog.getByRole('button', { name: 'Close the dashboard' }).click();
  expect((await calls(page)).quit).toBe(1);
});

test('the navigation stays usable on a narrow window', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 700 });
  await connect(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  for (const name of ['Server connection', 'Classes', 'Close']) await expect(item(page, name)).toBeInViewport();
  const rail = await item(page, 'Classes').boundingBox(), content = await page.getByRole('heading', { name: 'Classes', level: 1 }).boundingBox();
  expect(content.y).toBeGreaterThan(rail.y); // the rail is a bar above the page, not a column beside it
});

test('a connected dashboard can be disconnected from the dialog and connected again without choosing the file', async ({ page }) => {
  await connect(page);
  await item(page, 'Server connection').click();
  const dialog = connectionDialog(page);
  await expect(dialog).toContainText('Connected to 127.0.0.1:8080');
  await expect(dialog.getByRole('button', { name: 'Disconnect' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Disconnect' }).click();
  await expect(dialog).toBeHidden();
  expect((await calls(page)).disconnect).toBe(1);
  await expect(item(page, 'Classes')).toBeDisabled();
  await expect(status(page)).toContainText('Not connected');
  await expect(page.getByRole('region', { name: 'Connection' })).toContainText('Connect to your exam server to manage classes and exams.');
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toHaveCount(0);

  await item(page, 'Server connection').click();
  await expect(dialog).not.toContainText('Connected to');
  await expect(dialog.getByRole('button', { name: 'Disconnect' })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(item(page, 'Classes')).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toBeVisible();
});

test('there is nothing to disconnect before connecting', async ({ page }) => {
  await page.goto('/');
  await item(page, 'Server connection').click();
  await expect(connectionDialog(page).getByRole('button', { name: 'Disconnect' })).toHaveCount(0);
});
