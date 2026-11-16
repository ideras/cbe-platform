import { test, expect } from '@playwright/test';

async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

test('the rules say that remote desktop sessions and remote-control software are reported', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules).toContainText('Remote Desktop');
  await expect(rules).toContainText('remote-control software (such as TeamViewer or AnyDesk)');
  await expect(rules).toContainText('or that it is a virtual machine');
  await expect(rules).toContainText('Only the name of the program, or of the virtual machine\'s maker, is sent');
});

test('nothing is said while nothing has been found', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await expect(page.getByText('has been told that this computer', { exact: false })).toHaveCount(0);
});

test('the student is told what was found and that the instructor knows, and it goes when it is gone', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, remote: [
    { kind: 'remote_session', name: 'Remote Desktop', detail: 'LAPTOP-1' }, { kind: 'remote_tool', name: 'AnyDesk.exe' }] }));
  const notice = page.getByRole('status').filter({ hasText: 'Your instructor has been told' });
  await expect(notice).toContainText('a Remote Desktop session from LAPTOP-1, AnyDesk.exe');
  await expect(notice).toContainText('tell your instructor');
  // A session that does not say where it comes from is still named.
  await page.evaluate(() => window.__studentHarness.monitoring({ remote: [{ kind: 'remote_session', name: 'Remote Desktop' }] }));
  await expect(notice).toContainText('a Remote Desktop session.');
  await page.evaluate(() => window.__studentHarness.monitoring({ remote: [] }));
  await expect(notice).toHaveCount(0);
});

test('what was found is shown as plain text, never as markup', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, remote: [{ kind: 'remote_tool', name: '<img src=x onerror="window.injected=true">' }] }));
  await expect(page.getByRole('status').filter({ hasText: 'Your instructor has been told' })).toContainText('<img src=x');
  expect(await page.evaluate(() => window.injected)).toBeUndefined();
});

test('a virtual machine is told to the student apart from remote access, with the name of the hypervisor when it is known', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, remote: [{ kind: 'virtual_machine', name: 'VirtualBox', detail: 'VirtualBox' }] }));
  const notice = page.getByRole('status').filter({ hasText: 'Your instructor has been told' });
  await expect(notice).toHaveCount(1);
  await expect(notice).toContainText('this computer is a virtual machine (VirtualBox).');
  await expect(notice).not.toContainText('remote');
  // With a Remote Desktop session too, each has its own notice; an unknown hypervisor is not named.
  await page.evaluate(() => window.__studentHarness.monitoring({ remote: [{ kind: 'remote_tool', name: 'AnyDesk.exe' }, { kind: 'virtual_machine', name: 'unknown' }] }));
  await expect(notice).toHaveCount(2);
  await expect(notice.first()).toContainText('remote-control software running: AnyDesk.exe.');
  await expect(notice.last()).toContainText('this computer is a virtual machine.');
  await page.evaluate(() => window.__studentHarness.monitoring({ remote: [] }));
  await expect(notice).toHaveCount(0);
});
